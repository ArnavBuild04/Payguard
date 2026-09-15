package service

import (
	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
)


type serviceImpl struct{
    Db repo.Repo
}


func NewService(repo repo.Repo) Service {
	return &serviceImpl{
		 Db: repo,
	}
}

func (s *serviceImpl) Debit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error {
	
	err := s.Db.Debit(tenantID, userID, amount, source, referenceID)
	if err != nil {
		return err
	}
	return nil
}

func (s *serviceImpl) Credit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error {
	
	err := s.Db.Credit(tenantID, userID, amount, source, referenceID)
	if err != nil {
		return err
	}
	return nil
}