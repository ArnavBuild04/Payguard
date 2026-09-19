package models

import "time"

// UnmatchedWebhook is a provider event that named no payment we have a record of.
type UnmatchedWebhook struct {
	ID               uint64    `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	Source           string    `gorm:"column:source;not null;uniqueIndex:uq_unmatched_webhook,priority:1" json:"source"`
	EventID          string    `gorm:"column:event_id;not null;uniqueIndex:uq_unmatched_webhook,priority:2" json:"event_id"`
	PayloadJSON      string    `gorm:"column:payload_json;type:text;not null" json:"payload_json"`
	ReceivedAt       time.Time `gorm:"column:received_at;autoCreateTime" json:"received_at"`
	MatchedPaymentID *uint64   `gorm:"column:matched_payment_id" json:"matched_payment_id,omitempty"`
	CaseID           *uint64   `gorm:"column:case_id" json:"case_id,omitempty"`
}
