package providererr

import "errors"

var (
	// ErrUnknown covers a timeout, non-2xx, or connection error; never translate this into a failure.
	ErrUnknown = errors.New("provider outcome unknown")

	// ErrUnknownStatus fires when the provider's status string is absent from httpProvider's map.
	ErrUnknownStatus = errors.New("provider returned an unrecognized status")

	// ErrNotFound fires when the provider has no record of the given payment id.
	ErrNotFound = errors.New("payment not found at provider")
)
