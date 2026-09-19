package detector_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providertest"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/detector"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	reconcilerepo "github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
	walletmodels "github.com/ArnavBuild04/payguard/core/internal/wallet/models"
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
			dbErr = testDB.AutoMigrate(
				&paymentmodels.Payment{}, &coordinatormodels.Transaction{},
				&walletmodels.Account{}, &walletmodels.LedgerEntry{}, &models.Case{},
			)
		}
		if dbErr == nil {
			dbErr = reconcilerepo.Migrate(testDB)
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return testDB
}

func freshID(t *testing.T) uint64 {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return uint64(time.Now().UnixNano()) + uint64(n)
}

func seedProcessingPayment(t *testing.T, db *gorm.DB, providerPaymentID string, createdAt time.Time) *paymentmodels.Payment {
	t.Helper()
	tenant := fmt.Sprintf("tenant-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 1, SKU: "BUNDLE_10K", AmountMinor: 999, Currency: "USD",
		Status: paymentmodels.StatusProcessing, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerPaymentID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed create failed: %v", err)
	}
	if err := db.Model(p).UpdateColumn("created_at", createdAt).Error; err != nil {
		t.Fatalf("backdate failed: %v", err)
	}
	return p
}

func newDetector(db *gorm.DB, p provider.Provider) (*detector.Detector, reconcilerepo.Repo) {
	r := reconcilerepo.NewRepo(db)
	return detector.New(db, p, r), r
}

func TestDetectProviderMismatches_OpensProviderAhead(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	sim := providertest.New()

	created, err := sim.CreatePayment(ctx, provider.CreateRequest{IdempotencyKey: "k", AmountMinor: 999, Currency: "USD"})
	if err != nil {
		t.Fatalf("seed provider payment failed: %v", err)
	}
	sim.SetStatus(created.ID, provider.StatusSucceeded)

	p := seedProcessingPayment(t, db, created.ID, time.Now().UTC())
	det, r := newDetector(db, sim)

	det.Run(withTimeout(t), 200*time.Millisecond)

	cases, err := r.ListOpen(ctx)
	if err != nil {
		t.Fatalf("list open failed: %v", err)
	}
	found := false
	for _, c := range cases {
		if c.PaymentID != nil && *c.PaymentID == p.ID && c.Reason == models.ReasonProviderAhead {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a PROVIDER_AHEAD case for payment %d, got %+v", p.ID, cases)
	}
}

func TestDetectProviderMismatches_StuckProcessing_WhenProviderHasNoRecord(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	sim := providertest.New()
	sim.GetErr = providererr.ErrNotFound

	p := seedProcessingPayment(t, db, fmt.Sprintf("prov-unknown-%d", freshID(t)), time.Now().UTC().Add(-3*time.Minute))
	det, r := newDetector(db, sim)

	det.Run(withTimeout(t), 200*time.Millisecond)

	cases, err := r.ListOpen(ctx)
	if err != nil {
		t.Fatalf("list open failed: %v", err)
	}
	found := false
	for _, c := range cases {
		if c.PaymentID != nil && *c.PaymentID == p.ID && c.Reason == models.ReasonStuckProcessing {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a STUCK_PROCESSING case for payment %d, got %+v", p.ID, cases)
	}
}

func TestDetectLedgerMismatch_OpensCaseAndDedupsOnRerun(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	tenant := fmt.Sprintf("tenant-%d", freshID(t))

	if err := db.Create(&walletmodels.Account{TenantID: tenant, UserID: 1, Balance: 500}).Error; err != nil {
		t.Fatalf("seed account failed: %v", err)
	}
	// No ledger entries at all: balance (500) != SUM(ledger) (0).

	det, r := newDetector(db, providertest.New())
	det.Run(withTimeout(t), 200*time.Millisecond)

	cases, err := r.ListOpenByReason(ctx, models.ReasonLedgerMismatch)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	matching := 0
	for _, c := range cases {
		if containsTenant(c.EvidenceJSON, tenant) {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("matching LEDGER_MISMATCH cases = %d, want 1", matching)
	}

	// Run again — must not open a second case for the same account.
	det.Run(withTimeout(t), 200*time.Millisecond)
	cases, err = r.ListOpenByReason(ctx, models.ReasonLedgerMismatch)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	matching = 0
	for _, c := range cases {
		if containsTenant(c.EvidenceJSON, tenant) {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("after rerun, matching LEDGER_MISMATCH cases = %d, want still 1 (dedup)", matching)
	}
}

func TestDetectMissingGrantsFromTransactions_OpensPerOperationType(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	tenant := fmt.Sprintf("tenant-%d", freshID(t))

	providerID := fmt.Sprintf("prov-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 1, SKU: "BUNDLE_TICKET", AmountMinor: 499, Currency: "USD",
		Status: paymentmodels.StatusSucceeded, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}
	if err := db.Model(p).UpdateColumn("updated_at", time.Now().UTC().Add(-2*time.Minute)).Error; err != nil {
		t.Fatalf("backdate failed: %v", err)
	}
	// No transactions rows at all for this payment.

	det, r := newDetector(db, providertest.New())
	det.Run(withTimeout(t), 200*time.Millisecond)

	cases, err := r.ListOpen(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	ops := map[string]bool{}
	for _, c := range cases {
		if c.PaymentID != nil && *c.PaymentID == p.ID && c.Reason == models.ReasonMissingGrant {
			ops[c.SubKey] = true
		}
	}
	if !ops["CREDIT_CHIPS"] || !ops["GRANT_TICKET"] {
		t.Fatalf("expected MISSING_GRANT cases for both CREDIT_CHIPS and GRANT_TICKET, got %v", ops)
	}
}

// withTimeout bounds the test's own patience, not correctness: a deadline firing mid-transaction
// under real background load produces a spurious driver error, so this must stay comfortably
// larger than any single sweep could realistically take.
func withTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func containsTenant(evidenceJSON, tenant string) bool {
	return strings.Contains(evidenceJSON, tenant)
}
