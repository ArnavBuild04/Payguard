package service

import (
	"context"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

type Service interface {
	ListOpen(ctx context.Context) ([]models.Case, error)

	GetCase(ctx context.Context, id uint64) (*models.Case, error)

	// Approve resolves an OPEN case via the exact same resolve path RunAutoResolver uses. An empty
	// action uses the reason's sensible default, so `{"actor":"..."}` alone works for most cases.
	Approve(ctx context.Context, id uint64, actor string, action models.Action) error

	// Dismiss closes a case with no money moved; a note is required.
	Dismiss(ctx context.Context, id uint64, actor, note string) error

	// RunAutoResolver drives Lane 1 until ctx is cancelled.
	RunAutoResolver(ctx context.Context, interval time.Duration)
}
