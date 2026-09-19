package detector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

type balanceRow struct {
	TenantID string
	UserID   int64
	Balance  int64
	Sum      int64
}

// detectLedgerMismatch is pass 4: accounts.balance vs SUM(ledger_entries), swept across every
// account. PaymentID is nil here — this is account-scoped, not payment-scoped — so dedup against
// a fast sweep reopening the same case is done in application code, not the DB's partial index.
func (d *Detector) detectLedgerMismatch(ctx context.Context) {
	var rows []balanceRow
	err := d.db.WithContext(ctx).Raw(`
		SELECT a.tenant_id AS tenant_id, a.user_id AS user_id, a.balance AS balance,
		       COALESCE(SUM(l.amount), 0) AS sum
		FROM accounts a
		LEFT JOIN ledger_entries l ON l.tenant_id = a.tenant_id AND l.user_id = a.user_id
		GROUP BY a.tenant_id, a.user_id, a.balance
		HAVING a.balance != COALESCE(SUM(l.amount), 0)
	`).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return
	}

	alreadyOpen, err := d.repo.ListOpenByReason(ctx, models.ReasonLedgerMismatch)
	if err != nil {
		return
	}
	open := make(map[string]bool, len(alreadyOpen))
	for _, c := range alreadyOpen {
		var ev models.Evidence
		if json.Unmarshal([]byte(c.EvidenceJSON), &ev) == nil {
			open[fmt.Sprintf("%s|%d", ev.TenantID, ev.UserID)] = true
		}
	}

	for _, row := range rows {
		key := fmt.Sprintf("%s|%d", row.TenantID, row.UserID)
		if open[key] {
			continue
		}
		d.openCase(ctx, nil, models.ReasonLedgerMismatch, "", models.Evidence{
			TenantID: row.TenantID, UserID: row.UserID,
			LocalStatus: "balance_mismatch",
			LastError:   "accounts.balance does not equal SUM(ledger_entries.amount)",
			AmountMatch: false,
		})
	}
}
