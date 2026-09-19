package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coordinatorgranter "github.com/ArnavBuild04/payguard/core/internal/coordinator/granter"
	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	coordinatorrepo "github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
	coordinatorservice "github.com/ArnavBuild04/payguard/core/internal/coordinator/service"
	outboxmodels "github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	paymentrepo "github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	paymentservice "github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providertest"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	reconcilerepo "github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/service"
	walletmodels "github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	walletrepo "github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	walletservice "github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	testDSN         = "host=localhost user=payguard password=payguard dbname=payguard port=5432 sslmode=disable TimeZone=UTC"
	autoResolveCeil = int64(5000)
	maxAutoAttempts = 3
)

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
				&paymentmodels.Payment{}, &paymentmodels.PaymentEvent{},
				&outboxmodels.Event{}, &coordinatormodels.Transaction{},
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

func newServices(db *gorm.DB) (service.Service, paymentservice.Service, walletservice.Service) {
	reconcileSvc, paymentSvc, walletSvc, _ := newServicesWithTicketGranter(db, coordinatorgranter.NewInMemoryGranter())
	return reconcileSvc, paymentSvc, walletSvc
}

func newServicesWithTicketGranter(db *gorm.DB, ticketGranter coordinatorgranter.TicketGranter) (service.Service, paymentservice.Service, walletservice.Service, coordinatorservice.Service) {
	walletSvc := walletservice.NewService(walletrepo.NewRepo(db))
	paymentSvc := paymentservice.NewService(paymentrepo.NewRepo(db), providertest.New(), "", nil)
	coordinatorSvc := coordinatorservice.NewService(
		coordinatorrepo.NewRepo(db), walletSvc,
		coordinatorgranter.NewInMemoryGranter(), ticketGranter,
	)
	reconcileSvc := service.NewService(reconcilerepo.NewRepo(db), paymentSvc, coordinatorSvc, autoResolveCeil, maxAutoAttempts)
	return reconcileSvc, paymentSvc, walletSvc, coordinatorSvc
}

func seedProcessingPayment(t *testing.T, db *gorm.DB, amountMinor int64) *paymentmodels.Payment {
	t.Helper()
	tenant := fmt.Sprintf("tenant-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 1, SKU: "BUNDLE_10K", AmountMinor: amountMinor, Currency: "USD",
		Status: paymentmodels.StatusProcessing, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp",
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}
	return p
}

func openProviderAheadCase(t *testing.T, db *gorm.DB, paymentID uint64, amountMinor int64) uint64 {
	t.Helper()
	ev := models.Evidence{
		AmountMinor: amountMinor, ProviderStatus: "succeeded", ProviderPaymentID: fmt.Sprintf("prov-%d", paymentID),
		AuthoritativeTerminal: true, AmountMatch: true,
	}
	body, _ := json.Marshal(ev)
	pid := paymentID
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonProviderAhead, Status: models.StatusOpen, EvidenceJSON: string(body)}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}
	return c.ID
}

// A deadline firing mid-transaction under real background load produces a spurious driver error,
// so this must stay comfortably larger than any single sweep could realistically take.
func runAutoResolverBriefly(reconcileSvc service.Service) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reconcileSvc.RunAutoResolver(ctx, 200*time.Millisecond)
}

