package service

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
)

type Service interface {
	// HandlePaymentSucceeded fans a paid order out into its bundle's grants; idempotent under redelivery.
	HandlePaymentSucceeded(ctx context.Context, orderID uint64, tenantID string, userID int64, sku string, amountMinor int64) error

	// Compensate reverses every SUCCESS grant for an order.
	Compensate(ctx context.Context, orderID uint64) error

	// RetryGrant forces one fresh attempt at a single grant, including re-arming a previously
	// FAILED one — unlike the normal fan-out path, which treats FAILED as terminal. This is the
	// only caller reconciliation's MISSING_GRANT resolution uses; a redelivered Kafka event never
	// calls this.
	RetryGrant(ctx context.Context, orderID uint64, op models.OperationType, tenantID string, userID int64, amountMinor int64) error
}
