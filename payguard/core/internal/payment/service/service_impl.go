package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/cache"
	"github.com/ArnavBuild04/payguard/core/internal/payment/models"
	"github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	"github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

// paymentCacheTTL is short; nothing that mutates money reads from this cache.
const paymentCacheTTL = 2 * time.Second

type serviceImpl struct {
	repo       repo.Repo
	provider   provider.Provider
	webhookURL string
	cache      *cache.Client
}

func NewService(r repo.Repo, p provider.Provider, webhookURL string, paymentCache *cache.Client) Service {
	return &serviceImpl{repo: r, provider: p, webhookURL: webhookURL, cache: paymentCache}
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
		return created, err
	}

	// The client never waits on the provider call; the response is 202 before this runs.
	go s.confirmWithProvider(context.Background(), created)

	return created, nil
}

func (s *serviceImpl) GetPayment(ctx context.Context, id uint64) (*models.Payment, error) {
	if cached, ok := s.cache.GetPaymentJSON(ctx, id); ok {
		var p models.Payment
		if err := json.Unmarshal([]byte(cached), &p); err == nil {
			return &p, nil
		}
		// A corrupt cache entry is just a miss — fall through to the database.
	}

	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if body, err := json.Marshal(p); err == nil {
		s.cache.SetPaymentJSON(ctx, id, string(body), paymentCacheTTL)
	}
	return p, nil
}

// confirmWithProvider never marks FAILED on ambiguity; only an authoritative provider failure does.
func (s *serviceImpl) confirmWithProvider(ctx context.Context, p *models.Payment) {
	providerIdemKey := fmt.Sprintf("payguard_pay_%d", p.ID)
	result, err := s.provider.CreatePayment(ctx, provider.CreateRequest{
		IdempotencyKey: providerIdemKey,
		AmountMinor:    p.AmountMinor,
		Currency:       p.Currency,
		WebhookURL:     s.webhookURL,
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
		_ = s.AdvanceFromProvider(ctx, p.ID, models.StatusSucceeded, result.ID, result.RawStatus)
	case provider.StatusFailed:
		_ = s.AdvanceFromProvider(ctx, p.ID, models.StatusFailed, result.ID, result.RawStatus)
	default:
		// Still processing; attach the reference without changing status.
		if err := s.repo.SetProviderRef(ctx, p.ID, result.ID, result.RawStatus); err != nil && !errors.Is(err, paymenterr.ErrAlreadyProcessed) {
			slog.Error("payment: failed to set provider ref", "payment_id", p.ID, "err", err)
		}
		s.cache.InvalidatePayment(ctx, p.ID)
	}
}

// webhookTransitions maps a provider event type to the status it asks us to move to.
var webhookTransitions = map[string]models.Status{
	"payment.succeeded": models.StatusSucceeded,
	"payment.failed":    models.StatusFailed,
	"payment.refunded":  models.StatusRefundPending,
}

func (s *serviceImpl) HandleWebhook(ctx context.Context, source, eventID, eventType, providerPaymentID, rawStatus, payloadJSON string) error {
	payment, err := s.repo.GetByProviderPaymentID(ctx, providerPaymentID)
	if err != nil {
		if errors.Is(err, paymenterr.ErrPaymentNotFound) {
			slog.Warn("payment: webhook for unknown provider payment id", "provider_payment_id", providerPaymentID, "event_type", eventType)
			if recErr := s.repo.RecordUnmatchedWebhook(ctx, source, eventID, payloadJSON); recErr != nil {
				slog.Error("payment: failed to record unmatched webhook", "err", recErr)
			}
			return nil
		}
		return fmt.Errorf("payment: look up webhook target: %w", err)
	}

	recordErr := s.repo.RecordEvent(ctx, &models.PaymentEvent{
		PaymentID:   payment.ID,
		Source:      source,
		EventID:     eventID,
		EventType:   eventType,
		PayloadJSON: payloadJSON,
	})
	if recordErr != nil {
		if errors.Is(recordErr, paymenterr.ErrAlreadyProcessed) {
			// Duplicate delivery, no-op.
			slog.Info("payment: webhook replayed", "payment_id", payment.ID, "event_id", eventID)
			return nil
		}
		return fmt.Errorf("payment: record webhook event: %w", recordErr)
	}

	to, known := webhookTransitions[eventType]
	if !known {
		slog.Info("payment: webhook event type not acted on", "payment_id", payment.ID, "event_type", eventType)
		return nil
	}

	_, tErr := s.repo.Transition(ctx, payment.ID, to, providerPaymentID, rawStatus)
	s.cache.InvalidatePayment(ctx, payment.ID)
	switch {
	case tErr == nil:
		slog.Info("payment: transitioned via webhook", "payment_id", payment.ID, "to", to)
		return nil
	case errors.Is(tErr, paymenterr.ErrInvalidTransition):
		// Out-of-order or already applied via polling, safe no-op.
		slog.Info("payment: webhook transition rejected as out-of-order", "payment_id", payment.ID, "to", to)
		return nil
	default:
		return fmt.Errorf("payment: apply webhook transition: %w", tErr)
	}
}

func (s *serviceImpl) MarkRefunded(ctx context.Context, id uint64) error {
	_, err := s.repo.Transition(ctx, id, models.StatusRefunded, "", "")
	s.cache.InvalidatePayment(ctx, id)
	if errors.Is(err, paymenterr.ErrInvalidTransition) {
		// Already REFUNDED (redelivered dispatch) — safe no-op.
		return nil
	}
	return err
}

func (s *serviceImpl) AdvanceFromProvider(ctx context.Context, id uint64, to models.Status, providerPaymentID, providerRawStatus string) error {
	_, err := s.repo.Transition(ctx, id, to, providerPaymentID, providerRawStatus)
	s.cache.InvalidatePayment(ctx, id)
	switch {
	case err == nil:
		slog.Info("payment: transitioned", "payment_id", id, "to", to)
		return nil
	case errors.Is(err, paymenterr.ErrInvalidTransition):
		// Out-of-order or duplicate — expected under at-least-once delivery, not a bug.
		slog.Info("payment: transition rejected as out-of-order", "payment_id", id, "to", to)
		return nil
	case errors.Is(err, paymenterr.ErrAlreadyProcessed):
		slog.Info("payment: provider reference already recorded", "payment_id", id)
		return nil
	default:
		slog.Error("payment: transition failed", "payment_id", id, "to", to, "err", err)
		return err
	}
}
