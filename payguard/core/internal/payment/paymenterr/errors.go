package paymenterr

import "errors"

var (
	// ErrAlreadyProcessed is a success, not a failure: an idempotent replay of a create hitting
	// UNIQUE(tenant_id, idempotency_key), or a duplicate webhook hitting UNIQUE(source, event_id).
	ErrAlreadyProcessed = errors.New("payment operation already processed")

	// ErrFingerprintMismatch fires when a reused idempotency key arrives with a different
	// (user_id, sku, amount) body — the client has a bug, and silently returning the old order
	// would be worse than an error.
	ErrFingerprintMismatch = errors.New("idempotency key reused with a different request")

	ErrPaymentNotFound = errors.New("payment not found")

	// ErrInvalidTransition fires when a caller asks for a status change CanTransition rejects — an
	// out-of-order or duplicate provider update. The row is left untouched; this is expected to be
	// logged and ignored, not treated as an operational failure.
	ErrInvalidTransition = errors.New("invalid payment status transition")

	ErrInvalidSKU = errors.New("unknown or inactive sku")

	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
)
