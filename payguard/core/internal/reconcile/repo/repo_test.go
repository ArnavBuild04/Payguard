package repo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testDSN = "host=localhost user=payguard password=payguard dbname=payguard port=5432 sslmode=disable TimeZone=UTC"

var (
	dbOnce sync.Once
	testDB *gorm.DB
	dbErr  error
	idSeq  int64
)

func getRepo(t *testing.T) repo.Repo {
	t.Helper()
	dbOnce.Do(func() {
		testDB, dbErr = gorm.Open(postgres.Open(testDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if dbErr == nil {
			dbErr = testDB.AutoMigrate(&models.Case{})
		}
		if dbErr == nil {
			dbErr = repo.Migrate(testDB)
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return repo.NewRepo(testDB)
}

func freshPaymentID(t *testing.T) uint64 {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return uint64(time.Now().UnixNano()) + uint64(n)
}

func newCase(paymentID uint64, reason models.Reason, subKey string) *models.Case {
	pid := paymentID
	return &models.Case{
		PaymentID:    &pid,
		Reason:       reason,
		SubKey:       subKey,
		Status:       models.StatusOpen,
		EvidenceJSON: `{}`,
	}
}

func TestCreateCase_Success(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	c, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if c.Status != models.StatusOpen {
		t.Fatalf("status = %v, want OPEN", c.Status)
	}
}

func TestCreateCase_DuplicateOpenCase_ReturnsExisting(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	first, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	second, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if !errors.Is(err, reconcileerr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second.ID = %d, want %d", second.ID, first.ID)
	}
}

// TestCreateCase_DifferentSubKey_BothOpen asserts MISSING_GRANT scoping: two different
// operation types on the same payment are two independent cases, not a dedup collision.
func TestCreateCase_DifferentSubKey_BothOpen(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	chips, err := r.CreateCase(ctx, newCase(pid, models.ReasonMissingGrant, "CREDIT_CHIPS"))
	if err != nil {
		t.Fatalf("chips create failed: %v", err)
	}
	ticket, err := r.CreateCase(ctx, newCase(pid, models.ReasonMissingGrant, "GRANT_TICKET"))
	if err != nil {
		t.Fatalf("ticket create failed: %v", err)
	}
	if chips.ID == ticket.ID {
		t.Fatal("expected two distinct cases for two distinct sub keys")
	}
}

func TestCreateCase_ResolvedThenReopened_BothVisible(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	first, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	if _, err := r.MarkResolved(ctx, first.ID, models.ActionAdvanceSucceeded, "resolved in test", "system"); err != nil {
		t.Fatalf("mark resolved failed: %v", err)
	}

	// The partial index only covers OPEN rows, so a new case for the same key is allowed once the
	// old one is no longer OPEN.
	second, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new row, not the resolved one")
	}
}

func TestCountOtherOpenCases(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	first, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	if _, err := r.CreateCase(ctx, newCase(pid, models.ReasonMissingGrant, "CREDIT_CHIPS")); err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	count, err := r.CountOtherOpenCases(ctx, pid, first.ID, models.ReasonProviderAhead)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

// TestCountOtherOpenCases_ExcludesSameReasonSiblings asserts two independent MISSING_GRANT cases
// for different operation types on one payment never block each other's auto-resolution.
func TestCountOtherOpenCases_ExcludesSameReasonSiblings(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	chips, err := r.CreateCase(ctx, newCase(pid, models.ReasonMissingGrant, "CREDIT_CHIPS"))
	if err != nil {
		t.Fatalf("chips create failed: %v", err)
	}
	if _, err := r.CreateCase(ctx, newCase(pid, models.ReasonMissingGrant, "GRANT_TICKET")); err != nil {
		t.Fatalf("ticket create failed: %v", err)
	}

	count, err := r.CountOtherOpenCases(ctx, pid, chips.ID, models.ReasonMissingGrant)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 — a sibling MISSING_GRANT case must not block this one", count)
	}
}

func TestMarkResolved_OnAlreadyResolvedCase_ReturnsErrCaseNotOpen(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	c, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := r.MarkResolved(ctx, c.ID, models.ActionAdvanceSucceeded, "first", "system"); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if _, err := r.MarkResolved(ctx, c.ID, models.ActionAdvanceSucceeded, "second", "system"); !errors.Is(err, reconcileerr.ErrCaseNotOpen) {
		t.Fatalf("err = %v, want ErrCaseNotOpen", err)
	}
}

func TestMarkDismissed_Success(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	c, err := r.CreateCase(ctx, newCase(pid, models.ReasonLocalAhead, ""))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	dismissed, err := r.MarkDismissed(ctx, c.ID, "arnav", "confirmed test traffic")
	if err != nil {
		t.Fatalf("dismiss failed: %v", err)
	}
	if dismissed.Status != models.StatusDismissed {
		t.Fatalf("status = %v, want DISMISSED", dismissed.Status)
	}
	if dismissed.Resolution != "confirmed test traffic" {
		t.Fatalf("resolution = %q, want the note", dismissed.Resolution)
	}
}

// TestConcurrentCreateCase_SameKey fires the SAME case-create from 50 goroutines; exactly one
// must win — the mechanism that stops a fast detector sweep from reopening the same case forever.
func TestConcurrentCreateCase_SameKey(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	pid := freshPaymentID(t)

	const n = 50
	var wg sync.WaitGroup
	var succeeded, alreadyProcessed int64
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := r.CreateCase(ctx, newCase(pid, models.ReasonProviderAhead, ""))
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
				ids[i] = c.ID
			case errors.Is(err, reconcileerr.ErrAlreadyProcessed):
				atomic.AddInt64(&alreadyProcessed, 1)
				ids[i] = c.ID
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if succeeded != 1 {
		t.Fatalf("succeeded = %d, want exactly 1", succeeded)
	}
	if alreadyProcessed != n-1 {
		t.Fatalf("alreadyProcessed = %d, want %d", alreadyProcessed, n-1)
	}
	first := ids[0]
	for i, id := range ids {
		if id != first {
			t.Fatalf("goroutine %d got case id %d, want %d", i, id, first)
		}
	}
}
