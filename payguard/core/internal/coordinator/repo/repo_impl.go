package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/coordinatorerr"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const transactionOpConstraint = "uq_transaction_op" // (order_id, operation_type)

type repoImpl struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) Repo {
	return &repoImpl{db: db}
}

// Create attempts the insert first. On a 23505 the transaction is already aborted by Postgres, so
// the follow-up lookup deliberately happens OUTSIDE it, against a fresh statement — same hazard as
// payment/repo's Create, same fix.
func (r *repoImpl) Create(ctx context.Context, txn *models.Transaction) (*models.Transaction, error) {
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(txn).Error; err != nil {
				if isUniqueViolation(err, transactionOpConstraint) {
					return coordinatorerr.ErrAlreadyProcessed
				}
				return err
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})

	if errors.Is(txErr, coordinatorerr.ErrAlreadyProcessed) {
		existing, err := r.GetByOrderAndOp(ctx, txn.OrderID, txn.OperationType)
		if err != nil {
			return nil, err
		}
		return existing, coordinatorerr.ErrAlreadyProcessed
	}
	if txErr != nil {
		return nil, txErr
	}
	return txn, nil
}

func (r *repoImpl) GetByOrderAndOp(ctx context.Context, orderID uint64, op models.OperationType) (*models.Transaction, error) {
	var t models.Transaction
	err := r.db.WithContext(ctx).
		Where("order_id = ? AND operation_type = ?", orderID, op).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coordinatorerr.ErrTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *repoImpl) ListSuccessByOrder(ctx context.Context, orderID uint64) ([]models.Transaction, error) {
	var txns []models.Transaction
	err := r.db.WithContext(ctx).
		Where("order_id = ? AND status = ?", orderID, models.StatusSuccess).
		Find(&txns).Error
	if err != nil {
		return nil, err
	}
	return txns, nil
}

// Transition checks CanTransition entirely in Go before issuing any write, so a rejected
// transition never leaves a half-failed statement behind.
func (r *repoImpl) Transition(ctx context.Context, id uint64, to models.Status, lastError string) (*models.Transaction, error) {
	var result *models.Transaction
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var t models.Transaction
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return coordinatorerr.ErrTransactionNotFound
				}
				return err
			}

			if !models.CanTransition(t.Status, to) {
				result = &t
				return coordinatorerr.ErrInvalidTransition
			}

			t.Status = to
			t.LastError = lastError
			t.Attempts++

			if err := tx.Save(&t).Error; err != nil {
				return err
			}
			result = &t
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})

	if txErr != nil {
		return result, txErr
	}
	return result, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
