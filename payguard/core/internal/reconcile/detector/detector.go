// Package detector runs the five read-only comparisons that open reconciliation cases.
package detector

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
	"gorm.io/gorm"
)

type Detector struct {
	db       *gorm.DB
	provider provider.Provider
	repo     repo.Repo
	polls    *pollTracker
}

func New(db *gorm.DB, p provider.Provider, r repo.Repo) *Detector {
	return &Detector{db: db, provider: p, repo: r, polls: newPollTracker()}
}

// Run sweeps every pass until ctx is cancelled.
func (d *Detector) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.sweepOnce(ctx)
		}
	}
}

func (d *Detector) sweepOnce(ctx context.Context) {
	d.detectProviderMismatches(ctx)
	d.detectMissingGrantsFromTransactions(ctx)
	d.detectMissingGrantsFromLedger(ctx)
	d.detectLedgerMismatch(ctx)
	d.detectUnmatchedWebhooks(ctx)
}

// openCase is read-only from the detector's own tables' point of view: it never writes anything
// except a reconciliation_cases row, and a case already OPEN for this key is a safe no-op.
func (d *Detector) openCase(ctx context.Context, paymentID *uint64, reason models.Reason, subKey string, ev models.Evidence) *models.Case {
	body, err := json.Marshal(ev)
	if err != nil {
		slog.Error("detector: failed to encode evidence", "reason", reason, "err", err)
		return nil
	}
	c, err := d.repo.CreateCase(ctx, &models.Case{
		PaymentID:    paymentID,
		Reason:       reason,
		SubKey:       subKey,
		Status:       models.StatusOpen,
		EvidenceJSON: string(body),
	})
	if err != nil {
		if errors.Is(err, reconcileerr.ErrAlreadyProcessed) {
			return c
		}
		slog.Error("detector: failed to open case", "reason", reason, "err", err)
		return nil
	}
	slog.Info("detector: case opened", "case_id", c.ID, "reason", reason, "sub_key", subKey)
	return c
}

// pollTracker paces pass 1's provider polling per hld.md's tiered schedule; it is in-memory and
// resets on restart, which only ever makes polling briefly more eager, never wrong.
type pollTracker struct {
	mu   sync.Mutex
	last map[uint64]time.Time
}

func newPollTracker() *pollTracker {
	return &pollTracker{last: make(map[uint64]time.Time)}
}

func (t *pollTracker) due(paymentID uint64, age time.Duration, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	last, ok := t.last[paymentID]
	if ok && now.Sub(last) < tierInterval(age) {
		return false
	}
	t.last[paymentID] = now
	return true
}

func tierInterval(age time.Duration) time.Duration {
	switch {
	case age < time.Minute:
		return 7 * time.Second
	case age < 10*time.Minute:
		return 45 * time.Second
	default:
		return 3 * time.Minute
	}
}
