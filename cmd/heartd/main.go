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

	// Start the metrics collection loop.
	coll := collector.New(db, cfg.Server.Name, cfg.Server.MetricsInterval.Std(), cfg.Server.Retention.Std())
	go coll.Run(ctx)

	// Start the service-check scheduler.
	if len(cfg.Checks) > 0 {
		sched := scheduler.New(db, cfg.Server.Name, cfg.Checks)
		go sched.Run(ctx)
	}

	// Start the cluster poller (announce + poll peers) when peers are configured.
	if len(cfg.Peers) > 0 {
		poller := cluster.New(db, cfg.Server.Name, cfg.Server.AdvertiseURL, cfg.Server.PeerPollInterval.Std(), cfg.Peers)
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

	handler := server.New(server.Config{
		NodeName:    cfg.Server.Name,
		DB:          db,
		Checks:      cfg.Checks,
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
