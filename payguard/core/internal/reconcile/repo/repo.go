package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

type Repo interface {
	// CreateCase returns the existing OPEN row plus reconcileerr.ErrAlreadyProcessed on a replay.
	CreateCase(ctx context.Context, c *models.Case) (*models.Case, error)

	GetCase(ctx context.Context, id uint64) (*models.Case, error)

	ListOpen(ctx context.Context) ([]models.Case, error)

	ListOpenByReason(ctx context.Context, reason models.Reason) ([]models.Case, error)

	// CountOtherOpenCases excludes both the case itself and any sibling case of the same reason —
	// two independent MISSING_GRANT cases for different operation types on one payment must be
	// able to resolve independently (hld.md §6.3's partial-bundle design), so they must never
	// count as blocking each other.
	CountOtherOpenCases(ctx context.Context, paymentID uint64, excludeCaseID uint64, sameReason models.Reason) (int64, error)

	IncrementAttempt(ctx context.Context, id uint64) error

	// MarkResolved returns reconcileerr.ErrCaseNotOpen if the case isn't currently OPEN.
	MarkResolved(ctx context.Context, id uint64, action models.Action, resolution, resolvedBy string) (*models.Case, error)

	// MarkDismissed returns reconcileerr.ErrCaseNotOpen if the case isn't currently OPEN.
	MarkDismissed(ctx context.Context, id uint64, resolvedBy, note string) (*models.Case, error)
}
