package coordinatorerr

import "errors"

var (
	// ErrAlreadyProcessed is a success, not a failure: a duplicate grant attempt is a no-op.
	ErrAlreadyProcessed = errors.New("grant already processed")

	ErrTransactionNotFound = errors.New("transaction not found")

	ErrInvalidTransition = errors.New("invalid transaction status transition")

	// ErrClawbackShortfall fires when a compensating debit fails on insufficient balance.
	ErrClawbackShortfall = errors.New("clawback shortfall: balance insufficient for reversal")
)
