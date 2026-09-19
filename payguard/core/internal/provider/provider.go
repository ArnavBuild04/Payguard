// Package provider defines the boundary to the payment provider (PSP).
package provider

import (
	"context"
	"time"
)

// Status is our normalized view of a provider payment/refund state.
type Status string

const (
	StatusProcessing Status = "PROCESSING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed     Status = "FAILED"
	StatusRefunded   Status = "REFUNDED"
)

// CreateRequest carries everything the provider needs to attempt a charge.
type CreateRequest struct {
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
	WebhookURL     string
}

// Payment is the provider's view of a payment.
type Payment struct {
	ID          string
	Status      Status
	RawStatus   string
	AmountMinor int64
	Currency    string
	CreatedAt   time.Time
}

// Refund is the provider's view of a refund request.
type Refund struct {
	ID          string
	PaymentID   string
	Status      Status
	AmountMinor int64
}

// Provider is the only boundary this system has to an external payment processor.
type Provider interface {
	CreatePayment(ctx context.Context, req CreateRequest) (*Payment, error)
	GetPayment(ctx context.Context, id string) (*Payment, error)
	Refund(ctx context.Context, id string, amountMinor int64) (*Refund, error)
}
