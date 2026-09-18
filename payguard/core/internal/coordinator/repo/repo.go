package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
)

type Repo interface {
	// Create inserts a new PENDING transaction row. On a UNIQUE(order_id, operation_type) replay it
	// returns the EXISTING row alongside coordinatorerr.ErrAlreadyProcessed — the caller decides
	// whether that existing row still needs work (still PENDING) or is already done.
	Create(ctx context.Context, txn *models.Transaction) (*models.Transaction, error)

	GetByOrderAndOp(ctx context.Context, orderID uint64, op models.OperationType) (*models.Transaction, error)

	// ListSuccessByOrder returns every SUCCESS row for an order — what Compensate walks to reverse
	// a refunded/charged-back purchase (hld.md §5.6).
	ListSuccessByOrder(ctx context.Context, orderID uint64) ([]models.Transaction, error)

	// Transition moves a transaction to `to`. If CanTransition rejects the move, it returns the
	// CURRENT row unchanged alongside coordinatorerr.ErrInvalidTransition.
	Transition(ctx context.Context, id uint64, to models.Status, lastError string) (*models.Transaction, error)
}
