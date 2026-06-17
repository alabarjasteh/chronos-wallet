package service

import "errors"

var (
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrInvalidAmount      = errors.New("amount must be greater than zero")
	ErrExecuteAtNotFuture = errors.New("execute_at must be in the future")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrWithdrawalNotFound = errors.New("withdrawal not found")
)
