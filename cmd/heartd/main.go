// Command heartd is a lightweight self-hosted server health monitor.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/timanthonyalexander/heartd/internal/alert"
	"github.com/timanthonyalexander/heartd/internal/auth"
	"github.com/timanthonyalexander/heartd/internal/cluster"
	"github.com/timanthonyalexander/heartd/internal/collector"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/scheduler"
	"github.com/timanthonyalexander/heartd/internal/server"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

func main() {
	configPath := flag.String("config", "heartd.yaml", "path to heartd.yaml")
	addr := flag.String("addr", "", "address to listen on (overrides config port)")
	flag.Parse()

	if err := run(*configPath, *addr); err != nil {
		log.Fatal(err)
	}
}

func run(configPath, addrOverride string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	db, err := storage.Open(cfg.Server.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Root context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Runtime-configurable settings (intervals, thresholds, notify, checks),
	// seeded from the YAML config on first run and editable live thereafter.
	set := settings.New(db)
	if err := set.Load(cfg); err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	// Alert engine reads thresholds and notify channels fresh from settings, so
	// edits apply without a restart.
	engine := buildAlertEngine(set)

	// Start the metrics collection loop.
	coll := collector.New(db, cfg.Server.Name, set, engine)
	go coll.Run(ctx)

	// Start the service-check scheduler (the check list is read live).
	sched := scheduler.New(db, cfg.Server.Name, set, engine)
	go sched.Run(ctx)

	// Start the cluster poller (announce + poll peers). The peer list lives in
	// storage and is managed live from the dashboard, so the poller always runs
	// even when no peers are configured yet.
	poller := cluster.New(db, cfg.Server.Name, cfg.Server.AdvertiseURL, set, engine)
	go poller.Run(ctx)

	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf(":%d", cfg.Server.Port)
	}

	// Authentication service + periodic expired-session cleanup.
	authSvc := auth.NewService(db)
	go pruneSessions(ctx, authSvc)

	handler := server.New(server.Config{
		NodeName: cfg.Server.Name,
		DB:       db,
		Settings: set,
		Auth:     authSvc,
		Engine:   engine,
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut the HTTP server down when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("heartd listening on %s (node %q, db %q, interval %s)",
		addr, cfg.Server.Name, cfg.Server.DBPath, cfg.Server.MetricsInterval)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// pruneSessions removes expired login sessions hourly until ctx is cancelled.
func pruneSessions(ctx context.Context, a *auth.Service) {
	_ = a.PruneExpired()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = a.PruneExpired()
		}
	}
}

// buildAlertEngine assembles an alert engine whose thresholds and notification
// channels are read fresh from settings on every use, so runtime edits apply
// immediately.
func buildAlertEngine(set *settings.Service) *alert.Engine {
	dispatcher := alert.NewDynamicDispatcher(func() []alert.Notifier {
		return notifiersFromSettings(set.Notify())
	})
	return alert.NewEngine(dispatcher, func() config.Thresholds {
		return set.General().Thresholds()
	})
}

// notifiersFromSettings builds the active notifiers from current notify settings.
func notifiersFromSettings(n settings.Notify) []alert.Notifier {
	var out []alert.Notifier
	if n.Webhook.Enabled && n.Webhook.URL != "" {
		out = append(out, alert.NewWebhookNotifier(config.WebhookNotify{URL: n.Webhook.URL}))
	}
	if n.Email.Enabled {
		out = append(out, alert.NewEmailNotifier(config.EmailNotify{
			SMTPHost:      n.Email.SMTPHost,
			SMTPPort:      n.Email.SMTPPort,
			Username:      n.Email.Username,
			Password:      n.Email.Password,
			From:          n.Email.From,
			To:            n.Email.To,
			SubjectPrefix: n.Email.SubjectPrefix,
		}))
	}
	return out
}
