package detector

import (
	"context"
	"errors"
	"time"

	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"gorm.io/gorm"
)

// missingGrantSLA is the published grace period before a missing grant becomes a case — hld.md §5.3.
const missingGrantSLA = 60 * time.Second

// detectMissingGrantsFromTransactions is pass 2: payments vs transactions, against the SKU catalog.
func (d *Detector) detectMissingGrantsFromTransactions(ctx context.Context) {
	var payments []paymentmodels.Payment
	if err := d.db.WithContext(ctx).Where("status = ?", paymentmodels.StatusSucceeded).Find(&payments).Error; err != nil {
		return
	}

	for _, p := range payments {
		if time.Since(p.UpdatedAt) < missingGrantSLA {
			continue
		}
		for _, op := range grantsForSKU[p.SKU] {
			d.checkExpectedGrant(ctx, p, op)
		}
	}
}

func (d *Detector) checkExpectedGrant(ctx context.Context, p paymentmodels.Payment, op coordinatormodels.OperationType) {
	var txn coordinatormodels.Transaction
	err := d.db.WithContext(ctx).Where("order_id = ? AND operation_type = ?", p.ID, op).First(&txn).Error

	pid := p.ID
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		d.openCase(ctx, &pid, models.ReasonMissingGrant, string(op), models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			OperationType: string(op), GrantsExpected: 1, GrantsPresent: 0,
			AgeSeconds: int64(time.Since(p.UpdatedAt).Seconds()), LastError: "no transaction row exists",
		})
	case err != nil:
		return
	case txn.Status == coordinatormodels.StatusFailed || (txn.Status == coordinatormodels.StatusPending && time.Since(txn.CreatedAt) > missingGrantSLA):
		d.openCase(ctx, &pid, models.ReasonMissingGrant, string(op), models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			OperationType: string(op), GrantsExpected: 1, GrantsPresent: 0,
			AgeSeconds: int64(time.Since(p.UpdatedAt).Seconds()), LastError: txn.LastError,
		})
	}
}
