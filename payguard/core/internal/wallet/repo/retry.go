package repo

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// isRetryable matches 40001 only; under READ COMMITTED a single-row FOR UPDATE just blocks and
// re-reads instead of erroring, so this mainly guards future multi-row transactions (Phase B).
func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001"
	}
	return false
}

type retryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func withRetry(ctx context.Context, cfg retryConfig, fn func() error) error {
	var err error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}

		delay := time.Duration(math.Pow(2, float64(attempt))) * cfg.BaseDelay
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		jitter := time.Duration(rand.Int63n(int64(delay)))

		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
