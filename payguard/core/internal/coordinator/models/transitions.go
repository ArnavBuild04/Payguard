package models

// allowedTransitions is the DTC state machine from hld.md §4. Compensation exists only from
// SUCCESS, never from FAILED — nothing was delivered on a FAILED grant, so there is nothing to undo.
var allowedTransitions = map[Status]map[Status]bool{
	StatusPending:        {StatusSuccess: true, StatusFailed: true},
	StatusSuccess:        {StatusReversePending: true},
	StatusReversePending: {StatusReversed: true},
}

func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}
