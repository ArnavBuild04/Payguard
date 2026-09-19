package detector

import (
	"context"
	"errors"
	"time"

	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

const (
	stuckThreshold     = 2 * time.Minute // provider 404s past this age: STUCK_PROCESSING
	exhaustedThreshold = 5 * time.Minute // our own polling keeps failing past this age: STUCK_PROCESSING
	recentTerminalScan = 24 * time.Hour  // window for re-checking already-terminal payments (LOCAL_AHEAD)
)

// detectProviderMismatches is pass 1: payments vs the provider's own record of them.
func (d *Detector) detectProviderMismatches(ctx context.Context) {
	now := time.Now().UTC()

	var processing []paymentmodels.Payment
	if err := d.db.WithContext(ctx).Where("status = ?", paymentmodels.StatusProcessing).Find(&processing).Error; err != nil {
		return
	}
	for _, p := range processing {
		age := now.Sub(p.CreatedAt)
		if !d.polls.due(p.ID, age, now) {
			continue
		}
		d.checkProcessingPayment(ctx, p, age)
	}

	var recentTerminal []paymentmodels.Payment
	if err := d.db.WithContext(ctx).
		Where("status IN ? AND updated_at > ? AND provider_payment_id IS NOT NULL", []paymentmodels.Status{paymentmodels.StatusSucceeded}, now.Add(-recentTerminalScan)).
		Find(&recentTerminal).Error; err != nil {
		return
	}
	for _, p := range recentTerminal {
		if !d.polls.due(p.ID, recentTerminalScan, now) {
			continue
		}
		d.checkSucceededPayment(ctx, p)
	}
}

func (d *Detector) checkProcessingPayment(ctx context.Context, p paymentmodels.Payment, age time.Duration) {
	pid := p.ID
	if p.ProviderPaymentID == nil {
		if age > exhaustedThreshold {
			d.openCase(ctx, &pid, models.ReasonStuckProcessing, "", models.Evidence{
				TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
				LocalStatus: string(p.Status), AgeSeconds: int64(age.Seconds()),
				LastError: "no provider reference recorded",
			})
		}
		return
	}

	result, err := d.provider.GetPayment(ctx, *p.ProviderPaymentID)
	switch {
	case err == nil:
		d.compareProcessingToProvider(ctx, p, result)

	case errors.Is(err, providererr.ErrNotFound):
		if age > stuckThreshold {
			d.openCase(ctx, &pid, models.ReasonStuckProcessing, "", models.Evidence{
				TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
				LocalStatus: string(p.Status), ProviderPaymentID: *p.ProviderPaymentID,
				AgeSeconds: int64(age.Seconds()), LastError: "provider has no record of this payment",
			})
		}

	case errors.Is(err, providererr.ErrUnknownStatus):
		raw := ""
		var use *providererr.UnknownStatusError
		if errors.As(err, &use) {
			raw = use.Raw
		}
		d.openCase(ctx, &pid, models.ReasonUnknownProviderState, "", models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			LocalStatus: string(p.Status), ProviderPaymentID: *p.ProviderPaymentID,
			ProviderStatus: raw, AgeSeconds: int64(age.Seconds()),
		})

	default:
		if age > exhaustedThreshold {
			d.openCase(ctx, &pid, models.ReasonStuckProcessing, "", models.Evidence{
				TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
				LocalStatus: string(p.Status), ProviderPaymentID: *p.ProviderPaymentID,
				AgeSeconds: int64(age.Seconds()), LastError: err.Error(),
			})
		}
	}
}

func (d *Detector) compareProcessingToProvider(ctx context.Context, p paymentmodels.Payment, result *provider.Payment) {
	pid := p.ID
	switch result.Status {
	case provider.StatusSucceeded:
		d.openCase(ctx, &pid, models.ReasonProviderAhead, "", models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			LocalStatus: string(p.Status), ProviderStatus: string(result.Status),
			ProviderPaymentID: result.ID, AuthoritativeTerminal: true,
			AmountMatch: result.AmountMinor == p.AmountMinor,
		})
	case provider.StatusFailed:
		d.openCase(ctx, &pid, models.ReasonProviderAhead, "", models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			LocalStatus: string(p.Status), ProviderStatus: string(result.Status),
			ProviderPaymentID: result.ID, AuthoritativeTerminal: true, AmountMatch: true,
		})
	default:
		// Still processing at the provider too — no discrepancy yet.
	}
}

func (d *Detector) checkSucceededPayment(ctx context.Context, p paymentmodels.Payment) {
	result, err := d.provider.GetPayment(ctx, *p.ProviderPaymentID)
	if err != nil {
		return
	}
	if result.Status == provider.StatusRefunded || result.Status == provider.StatusFailed {
		pid := p.ID
		d.openCase(ctx, &pid, models.ReasonLocalAhead, "", models.Evidence{
			TenantID: p.TenantID, UserID: p.UserID, SKU: p.SKU, AmountMinor: p.AmountMinor,
			LocalStatus: string(p.Status), ProviderStatus: string(result.Status),
			ProviderPaymentID: result.ID, AuthoritativeTerminal: true,
		})
	}
}
