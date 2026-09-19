package models

// allowedTransitions is the payment state machine.
var allowedTransitions = map[Status]map[Status]bool{
	StatusProcessing:    {StatusSucceeded: true, StatusFailed: true},
	StatusSucceeded:     {StatusRefundPending: true},
	StatusRefundPending: {StatusRefunded: true},
}

// CanTransition reports whether moving a payment from `from` to `to` is a valid state change.
func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}
