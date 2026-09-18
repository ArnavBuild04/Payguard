package repo

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
)

type Repo interface {
	Debit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error
	Credit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error
	GetAccount(ctx context.Context, tenantID string, userID int64) (*models.Account, error)
}
