package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	coordinatorservice "github.com/ArnavBuild04/payguard/core/internal/coordinator/service"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	paymentservice "github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
)

type serviceImpl struct {
	repo           repo.Repo
	paymentSvc     paymentservice.Service
	coordinatorSvc coordinatorservice.Service

	autoResolveCeiling int64
	maxAutoAttempts    int
}

func NewService(r repo.Repo, paymentSvc paymentservice.Service, coordinatorSvc coordinatorservice.Service, autoResolveCeiling int64, maxAutoAttempts int) Service {
	return &serviceImpl{
		repo: r, paymentSvc: paymentSvc, coordinatorSvc: coordinatorSvc,
		autoResolveCeiling: autoResolveCeiling, maxAutoAttempts: maxAutoAttempts,
	}
}

func (s *serviceImpl) ListOpen(ctx context.Context) ([]models.Case, error) {
	return s.repo.ListOpen(ctx)
}

func (s *serviceImpl) GetCase(ctx context.Context, id uint64) (*models.Case, error) {
	return s.repo.GetCase(ctx, id)
}

func (s *serviceImpl) Approve(ctx context.Context, id uint64, actor string, action models.Action) error {
	c, err := s.repo.GetCase(ctx, id)
	if err != nil {
		return err
	}
	if c.Status != models.StatusOpen {
		return reconcileerr.ErrCaseNotOpen
	}

	var ev models.Evidence
	_ = json.Unmarshal([]byte(c.EvidenceJSON), &ev)
	if action == "" {
		action = defaultAction(c.Reason, ev)
	}
	return s.resolve(ctx, c, actor, action)
}

func (s *serviceImpl) Dismiss(ctx context.Context, id uint64, actor, note string) error {
	if note == "" {
		return reconcileerr.ErrNoteRequired
	}
	_, err := s.repo.MarkDismissed(ctx, id, actor, note)
	return err
}

func (s *serviceImpl) RunAutoResolver(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.autoResolveOnce(ctx)
		}
	}
}

func (s *serviceImpl) autoResolveOnce(ctx context.Context) {
	for reason := range models.Lane1Reasons {
		cases, err := s.repo.ListOpenByReason(ctx, reason)
		if err != nil {
			continue
		}
		for i := range cases {
			c := cases[i]
			var ev models.Evidence
			_ = json.Unmarshal([]byte(c.EvidenceJSON), &ev)
			if !s.passesGate(ctx, &c, ev) {
				continue
			}
			action := defaultAction(c.Reason, ev)
			if err := s.repo.IncrementAttempt(ctx, c.ID); err != nil {
				slog.Error("reconcile: failed to record auto-resolve attempt", "case_id", c.ID, "err", err)
			}
			if err := s.resolve(ctx, &c, "system", action); err != nil {
				slog.Error("reconcile: auto-resolve failed", "case_id", c.ID, "reason", c.Reason, "err", err)
			}
		}
	}
}

// passesGate is Lane 1 only — every failing check drops the case to a human, which is what
// Approve is for; a human calling Approve never goes through this gate.
func (s *serviceImpl) passesGate(ctx context.Context, c *models.Case, ev models.Evidence) bool {
	if !models.Lane1Reasons[c.Reason] {
		return false
	}
	switch c.Reason {
	case models.ReasonProviderAhead:
		if !ev.AuthoritativeTerminal || !ev.AmountMatch {
			return false
		}
	case models.ReasonMissingGrant:
		// Redriving a grant is always additive; no further evidence check needed.
	default:
		return false
	}
	if ev.AmountMinor > s.autoResolveCeiling {
		return false
	}
	if c.Attempts >= s.maxAutoAttempts {
		return false
	}
	if c.PaymentID != nil {
		n, err := s.repo.CountOtherOpenCases(ctx, *c.PaymentID, c.ID, c.Reason)
		if err != nil || n > 0 {
			return false
		}
	}
	return true
}

// defaultAction is what Approve uses when the human doesn't specify one, and what
// RunAutoResolver always uses for Lane 1 — the single deterministic action per reason.
func defaultAction(reason models.Reason, ev models.Evidence) models.Action {
	switch reason {
	case models.ReasonProviderAhead:
		if ev.ProviderStatus == string(provider.StatusFailed) {
			return models.ActionAdvanceFailed
		}
		return models.ActionAdvanceSucceeded
	case models.ReasonMissingGrant:
		return models.ActionRedriveGrant
	case models.ReasonLocalAhead:
		return models.ActionCompensate
	default:
		return models.ActionNoteOnly
	}
}

// resolve is the one function both lanes call — the auto-loop calls it after the gate passes;
// Approve calls it directly, a human's judgment standing in for the gate.
func (s *serviceImpl) resolve(ctx context.Context, c *models.Case, resolvedBy string, action models.Action) error {
	var ev models.Evidence
	_ = json.Unmarshal([]byte(c.EvidenceJSON), &ev)

	var opErr error
	switch action {
	case models.ActionAdvanceSucceeded:
		opErr = s.requirePaymentID(c, func(id uint64) error {
			return s.paymentSvc.AdvanceFromProvider(ctx, id, paymentmodels.StatusSucceeded, ev.ProviderPaymentID, ev.ProviderStatus)
		})
	case models.ActionAdvanceFailed:
		opErr = s.requirePaymentID(c, func(id uint64) error {
			return s.paymentSvc.AdvanceFromProvider(ctx, id, paymentmodels.StatusFailed, ev.ProviderPaymentID, ev.ProviderStatus)
		})
	case models.ActionRedriveGrant:
		opErr = s.requirePaymentID(c, func(id uint64) error {
			return s.coordinatorSvc.RetryGrant(ctx, id, coordinatormodels.OperationType(ev.OperationType), ev.TenantID, ev.UserID, ev.AmountMinor)
		})
	case models.ActionCompensate:
		opErr = s.requirePaymentID(c, func(id uint64) error {
			if err := s.coordinatorSvc.Compensate(ctx, id); err != nil {
				return err
			}
			// A LOCAL_AHEAD case found by the detector (no webhook ever arrived) leaves the
			// payment at SUCCEEDED, not REFUND_PENDING; MarkRefunded alone can't reach REFUNDED
			// from there. This is a no-op if a webhook already advanced it — CanTransition
			// rejects REFUND_PENDING -> REFUND_PENDING safely.
			if err := s.paymentSvc.AdvanceFromProvider(ctx, id, paymentmodels.StatusRefundPending, ev.ProviderPaymentID, ev.ProviderStatus); err != nil {
				return err
			}
			return s.paymentSvc.MarkRefunded(ctx, id)
		})
	case models.ActionNoteOnly:
		// No money movement — approving this case just records the decision.
	default:
		return reconcileerr.ErrUnknownAction
	}
	if opErr != nil {
		return fmt.Errorf("reconcile: resolve action %s failed: %w", action, opErr)
	}

	_, err := s.repo.MarkResolved(ctx, c.ID, action, fmt.Sprintf("applied %s", action), resolvedBy)
	return err
}

func (s *serviceImpl) requirePaymentID(c *models.Case, fn func(id uint64) error) error {
	if c.PaymentID == nil {
		return fmt.Errorf("reconcile: action requires a payment id, case %d has none", c.ID)
	}
	return fn(*c.PaymentID)
}
