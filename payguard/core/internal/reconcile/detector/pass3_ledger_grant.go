package detector

import (
	"context"
	"fmt"
	"time"

	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	walletmodels "github.com/ArnavBuild04/payguard/core/internal/wallet/models"
)

// detectMissingGrantsFromLedger is pass 3: a transaction row can say SUCCESS while the wallet
// ledger entry it claims to have produced is actually missing (hld.md edge case F4 gone wrong).
func (d *Detector) detectMissingGrantsFromLedger(ctx context.Context) {
	var txns []coordinatormodels.Transaction
	if err := d.db.WithContext(ctx).
		Where("status = ? AND operation_type = ?", coordinatormodels.StatusSuccess, coordinatormodels.OpCreditChips).
		Find(&txns).Error; err != nil {
		return
	}

	for _, txn := range txns {
		refID := fmt.Sprintf("order-%d-credit_chips", txn.OrderID)
		var count int64
		d.db.WithContext(ctx).Model(&walletmodels.LedgerEntry{}).
			Where("tenant_id = ? AND source = ? AND reference_id = ?", txn.TenantID, walletmodels.SourcePurchase, refID).
			Count(&count)
		if count > 0 {
			continue
		}

		// The redrive action needs the SKU, which transactions never stores — look it up from the
		// payment this transaction belongs to. A real transaction row always has one; if it
		// doesn't, that itself is a data-integrity anomaly this pass can't safely act on.
		var p paymentmodels.Payment
		if err := d.db.WithContext(ctx).First(&p, txn.OrderID).Error; err != nil {
			continue
		}

		pid := txn.OrderID
		d.openCase(ctx, &pid, models.ReasonMissingGrant, string(coordinatormodels.OpCreditChips), models.Evidence{
			TenantID: txn.TenantID, UserID: txn.UserID, SKU: p.SKU, AmountMinor: txn.AmountMinor,
			OperationType: string(coordinatormodels.OpCreditChips), GrantsExpected: 1, GrantsPresent: 0,
			AgeSeconds: int64(time.Since(txn.UpdatedAt).Seconds()),
			LastError:  "transaction row is SUCCESS but no matching ledger entry exists",
		})
	}
}
