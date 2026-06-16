package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alibaba/chronos-wallet/internal/bank"
)

type Store interface {
	CreateWallet(ctx context.Context) (Wallet, error)
	WalletBalance(ctx context.Context, walletID string) (Balance, error)
	Deposit(ctx context.Context, walletID string, amount int64) (Balance, error)
	ScheduleWithdrawal(ctx context.Context, walletID string, amount int64, executeAt time.Time) (Withdrawal, error)
	Withdrawal(ctx context.Context, id string) (Withdrawal, error)
	ClaimDueWithdrawal(ctx context.Context) (DueWithdrawal, bool, error)
	ResetStaleProcessing(ctx context.Context, timeout time.Duration) (int64, error)
	PrepareWithdrawalDebit(ctx context.Context, withdrawal DueWithdrawal) (bool, error)
	MarkWithdrawalSucceeded(ctx context.Context, withdrawalID, bankReference string) error
	MarkRequiresReconciliation(ctx context.Context, withdrawalID string, cause error) error
	MarkBankRejected(ctx context.Context, withdrawal DueWithdrawal, cause string) error
	ReconciliationCandidates(ctx context.Context, limit int) ([]DueWithdrawal, error)
}

type Service struct {
	store       Store
	bank        bank.Client
	bankTimeout time.Duration
	now         func() time.Time
}

func New(store Store, bankClient bank.Client, bankTimeout time.Duration) *Service {
	return &Service{
		store:       store,
		bank:        bankClient,
		bankTimeout: bankTimeout,
		now:         time.Now,
	}
}

func (s *Service) CreateWallet(ctx context.Context) (Wallet, error) {
	return s.store.CreateWallet(ctx)
}

func (s *Service) Balance(ctx context.Context, walletID string) (Balance, error) {
	return s.store.WalletBalance(ctx, walletID)
}

func (s *Service) Deposit(ctx context.Context, walletID string, amount int64) (Balance, error) {
	if amount <= 0 {
		return Balance{}, ErrInvalidAmount
	}
	return s.store.Deposit(ctx, walletID, amount)
}

func (s *Service) ScheduleWithdrawal(ctx context.Context, walletID string, amount int64, executeAt time.Time) (Withdrawal, error) {
	if amount <= 0 {
		return Withdrawal{}, ErrInvalidAmount
	}
	if !executeAt.After(s.now().UTC()) {
		return Withdrawal{}, ErrExecuteAtNotFuture
	}
	return s.store.ScheduleWithdrawal(ctx, walletID, amount, executeAt.UTC())
}

func (s *Service) Withdrawal(ctx context.Context, id string) (Withdrawal, error) {
	return s.store.Withdrawal(ctx, id)
}

func (s *Service) ClaimDueWithdrawal(ctx context.Context) (DueWithdrawal, bool, error) {
	return s.store.ClaimDueWithdrawal(ctx)
}

func (s *Service) ResetStaleProcessing(ctx context.Context, timeout time.Duration) (int64, error) {
	return s.store.ResetStaleProcessing(ctx, timeout)
}

func (s *Service) ExecuteWithdrawal(ctx context.Context, withdrawal DueWithdrawal) error {
	prepared, err := s.store.PrepareWithdrawalDebit(ctx, withdrawal)
	if errors.Is(err, ErrInsufficientFunds) {
		return nil
	}
	if err != nil {
		return err
	}
	if !prepared {
		return nil
	}

	bankCtx, cancel := context.WithTimeout(ctx, s.bankTimeout)
	defer cancel()

	result, err := s.bank.Transfer(bankCtx, bank.TransferRequest{
		IdempotencyKey: withdrawal.IdempotencyKey,
		WalletID:       withdrawal.WalletID,
		Amount:         withdrawal.Amount,
	})
	if err == nil && result.Status == bank.TransferSucceeded {
		return s.store.MarkWithdrawalSucceeded(ctx, withdrawal.ID, result.Reference)
	}
	if errors.Is(err, bank.ErrTransferRejected) || result.Status == bank.TransferRejected {
		return s.store.MarkBankRejected(ctx, withdrawal, "bank rejected transfer")
	}
	if err == nil {
		err = fmt.Errorf("bank transfer ended with status %s", result.Status)
	}
	return s.store.MarkRequiresReconciliation(ctx, withdrawal.ID, err)
}

func (s *Service) Reconcile(ctx context.Context, limit int) error {
	withdrawals, err := s.store.ReconciliationCandidates(ctx, limit)
	if err != nil {
		return err
	}
	for _, withdrawal := range withdrawals {
		result, err := s.bank.Status(ctx, withdrawal.IdempotencyKey)
		if err != nil {
			continue
		}
		switch result.Status {
		case bank.TransferSucceeded:
			if err := s.store.MarkWithdrawalSucceeded(ctx, withdrawal.ID, result.Reference); err != nil {
				return err
			}
		case bank.TransferRejected:
			if err := s.store.MarkBankRejected(ctx, withdrawal, "bank rejected transfer during reconciliation"); err != nil {
				return err
			}
		case bank.TransferUnknown:
			continue
		}
	}
	return nil
}
