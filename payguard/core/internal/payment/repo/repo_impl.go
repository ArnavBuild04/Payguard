package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/outbox"
	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	paymentIdemConstraint       = "uq_payment_idem"        // (tenant_id, idempotency_key)
	paymentProviderIDConstraint = "uq_payment_provider_id" // provider_payment_id
	paymentEventConstraint      = "uq_payment_event"       // (source, event_id)
)

type repoImpl struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) Repo {
	return &repoImpl{db: db}
}

// Create attempts the insert first. On a 23505 against paymentIdemConstraint, the transaction is
// already aborted by Postgres, so the follow-up lookup deliberately happens OUTSIDE it, against a
// fresh statement — querying inside an aborted transaction would itself error.
func (r *repoImpl) Create(ctx context.Context, p *models.Payment) (*models.Payment, error) {
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(p).Error; err != nil {
				if isUniqueViolation(err, paymentIdemConstraint) {
					return paymenterr.ErrAlreadyProcessed
				}
				return err
			}
			payload := newPaymentCreatedPayload(p)
			return outbox.Write(tx, "payment", formatID(p.ID), "PAYMENT_CREATED", payload)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})

	if errors.Is(txErr, paymenterr.ErrAlreadyProcessed) {
		existing, err := r.getByIdempotencyKey(ctx, p.TenantID, p.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing.RequestFingerprint != p.RequestFingerprint {
			return nil, paymenterr.ErrFingerprintMismatch
		}
		return existing, paymenterr.ErrAlreadyProcessed
	}
	if txErr != nil {
		return nil, txErr
	}
	return p, nil
}

func (r *repoImpl) getByIdempotencyKey(ctx context.Context, tenantID, idemKey string) (*models.Payment, error) {
	var p models.Payment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", tenantID, idemKey).
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, paymenterr.ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *repoImpl) GetByID(ctx context.Context, id uint64) (*models.Payment, error) {
	var p models.Payment
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, paymenterr.ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Transition checks CanTransition entirely in Go before issuing any write, so a rejected
// transition never leaves a half-failed statement behind — unlike Create, there is no aborted-
// transaction hazard here.
func (r *repoImpl) Transition(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) (*models.Payment, error) {
	var result *models.Payment
	txErr := withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var p models.Payment
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return paymenterr.ErrPaymentNotFound
				}
				return err
			}

			if !models.CanTransition(p.Status, to) {
				result = &p
				return paymenterr.ErrInvalidTransition
			}

			p.Status = to
			if providerPaymentID != "" {
				p.ProviderPaymentID = &providerPaymentID
			}
			if providerRawStatus != "" {
				p.ProviderStatus = &providerRawStatus
			}
			now := time.Now().UTC()
			p.LastAttemptAt = &now
			p.Attempts++

			if err := tx.Save(&p).Error; err != nil {
				if isUniqueViolation(err, paymentProviderIDConstraint) {
					return paymenterr.ErrAlreadyProcessed
				}
				return err
			}

			payload := newPaymentTransitionPayload(&p)
			if err := outbox.Write(tx, "payment", formatID(p.ID), "PAYMENT_"+string(to), payload); err != nil {
				return err
			}
			result = &p
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})

	if txErr != nil {
		return result, txErr
	}
	return result, nil
}

func (r *repoImpl) SetProviderRef(ctx context.Context, id uint64, providerPaymentID, providerRawStatus string) error {
	return withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&models.Payment{}).Where("id = ?", id).Updates(map[string]any{
				"provider_payment_id": providerPaymentID,
				"provider_status":     providerRawStatus,
				"attempts":            gorm.Expr("attempts + 1"),
				"last_attempt_at":     time.Now().UTC(),
			})
			if res.Error != nil {
				if isUniqueViolation(res.Error, paymentProviderIDConstraint) {
					return paymenterr.ErrAlreadyProcessed
				}
				return res.Error
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})
}

func (r *repoImpl) IncrementAttempt(ctx context.Context, id uint64) error {
	return withRetry(ctx, defaultRetryConfig, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.Model(&models.Payment{}).Where("id = ?", id).Updates(map[string]any{
				"attempts":        gorm.Expr("attempts + 1"),
				"last_attempt_at": time.Now().UTC(),
			}).Error
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	})
}

func (r *repoImpl) RecordEvent(ctx context.Context, evt *models.PaymentEvent) error {
	return withRetry(ctx, defaultRetryConfig, func() error {
		err := r.db.WithContext(ctx).Create(evt).Error
		if err == nil {
			return nil
		}
		if isUniqueViolation(err, paymentEventConstraint) {
			return paymenterr.ErrAlreadyProcessed
		}
		return err
	})
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
