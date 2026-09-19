package models

// allowedTransitions is the DTC state machine; compensation only ever starts from SUCCESS.
// FAILED -> PENDING is the one deliberate exception: a reconciliation-driven retry re-arming a
// previously-failed grant for one more attempt. It is never taken automatically by the normal
// fan-out path (hld.md F6: compensating a FAILED grant is "not a thing") — only RetryGrant uses it.
var allowedTransitions = map[Status]map[Status]bool{
	StatusPending:        {StatusSuccess: true, StatusFailed: true},
	StatusFailed:         {StatusPending: true},
	StatusSuccess:        {StatusReversePending: true},
	StatusReversePending: {StatusReversed: true},
}

func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}
