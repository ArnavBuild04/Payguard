package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ledgerIdempotencyConstraint must match models.LedgerEntry's uniqueIndex name.
const ledgerIdempotencyConstraint = "uq_ledger_idem"

type repoImpl struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) Repo {
	return &repoImpl{db: db}
}

var defaultRetryConfig = retryConfig{
	MaxAttempts: 5,
	BaseDelay:   25 * time.Millisecond,
	MaxDelay:    1 * time.Second,
}

func (r *repoImpl) Debit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error {
	return withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var account models.Account
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND user_id = ?", tenantID, userID).
				First(&account)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return walleterr.ErrAccountNotFound
			} else if result.Error != nil {
				return result.Error
			}

			if account.Balance < amount {
				return walleterr.ErrInsufficientBalance
			}
			account.Balance -= amount
			if result := tx.Save(&account); result.Error != nil {
				return result.Error
			}

			entry := models.LedgerEntry{
				TenantID:     tenantID,
				UserID:       userID,
				Amount:       -amount,
				BalanceAfter: account.Balance,
				Source:       source,
				ReferenceID:  referenceID,
			}
			return createLedgerEntry(tx, &entry)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})
}

func (r *repoImpl) Credit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error {
	return withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// ON CONFLICT DO NOTHING avoids a PK-violation race on a brand-new account's first credit.
			if result := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&models.Account{TenantID: tenantID, UserID: userID, Balance: 0}); result.Error != nil {
				return result.Error
			}

			var account models.Account
			if result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND user_id = ?", tenantID, userID).
				First(&account); result.Error != nil {
				return result.Error
			}

			account.Balance += amount
			if result := tx.Save(&account); result.Error != nil {
				return result.Error
			}

			entry := models.LedgerEntry{
				TenantID:     tenantID,
				UserID:       userID,
				Amount:       amount,
				BalanceAfter: account.Balance,
				Source:       source,
				ReferenceID:  referenceID,
			}
			return createLedgerEntry(tx, &entry)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})
}

func (r *repoImpl) GetAccount(ctx context.Context, tenantID string, userID int64) (*models.Account, error) {
	var account models.Account
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, walleterr.ErrAccountNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &account, nil
}

// createLedgerEntry turns a duplicate-key insert on the idempotency constraint into ErrAlreadyProcessed.
func createLedgerEntry(tx *gorm.DB, entry *models.LedgerEntry) error {
	result := tx.Create(entry)
	if result.Error == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(result.Error, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == ledgerIdempotencyConstraint {
		return walleterr.ErrAlreadyProcessed
	}
	return result.Error
}
