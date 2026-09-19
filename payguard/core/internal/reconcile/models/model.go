package models

import "time"

type Reason string

const (
	ReasonProviderAhead        Reason = "PROVIDER_AHEAD"
	ReasonLocalAhead           Reason = "LOCAL_AHEAD"
	ReasonMissingGrant         Reason = "MISSING_GRANT"
	ReasonStuckProcessing      Reason = "STUCK_PROCESSING"
	ReasonUnknownProviderState Reason = "UNKNOWN_PROVIDER_STATE"
	ReasonLedgerMismatch       Reason = "LEDGER_MISMATCH"
	ReasonUnmatchedWebhook     Reason = "UNMATCHED_WEBHOOK"
	ReasonClawbackShortfall    Reason = "CLAWBACK_SHORTFALL"
)

// Lane1Reasons auto-resolve behind a strict gate; every other reason always waits for a human.
var Lane1Reasons = map[Reason]bool{
	ReasonProviderAhead: true,
	ReasonMissingGrant:  true,
}

type Status string

const (
	StatusOpen      Status = "OPEN"
	StatusResolved  Status = "RESOLVED"
	StatusDismissed Status = "DISMISSED"
)

type Action string

const (
	ActionAdvanceSucceeded Action = "ADVANCE_SUCCEEDED"
	ActionAdvanceFailed    Action = "ADVANCE_FAILED"
	ActionRedriveGrant     Action = "REDRIVE_GRANT"
	ActionCompensate       Action = "COMPENSATE"
	ActionNoteOnly         Action = "NOTE_ONLY"
)

// Case is a reconciliation_cases row. PaymentID is nil for account-scoped reasons (LEDGER_MISMATCH)
// and possibly UNMATCHED_WEBHOOK. SubKey scopes the dedup index below payment_id — MISSING_GRANT
// uses the operation_type so a bundle missing two different grants opens two cases, not one.
type Case struct {
	ID           uint64     `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	PaymentID    *uint64    `gorm:"column:payment_id;index" json:"payment_id,omitempty"`
	Reason       Reason     `gorm:"column:reason;not null;index" json:"reason"`
	SubKey       string     `gorm:"column:sub_key;not null;default:''" json:"sub_key,omitempty"`
	Status       Status     `gorm:"column:status;not null;index" json:"status"`
	EvidenceJSON string     `gorm:"column:evidence_json;type:text;not null" json:"evidence_json"`
	DetectedAt   time.Time  `gorm:"column:detected_at;autoCreateTime" json:"detected_at"`
	Action       string     `gorm:"column:action" json:"action,omitempty"`
	Resolution   string     `gorm:"column:resolution" json:"resolution,omitempty"`
	ResolvedBy   string     `gorm:"column:resolved_by" json:"resolved_by,omitempty"`
	ResolvedAt   *time.Time `gorm:"column:resolved_at" json:"resolved_at,omitempty"`
	Attempts     int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName overrides GORM's default pluralized-struct-name convention ("cases"), which would
// collide with nothing here but wouldn't match hld.md's schema either.
func (Case) TableName() string {
	return "reconciliation_cases"
}

// Evidence is the structured content of Case.EvidenceJSON.
type Evidence struct {
	TenantID              string `json:"tenant_id,omitempty"`
	UserID                int64  `json:"user_id,omitempty"`
	SKU                   string `json:"sku,omitempty"`
	AmountMinor           int64  `json:"amount_minor,omitempty"`
	LocalStatus           string `json:"local_status,omitempty"`
	ProviderStatus        string `json:"provider_status,omitempty"`
	ProviderPaymentID     string `json:"provider_payment_id,omitempty"`
	AuthoritativeTerminal bool   `json:"authoritative_terminal"`
	AmountMatch           bool   `json:"amount_match"`
	OperationType         string `json:"operation_type,omitempty"`
	GrantsExpected        int    `json:"grants_expected,omitempty"`
	GrantsPresent         int    `json:"grants_present,omitempty"`
	AgeSeconds            int64  `json:"age_seconds"`
	LastError             string `json:"last_error,omitempty"`
}
