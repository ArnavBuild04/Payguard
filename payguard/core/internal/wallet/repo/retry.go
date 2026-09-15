package repo

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
	"context"
	"math"
	"math/rand"
)

func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "40001" || pgErr.Code == "23505" {
			return true
		}
	}
	return false
}


type retryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration 
	MaxDelay    time.Duration
}

func withRetry(ctx context.Context, cfg retryConfig , fn func() error) error {
      var err error
	  for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		  err = fn()
		  if err == nil {
			  return nil
		  }
		  if !isRetryable(err) {
			  return err
		  }
		  delay := time.Duration(math.Pow(2,float64(attempt))) * cfg.BaseDelay
		  if delay > time.Duration(cfg.MaxDelay) {
			  delay = time.Duration(cfg.MaxDelay)
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