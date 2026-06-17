package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/alibaba/chronos-wallet/internal/service"
)

type Runner struct {
	service           *service.Service
	logger            *slog.Logger
	workerCount       int
	pollInterval      time.Duration
	reconcileInterval time.Duration
	processingTimeout time.Duration
}

func NewRunner(
	service *service.Service,
	logger *slog.Logger,
	workerCount int,
	pollInterval time.Duration,
	reconcileInterval time.Duration,
	processingTimeout time.Duration,
) *Runner {
	return &Runner{
		service:           service,
		logger:            logger,
		workerCount:       workerCount,
		pollInterval:      pollInterval,
		reconcileInterval: reconcileInterval,
		processingTimeout: processingTimeout,
	}
}

func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for i := 0; i < r.workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r.runExecutionWorker(ctx, workerID)
		}(i + 1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.runReconciliationWorker(ctx)
	}()

	wg.Wait()
}

func (r *Runner) runExecutionWorker(ctx context.Context, workerID int) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		if err := r.tickExecution(ctx, workerID); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("execution worker tick failed", "worker_id", workerID, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) tickExecution(ctx context.Context, workerID int) error {
	if _, err := r.service.ResetStaleProcessing(ctx, r.processingTimeout); err != nil {
		return err
	}

	for {
		withdrawal, ok, err := r.service.ClaimDueWithdrawal(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := r.service.ExecuteWithdrawal(ctx, withdrawal); err != nil {
			r.logger.Error("execute withdrawal failed", "worker_id", workerID, "withdrawal_id", withdrawal.ID, "error", err)
		}
	}
}

func (r *Runner) runReconciliationWorker(ctx context.Context) {
	ticker := time.NewTicker(r.reconcileInterval)
	defer ticker.Stop()

	for {
		if err := r.service.Reconcile(ctx, 100); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("reconciliation tick failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
