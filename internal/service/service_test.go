package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alibaba/chronos-wallet/internal/bank"
)

func TestScheduleWithdrawalValidatesFutureExecution(t *testing.T) {
	store := &fakeStore{}
	bankClient := bank.NewMockClient(1, 0, 0)
	svc := New(store, bankClient, time.Second)
	svc.now = func() time.Time {
		return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	}

	_, err := svc.ScheduleWithdrawal(context.Background(), "wallet-1", 100, svc.now())
	if !errors.Is(err, ErrExecuteAtNotFuture) {
		t.Fatalf("expected ErrExecuteAtNotFuture, got %v", err)
	}

	_, err = svc.ScheduleWithdrawal(context.Background(), "wallet-1", 100, svc.now().Add(time.Minute))
	if err != nil {
		t.Fatalf("expected future withdrawal to be scheduled, got %v", err)
	}
	if store.scheduledAmount != 100 {
		t.Fatalf("expected scheduled amount 100, got %d", store.scheduledAmount)
	}
}

func TestExecuteWithdrawalMarksSuccess(t *testing.T) {
	store := &fakeStore{}
	svc := New(store, bank.NewMockClient(1, 0, 0), time.Second)
	withdrawal := DueWithdrawal{ID: "wd-1", WalletID: "wallet-1", Amount: 500, IdempotencyKey: "idem-1"}

	if err := svc.ExecuteWithdrawal(context.Background(), withdrawal); err != nil {
		t.Fatalf("execute withdrawal: %v", err)
	}

	if !store.preparedDebit {
		t.Fatal("expected debit to be prepared")
	}
	if store.succeededID != "wd-1" {
		t.Fatalf("expected withdrawal to be marked succeeded, got %q", store.succeededID)
	}
}

func TestExecuteWithdrawalReversesOnBankReject(t *testing.T) {
	store := &fakeStore{}
	svc := New(store, bank.NewMockClient(1, 1, 0), time.Second)
	withdrawal := DueWithdrawal{ID: "wd-1", WalletID: "wallet-1", Amount: 500, IdempotencyKey: "idem-1"}

	if err := svc.ExecuteWithdrawal(context.Background(), withdrawal); err != nil {
		t.Fatalf("execute withdrawal: %v", err)
	}

	if store.rejectedID != "wd-1" {
		t.Fatalf("expected withdrawal to be marked rejected, got %q", store.rejectedID)
	}
}

func TestExecuteWithdrawalRequiresReconciliationOnUnknownBankResult(t *testing.T) {
	store := &fakeStore{}
	svc := New(store, bank.NewMockClient(1, 0, 1), time.Second)
	withdrawal := DueWithdrawal{ID: "wd-1", WalletID: "wallet-1", Amount: 500, IdempotencyKey: "idem-1"}

	if err := svc.ExecuteWithdrawal(context.Background(), withdrawal); err != nil {
		t.Fatalf("execute withdrawal: %v", err)
	}

	if store.reconcileID != "wd-1" {
		t.Fatalf("expected withdrawal to require reconciliation, got %q", store.reconcileID)
	}
}

type fakeStore struct {
	scheduledAmount int64
	preparedDebit   bool
	succeededID     string
	rejectedID      string
	reconcileID     string
}

func (s *fakeStore) CreateWallet(context.Context) (Wallet, error) {
	return Wallet{ID: "wallet-1"}, nil
}

func (s *fakeStore) WalletBalance(context.Context, string) (Balance, error) {
	return Balance{WalletID: "wallet-1", Balance: 1000}, nil
}

func (s *fakeStore) Deposit(context.Context, string, int64) (Balance, error) {
	return Balance{WalletID: "wallet-1", Balance: 1000}, nil
}

func (s *fakeStore) ScheduleWithdrawal(_ context.Context, walletID string, amount int64, executeAt time.Time) (Withdrawal, error) {
	s.scheduledAmount = amount
	return Withdrawal{
		ID:        "wd-1",
		WalletID:  walletID,
		Amount:    amount,
		ExecuteAt: executeAt,
		Status:    StatusScheduled,
	}, nil
}

func (s *fakeStore) Withdrawal(context.Context, string) (Withdrawal, error) {
	return Withdrawal{ID: "wd-1"}, nil
}

func (s *fakeStore) ClaimDueWithdrawal(context.Context) (DueWithdrawal, bool, error) {
	return DueWithdrawal{}, false, nil
}

func (s *fakeStore) ResetStaleProcessing(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (s *fakeStore) PrepareWithdrawalDebit(context.Context, DueWithdrawal) (bool, error) {
	s.preparedDebit = true
	return true, nil
}

func (s *fakeStore) MarkWithdrawalSucceeded(_ context.Context, withdrawalID, _ string) error {
	s.succeededID = withdrawalID
	return nil
}

func (s *fakeStore) MarkRequiresReconciliation(_ context.Context, withdrawalID string, _ error) error {
	s.reconcileID = withdrawalID
	return nil
}

func (s *fakeStore) MarkBankRejected(_ context.Context, withdrawal DueWithdrawal, _ string) error {
	s.rejectedID = withdrawal.ID
	return nil
}

func (s *fakeStore) ReconciliationCandidates(context.Context, int) ([]DueWithdrawal, error) {
	return nil, nil
}
