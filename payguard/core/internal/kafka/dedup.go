package kafka

import (
	"context"
	"errors"

	"github.com/ArnavBuild04/payguard/core/internal/kafka/models"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// dedupStore is consumer-side idempotency, PK (consumer_group, event_id).
type dedupStore struct {
	db *gorm.DB
}

func newDedupStore(db *gorm.DB) *dedupStore {
	return &dedupStore{db: db}
}

func (d *dedupStore) alreadyProcessed(ctx context.Context, consumerGroup, eventID string) (bool, error) {
	var pe models.ProcessedEvent
	err := d.db.WithContext(ctx).
		Where("consumer_group = ? AND event_id = ?", consumerGroup, eventID).
		First(&pe).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// markProcessed treats 23505 as success: a concurrent delivery already recorded this event.
func (d *dedupStore) markProcessed(ctx context.Context, consumerGroup, eventID string) error {
	err := d.db.WithContext(ctx).Create(&models.ProcessedEvent{ConsumerGroup: consumerGroup, EventID: eventID}).Error
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil
	}
	return err
}
