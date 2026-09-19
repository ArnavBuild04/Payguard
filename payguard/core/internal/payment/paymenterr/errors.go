package paymenterr

import "errors"

var (
	// ErrAlreadyProcessed is a success, not a failure: an idempotent replay.
	ErrAlreadyProcessed = errors.New("payment operation already processed")

	// ErrFingerprintMismatch fires when a reused idempotency key arrives with a different body.
	ErrFingerprintMismatch = errors.New("idempotency key reused with a different request")

	ErrPaymentNotFound = errors.New("payment not found")

	// ErrInvalidTransition fires on an out-of-order or duplicate provider update.
	ErrInvalidTransition = errors.New("invalid payment status transition")

	ErrInvalidSKU = errors.New("unknown or inactive sku")

	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
)
