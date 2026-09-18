package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/outbox"
	"github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testDSN = "host=localhost user=payguard password=payguard dbname=payguard port=5432 sslmode=disable TimeZone=UTC"

var (
	dbOnce sync.Once
	testDB *gorm.DB
	dbErr  error
)

func getDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbOnce.Do(func() {
		testDB, dbErr = gorm.Open(postgres.Open(testDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if dbErr == nil {
			dbErr = testDB.AutoMigrate(&models.Event{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return testDB
}

func TestWrite_CommitsInsideTransaction(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	aggregateID := fmt.Sprintf("commit-test-%d", time.Now().UnixNano())

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return outbox.Write(tx, "payment", aggregateID, "PAYMENT_CREATED", map[string]string{"k": "v"})
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var count int64
	if err := db.Model(&models.Event{}).Where("aggregate_id = ?", aggregateID).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

// TestWrite_RolledBackWithTransaction is the dual-write proof: if the state change the event
// represents fails to commit, the event must not exist either — there is no window where an event
// is recorded for a change that never happened.
func TestWrite_RolledBackWithTransaction(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	sentinelErr := errors.New("simulated failure after the outbox write")

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := outbox.Write(tx, "payment", "rollback-test", "PAYMENT_CREATED", map[string]string{"k": "v"}); err != nil {
			return err
		}
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("err = %v, want sentinelErr", err)
	}

	var count int64
	if err := db.Model(&models.Event{}).Where("aggregate_id = ?", "rollback-test").Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 — the rolled-back write must leave no row", count)
	}
}
