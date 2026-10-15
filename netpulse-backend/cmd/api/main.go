package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/netpulse/netpulse-backend/internal/config"
	"github.com/netpulse/netpulse-backend/internal/probes"
	"github.com/netpulse/netpulse-backend/internal/repository/postgres"
	"github.com/netpulse/netpulse-backend/internal/security"
	"github.com/netpulse/netpulse-backend/internal/service"
	"github.com/netpulse/netpulse-backend/internal/transport/httpapi"
	"github.com/netpulse/netpulse-backend/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Error("parse database config", "error", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = 12
	poolCfg.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		logger.Error("database is not ready; apply migrations first", "error", err)
		os.Exit(1)
	}
	repo := postgres.New(pool)
	tokens, err := security.NewManager(cfg.JWTSecret, 15*time.Minute)
	if err != nil {
		logger.Error("configure authentication", "error", err)
		os.Exit(1)
	}
	app := service.New(repo)
	runner := probes.New(cfg.RequestTimeout)
	monitor := worker.New(repo, runner, cfg.CheckInterval, cfg.RequestTimeout, cfg.MetricsRetentionDays, logger)
	go monitor.Run(ctx)
	api := httpapi.New(app, repo, repo, tokens, logger)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		logger.Info("NetPulse API listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
