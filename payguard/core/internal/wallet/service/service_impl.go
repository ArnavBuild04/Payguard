package service

import (
	"context"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
)

type serviceImpl struct {
	Db repo.Repo
}

func NewService(repo repo.Repo) Service {
	return &serviceImpl{
		Db: repo,
	}
}

func (s *serviceImpl) Debit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error {
	if amount <= 0 {
		return walleterr.ErrInvalidAmount
	}
	return s.Db.Debit(ctx, tenantID, userID, amount, source, referenceID)
}

func (s *serviceImpl) Credit(ctx context.Context, tenantID string, userID int64, amount int64, source models.Source, referenceID string) error {
	if amount <= 0 {
		return walleterr.ErrInvalidAmount
	}
	return s.Db.Credit(ctx, tenantID, userID, amount, source, referenceID)
}

func (s *serviceImpl) GetAccount(ctx context.Context, tenantID string, userID int64) (*models.Account, error) {
	return s.Db.GetAccount(ctx, tenantID, userID)
}
