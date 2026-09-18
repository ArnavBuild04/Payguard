package coordinatorerr

import "errors"

var (
	// ErrAlreadyProcessed is a success, not a failure: UNIQUE(order_id, operation_type) rejecting a
	// duplicate grant attempt (hld.md edge case F1) — the redelivery is a no-op, not an error.
	ErrAlreadyProcessed = errors.New("grant already processed")

	ErrTransactionNotFound = errors.New("transaction not found")

	ErrInvalidTransition = errors.New("invalid transaction status transition")

	// ErrClawbackShortfall fires when a compensating debit fails on insufficient balance — the
	// player already spent the chips. hld.md §5.6: the system never allows a negative balance;
	// this is a business decision for a human, not something Compensate can resolve alone.
	ErrClawbackShortfall = errors.New("clawback shortfall: balance insufficient for reversal")
)
