package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alibaba/chronos-wallet/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateWallet(ctx context.Context) (service.Wallet, error) {
	var wallet service.Wallet
	err := r.pool.QueryRow(ctx, `
		INSERT INTO wallets DEFAULT VALUES
		RETURNING id::text, created_at
	`).Scan(&wallet.ID, &wallet.CreatedAt)
	if err != nil {
		return service.Wallet{}, fmt.Errorf("create wallet: %w", err)
	}
	return wallet, nil
}

func (r *Repository) WalletBalance(ctx context.Context, walletID string) (service.Balance, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wallets WHERE id = $1)`, walletID).Scan(&exists)
	if err != nil {
		return service.Balance{}, fmt.Errorf("check wallet: %w", err)
	}
	if !exists {
		return service.Balance{}, service.ErrWalletNotFound
	}

	var balance int64
	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`, walletID).Scan(&balance)
	if err != nil {
		return service.Balance{}, fmt.Errorf("wallet balance: %w", err)
	}
	return service.Balance{WalletID: walletID, Balance: balance}, nil
}

func (r *Repository) Deposit(ctx context.Context, walletID string, amount int64) (service.Balance, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return service.Balance{}, fmt.Errorf("begin deposit: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockWallet(ctx, tx, walletID); err != nil {
		return service.Balance{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries(wallet_id, amount, kind)
		VALUES ($1, $2, 'DEPOSIT')
	`, walletID, amount); err != nil {
		return service.Balance{}, fmt.Errorf("insert deposit: %w", err)
	}
	balance, err := balanceInTx(ctx, tx, walletID)
	if err != nil {
		return service.Balance{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return service.Balance{}, fmt.Errorf("commit deposit: %w", err)
	}
	return service.Balance{WalletID: walletID, Balance: balance}, nil
}

func (r *Repository) ScheduleWithdrawal(ctx context.Context, walletID string, amount int64, executeAt time.Time) (service.Withdrawal, error) {
	var withdrawal service.Withdrawal
	err := r.pool.QueryRow(ctx, `
		INSERT INTO scheduled_withdrawals(wallet_id, amount, execute_at)
		VALUES ($1, $2, $3)
		RETURNING id::text, wallet_id::text, amount, execute_at, status, idempotency_key::text,
			bank_reference, attempts, last_error, processed_at, created_at, updated_at
	`, walletID, amount, executeAt.UTC()).Scan(
		&withdrawal.ID,
		&withdrawal.WalletID,
		&withdrawal.Amount,
		&withdrawal.ExecuteAt,
		&withdrawal.Status,
		&withdrawal.IdempotencyKey,
		&withdrawal.BankReference,
		&withdrawal.Attempts,
		&withdrawal.LastError,
		&withdrawal.ProcessedAt,
		&withdrawal.CreatedAt,
		&withdrawal.UpdatedAt,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return service.Withdrawal{}, service.ErrWalletNotFound
		}
		return service.Withdrawal{}, fmt.Errorf("schedule withdrawal: %w", err)
	}
	return withdrawal, nil
}

func (r *Repository) Withdrawal(ctx context.Context, id string) (service.Withdrawal, error) {
	var withdrawal service.Withdrawal
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, wallet_id::text, amount, execute_at, status, idempotency_key::text,
			bank_reference, attempts, last_error, processed_at, created_at, updated_at
		FROM scheduled_withdrawals
		WHERE id = $1
	`, id).Scan(
		&withdrawal.ID,
		&withdrawal.WalletID,
		&withdrawal.Amount,
		&withdrawal.ExecuteAt,
		&withdrawal.Status,
		&withdrawal.IdempotencyKey,
		&withdrawal.BankReference,
		&withdrawal.Attempts,
		&withdrawal.LastError,
		&withdrawal.ProcessedAt,
		&withdrawal.CreatedAt,
		&withdrawal.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.Withdrawal{}, service.ErrWithdrawalNotFound
	}
	if err != nil {
		return service.Withdrawal{}, fmt.Errorf("get withdrawal: %w", err)
	}
	return withdrawal, nil
}

func (r *Repository) ClaimDueWithdrawal(ctx context.Context) (service.DueWithdrawal, bool, error) {
	var withdrawal service.DueWithdrawal
	err := r.pool.QueryRow(ctx, `
		UPDATE scheduled_withdrawals
		SET status = 'PROCESSING',
			processing_started_at = NOW(),
			updated_at = NOW(),
			attempts = attempts + 1,
			last_error = NULL
		WHERE id = (
			SELECT id
			FROM scheduled_withdrawals
			WHERE status = 'SCHEDULED' AND execute_at <= NOW()
			ORDER BY execute_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id::text, wallet_id::text, amount, idempotency_key::text
	`).Scan(&withdrawal.ID, &withdrawal.WalletID, &withdrawal.Amount, &withdrawal.IdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.DueWithdrawal{}, false, nil
	}
	if err != nil {
		return service.DueWithdrawal{}, false, fmt.Errorf("claim due withdrawal: %w", err)
	}
	return withdrawal, true, nil
}

func (r *Repository) ResetStaleProcessing(ctx context.Context, timeout time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE scheduled_withdrawals
		SET status = 'SCHEDULED',
			processing_started_at = NULL,
			updated_at = NOW(),
			last_error = 'processing timed out and was rescheduled'
		WHERE status = 'PROCESSING'
			AND processing_started_at < NOW() - $1::interval
	`, pgInterval(timeout))
	if err != nil {
		return 0, fmt.Errorf("reset stale processing: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) PrepareWithdrawalDebit(ctx context.Context, withdrawal service.DueWithdrawal) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin withdrawal debit: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockWallet(ctx, tx, withdrawal.WalletID); err != nil {
		return false, err
	}

	debitExists, err := ledgerEntryExists(ctx, tx, withdrawal.ID, "WITHDRAWAL_DEBIT")
	if err != nil {
		return false, err
	}
	if debitExists {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit existing withdrawal debit: %w", err)
		}
		return true, nil
	}

	balance, err := balanceInTx(ctx, tx, withdrawal.WalletID)
	if err != nil {
		return false, err
	}
	if balance < withdrawal.Amount {
		if _, err := tx.Exec(ctx, `
			UPDATE scheduled_withdrawals
			SET status = 'FAILED_INSUFFICIENT_FUNDS',
				processing_started_at = NULL,
				processed_at = NOW(),
				updated_at = NOW(),
				last_error = 'insufficient funds at execution time'
			WHERE id = $1 AND status = 'PROCESSING'
		`, withdrawal.ID); err != nil {
			return false, fmt.Errorf("mark insufficient funds: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit insufficient funds: %w", err)
		}
		return false, service.ErrInsufficientFunds
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries(wallet_id, withdrawal_id, amount, kind, idempotency_key)
		VALUES ($1, $2, $3, 'WITHDRAWAL_DEBIT', $4)
	`, withdrawal.WalletID, withdrawal.ID, -withdrawal.Amount, withdrawal.IdempotencyKey); err != nil {
		return false, fmt.Errorf("insert withdrawal debit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit withdrawal debit: %w", err)
	}
	return true, nil
}

