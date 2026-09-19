package service

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
)

type Service interface {
	// CreatePayment inserts a PROCESSING payment and kicks off the provider call in the background.
	CreatePayment(ctx context.Context, tenantID string, userID int64, sku, idempotencyKey string) (*models.Payment, error)

	GetPayment(ctx context.Context, id uint64) (*models.Payment, error)

	// HandleWebhook applies an inbound provider event; idempotent and safe under out-of-order delivery.
	HandleWebhook(ctx context.Context, source, eventID, eventType, providerPaymentID, rawStatus string, payloadJSON string) error

	// MarkRefunded completes REFUND_PENDING → REFUNDED once every grant has been reversed.
	MarkRefunded(ctx context.Context, id uint64) error

	// AdvanceFromProvider applies an authoritative provider terminal state via the normal CanTransition-guarded path.
	AdvanceFromProvider(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) error
}
