package outbox

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Handler dispatches one outbox event; a non-nil error leaves the row unpublished for the next poll.
type Handler func(ctx context.Context, event models.Event) error

// Locker is an optional leader lock for the sweep; a nil Locker just means every instance sweeps every tick.
type Locker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) bool
	Unlock(ctx context.Context, key string)
}

const (
	relayBatchSize = 20
	relayLockKey   = "outbox-relay-sweep"
	relayLockTTL   = 10 * time.Second
)

// RunRelay drains the outbox until ctx is cancelled; safe to run more than one instance.
func RunRelay(ctx context.Context, db *gorm.DB, locker Locker, handler Handler, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce(ctx, db, locker, handler)
		}
	}
}

func sweepOnce(ctx context.Context, db *gorm.DB, locker Locker, handler Handler) {
	if locker != nil {
		if !locker.TryLock(ctx, relayLockKey, relayLockTTL) {
			return // another instance already has the sweep this tick
		}
		defer locker.Unlock(ctx, relayLockKey)
	}
	drainUntilEmpty(ctx, db, handler)
}

func drainUntilEmpty(ctx context.Context, db *gorm.DB, handler Handler) {
	for {
		n, err := drainBatch(ctx, db, handler)
		if err != nil {
			slog.Error("outbox: drain batch failed", "err", err)
			return
		}
		if n < relayBatchSize {
			return
		}
	}
}

func drainBatch(ctx context.Context, db *gorm.DB, handler Handler) (int, error) {
	var events []models.Event
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("published_at IS NULL").
			Order("id").
			Limit(relayBatchSize).
			Find(&events).Error; err != nil {
			return err
		}

		for i := range events {
			ev := events[i]
			if hErr := handler(ctx, ev); hErr != nil {
				slog.Error("outbox: dispatch failed, will retry next poll", "event_id", ev.ID, "event_type", ev.EventType, "err", hErr)
				if err := tx.Model(&models.Event{}).Where("id = ?", ev.ID).
					Update("attempts", gorm.Expr("attempts + 1")).Error; err != nil {
					return err
				}
				continue
			}
			now := time.Now().UTC()
			if err := tx.Model(&models.Event{}).Where("id = ?", ev.ID).
				Updates(map[string]any{"published_at": now, "attempts": gorm.Expr("attempts + 1")}).Error; err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})

	return len(events), err
}