func (r *Repository) MarkWithdrawalSucceeded(ctx context.Context, withdrawalID, bankReference string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE scheduled_withdrawals
		SET status = 'SUCCEEDED',
			bank_reference = $2,
			processing_started_at = NULL,
			processed_at = NOW(),
			updated_at = NOW(),
			last_error = NULL
		WHERE id = $1 AND status IN ('PROCESSING', 'REQUIRES_RECONCILIATION')
	`, withdrawalID, bankReference)
	if err != nil {
		return fmt.Errorf("mark withdrawal succeeded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return service.ErrWithdrawalNotFound
	}
	return nil
}

func (r *Repository) MarkRequiresReconciliation(ctx context.Context, withdrawalID string, cause error) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE scheduled_withdrawals
		SET status = 'REQUIRES_RECONCILIATION',
			processing_started_at = NULL,
			updated_at = NOW(),
			last_error = $2
		WHERE id = $1 AND status = 'PROCESSING'
	`, withdrawalID, cause.Error())
	if err != nil {
		return fmt.Errorf("mark requires reconciliation: %w", err)
	}
	return nil
}

func (r *Repository) MarkBankRejected(ctx context.Context, withdrawal service.DueWithdrawal, cause string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin bank rejection: %w", err)
	}
	defer rollback(ctx, tx)

	if err := lockWallet(ctx, tx, withdrawal.WalletID); err != nil {
		return err
	}

	reversalExists, err := ledgerEntryExists(ctx, tx, withdrawal.ID, "WITHDRAWAL_REVERSAL")
	if err != nil {
		return err
	}
	if !reversalExists {
		debitExists, err := ledgerEntryExists(ctx, tx, withdrawal.ID, "WITHDRAWAL_DEBIT")
		if err != nil {
			return err
		}
		if debitExists {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ledger_entries(wallet_id, withdrawal_id, amount, kind, idempotency_key)
				VALUES ($1, $2, $3, 'WITHDRAWAL_REVERSAL', $4)
			`, withdrawal.WalletID, withdrawal.ID, withdrawal.Amount, withdrawal.IdempotencyKey); err != nil {
				return fmt.Errorf("insert withdrawal reversal: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE scheduled_withdrawals
		SET status = 'FAILED_BANK_REJECTED',
			processing_started_at = NULL,
			processed_at = NOW(),
			updated_at = NOW(),
			last_error = $2
		WHERE id = $1 AND status IN ('PROCESSING', 'REQUIRES_RECONCILIATION')
	`, withdrawal.ID, cause); err != nil {
		return fmt.Errorf("mark bank rejected: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bank rejection: %w", err)
	}
	return nil
}