func TestAutoResolve_ProviderAhead_AdvancesPaymentAndResolvesCase(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, paymentSvc, _ := newServices(db)

	p := seedProcessingPayment(t, db, 999)
	caseID := openProviderAheadCase(t, db, p.ID, 999)

	runAutoResolverBriefly(reconcileSvc)

	c, err := reconcileSvc.GetCase(ctx, caseID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if c.Status != models.StatusResolved {
		t.Fatalf("status = %v, want RESOLVED", c.Status)
	}
	if c.ResolvedBy != "system" {
		t.Fatalf("resolved_by = %q, want system", c.ResolvedBy)
	}

	got, err := paymentSvc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if got.Status != paymentmodels.StatusSucceeded {
		t.Fatalf("payment status = %v, want SUCCEEDED", got.Status)
	}
}

func TestAutoResolve_SkipsWhenAmountExceedsCeiling(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, _ := newServices(db)

	p := seedProcessingPayment(t, db, autoResolveCeil+1)
	caseID := openProviderAheadCase(t, db, p.ID, autoResolveCeil+1)

	runAutoResolverBriefly(reconcileSvc)

	c, err := reconcileSvc.GetCase(ctx, caseID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if c.Status != models.StatusOpen {
		t.Fatalf("status = %v, want OPEN (amount over ceiling must never auto-resolve)", c.Status)
	}
}

func TestAutoResolve_SkipsWhenAnotherCaseIsOpenOnTheSamePayment(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, _ := newServices(db)

	p := seedProcessingPayment(t, db, 999)
	caseID := openProviderAheadCase(t, db, p.ID, 999)

	pid := p.ID
	other := &models.Case{PaymentID: &pid, Reason: models.ReasonMissingGrant, SubKey: "GRANT_TICKET", Status: models.StatusOpen, EvidenceJSON: `{}`}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("seed other case failed: %v", err)
	}

	runAutoResolverBriefly(reconcileSvc)

	c, err := reconcileSvc.GetCase(ctx, caseID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if c.Status != models.StatusOpen {
		t.Fatalf("status = %v, want OPEN (another open case on the payment must block auto-resolve)", c.Status)
	}
}

// TestAutoResolve_SiblingMissingGrantCases_BothResolveIndependently is a regression test: two
// MISSING_GRANT cases for different operation types on the same payment must not block each
// other's auto-resolution (hld.md §6.3's partial-bundle design — each grant stands on its own).
func TestAutoResolve_SiblingMissingGrantCases_BothResolveIndependently(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, walletSvc := newServices(db)

	tenant := fmt.Sprintf("tenant-%d", freshID(t))
	providerID := fmt.Sprintf("prov-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 9, SKU: "BUNDLE_TICKET", AmountMinor: 499, Currency: "USD",
		Status: paymentmodels.StatusSucceeded, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}

	pid := p.ID
	chipsEv := models.Evidence{TenantID: tenant, UserID: 9, SKU: "BUNDLE_TICKET", AmountMinor: 499, OperationType: "CREDIT_CHIPS"}
	ticketEv := models.Evidence{TenantID: tenant, UserID: 9, SKU: "BUNDLE_TICKET", AmountMinor: 499, OperationType: "GRANT_TICKET"}
	chipsBody, _ := json.Marshal(chipsEv)
	ticketBody, _ := json.Marshal(ticketEv)
	chips := &models.Case{PaymentID: &pid, Reason: models.ReasonMissingGrant, SubKey: "CREDIT_CHIPS", Status: models.StatusOpen, EvidenceJSON: string(chipsBody)}
	ticket := &models.Case{PaymentID: &pid, Reason: models.ReasonMissingGrant, SubKey: "GRANT_TICKET", Status: models.StatusOpen, EvidenceJSON: string(ticketBody)}
	if err := db.Create(chips).Error; err != nil {
		t.Fatalf("seed chips case failed: %v", err)
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket case failed: %v", err)
	}

	runAutoResolverBriefly(reconcileSvc)

	for _, id := range []uint64{chips.ID, ticket.ID} {
		c, err := reconcileSvc.GetCase(ctx, id)
		if err != nil {
			t.Fatalf("get case failed: %v", err)
		}
		if c.Status != models.StatusResolved {
			t.Fatalf("case %d status = %v, want RESOLVED", id, c.Status)
		}
	}

	account, err := walletSvc.GetAccount(ctx, tenant, 9)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 499 {
		t.Fatalf("balance = %d, want 499", account.Balance)
	}
}

// TestAutoResolve_MissingGrant_ActuallyGrantsNotJustClosesCase is the regression test for the
// case-storm bug: redriving a MISSING_GRANT case for a grant that's already permanently FAILED
// must actually re-attempt the grant (and only mark the case RESOLVED once it truly lands), not
// silently no-op against the terminal row while reporting success.
func TestAutoResolve_MissingGrant_ActuallyGrantsNotJustClosesCase(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	tenant := fmt.Sprintf("tenant-%d", freshID(t))

	ticketGranter := coordinatorgranter.NewInMemoryGranter()
	ticketGranter.GrantErr = errors.New("ticket service down")
	reconcileSvc, _, _, coordinatorSvc := newServicesWithTicketGranter(db, ticketGranter)

	providerID := fmt.Sprintf("prov-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 5, SKU: "BUNDLE_TICKET", AmountMinor: 499, Currency: "USD",
		Status: paymentmodels.StatusSucceeded, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}

	// Genuinely fail the ticket grant first, exactly like the live case-storm scenario.
	if err := coordinatorSvc.HandlePaymentSucceeded(ctx, p.ID, tenant, 5, "BUNDLE_TICKET", 499); err == nil {
		t.Fatal("expected the ticket grant to fail")
	}
	if ticketGranter.WasGranted(p.ID) {
		t.Fatal("ticket should not have been granted yet")
	}

	ev := models.Evidence{TenantID: tenant, UserID: 5, SKU: "BUNDLE_TICKET", AmountMinor: 499, OperationType: "GRANT_TICKET"}
	body, _ := json.Marshal(ev)
	pid := p.ID
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonMissingGrant, SubKey: "GRANT_TICKET", Status: models.StatusOpen, EvidenceJSON: string(body)}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}

	// The grant service is still down: approving now must fail, and the case must stay OPEN —
	// never marked RESOLVED against a grant that didn't actually land.
	if err := reconcileSvc.Approve(ctx, c.ID, "arnav", ""); err == nil {
		t.Fatal("expected approve to fail while the grant service is still down")
	}
	stillOpen, err := reconcileSvc.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if stillOpen.Status != models.StatusOpen {
		t.Fatalf("status = %v, want OPEN — must not be marked resolved without the grant landing", stillOpen.Status)
	}

	// The grant service recovers; approving again must actually grant the ticket this time.
	ticketGranter.GrantErr = nil
	if err := reconcileSvc.Approve(ctx, c.ID, "arnav", ""); err != nil {
		t.Fatalf("approve failed after recovery: %v", err)
	}
	if !ticketGranter.WasGranted(p.ID) {
		t.Fatal("ticket should have been granted after the retry")
	}
	resolved, err := reconcileSvc.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if resolved.Status != models.StatusResolved {
		t.Fatalf("status = %v, want RESOLVED", resolved.Status)
	}
}

