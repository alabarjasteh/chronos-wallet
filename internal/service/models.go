package service

import "time"

type Wallet struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type Balance struct {
	WalletID string `json:"wallet_id"`
	Balance  int64  `json:"balance"`
}

type WithdrawalStatus string

const (
	StatusScheduled               WithdrawalStatus = "SCHEDULED"
	StatusProcessing              WithdrawalStatus = "PROCESSING"
	StatusSucceeded               WithdrawalStatus = "SUCCEEDED"
	StatusFailedInsufficientFunds WithdrawalStatus = "FAILED_INSUFFICIENT_FUNDS"
	StatusFailedBankRejected      WithdrawalStatus = "FAILED_BANK_REJECTED"
	StatusRequiresReconciliation  WithdrawalStatus = "REQUIRES_RECONCILIATION"
)

type Withdrawal struct {
	ID             string           `json:"id"`
	WalletID       string           `json:"wallet_id"`
	Amount         int64            `json:"amount"`
	ExecuteAt      time.Time        `json:"execute_at"`
	Status         WithdrawalStatus `json:"status"`
	IdempotencyKey string           `json:"idempotency_key"`
	BankReference  *string          `json:"bank_reference,omitempty"`
	Attempts       int              `json:"attempts"`
	LastError      *string          `json:"last_error,omitempty"`
	ProcessedAt    *time.Time       `json:"processed_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type DueWithdrawal struct {
	ID             string
	WalletID       string
	Amount         int64
	IdempotencyKey string
}
