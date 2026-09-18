package walleterr

import "errors"

var (
	ErrAccountNotFound     = errors.New("account not found") // debit target that was never credited
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrAlreadyProcessed    = errors.New("operation already processed") // idempotent replay, not a failure
	ErrInvalidAmount       = errors.New("amount must be positive")
)
