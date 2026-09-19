package models

// allowedTransitions is the DTC state machine; compensation only ever starts from SUCCESS.
var allowedTransitions = map[Status]map[Status]bool{
	StatusPending:        {StatusSuccess: true, StatusFailed: true},
	StatusSuccess:        {StatusReversePending: true},
	StatusReversePending: {StatusReversed: true},
}

func CanTransition(from, to Status) bool {
	return allowedTransitions[from][to]
}
