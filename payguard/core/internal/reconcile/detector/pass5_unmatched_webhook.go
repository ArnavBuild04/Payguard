package detector

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/unmatchedwebhook"
)

// detectUnmatchedWebhooks is pass 5: a provider event naming a payment we have no record of.
// This webhook may be the only evidence a paying customer exists — a row here is never discarded.
func (d *Detector) detectUnmatchedWebhooks(ctx context.Context) {
	rows, err := unmatchedwebhook.ListUncased(ctx, d.db)
	if err != nil {
		return
	}

	for _, row := range rows {
		c := d.openCase(ctx, nil, models.ReasonUnmatchedWebhook, "", models.Evidence{
			LocalStatus: "no matching payment",
			LastError:   row.PayloadJSON,
		})
		if c != nil {
			_ = unmatchedwebhook.MarkCased(ctx, d.db, row.ID, c.ID)
		}
	}
}
