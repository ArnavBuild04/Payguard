package detector

import coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"

// grantsForSKU mirrors coordinator/service's own catalog — pass 2 needs the same shape to know
// what a bundle was supposed to deliver.
var grantsForSKU = map[string][]coordinatormodels.OperationType{
	"BUNDLE_10K":    {coordinatormodels.OpCreditChips},
	"BUNDLE_50K":    {coordinatormodels.OpCreditChips},
	"BUNDLE_TICKET": {coordinatormodels.OpCreditChips, coordinatormodels.OpGrantTicket},
}