func TestAutoResolve_NeverTouchesLane2Reasons(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, _ := newServices(db)

	p := seedProcessingPayment(t, db, 999)
	pid := p.ID
	ev := models.Evidence{AmountMinor: 999, AuthoritativeTerminal: true, AmountMatch: true, ProviderStatus: "refunded"}
	body, _ := json.Marshal(ev)
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonLocalAhead, Status: models.StatusOpen, EvidenceJSON: string(body)}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}

	runAutoResolverBriefly(reconcileSvc)

	got, err := reconcileSvc.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %v, want OPEN — LOCAL_AHEAD must never auto-resolve", got.Status)
	}
}

func TestApprove_MissingGrant_DefaultActionRedrivesGrant(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, walletSvc := newServices(db)

	tenant := fmt.Sprintf("tenant-%d", freshID(t))
	providerID := fmt.Sprintf("prov-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 7, SKU: "BUNDLE_10K", AmountMinor: 999, Currency: "USD",
		Status: paymentmodels.StatusSucceeded, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}

	ev := models.Evidence{TenantID: tenant, UserID: 7, SKU: "BUNDLE_10K", AmountMinor: 999, OperationType: "CREDIT_CHIPS"}
	body, _ := json.Marshal(ev)
	pid := p.ID
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonMissingGrant, SubKey: "CREDIT_CHIPS", Status: models.StatusOpen, EvidenceJSON: string(body)}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}

	if err := reconcileSvc.Approve(ctx, c.ID, "arnav", ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	got, err := reconcileSvc.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if got.Status != models.StatusResolved || got.ResolvedBy != "arnav" {
		t.Fatalf("case = %+v, want RESOLVED by arnav", got)
	}

	account, err := walletSvc.GetAccount(ctx, tenant, 7)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 999 {
		t.Fatalf("balance = %d, want 999 (grant redriven)", account.Balance)
	}
}

