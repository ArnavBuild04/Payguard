package models

import "time"

type Status string

const (
	StatusProcessing    Status = "PROCESSING"
	StatusSucceeded     Status = "SUCCEEDED"
	StatusFailed        Status = "FAILED"
	StatusRefundPending Status = "REFUND_PENDING"
	StatusRefunded      Status = "REFUNDED"
)

// Payment is the Purchase Service's own record. provider_payment_id/provider_status are pointers so
// multiple not-yet-confirmed rows can all be NULL under the unique index on provider_payment_id —
// Postgres treats NULLs as distinct, unlike empty strings.
type Payment struct {
	ID                 uint64     `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	TenantID           string     `gorm:"column:tenant_id;not null;uniqueIndex:uq_payment_idem,priority:1" json:"tenant_id"`
	UserID             int64      `gorm:"column:user_id;not null;index" json:"user_id"`
	SKU                string     `gorm:"column:sku;not null" json:"sku"`
	AmountMinor        int64      `gorm:"column:amount_minor;not null" json:"amount_minor"`
	Currency           string     `gorm:"column:currency;not null" json:"currency"`
	Status             Status     `gorm:"column:status;not null" json:"status"`
	IdempotencyKey     string     `gorm:"column:idempotency_key;not null;uniqueIndex:uq_payment_idem,priority:2" json:"idempotency_key"`
	RequestFingerprint string     `gorm:"column:request_fingerprint;not null" json:"-"`
	ProviderPaymentID  *string    `gorm:"column:provider_payment_id;uniqueIndex:uq_payment_provider_id" json:"provider_payment_id,omitempty"`
	ProviderStatus     *string    `gorm:"column:provider_status" json:"provider_status,omitempty"`
	Attempts           int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
	LastAttemptAt      *time.Time `gorm:"column:last_attempt_at" json:"last_attempt_at,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// PaymentEvent is the append-only inbox for everything the provider tells us (webhooks today, could
// carry polling snapshots too). Rows are kept even when a rejected transition follows — the evidence
// is retained regardless of what we act on.
type PaymentEvent struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	PaymentID   uint64    `gorm:"column:payment_id;not null;index" json:"payment_id"`
	Source      string    `gorm:"column:source;not null;uniqueIndex:uq_payment_event,priority:1" json:"source"`
	EventID     string    `gorm:"column:event_id;not null;uniqueIndex:uq_payment_event,priority:2" json:"event_id"`
	EventType   string    `gorm:"column:event_type;not null" json:"event_type"`
	PayloadJSON string    `gorm:"column:payload_json;type:text;not null" json:"payload_json"`
	ReceivedAt  time.Time `gorm:"column:received_at;autoCreateTime" json:"received_at"`
}
