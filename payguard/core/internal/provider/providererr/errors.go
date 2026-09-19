package providererr

import "errors"

var (
	// ErrUnknown covers a timeout, non-2xx, or connection error; never translate this into a failure.
	ErrUnknown = errors.New("provider outcome unknown")

	// ErrUnknownStatus fires when the provider's status string is absent from httpProvider's map.
	// Wrap it in UnknownStatusError to carry the raw string along for investigation.
	ErrUnknownStatus = errors.New("provider returned an unrecognized status")

	// ErrNotFound fires when the provider has no record of the given payment id.
	ErrNotFound = errors.New("payment not found at provider")
)

// UnknownStatusError carries the exact string the provider sent, for a human or the agent to see
// what "under_review" (or whatever it was) actually said. errors.Is against ErrUnknownStatus still
// works via Unwrap.
type UnknownStatusError struct {
	Raw string
}

func (e *UnknownStatusError) Error() string { return ErrUnknownStatus.Error() + ": " + e.Raw }
func (e *UnknownStatusError) Unwrap() error { return ErrUnknownStatus }
