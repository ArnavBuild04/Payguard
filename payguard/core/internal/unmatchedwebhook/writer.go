// Package unmatchedwebhook records provider events that matched no known payment.
package unmatchedwebhook

import (
	"context"
	"errors"

	"github.com/ArnavBuild04/payguard/core/internal/unmatchedwebhook/models"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const uniqueConstraint = "uq_unmatched_webhook"

// Record inserts a row; a duplicate delivery (same source, event_id) is a no-op.
func Record(ctx context.Context, db *gorm.DB, source, eventID, payloadJSON string) error {
	err := db.WithContext(ctx).Create(&models.UnmatchedWebhook{
		Source:      source,
		EventID:     eventID,
		PayloadJSON: payloadJSON,
	}).Error
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == uniqueConstraint {
		return nil
	}
	return err
}

// ListUncased returns every row not yet matched to a payment and not yet turned into a case.
func ListUncased(ctx context.Context, db *gorm.DB) ([]models.UnmatchedWebhook, error) {
	var rows []models.UnmatchedWebhook
	err := db.WithContext(ctx).Where("matched_payment_id IS NULL AND case_id IS NULL").Find(&rows).Error
	return rows, err
}

// MarkMatched links a row to the payment it turned out to belong to.
func MarkMatched(ctx context.Context, db *gorm.DB, id, paymentID uint64) error {
	return db.WithContext(ctx).Model(&models.UnmatchedWebhook{}).Where("id = ?", id).
		Update("matched_payment_id", paymentID).Error
}

// MarkCased records which reconciliation case a row produced, so it is not cased again.
func MarkCased(ctx context.Context, db *gorm.DB, id, caseID uint64) error {
	return db.WithContext(ctx).Model(&models.UnmatchedWebhook{}).Where("id = ?", id).
		Update("case_id", caseID).Error
}
