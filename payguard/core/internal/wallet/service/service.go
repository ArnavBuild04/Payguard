package service

import "github.com/ArnavBuild04/payguard/core/internal/wallet/models"

type Service interface {
	Debit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error
	Credit(tenantID string, userID string, amount int64, source models.Source, referenceID string) error
}