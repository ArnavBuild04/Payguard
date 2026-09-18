// Package provider defines the boundary to the payment provider (PSP). The interface is the swap
// point: httpProvider talks to the mock service today and to a real PSP later, unchanged; fakeProvider
// (in the providertest subpackage) drives every failure path in tests. See hld.md §9.5.
package provider

import (
	"context"
	"time"
)

// Status is our normalized view of a provider payment/refund state. httpProvider is the only place
// that maps a raw provider string onto one of these — see http_provider.go's statusFromRaw.
type Status string

const (
	StatusProcessing Status = "PROCESSING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed     Status = "FAILED"
	StatusRefunded   Status = "REFUNDED"
)

// CreateRequest carries everything the provider needs to attempt a charge. IdempotencyKey is derived
// from our own payment_id (the Purchase → Provider idempotency layer, hld.md §3) so a retried
// CreatePayment call can never double-charge.
type CreateRequest struct {
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
	// WebhookURL is optional; when set, the provider delivers an outbound webhook on terminal
	// transitions. Polling remains the source of truth regardless (hld.md §9.5 rule 2).
	WebhookURL string
}

// Payment is the provider's view of a payment. RawStatus is kept alongside the normalized Status so
// evidence_json (Phase C) can show exactly what the provider said, not just how we interpreted it.
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

// Provider is the only boundary this system has to an external payment processor. Every failure this
// system can produce at that boundary is expressed through the two sentinels in providererr, never a
// silent guess — see hld.md §9.5.
type Provider interface {
	CreatePayment(ctx context.Context, req CreateRequest) (*Payment, error)
	GetPayment(ctx context.Context, id string) (*Payment, error)
	Refund(ctx context.Context, id string, amountMinor int64) (*Refund, error)
}
