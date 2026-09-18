package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	"github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

type serviceImpl struct {
	repo     repo.Repo
	provider provider.Provider
}

func NewService(r repo.Repo, p provider.Provider) Service {
	return &serviceImpl{repo: r, provider: p}
}

func (s *serviceImpl) CreatePayment(ctx context.Context, tenantID string, userID int64, sku, idempotencyKey string) (*models.Payment, error) {
	if idempotencyKey == "" {
		return nil, paymenterr.ErrMissingIdempotencyKey
	}
	def, ok := lookupSKU(sku)
	if !ok {
		return nil, paymenterr.ErrInvalidSKU
	}

	p := &models.Payment{
		TenantID:           tenantID,
		UserID:             userID,
		SKU:                sku,
		AmountMinor:        def.AmountMinor,
		Currency:           def.Currency,
		Status:             models.StatusProcessing,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint(userID, sku, def.AmountMinor),
	}

	created, err := s.repo.Create(ctx, p)
	if err != nil {
		// ErrAlreadyProcessed still carries the original row — the client gets it back, just not a
		// new charge. Everything else (ErrFingerprintMismatch, a genuine DB error) is a real error.
		return created, err
	}

	// The client never waits on this — see hld.md §5.1: the response is 202 before the provider is
	// ever called. A detached context because the request's own context may be cancelled the
	// moment the HTTP handler returns.
	go s.confirmWithProvider(context.Background(), created)

	return created, nil
}

func (s *serviceImpl) GetPayment(ctx context.Context, id uint64) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id)
}

// confirmWithProvider makes the one synchronous-from-our-side call to the provider and applies
// whatever it honestly says. A timeout or unrecognized status leaves the payment PROCESSING and
// only records that we tried — hld.md's single most important rule: PROCESSING → FAILED happens
// only on an authoritative provider failure.
func (s *serviceImpl) confirmWithProvider(ctx context.Context, p *models.Payment) {
	providerIdemKey := fmt.Sprintf("payguard_pay_%d", p.ID)
	result, err := s.provider.CreatePayment(ctx, provider.CreateRequest{
		IdempotencyKey: providerIdemKey,
		AmountMinor:    p.AmountMinor,
		Currency:       p.Currency,
	})
	if err != nil {
		if incErr := s.repo.IncrementAttempt(ctx, p.ID); incErr != nil {
			slog.Error("payment: failed to record provider attempt", "payment_id", p.ID, "err", incErr)
		}
		outcome := "unknown"
		if errors.Is(err, providererr.ErrUnknownStatus) {
			outcome = "unknown_status"
		}
		slog.Warn("payment: provider outcome ambiguous, staying PROCESSING", "payment_id", p.ID, "reason", outcome, "err", err)
		return
	}

	switch result.Status {
	case provider.StatusSucceeded:
		s.applyTransition(ctx, p.ID, models.StatusSucceeded, result.ID, result.RawStatus)
	case provider.StatusFailed:
		s.applyTransition(ctx, p.ID, models.StatusFailed, result.ID, result.RawStatus)
	default:
		// Still processing (or a non-terminal status we don't otherwise act on) — attach the
		// reference so a later poll or retry can find this payment at the provider, but do not
		// change status: nothing terminal has happened yet.
		if err := s.repo.SetProviderRef(ctx, p.ID, result.ID, result.RawStatus); err != nil && !errors.Is(err, paymenterr.ErrAlreadyProcessed) {
			slog.Error("payment: failed to set provider ref", "payment_id", p.ID, "err", err)
		}
	}
}

func (s *serviceImpl) applyTransition(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) {
	_, err := s.repo.Transition(ctx, id, to, providerPaymentID, providerRawStatus)
	switch {
	case err == nil:
		slog.Info("payment: transitioned", "payment_id", id, "to", to)
	case errors.Is(err, paymenterr.ErrInvalidTransition):
		// Out-of-order or duplicate — expected under at-least-once delivery, not a bug.
		slog.Info("payment: transition rejected as out-of-order", "payment_id", id, "to", to)
	case errors.Is(err, paymenterr.ErrAlreadyProcessed):
		slog.Info("payment: provider reference already recorded", "payment_id", id)
	default:
		slog.Error("payment: transition failed", "payment_id", id, "to", to, "err", err)
	}
}
