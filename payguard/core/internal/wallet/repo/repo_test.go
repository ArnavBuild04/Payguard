package repo_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Matches docker-compose.yml. Overridable isn't needed for a local speed-run
// suite, but if this ever needs to run in CI against a different host, this
// is the one line to change.
const testDSN = "host=localhost user=payguard password=payguard dbname=payguard port=5432 sslmode=disable TimeZone=UTC"

var (
	dbOnce sync.Once
	testDB *gorm.DB
	dbErr  error
	idSeq  int64
)

// getRepo skips the test if Postgres isn't reachable, so go test ./... stays green with no infra.
func getRepo(t *testing.T) repo.Repo {
	t.Helper()
	dbOnce.Do(func() {
		testDB, dbErr = gorm.Open(postgres.Open(testDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if dbErr == nil {
			dbErr = testDB.AutoMigrate(&models.Account{}, &models.LedgerEntry{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return repo.NewRepo(testDB)
}

// freshTenant isolates each test's accounts without needing to truncate shared tables.
func freshTenant(t *testing.T) (string, int64) {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return fmt.Sprintf("tenant-%d-%d", time.Now().UnixNano(), n), n
}

func TestDebit_Success(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 1000, models.SourceInGame, "seed-1"); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}
	if err := r.Debit(ctx, tenant, user, 300, models.SourceInGame, "debit-1"); err != nil {
		t.Fatalf("debit failed: %v", err)
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 700 {
		t.Fatalf("balance = %d, want 700", account.Balance)
	}
}

func TestDebit_InsufficientBalance(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 100, models.SourceInGame, "seed-1"); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}

	err := r.Debit(ctx, tenant, user, 500, models.SourceInGame, "debit-1")
	if !errors.Is(err, walleterr.ErrInsufficientBalance) {
		t.Fatalf("err = %v, want ErrInsufficientBalance", err)
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 100 {
		t.Fatalf("balance = %d, want unchanged 100", account.Balance)
	}
}

func TestDebit_AccountNotFound(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	err := r.Debit(ctx, tenant, user, 10, models.SourceInGame, "debit-1")
	if !errors.Is(err, walleterr.ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
}

func TestDebit_DuplicateReferenceID(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 1000, models.SourceInGame, "seed-1"); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}
	if err := r.Debit(ctx, tenant, user, 200, models.SourceInGame, "debit-dup"); err != nil {
		t.Fatalf("first debit failed: %v", err)
	}

	err := r.Debit(ctx, tenant, user, 200, models.SourceInGame, "debit-dup")
	if !errors.Is(err, walleterr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 800 {
		t.Fatalf("balance = %d, want 800", account.Balance)
	}
}

func TestCredit_DuplicateReferenceID(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 500, models.SourceInGame, "credit-dup"); err != nil {
		t.Fatalf("first credit failed: %v", err)
	}

	err := r.Credit(ctx, tenant, user, 500, models.SourceInGame, "credit-dup")
	if !errors.Is(err, walleterr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if account.Balance != 500 {
		t.Fatalf("balance = %d, want 500", account.Balance)
	}
}

// TestConcurrentDebit_DistinctReferences proves the row lock serializes distinct concurrent debits with no lost updates.
func TestConcurrentDebit_DistinctReferences(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 10_000, models.SourceInGame, "seed-1"); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = r.Debit(ctx, tenant, user, 100, models.SourceInGame, fmt.Sprintf("debit-%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("debit %d failed: %v", i, err)
		}
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if want := int64(10_000 - n*100); account.Balance != want {
		t.Fatalf("balance = %d, want %d", account.Balance, want)
	}
}

// TestConcurrentDebit_SameReference fires the SAME debit from 50 goroutines; exactly one must apply.
func TestConcurrentDebit_SameReference(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	if err := r.Credit(ctx, tenant, user, 10_000, models.SourceInGame, "seed-1"); err != nil {
		t.Fatalf("seed credit failed: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	var succeeded, alreadyProcessed int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Debit(ctx, tenant, user, 100, models.SourceInGame, "the-same-reference")
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
			case errors.Is(err, walleterr.ErrAlreadyProcessed):
				atomic.AddInt64(&alreadyProcessed, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if succeeded != 1 {
		t.Fatalf("succeeded = %d, want exactly 1", succeeded)
	}
	if alreadyProcessed != n-1 {
		t.Fatalf("alreadyProcessed = %d, want %d", alreadyProcessed, n-1)
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if want := int64(10_000 - 100); account.Balance != want {
		t.Fatalf("balance = %d, want %d", account.Balance, want)
	}
}

// TestLedgerSumsToBalance is the reconciliation invariant: balance must always equal sum(ledger).
func TestLedgerSumsToBalance(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	tenant, user := freshTenant(t)

	ops := []struct {
		credit bool
		amount int64
		ref    string
	}{
		{true, 1000, "op-1"},
		{false, 250, "op-2"},
		{true, 500, "op-3"},
		{false, 100, "op-4"},
	}
	for _, op := range ops {
		var err error
		if op.credit {
			err = r.Credit(ctx, tenant, user, op.amount, models.SourceInGame, op.ref)
		} else {
			err = r.Debit(ctx, tenant, user, op.amount, models.SourceInGame, op.ref)
		}
		if err != nil {
			t.Fatalf("op %s failed: %v", op.ref, err)
		}
	}

	account, err := r.GetAccount(ctx, tenant, user)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}

	var ledgerSum int64
	row := testDB.WithContext(ctx).
		Model(&models.LedgerEntry{}).
		Where("tenant_id = ? AND user_id = ?", tenant, user).
		Select("COALESCE(SUM(amount), 0)").
		Row()
	if err := row.Scan(&ledgerSum); err != nil {
		t.Fatalf("ledger sum query failed: %v", err)
	}

	if account.Balance != ledgerSum {
		t.Fatalf("balance (%d) != sum(ledger) (%d)", account.Balance, ledgerSum)
	}
}
