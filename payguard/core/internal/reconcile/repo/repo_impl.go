package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const openCaseConstraint = "uq_open_case"

// Migrate creates the partial unique index AutoMigrate cannot express from struct tags: at most one
// OPEN case per (payment_id, reason, sub_key). NULL payment_id rows (LEDGER_MISMATCH) are exempt —
// Postgres treats NULLs as distinct — so those reasons are deduped by the detector itself.
func Migrate(db *gorm.DB) error {
	return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + openCaseConstraint + ` ON reconciliation_cases (payment_id, reason, sub_key) WHERE status = 'OPEN'`).Error
}

type repoImpl struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) Repo {
	return &repoImpl{db: db}
}

// CreateCase's follow-up lookup runs outside the failed transaction, which Postgres has already aborted.
func (r *repoImpl) CreateCase(ctx context.Context, c *models.Case) (*models.Case, error) {
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(c).Error; err != nil {
				if isUniqueViolation(err, openCaseConstraint) {
					return reconcileerr.ErrAlreadyProcessed
				}
				return err
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})

	if errors.Is(txErr, reconcileerr.ErrAlreadyProcessed) {
		existing, err := r.getOpenByKey(ctx, c.PaymentID, c.Reason, c.SubKey)
		if err != nil {
			return nil, err
		}
		return existing, reconcileerr.ErrAlreadyProcessed
	}
	if txErr != nil {
		return nil, txErr
	}
	return c, nil
}

func (r *repoImpl) getOpenByKey(ctx context.Context, paymentID *uint64, reason models.Reason, subKey string) (*models.Case, error) {
	var c models.Case
	q := r.db.WithContext(ctx).Where("reason = ? AND sub_key = ? AND status = ?", reason, subKey, models.StatusOpen)
	if paymentID != nil {
		q = q.Where("payment_id = ?", *paymentID)
	} else {
		q = q.Where("payment_id IS NULL")
	}
	err := q.First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, reconcileerr.ErrCaseNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repoImpl) GetCase(ctx context.Context, id uint64) (*models.Case, error) {
	var c models.Case
	err := r.db.WithContext(ctx).First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, reconcileerr.ErrCaseNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repoImpl) ListOpen(ctx context.Context) ([]models.Case, error) {
	var cases []models.Case
	err := r.db.WithContext(ctx).Where("status = ?", models.StatusOpen).Order("id").Find(&cases).Error
	return cases, err
}

func (r *repoImpl) ListOpenByReason(ctx context.Context, reason models.Reason) ([]models.Case, error) {
	var cases []models.Case
	err := r.db.WithContext(ctx).Where("status = ? AND reason = ?", models.StatusOpen, reason).Order("id").Find(&cases).Error
	return cases, err
}

func (r *repoImpl) CountOtherOpenCases(ctx context.Context, paymentID uint64, excludeCaseID uint64, sameReason models.Reason) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Case{}).
		Where("payment_id = ? AND status = ? AND id != ? AND reason != ?", paymentID, models.StatusOpen, excludeCaseID, sameReason).
		Count(&count).Error
	return count, err
}

func (r *repoImpl) IncrementAttempt(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&models.Case{}).Where("id = ?", id).
		Update("attempts", gorm.Expr("attempts + 1")).Error
}

// MarkResolved checks CanTransition entirely in Go before issuing any write.
func (r *repoImpl) MarkResolved(ctx context.Context, id uint64, action models.Action, resolution, resolvedBy string) (*models.Case, error) {
	return r.finalize(ctx, id, models.StatusResolved, string(action), resolution, resolvedBy)
}

func (r *repoImpl) MarkDismissed(ctx context.Context, id uint64, resolvedBy, note string) (*models.Case, error) {
	return r.finalize(ctx, id, models.StatusDismissed, "", note, resolvedBy)
}

func (r *repoImpl) finalize(ctx context.Context, id uint64, to models.Status, action, resolution, resolvedBy string) (*models.Case, error) {
	var result *models.Case
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var c models.Case
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return reconcileerr.ErrCaseNotFound
				}
				return err
			}
			if c.Status != models.StatusOpen {
				return reconcileerr.ErrCaseNotOpen
			}

			now := time.Now().UTC()
			c.Status = to
			c.Action = action
			c.Resolution = resolution
			c.ResolvedBy = resolvedBy
			c.ResolvedAt = &now

			if err := tx.Save(&c).Error; err != nil {
				return err
			}
			result = &c
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})
	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
