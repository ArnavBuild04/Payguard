package service

import "context"

type Service interface {
	// HandlePaymentSucceeded fans a paid order out into its bundle's grants; idempotent under redelivery.
	HandlePaymentSucceeded(ctx context.Context, orderID uint64, tenantID string, userID int64, sku string, amountMinor int64) error

	// Compensate reverses every SUCCESS grant for an order.
	Compensate(ctx context.Context, orderID uint64) error
}
