package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
)

type Repo interface {
	// Create returns the existing row plus ErrAlreadyProcessed/ErrFingerprintMismatch on a replay.
	Create(ctx context.Context, p *models.Payment) (*models.Payment, error)

	GetByID(ctx context.Context, id uint64) (*models.Payment, error)

	GetByProviderPaymentID(ctx context.Context, providerPaymentID string) (*models.Payment, error)

	// Transition returns the current row unchanged plus ErrInvalidTransition if CanTransition rejects the move.
	Transition(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) (*models.Payment, error)

	SetProviderRef(ctx context.Context, id uint64, providerPaymentID, providerRawStatus string) error

	IncrementAttempt(ctx context.Context, id uint64) error

	// RecordEvent appends to the provider inbox; a duplicate delivery is ErrAlreadyProcessed.
	RecordEvent(ctx context.Context, evt *models.PaymentEvent) error
}
