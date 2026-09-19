package reconcileerr

import "errors"

var (
	// ErrAlreadyProcessed is a success, not a failure: an OPEN case already exists for this key.
	ErrAlreadyProcessed = errors.New("reconciliation case already open")

	ErrCaseNotFound = errors.New("reconciliation case not found")

	// ErrCaseNotOpen fires when approve/dismiss/resolve targets a case that isn't OPEN.
	ErrCaseNotOpen = errors.New("reconciliation case is not open")

	// ErrGateFailed fires when an auto-resolve attempt is made on a case the gate rejects.
	ErrGateFailed = errors.New("case does not pass the auto-resolution gate")

	// ErrActionRequired fires when a Lane 2 case has no safe default action and none was supplied.
	ErrActionRequired = errors.New("this case requires an explicit action")

	ErrUnknownAction = errors.New("unknown resolution action")

	// ErrNoteRequired fires on a dismiss with no note — no money moved, so the reasoning must be on record.
	ErrNoteRequired = errors.New("a note is required to dismiss a case")
)
