package repo

import (
	"errors"
	"time"

	"context"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"gorm.io/gorm"

	"database/sql"

	"gorm.io/gorm/clause"
)

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

func (r *repoImpl) Debit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error {
	return withRetry(context.Background(), defaultRetryConfig, func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			var account models.Account
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND user_id = ?", tenantID, userID).
				First(&account)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return errors.New("account not found")
			} else if result.Error != nil {
				return result.Error
			}

			if account.Balance < amount {
				return errors.New("insufficient balance")
			}
			account.Balance -= amount
			result = tx.Save(&account)
			if result.Error != nil {
				return result.Error
			}

			entry := models.LedgerEntry{
				TenantID:    tenantID,
				UserID:      userID,
				Amount:      -amount,
				Source:      source,
				ReferenceID: referenceID,
			}
			result = tx.Create(&entry)
			if result.Error != nil {
				return result.Error
			}

			return nil
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	})
}

func (r *repoImpl) Credit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error {
	return withRetry(context.Background(), defaultRetryConfig, func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			var account models.Account
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND user_id = ?", tenantID, userID).
				First(&account)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				// If the account does not exist, create a new one with the initial balance
				account = models.Account{
					TenantID: tenantID,
					UserID:   userID,
					Balance:  0,
				}
				result = tx.Create(&account)
				if result.Error != nil {
					return result.Error
				}
			} else if result.Error != nil {
				return result.Error
			}

			account.Balance += amount
			result = tx.Save(&account)
			if result.Error != nil {
				return result.Error
			}

			entry := models.LedgerEntry{
				TenantID:    tenantID,
				UserID:      userID,
				Amount:      amount,
				Source:      source,
				ReferenceID: referenceID,
			}
			result = tx.Create(&entry)
			if result.Error != nil {
				return result.Error
			}

			return nil
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	})

}
