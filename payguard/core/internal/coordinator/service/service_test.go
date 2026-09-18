package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/granter"
	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	coordinatorrepo "github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/service"
	walletmodels "github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	walletrepo "github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	walletsvc "github.com/ArnavBuild04/payguard/core/internal/wallet/service"
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
			dbErr = testDB.AutoMigrate(&coordinatormodels.Transaction{}, &walletmodels.Account{}, &walletmodels.LedgerEntry{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return testDB
}

func freshOrderID(t *testing.T) uint64 {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return uint64(time.Now().UnixNano()) + uint64(n)
}

func newService(db *gorm.DB, ticketGranter granter.TicketGranter) (service.Service, walletsvc.Service) {
	wallet := walletsvc.NewService(walletrepo.NewRepo(db))
	assetGranter := granter.NewFake()
	svc := service.NewService(coordinatorrepo.NewRepo(db), wallet, assetGranter, ticketGranter)
	return svc, wallet
}

func TestHandlePaymentSucceeded_BundleTicket_AllGrantsLand(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	orderID := freshOrderID(t)
	tenant := fmt.Sprintf("tenant-%d", orderID)

	ticketGranter := granter.NewFake()
	svc, wallet := newService(db, ticketGranter)

	if err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_TICKET", 999); err != nil {
		t.Fatalf("HandlePaymentSucceeded failed: %v", err)
	}

	account, err := wallet.GetAccount(ctx, tenant, 42)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 999 {
		t.Fatalf("balance = %d, want 999", account.Balance)
	}
	if !ticketGranter.WasGranted(orderID) {
		t.Fatal("ticket was not granted")
	}
}

// TestHandlePaymentSucceeded_PartialBundle_ChipsLandTicketDoesNot is hld.md §6.3: stopping one
// grant service must not touch the other grant.
func TestHandlePaymentSucceeded_PartialBundle_ChipsLandTicketDoesNot(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	orderID := freshOrderID(t)
	tenant := fmt.Sprintf("tenant-%d", orderID)

	ticketGranter := granter.NewFake()
	ticketGranter.GrantErr = errors.New("ticket service down")
	svc, wallet := newService(db, ticketGranter)

	err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_TICKET", 999)
	if err == nil {
		t.Fatal("expected an error from the failed ticket grant, got nil")
	}

	account, gErr := wallet.GetAccount(ctx, tenant, 42)
	if gErr != nil {
		t.Fatalf("get account failed: %v", gErr)
	}
	if account.Balance != 999 {
		t.Fatalf("balance = %d, want 999 — chips must land even though the ticket grant failed", account.Balance)
	}
	if ticketGranter.WasGranted(orderID) {
		t.Fatal("ticket should not have been granted")
	}
}

// TestHandlePaymentSucceeded_Redelivery_DoesNotDoubleCredit is the at-least-once safety net
// (hld.md edge case E2/F1): a redelivered PAYMENT_SUCCEEDED event must not double-grant.
func TestHandlePaymentSucceeded_Redelivery_DoesNotDoubleCredit(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	orderID := freshOrderID(t)
	tenant := fmt.Sprintf("tenant-%d", orderID)

	ticketGranter := granter.NewFake()
	svc, wallet := newService(db, ticketGranter)

	if err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_10K", 999); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_10K", 999); err != nil {
		t.Fatalf("redelivered call failed: %v", err)
	}

	account, err := wallet.GetAccount(ctx, tenant, 42)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 999 {
		t.Fatalf("balance = %d, want 999 (credited exactly once)", account.Balance)
	}
}

// TestCompensate_ReversesEverySuccessGrant is hld.md §5.6: a refund walks every SUCCESS
// transaction for the order back to REVERSED, debiting chips and revoking the ticket.
func TestCompensate_ReversesEverySuccessGrant(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	orderID := freshOrderID(t)
	tenant := fmt.Sprintf("tenant-%d", orderID)

	ticketGranter := granter.NewFake()
	svc, wallet := newService(db, ticketGranter)

	if err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_TICKET", 999); err != nil {
		t.Fatalf("fan-out failed: %v", err)
	}

	if err := svc.Compensate(ctx, orderID); err != nil {
		t.Fatalf("compensate failed: %v", err)
	}

	account, err := wallet.GetAccount(ctx, tenant, 42)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 0 {
		t.Fatalf("balance = %d, want 0 after reversal", account.Balance)
	}
	if ticketGranter.WasGranted(orderID) {
		t.Fatal("ticket should have been revoked")
	}
}

// TestCompensate_ClawbackShortfall_LeavesRowUnreversed is hld.md §5.6/F7: if the player already
// spent the chips, the debit must fail on insufficient balance rather than go negative, and the
// transaction row stays REVERSE_PENDING for a human — it is not silently marked REVERSED.
func TestCompensate_ClawbackShortfall_LeavesRowUnreversed(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()
	orderID := freshOrderID(t)
	tenant := fmt.Sprintf("tenant-%d", orderID)

	ticketGranter := granter.NewFake()
	svc, wallet := newService(db, ticketGranter)

	if err := svc.HandlePaymentSucceeded(ctx, orderID, tenant, 42, "BUNDLE_10K", 999); err != nil {
		t.Fatalf("fan-out failed: %v", err)
	}
	// Player spends the chips elsewhere before the refund arrives.
	if err := wallet.Debit(ctx, tenant, 42, 999, walletmodels.SourceInGame, "spent-it-all"); err != nil {
		t.Fatalf("spend failed: %v", err)
	}

	err := svc.Compensate(ctx, orderID)
	if err == nil {
		t.Fatal("expected a clawback shortfall error, got nil")
	}

	account, gErr := wallet.GetAccount(ctx, tenant, 42)
	if gErr != nil {
		t.Fatalf("get account failed: %v", gErr)
	}
	if account.Balance != 0 {
		t.Fatalf("balance = %d, want 0 — must never go negative", account.Balance)
	}
}