// TestApprove_LocalAhead_CompensatesAndMarksPaymentRefunded is the regression test for a real gap
// found live: a LOCAL_AHEAD case the detector finds (no webhook ever arrived) leaves the payment
// at SUCCEEDED, not REFUND_PENDING — approving it must still reach REFUNDED, not silently no-op.
func TestApprove_LocalAhead_CompensatesAndMarksPaymentRefunded(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, paymentSvc, walletSvc := newServices(db)

	tenant := fmt.Sprintf("tenant-%d", freshID(t))
	providerID := fmt.Sprintf("prov-%d", freshID(t))
	p := &paymentmodels.Payment{
		TenantID: tenant, UserID: 11, SKU: "BUNDLE_10K", AmountMinor: 999, Currency: "USD",
		Status: paymentmodels.StatusSucceeded, IdempotencyKey: fmt.Sprintf("idem-%d", freshID(t)),
		RequestFingerprint: "fp", ProviderPaymentID: &providerID,
	}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed payment failed: %v", err)
	}
	if err := db.Create(&coordinatormodels.Transaction{
		OrderID: p.ID, OperationType: coordinatormodels.OpCreditChips, Status: coordinatormodels.StatusSuccess,
		TenantID: tenant, UserID: 11, AmountMinor: 999,
	}).Error; err != nil {
		t.Fatalf("seed transaction failed: %v", err)
	}
	if err := walletSvc.Credit(ctx, tenant, 11, 999, walletmodels.SourcePurchase, fmt.Sprintf("order-%d-credit_chips", p.ID)); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}

	ev := models.Evidence{
		TenantID: tenant, UserID: 11, SKU: "BUNDLE_10K", AmountMinor: 999,
		ProviderPaymentID: providerID, ProviderStatus: "refunded", AuthoritativeTerminal: true,
	}
	body, _ := json.Marshal(ev)
	pid := p.ID
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonLocalAhead, Status: models.StatusOpen, EvidenceJSON: string(body)}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}

	if err := reconcileSvc.Approve(ctx, c.ID, "arnav", ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	account, err := walletSvc.GetAccount(ctx, tenant, 11)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 0 {
		t.Fatalf("balance = %d, want 0 after clawback", account.Balance)
	}

	payment, err := paymentSvc.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if payment.Status != paymentmodels.StatusRefunded {
		t.Fatalf("payment status = %v, want REFUNDED", payment.Status)
	}
}

func TestDismiss_RequiresNote(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	reconcileSvc, _, _ := newServices(db)

	p := seedProcessingPayment(t, db, 999)
	pid := p.ID
	c := &models.Case{PaymentID: &pid, Reason: models.ReasonClawbackShortfall, Status: models.StatusOpen, EvidenceJSON: `{}`}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed case failed: %v", err)
	}

	if err := reconcileSvc.Dismiss(ctx, c.ID, "arnav", ""); !errors.Is(err, reconcileerr.ErrNoteRequired) {
		t.Fatalf("err = %v, want ErrNoteRequired", err)
	}

	if err := reconcileSvc.Dismiss(ctx, c.ID, "arnav", "wrote off per policy"); err != nil {
		t.Fatalf("dismiss with note failed: %v", err)
	}

	got, err := reconcileSvc.GetCase(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case failed: %v", err)
	}
	if got.Status != models.StatusDismissed {
		t.Fatalf("status = %v, want DISMISSED", got.Status)
	}
}
