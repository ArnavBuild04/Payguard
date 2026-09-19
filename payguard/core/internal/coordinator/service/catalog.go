package service

import "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"

// grantsForSKU maps a SKU to its bundle of grants.
var grantsForSKU = map[string][]models.OperationType{
	"BUNDLE_10K":    {models.OpCreditChips},
	"BUNDLE_50K":    {models.OpCreditChips},
	"BUNDLE_TICKET": {models.OpCreditChips, models.OpGrantTicket},
}

func grantsFor(sku string) []models.OperationType {
	return grantsForSKU[sku]
}
