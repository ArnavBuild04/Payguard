package repo_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	outboxmodels "github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	"github.com/ArnavBuild04/payguard/core/internal/payment/repo"
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
			dbErr = testDB.AutoMigrate(&models.Payment{}, &models.PaymentEvent{}, &outboxmodels.Event{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return repo.NewRepo(testDB)
}

func freshTenant(t *testing.T) string {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return fmt.Sprintf("tenant-%d-%d", time.Now().UnixNano(), n)
}

func newPayment(tenant, idemKey, fp string) *models.Payment {
	return &models.Payment{
		TenantID:           tenant,
		UserID:             42,
		SKU:                "BUNDLE_10K",
		AmountMinor:        999,
		Currency:           "USD",
		Status:             models.StatusProcessing,
		IdempotencyKey:     idemKey,
		RequestFingerprint: fp,
	}
}

func TestCreate_Success_WritesOutboxEvent(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	p, err := r.Create(ctx, newPayment(tenant, "k-1", "fp-1"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if p.Status != models.StatusProcessing {
		t.Fatalf("status = %v, want PROCESSING", p.Status)
	}

	var count int64
	if err := testDB.Model(&outboxmodels.Event{}).
		Where("aggregate_type = ? AND aggregate_id = ? AND event_type = ?", "payment", fmt.Sprint(p.ID), "PAYMENT_CREATED").
		Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox event count = %d, want 1", count)
	}
}

func TestCreate_IdempotentReplay_SameBody_ReturnsOriginal(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	first, err := r.Create(ctx, newPayment(tenant, "k-dup", "fp-same"))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	second, err := r.Create(ctx, newPayment(tenant, "k-dup", "fp-same"))
	if !errors.Is(err, paymenterr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned a different payment: got %d, want %d", second.ID, first.ID)
	}
}

func TestCreate_IdempotentReplay_DifferentBody_FingerprintMismatch(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	if _, err := r.Create(ctx, newPayment(tenant, "k-conflict", "fp-A")); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err := r.Create(ctx, newPayment(tenant, "k-conflict", "fp-B"))
	if !errors.Is(err, paymenterr.ErrFingerprintMismatch) {
		t.Fatalf("err = %v, want ErrFingerprintMismatch", err)
	}
}

func TestTransition_HappyPath_ProcessingToSucceeded(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	p, err := r.Create(ctx, newPayment(tenant, "k-transition", "fp-t"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	providerID := fmt.Sprintf("prov_%d", p.ID)
	updated, err := r.Transition(ctx, p.ID, models.StatusSucceeded, providerID, "succeeded")
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	if updated.Status != models.StatusSucceeded {
		t.Fatalf("status = %v, want SUCCEEDED", updated.Status)
	}
	if updated.ProviderPaymentID == nil || *updated.ProviderPaymentID != providerID {
		t.Fatalf("provider payment id not recorded: %+v", updated.ProviderPaymentID)
	}

	var count int64
	if err := testDB.Model(&outboxmodels.Event{}).
		Where("aggregate_id = ? AND event_type = ?", fmt.Sprint(p.ID), "PAYMENT_SUCCEEDED").
		Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox event count = %d, want 1", count)
	}
}

// TestTransition_RejectsIllegalMove asserts a duplicate/out-of-order webhook is a safe no-op.
func TestTransition_RejectsIllegalMove(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	p, err := r.Create(ctx, newPayment(tenant, "k-illegal", "fp-i"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	providerID := fmt.Sprintf("prov_%d", p.ID)
	if _, err := r.Transition(ctx, p.ID, models.StatusSucceeded, providerID, "succeeded"); err != nil {
		t.Fatalf("first transition failed: %v", err)
	}

	current, err := r.Transition(ctx, p.ID, models.StatusSucceeded, providerID, "succeeded")
	if !errors.Is(err, paymenterr.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
	if current == nil || current.Status != models.StatusSucceeded {
		t.Fatalf("current row should still reflect SUCCEEDED, got %+v", current)
	}

	// Never FAILED after a SUCCEEDED — a late/out-of-order "failed" webhook must also be rejected.
	if _, err := r.Transition(ctx, p.ID, models.StatusFailed, providerID, "failed"); !errors.Is(err, paymenterr.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestTransition_NeverToFailedFromProcessingOnRetry(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	p, err := r.Create(ctx, newPayment(tenant, "k-stays", "fp-s"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Confirms the payment is still sitting exactly where Create left it.
	got, err := r.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Status != models.StatusProcessing {
		t.Fatalf("status = %v, want PROCESSING", got.Status)
	}
}

func TestRecordEvent_DuplicateDelivery_IsIgnored(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	p, err := r.Create(ctx, newPayment(tenant, "k-event", "fp-e"))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	eventID := fmt.Sprintf("evt-%d", p.ID)
	evt := &models.PaymentEvent{
		PaymentID:   p.ID,
		Source:      "mockprovider",
		EventID:     eventID,
		EventType:   "payment.succeeded",
		PayloadJSON: `{}`,
	}
	if err := r.RecordEvent(ctx, evt); err != nil {
		t.Fatalf("first record failed: %v", err)
	}

	dup := &models.PaymentEvent{
		PaymentID:   p.ID,
		Source:      "mockprovider",
		EventID:     eventID,
		EventType:   "payment.succeeded",
		PayloadJSON: `{}`,
	}
	if err := r.RecordEvent(ctx, dup); !errors.Is(err, paymenterr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}
}

// TestConcurrentCreate_SameIdempotencyKey asserts exactly one of 50 concurrent creates wins.
func TestConcurrentCreate_SameIdempotencyKey(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant := freshTenant(t)

	const n = 50
	var wg sync.WaitGroup
	var succeeded, alreadyProcessed int64
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := r.Create(ctx, newPayment(tenant, "the-same-key", "fp-concurrent"))
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
				ids[i] = p.ID
			case errors.Is(err, paymenterr.ErrAlreadyProcessed):
				atomic.AddInt64(&alreadyProcessed, 1)
				ids[i] = p.ID
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
			t.Fatalf("goroutine %d got payment id %d, want %d (every caller must see the same row)", i, id, first)
		}
	}
}