func (r *Repository) ReconciliationCandidates(ctx context.Context, limit int) ([]service.DueWithdrawal, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, wallet_id::text, amount, idempotency_key::text
		FROM scheduled_withdrawals
		WHERE status = 'REQUIRES_RECONCILIATION'
		ORDER BY updated_at ASC, id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list reconciliation candidates: %w", err)
	}
	defer rows.Close()

	var withdrawals []service.DueWithdrawal
	for rows.Next() {
		var withdrawal service.DueWithdrawal
		if err := rows.Scan(&withdrawal.ID, &withdrawal.WalletID, &withdrawal.Amount, &withdrawal.IdempotencyKey); err != nil {
			return nil, fmt.Errorf("scan reconciliation candidate: %w", err)
		}
		withdrawals = append(withdrawals, withdrawal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconciliation candidates: %w", err)
	}
	return withdrawals, nil
}

func lockWallet(ctx context.Context, tx pgx.Tx, walletID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM wallets WHERE id = $1 FOR UPDATE`, walletID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrWalletNotFound
	}
	if err != nil {
		return fmt.Errorf("lock wallet: %w", err)
	}
	return nil
}

func balanceInTx(ctx context.Context, tx pgx.Tx, walletID string) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`, walletID).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("balance in transaction: %w", err)
	}
	return balance, nil
}

func ledgerEntryExists(ctx context.Context, tx pgx.Tx, withdrawalID, kind string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM ledger_entries
			WHERE withdrawal_id = $1 AND kind = $2
		)
	`, withdrawalID, kind).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check ledger entry exists: %w", err)
	}
	return exists, nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func pgInterval(duration time.Duration) string {
	return fmt.Sprintf("%f seconds", duration.Seconds())
}

func isForeignKeyViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "SQLSTATE 23503") || strings.Contains(err.Error(), "violates foreign key constraint"))
}
