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

	"github.com/alibaba/chronos-wallet/internal/bank"
	"github.com/alibaba/chronos-wallet/internal/config"
	"github.com/alibaba/chronos-wallet/internal/httpapi"
	"github.com/alibaba/chronos-wallet/internal/platform"
	"github.com/alibaba/chronos-wallet/internal/repository"
	"github.com/alibaba/chronos-wallet/internal/service"
	"github.com/alibaba/chronos-wallet/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect database failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("ping database failed", "error", err)
		os.Exit(1)
	}

	if err := platform.RunMigrations(ctx, pool); err != nil {
		logger.Error("run migrations failed", "error", err)
		os.Exit(1)
	}

	store := repository.New(pool)
	bankClient := bank.NewDefaultMockClient(cfg.BankMockFailureRate, cfg.BankMockNetworkErrorRate)
	appService := service.New(store, bankClient, cfg.BankTimeout)
	runner := worker.NewRunner(
		appService,
		logger,
		cfg.WorkerCount,
		cfg.WorkerPollInterval,
		cfg.ReconcileInterval,
		cfg.ProcessingTimeout,
	)

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		runner.Run(workerCtx)
	}()

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(appService),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			cancelWorkers()
			<-workerDone
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown failed", "error", err)
	}
	cancelWorkers()
	<-workerDone
	logger.Info("shutdown complete")
}
