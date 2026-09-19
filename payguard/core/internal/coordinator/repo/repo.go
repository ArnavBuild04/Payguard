package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
)

type Repo interface {
	// Create returns the existing row alongside coordinatorerr.ErrAlreadyProcessed on a replay.
	Create(ctx context.Context, txn *models.Transaction) (*models.Transaction, error)

	GetByOrderAndOp(ctx context.Context, orderID uint64, op models.OperationType) (*models.Transaction, error)

	ListSuccessByOrder(ctx context.Context, orderID uint64) ([]models.Transaction, error)

	// Transition returns the current row unchanged plus ErrInvalidTransition if CanTransition rejects the move.
	Transition(ctx context.Context, id uint64, to models.Status, lastError string) (*models.Transaction, error)
}
