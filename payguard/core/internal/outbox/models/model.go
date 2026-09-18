package models

import "time"

// Event is an outbox row. PublishedAt is nil until the relay (Phase B2's Kafka publish; an
// in-process drain for now) marks it delivered.
type Event struct {
	ID            uint64     `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	AggregateType string     `gorm:"column:aggregate_type;not null;index" json:"aggregate_type"`
	AggregateID   string     `gorm:"column:aggregate_id;not null;index" json:"aggregate_id"`
	EventType     string     `gorm:"column:event_type;not null" json:"event_type"`
	PayloadJSON   string     `gorm:"column:payload_json;type:text;not null" json:"payload_json"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	PublishedAt   *time.Time `gorm:"column:published_at" json:"published_at,omitempty"`
	Attempts      int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
}
