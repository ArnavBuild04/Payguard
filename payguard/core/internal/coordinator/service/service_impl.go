package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/coordinator/coordinatorerr"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/granter"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	"github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
	walletmodels "github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	walletsvc "github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
)

type serviceImpl struct {
	repo          repo.Repo
	wallet        walletsvc.Service
	assetGranter  granter.AssetGranter
	ticketGranter granter.TicketGranter
}

func NewService(r repo.Repo, wallet walletsvc.Service, assetGranter granter.AssetGranter, ticketGranter granter.TicketGranter) Service {
	return &serviceImpl{repo: r, wallet: wallet, assetGranter: assetGranter, ticketGranter: ticketGranter}
}

func (s *serviceImpl) HandlePaymentSucceeded(ctx context.Context, orderID uint64, tenantID string, userID int64, sku string, amountMinor int64) error {
	ops := grantsFor(sku)
	if len(ops) == 0 {
		return fmt.Errorf("coordinator: no grants configured for sku %q", sku)
	}

	// Per-grant rows, processed independently — hld.md §6.3: one grant failing must never block or
	// hide the others, so every op runs regardless of an earlier one's outcome.
	var firstErr error
	for _, op := range ops {
		if err := s.processGrant(ctx, orderID, tenantID, userID, op, amountMinor); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *serviceImpl) processGrant(ctx context.Context, orderID uint64, tenantID string, userID int64, op models.OperationType, amountMinor int64) error {
	txnAmount := int64(0)
	if op == models.OpCreditChips {
		txnAmount = amountMinor
	}

	txn := &models.Transaction{
		OrderID:       orderID,
		OperationType: op,
		Status:        models.StatusPending,
		TenantID:      tenantID,
		UserID:        userID,
		AmountMinor:   txnAmount,
	}
	created, err := s.repo.Create(ctx, txn)
	switch {
	case err == nil:
		txn = created
	case errors.Is(err, coordinatorerr.ErrAlreadyProcessed):
		if created.Status != models.StatusPending {
			// Terminal already (SUCCESS or FAILED from an earlier delivery) — a redelivered
			// fan-out is a no-op, exactly like hld.md edge case F1.
			slog.Info("coordinator: grant already processed", "order_id", orderID, "op", op, "status", created.Status)
			return nil
		}
		// Still PENDING from a run that never finished (e.g. a crash mid-fan-out) — retry it. The
		// downstream call's own idempotency (F4/F5) makes this safe even if it actually landed.
		txn = created
	default:
		return fmt.Errorf("coordinator: create transaction: %w", err)
	}

	grantErr := callWithRetry(ctx, func() error {
		return s.performGrant(ctx, op, tenantID, userID, orderID, amountMinor)
	})

	if grantErr != nil {
		if _, tErr := s.repo.Transition(ctx, txn.ID, models.StatusFailed, grantErr.Error()); tErr != nil && !errors.Is(tErr, coordinatorerr.ErrInvalidTransition) {
			slog.Error("coordinator: failed to record grant failure", "order_id", orderID, "op", op, "err", tErr)
		}
		slog.Error("coordinator: grant failed", "order_id", orderID, "op", op, "err", grantErr)
		return grantErr
	}

	if _, tErr := s.repo.Transition(ctx, txn.ID, models.StatusSuccess, ""); tErr != nil && !errors.Is(tErr, coordinatorerr.ErrInvalidTransition) {
		slog.Error("coordinator: failed to record grant success", "order_id", orderID, "op", op, "err", tErr)
		return tErr
	}
	slog.Info("coordinator: grant succeeded", "order_id", orderID, "op", op)
	return nil
}

func (s *serviceImpl) performGrant(ctx context.Context, op models.OperationType, tenantID string, userID int64, orderID uint64, amountMinor int64) error {
	switch op {
	case models.OpCreditChips:
		referenceID := fmt.Sprintf("order-%d-credit_chips", orderID)
		err := s.wallet.Credit(ctx, tenantID, userID, amountMinor, walletmodels.SourcePurchase, referenceID)
		if errors.Is(err, walleterr.ErrAlreadyProcessed) {
			return nil
		}
		return err
	case models.OpGrantAsset:
		return s.assetGranter.Grant(ctx, tenantID, userID, orderID)
	case models.OpGrantTicket:
		return s.ticketGranter.Grant(ctx, tenantID, userID, orderID)
	default:
		return fmt.Errorf("coordinator: unsupported operation type %q", op)
	}
}

func (s *serviceImpl) Compensate(ctx context.Context, orderID uint64) error {
	successRows, err := s.repo.ListSuccessByOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("coordinator: list success transactions: %w", err)
	}

	var firstErr error
	for _, txn := range successRows {
		if err := s.reverseOne(ctx, txn); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *serviceImpl) reverseOne(ctx context.Context, txn models.Transaction) error {
	reversed, err := s.repo.Transition(ctx, txn.ID, models.StatusReversePending, "")
	if err != nil {
		if errors.Is(err, coordinatorerr.ErrInvalidTransition) {
			// Already reversed, or being reversed by a concurrent call — safe no-op.
			return nil
		}
		return fmt.Errorf("coordinator: mark reverse-pending: %w", err)
	}

	if err := s.performReversal(ctx, reversed); err != nil {
		if errors.Is(err, coordinatorerr.ErrClawbackShortfall) {
			// Left at REVERSE_PENDING deliberately — hld.md §5.6: the player already spent the
			// chips, balance never goes negative, and this is a business decision for a human.
			slog.Warn("coordinator: clawback shortfall, left REVERSE_PENDING for a human", "order_id", txn.OrderID, "op", txn.OperationType)
		} else {
			slog.Error("coordinator: reversal failed", "order_id", txn.OrderID, "op", txn.OperationType, "err", err)
		}
		return err
	}

	if _, tErr := s.repo.Transition(ctx, txn.ID, models.StatusReversed, ""); tErr != nil && !errors.Is(tErr, coordinatorerr.ErrInvalidTransition) {
		return tErr
	}
	return nil
}

func (s *serviceImpl) performReversal(ctx context.Context, txn *models.Transaction) error {
	switch txn.OperationType {
	case models.OpCreditChips:
		referenceID := fmt.Sprintf("order-%d-refund_chips", txn.OrderID)
		err := s.wallet.Debit(ctx, txn.TenantID, txn.UserID, txn.AmountMinor, walletmodels.SourcePurchase, referenceID)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, walleterr.ErrAlreadyProcessed):
			return nil
		case errors.Is(err, walleterr.ErrInsufficientBalance):
			return coordinatorerr.ErrClawbackShortfall
		default:
			return err
		}
	case models.OpGrantAsset:
		return s.assetGranter.Revoke(ctx, txn.TenantID, txn.UserID, txn.OrderID)
	case models.OpGrantTicket:
		return s.ticketGranter.Revoke(ctx, txn.TenantID, txn.UserID, txn.OrderID)
	default:
		return fmt.Errorf("coordinator: unsupported operation type %q", txn.OperationType)
	}
}

// callWithRetry is a small bounded retry, full jitter, matching §4.7's convention (no backoff
// library). hld.md edge case F3: a grant service down gets bounded retries, then FAILED + case.
func callWithRetry(ctx context.Context, fn func() error) error {
	const maxAttempts = 3
	base := 50 * time.Millisecond

	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == maxAttempts {
			break
		}

		delay := time.Duration(1<<attempt) * base
		jitter := time.Duration(rand.Int63n(int64(delay)))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
