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

	// Build the alert engine from configured notification channels. nil when
	// neither email nor webhook is configured, which disables alerting.
	engine := buildAlertEngine(cfg)

	// Start the metrics collection loop.
	coll := collector.New(db, cfg.Server.Name, cfg.Server.MetricsInterval.Std(), cfg.Server.Retention.Std(), engine)
	go coll.Run(ctx)

	// Start the service-check scheduler.
	if len(cfg.Checks) > 0 {
		sched := scheduler.New(db, cfg.Server.Name, cfg.Checks, engine)
		go sched.Run(ctx)
	}

	// Start the cluster poller (announce + poll peers) when peers are configured.
	if len(cfg.Peers) > 0 {
		poller := cluster.New(db, cfg.Server.Name, cfg.Server.AdvertiseURL, cfg.Server.PeerPollInterval.Std(), cfg.Peers, engine)
		go poller.Run(ctx)
	}

	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf(":%d", cfg.Server.Port)
	}

	peerSecrets := make([]string, 0, len(cfg.Peers))
	for _, p := range cfg.Peers {
		peerSecrets = append(peerSecrets, p.Secret)
	}

	// Authentication service + periodic expired-session cleanup.
	authSvc := auth.NewService(db)
	go pruneSessions(ctx, authSvc)

	handler := server.New(server.Config{
		NodeName:    cfg.Server.Name,
		DB:          db,
		Checks:      cfg.Checks,
		Auth:        authSvc,
		PeerSecrets: peerSecrets,
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

// buildAlertEngine assembles the alert engine from configured notify channels.
// Returns nil when no channel is configured (alerting disabled).
func buildAlertEngine(cfg config.Config) *alert.Engine {
	var notifiers []alert.Notifier
	if cfg.Notify.Email != nil {
		notifiers = append(notifiers, alert.NewEmailNotifier(*cfg.Notify.Email))
	}
	if cfg.Notify.Webhook != nil {
		notifiers = append(notifiers, alert.NewWebhookNotifier(*cfg.Notify.Webhook))
	}

	dispatcher := alert.NewDispatcher(notifiers...)
	if dispatcher.Empty() {
		log.Printf("alerting disabled (no notify channels configured)")
		return nil
	}
	log.Printf("alerting enabled (%d channel(s))", len(notifiers))
	return alert.NewEngine(dispatcher, cfg.Thresholds)
}
