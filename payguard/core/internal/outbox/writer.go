// Package outbox writes the transactional outbox row.
package outbox

import (
	"encoding/json"
	"fmt"

	"github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"gorm.io/gorm"
)

// Write must be called with the caller's own open transaction, never a fresh *gorm.DB.
func Write(tx *gorm.DB, aggregateType, aggregateID, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: encode payload: %w", err)
	}

	event := models.Event{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		PayloadJSON:   string(body),
	}
	return tx.Create(&event).Error
}
