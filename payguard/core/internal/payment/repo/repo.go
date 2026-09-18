package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
)

type Repo interface {
	// Create inserts a new PROCESSING payment plus its PAYMENT_CREATED outbox event, in one
	// transaction. On an idempotency-key replay it returns the ORIGINAL row alongside
	// paymenterr.ErrAlreadyProcessed (same body) or paymenterr.ErrFingerprintMismatch (different
	// body, no payment returned).
	Create(ctx context.Context, p *models.Payment) (*models.Payment, error)

	GetByID(ctx context.Context, id uint64) (*models.Payment, error)

	// Transition moves a payment to `to`, writing the matching outbox event in the same
	// transaction. If CanTransition(current, to) rejects the move, it returns the CURRENT row
	// unchanged alongside paymenterr.ErrInvalidTransition — safe for a caller to log and ignore.
	Transition(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) (*models.Payment, error)

	// SetProviderRef attaches the provider's reference to a payment that is still PROCESSING (no
	// status change, no outbox event — there is no state change to announce yet).
	SetProviderRef(ctx context.Context, id uint64, providerPaymentID, providerRawStatus string) error

	// IncrementAttempt records a provider call whose outcome was ambiguous (timeout, unknown
	// status) — evidence that we tried, without asserting anything about what happened.
	IncrementAttempt(ctx context.Context, id uint64) error

	// RecordEvent appends to the provider inbox. UNIQUE(source, event_id) makes a duplicate
	// delivery a no-op: paymenterr.ErrAlreadyProcessed, not a failure.
	RecordEvent(ctx context.Context, evt *models.PaymentEvent) error
}
