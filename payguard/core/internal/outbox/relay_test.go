package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/outbox"
	"github.com/ArnavBuild04/payguard/core/internal/outbox/models"
)

func insertEvent(t *testing.T, aggregateID string) *models.Event {
	t.Helper()
	ev := &models.Event{AggregateType: "test", AggregateID: aggregateID, EventType: "TEST_EVENT", PayloadJSON: `{}`}
	if err := testDB.Create(ev).Error; err != nil {
		t.Fatalf("insert event failed: %v", err)
	}
	return ev
}

func TestRelay_DispatchSuccess_MarksPublished(t *testing.T) {
	getDB(t)
	aggregateID := fmt.Sprintf("relay-success-%d", time.Now().UnixNano())
	insertEvent(t, aggregateID)

	var dispatched int64
	handler := func(_ context.Context, ev models.Event) error {
		if ev.AggregateID == aggregateID {
			atomic.AddInt64(&dispatched, 1)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go outbox.RunRelay(ctx, testDB, handler, 20*time.Millisecond)
	<-ctx.Done()

	if atomic.LoadInt64(&dispatched) != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}

	var published bool
	if err := testDB.Model(&models.Event{}).
		Select("published_at IS NOT NULL").
		Where("aggregate_id = ?", aggregateID).
		Scan(&published).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !published {
		t.Fatal("event was not marked published")
	}
}

func TestRelay_DispatchFailure_LeavesUnpublishedAndRetries(t *testing.T) {
	getDB(t)
	aggregateID := fmt.Sprintf("relay-failure-%d", time.Now().UnixNano())
	insertEvent(t, aggregateID)

	var attempts int64
	handler := func(_ context.Context, ev models.Event) error {
		if ev.AggregateID != aggregateID {
			return nil
		}
		n := atomic.AddInt64(&attempts, 1)
		if n < 3 {
			return errors.New("simulated transient dispatch failure")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	go outbox.RunRelay(ctx, testDB, handler, 20*time.Millisecond)
	<-ctx.Done()

	if atomic.LoadInt64(&attempts) < 3 {
		t.Fatalf("attempts = %d, want at least 3 (a failed dispatch must be retried)", attempts)
	}

	var published bool
	if err := testDB.Model(&models.Event{}).
		Select("published_at IS NOT NULL").
		Where("aggregate_id = ?", aggregateID).
		Scan(&published).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !published {
		t.Fatal("event should have eventually been published after retries succeeded")
	}
}

// TestRelay_TwoInstances_NeverDoubleDispatch is hld.md edge case D4: SELECT ... FOR UPDATE SKIP
// LOCKED means two concurrent relay instances draining the same table never both process the same
// row.
func TestRelay_TwoInstances_NeverDoubleDispatch(t *testing.T) {
	getDB(t)
	const n = 30
	prefix := fmt.Sprintf("relay-concurrent-%d", time.Now().UnixNano())
	for i := 0; i < n; i++ {
		insertEvent(t, fmt.Sprintf("%s-%d", prefix, i))
	}

	var mu sync.Mutex
	seen := make(map[string]int)
	handler := func(_ context.Context, ev models.Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen[ev.AggregateID]++
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outbox.RunRelay(ctx, testDB, handler, 10*time.Millisecond)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d", prefix, i)
		if seen[id] != 1 {
			t.Fatalf("event %s dispatched %d times, want exactly 1", id, seen[id])
		}
	}
}
