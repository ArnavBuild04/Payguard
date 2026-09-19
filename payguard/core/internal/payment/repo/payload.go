package repo

import (
	"strconv"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
)

// paymentEventPayload is the outbox event body.
type paymentEventPayload struct {
	PaymentID   uint64        `json:"payment_id"`
	TenantID    string        `json:"tenant_id"`
	UserID      int64         `json:"user_id"`
	SKU         string        `json:"sku"`
	AmountMinor int64         `json:"amount_minor"`
	Currency    string        `json:"currency"`
	Status      models.Status `json:"status"`
}

func newPaymentCreatedPayload(p *models.Payment) paymentEventPayload {
	return paymentEventPayload{
		PaymentID:   p.ID,
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		SKU:         p.SKU,
		AmountMinor: p.AmountMinor,
		Currency:    p.Currency,
		Status:      p.Status,
	}
}

func newPaymentTransitionPayload(p *models.Payment) paymentEventPayload {
	return newPaymentCreatedPayload(p)
}

func formatID(id uint64) string {
	return strconv.FormatUint(id, 10)
}
