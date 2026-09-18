package service

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
)

type Service interface {
	// CreatePayment prices the SKU server-side, inserts a PROCESSING payment, and kicks off the
	// provider call in the background — the caller gets this row back immediately (202 territory);
	// it never blocks on the provider. On a same-body idempotency-key replay it returns the
	// original row alongside paymenterr.ErrAlreadyProcessed (200 territory, not an error to the
	// client).
	CreatePayment(ctx context.Context, tenantID string, userID int64, sku, idempotencyKey string) (*models.Payment, error)

	GetPayment(ctx context.Context, id uint64) (*models.Payment, error)
}
