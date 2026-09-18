package providererr

import "errors"

var (
	// ErrUnknown covers a timeout, a non-2xx response, or a connection error — anything where we
	// genuinely do not know what the provider did. Never translate this into a failure: the charge
	// may have gone through. Callers must leave the payment PROCESSING, not FAILED.
	ErrUnknown = errors.New("provider outcome unknown")

	// ErrUnknownStatus fires when the provider answered (a real 2xx body) but with a status string
	// absent from httpProvider's map. Folding it onto a known status would be a guess; this stays a
	// case for a human (UNKNOWN_PROVIDER_STATE), never an automatic mapping.
	ErrUnknownStatus = errors.New("provider returned an unrecognized status")

	// ErrNotFound fires when the provider has no record of the given payment id.
	ErrNotFound = errors.New("payment not found at provider")
)
