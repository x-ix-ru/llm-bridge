// Package main is the entry point for the llm-bridge proxy.
// It initializes all components (config, backend pool, discovery,
// router, metrics collector, HTTP server) and orchestrates graceful shutdown.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"llm-bridge/backend"
	"llm-bridge/config"
	"llm-bridge/discovery"
	"llm-bridge/metrics"
	"llm-bridge/router"
	"llm-bridge/server"
)

func main() {
	configPath := flag.String("config", envOrDefault("CONFIG_PATH", "config.yaml"), "path to config file")
	port := flag.String("port", envOrDefault("PORT", "8080"), "HTTP port")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	store := config.NewStore(*configPath)
	if err := store.Load(); err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg := store.Get()

	pool := backend.NewPool()

	disc := discovery.New(time.Duration(cfg.Global.DiscoveryIntervalSec) * time.Second)

	urls := make([]string, len(cfg.Servers))
	for i, s := range cfg.Servers {
		urls[i] = s.URL
	}
	disc.SetServers(urls)

	rtr := router.New(store, disc, pool)

	mc := metrics.New(time.Duration(cfg.Global.DiscoveryIntervalSec) * time.Second)

	addr := fmt.Sprintf(":%s", *port)
	srv := server.New(store, disc, pool, rtr, mc, addr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	slog.Info("starting llm-bridge", "config", *configPath, "addr", addr)

	disc.Start(ctx)
	mc.Start(ctx, urls)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "error", err)
	}

	// Graceful shutdown sequence
	slog.Info("shutdown signal received")

	drainTimeout := time.Duration(cfg.Global.DrainTimeoutSec) * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), drainTimeout)
	defer shutdownCancel()

	slog.Info("stopping discovery")
	disc.Stop()

	slog.Info("stopping metrics collector")
	mc.Stop()

	slog.Info("draining queue")
	rtr.Drain()

	slog.Info("stopping queue manager")
	rtr.Stop()

	slog.Info("shutting down HTTP server", "drain_timeout", drainTimeout)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP shutdown error", "error", err)
	}

	slog.Info("llm-bridge stopped")
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
