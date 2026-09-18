package service

import "context"

type Service interface {
	// HandlePaymentSucceeded fans a paid order out into its bundle's grants (hld.md §6.1/§6.3).
	// Idempotent: a redelivery of the same orderID is safe — each grant's own UNIQUE(order_id,
	// operation_type) row, plus the downstream call's own idempotency, absorbs the replay. One
	// grant failing never blocks or reverses the others.
	HandlePaymentSucceeded(ctx context.Context, orderID uint64, tenantID string, userID int64, sku string, amountMinor int64) error

	// Compensate reverses every SUCCESS grant for an order (hld.md §5.6: a refund or chargeback
	// arriving after the purchase already delivered). A grant that never succeeded has nothing to
	// undo — compensation only ever starts from SUCCESS.
	Compensate(ctx context.Context, orderID uint64) error
}
