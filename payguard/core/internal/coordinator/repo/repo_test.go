package repo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/coordinatorerr"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
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
			dbErr = testDB.AutoMigrate(&models.Transaction{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return repo.NewRepo(testDB)
}

func freshOrderID(t *testing.T) uint64 {
	t.Helper()
	n := atomic.AddInt64(&idSeq, 1)
	return uint64(time.Now().UnixNano()) + uint64(n)
}

func TestCreate_Success(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	txn, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpCreditChips, Status: models.StatusPending,
		TenantID: "t1", UserID: 42, AmountMinor: 999,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if txn.Status != models.StatusPending {
		t.Fatalf("status = %v, want PENDING", txn.Status)
	}
}

func TestCreate_DuplicateOrderAndOp_ReturnsExisting(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	first, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpCreditChips, Status: models.StatusPending,
		TenantID: "t1", UserID: 42, AmountMinor: 999,
	})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	second, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpCreditChips, Status: models.StatusPending,
		TenantID: "t1", UserID: 42, AmountMinor: 999,
	})
	if !errors.Is(err, coordinatorerr.ErrAlreadyProcessed) {
		t.Fatalf("err = %v, want ErrAlreadyProcessed", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second.ID = %d, want %d", second.ID, first.ID)
	}
}

func TestTransition_HappyPath_PendingToSuccess(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	txn, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpGrantTicket, Status: models.StatusPending,
		TenantID: "t1", UserID: 42,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	updated, err := r.Transition(ctx, txn.ID, models.StatusSuccess, "")
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	if updated.Status != models.StatusSuccess {
		t.Fatalf("status = %v, want SUCCESS", updated.Status)
	}
}

// TestTransition_NoCompensationFromFailed asserts FAILED → REVERSE_PENDING is rejected.
func TestTransition_NoCompensationFromFailed(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	txn, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpGrantTicket, Status: models.StatusPending,
		TenantID: "t1", UserID: 42,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := r.Transition(ctx, txn.ID, models.StatusFailed, "grant service down"); err != nil {
		t.Fatalf("transition to FAILED failed: %v", err)
	}

	if _, err := r.Transition(ctx, txn.ID, models.StatusReversePending, ""); !errors.Is(err, coordinatorerr.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestListSuccessByOrder_OnlyReturnsSuccessRows(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	chips, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpCreditChips, Status: models.StatusPending,
		TenantID: "t1", UserID: 42, AmountMinor: 999,
	})
	if err != nil {
		t.Fatalf("create chips failed: %v", err)
	}
	ticket, err := r.Create(ctx, &models.Transaction{
		OrderID: orderID, OperationType: models.OpGrantTicket, Status: models.StatusPending,
		TenantID: "t1", UserID: 42,
	})
	if err != nil {
		t.Fatalf("create ticket failed: %v", err)
	}

	if _, err := r.Transition(ctx, chips.ID, models.StatusSuccess, ""); err != nil {
		t.Fatalf("transition chips failed: %v", err)
	}
	if _, err := r.Transition(ctx, ticket.ID, models.StatusFailed, "ticket service down"); err != nil {
		t.Fatalf("transition ticket failed: %v", err)
	}

	successRows, err := r.ListSuccessByOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(successRows) != 1 {
		t.Fatalf("len(successRows) = %d, want 1", len(successRows))
	}
	if successRows[0].OperationType != models.OpCreditChips {
		t.Fatalf("successRows[0].OperationType = %v, want CREDIT_CHIPS", successRows[0].OperationType)
	}
}

// TestConcurrentCreate_SameOrderAndOp asserts exactly one of 50 concurrent creates wins.
func TestConcurrentCreate_SameOrderAndOp(t *testing.T) {
	r := getRepo(t)
	ctx := context.Background()
	orderID := freshOrderID(t)

	const n = 50
	var wg sync.WaitGroup
	var succeeded, alreadyProcessed int64
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			txn, err := r.Create(ctx, &models.Transaction{
				OrderID: orderID, OperationType: models.OpCreditChips, Status: models.StatusPending,
				TenantID: "t1", UserID: 42, AmountMinor: 999,
			})
			switch {
			case err == nil:
				atomic.AddInt64(&succeeded, 1)
				ids[i] = txn.ID
			case errors.Is(err, coordinatorerr.ErrAlreadyProcessed):
				atomic.AddInt64(&alreadyProcessed, 1)
				ids[i] = txn.ID
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
			t.Fatalf("goroutine %d got transaction id %d, want %d", i, id, first)
		}
	}
}
