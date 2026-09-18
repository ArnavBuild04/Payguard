package models

// allowedTransitions is the payment state machine from hld.md §4. Anything not listed here is
// rejected by CanTransition — this is what makes an out-of-order or duplicate webhook safe to just
// log and ignore, and it is the only place PROCESSING → FAILED is permitted, which callers must only
// reach on an authoritative provider failure, never a timeout.
var allowedTransitions = map[Status]map[Status]bool{
	StatusProcessing:    {StatusSucceeded: true, StatusFailed: true},
	StatusSucceeded:     {StatusRefundPending: true},
	StatusRefundPending: {StatusRefunded: true},
}

// CanTransition reports whether moving a payment from `from` to `to` is a valid state change.
func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}
