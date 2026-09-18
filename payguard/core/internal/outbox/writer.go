// Package outbox is deliberately just a writer today. Write is called from inside another
// package's own transaction (payment, later coordinator) so the event row commits atomically with
// the state change it represents — this is what makes the dual-write problem structurally
// impossible, not just unlikely (PLAN.md Phase B1). The relay that drains these rows (in-process for
// now, Kafka in Phase B2) is a later addition; nothing here assumes it exists yet.
package outbox

import (
	"encoding/json"
	"fmt"

	"github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"gorm.io/gorm"
)

// Write inserts an outbox row using tx — the caller's own open transaction, never a new one of its
// own. Callers must pass tx, not a fresh *gorm.DB, or the atomicity guarantee this package exists
// for does not hold.
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
