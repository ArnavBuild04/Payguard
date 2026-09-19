package service_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	outboxmodels "github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	"github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providertest"
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

func getDB(t *testing.T) *gorm.DB {
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
	return testDB
}

func freshTenant(t *testing.T) string {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return fmt.Sprintf("tenant-%d-%d", time.Now().UnixNano(), n)
}

// seedProcessingPayment inserts a payment already carrying a provider reference.
func seedProcessingPayment(t *testing.T, r repo.Repo, providerPaymentID string) *models.Payment {
	t.Helper()
	ctx := context.Background()
	tenant := freshTenant(t)
	p, err := r.Create(ctx, &models.Payment{
		TenantID: tenant, UserID: 7, SKU: "BUNDLE_10K", AmountMinor: 999, Currency: "USD",
		Status: models.StatusProcessing, IdempotencyKey: fmt.Sprintf("idem-%s", tenant), RequestFingerprint: "fp",
	})
	if err != nil {
		t.Fatalf("seed create failed: %v", err)
	}
	if err := r.SetProviderRef(ctx, p.ID, providerPaymentID, "processing"); err != nil {
		t.Fatalf("seed set provider ref failed: %v", err)
	}
	return p
}

func newTestService(db *gorm.DB) service.Service {
	return service.NewService(repo.NewRepo(db), providertest.New(), "", nil)
}

func TestHandleWebhook_Succeeded_TransitionsPayment(t *testing.T) {
	db := getDB(t)
	r := repo.NewRepo(db)
	svc := newTestService(db)
	ctx := context.Background()

	providerPaymentID := fmt.Sprintf("prov-%d", time.Now().UnixNano())
	p := seedProcessingPayment(t, r, providerPaymentID)

	err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-1", "payment.succeeded", providerPaymentID, "succeeded", `{}`)
	if err != nil {
		t.Fatalf("HandleWebhook failed: %v", err)
	}

	got, err := svc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != models.StatusSucceeded {
		t.Fatalf("status = %v, want SUCCEEDED", got.Status)
	}
}

// TestHandleWebhook_DuplicateDelivery_IsNoOp is hld.md edge case C1.
func TestHandleWebhook_DuplicateDelivery_IsNoOp(t *testing.T) {
	db := getDB(t)
	r := repo.NewRepo(db)
	svc := newTestService(db)
	ctx := context.Background()

	providerPaymentID := fmt.Sprintf("prov-%d", time.Now().UnixNano())
	p := seedProcessingPayment(t, r, providerPaymentID)

	eventID := providerPaymentID + "-evt-dup"
	if err := svc.HandleWebhook(ctx, "provider", eventID, "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("first delivery failed: %v", err)
	}
	if err := svc.HandleWebhook(ctx, "provider", eventID, "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("duplicate delivery should be a no-op, got error: %v", err)
	}

	got, err := svc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != models.StatusSucceeded {
		t.Fatalf("status = %v, want SUCCEEDED", got.Status)
	}
}

// TestHandleWebhook_OutOfOrderRedelivery_SafeNoOp asserts a redelivered event doesn't disturb the row.
func TestHandleWebhook_OutOfOrderRedelivery_SafeNoOp(t *testing.T) {
	db := getDB(t)
	r := repo.NewRepo(db)
	svc := newTestService(db)
	ctx := context.Background()

	providerPaymentID := fmt.Sprintf("prov-%d", time.Now().UnixNano())
	p := seedProcessingPayment(t, r, providerPaymentID)

	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-1", "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("first delivery failed: %v", err)
	}
	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-2", "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("redelivered event should be a safe no-op, got error: %v", err)
	}

	got, err := svc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != models.StatusSucceeded {
		t.Fatalf("status = %v, want SUCCEEDED", got.Status)
	}
}

// TestHandleWebhook_UnknownProviderPaymentID_NeverErrors asserts an unmatched webhook never errors.
func TestHandleWebhook_UnknownProviderPaymentID_NeverErrors(t *testing.T) {
	db := getDB(t)
	svc := newTestService(db)
	ctx := context.Background()

	err := svc.HandleWebhook(ctx, "provider", "evt-unknown", "payment.succeeded", "no-such-provider-id", "succeeded", `{}`)
	if err != nil {
		t.Fatalf("unmatched webhook should not error, got: %v", err)
	}
}

// TestHandleWebhook_Refunded_MovesToRefundPending is hld.md §5.6/§4: SUCCEEDED → REFUND_PENDING.
func TestHandleWebhook_Refunded_MovesToRefundPending(t *testing.T) {
	db := getDB(t)
	r := repo.NewRepo(db)
	svc := newTestService(db)
	ctx := context.Background()

	providerPaymentID := fmt.Sprintf("prov-%d", time.Now().UnixNano())
	p := seedProcessingPayment(t, r, providerPaymentID)

	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-1", "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("succeeded delivery failed: %v", err)
	}
	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-2", "payment.refunded", providerPaymentID, "refunded", `{}`); err != nil {
		t.Fatalf("refunded delivery failed: %v", err)
	}

	got, err := svc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != models.StatusRefundPending {
		t.Fatalf("status = %v, want REFUND_PENDING", got.Status)
	}
}

func TestMarkRefunded_CompletesStateMachine(t *testing.T) {
	db := getDB(t)
	r := repo.NewRepo(db)
	svc := newTestService(db)
	ctx := context.Background()

	providerPaymentID := fmt.Sprintf("prov-%d", time.Now().UnixNano())
	p := seedProcessingPayment(t, r, providerPaymentID)

	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-1", "payment.succeeded", providerPaymentID, "succeeded", `{}`); err != nil {
		t.Fatalf("succeeded delivery failed: %v", err)
	}
	if err := svc.HandleWebhook(ctx, "provider", providerPaymentID+"-evt-2", "payment.refunded", providerPaymentID, "refunded", `{}`); err != nil {
		t.Fatalf("refunded delivery failed: %v", err)
	}
	if err := svc.MarkRefunded(ctx, p.ID); err != nil {
		t.Fatalf("mark refunded failed: %v", err)
	}

	got, err := svc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != models.StatusRefunded {
		t.Fatalf("status = %v, want REFUNDED", got.Status)
	}
}
