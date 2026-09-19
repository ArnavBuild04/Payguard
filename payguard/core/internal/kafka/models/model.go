package models

import "time"

// ProcessedEvent is the consumer-side idempotency record, PK (consumer_group, event_id).
type ProcessedEvent struct {
	ConsumerGroup string    `gorm:"column:consumer_group;primaryKey" json:"consumer_group"`
	EventID       string    `gorm:"column:event_id;primaryKey" json:"event_id"`
	ProcessedAt   time.Time `gorm:"column:processed_at;autoCreateTime" json:"processed_at"`
}
