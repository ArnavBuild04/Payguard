// Package policy is the deterministic Go gate on every reconciliation resolution. Per hld.md
// Phase E: an LLM may propose and cite a resolution, but only this package decides permitted or
// denied — the model never gets a vote on whether money is allowed to move.
package policy

import (
	"fmt"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

// permittedActions is which actions make sense for which reason — a sanity boundary independent
// of the Lane 1/Lane 2 split. Approving a LEDGER_MISMATCH case with ADVANCE_SUCCEEDED, for
// instance, is nonsensical regardless of who or what proposed it.
var permittedActions = map[models.Reason]map[models.Action]bool{
	models.ReasonProviderAhead:        {models.ActionAdvanceSucceeded: true, models.ActionAdvanceFailed: true, models.ActionNoteOnly: true},
	models.ReasonMissingGrant:         {models.ActionRedriveGrant: true, models.ActionNoteOnly: true},
	models.ReasonLocalAhead:           {models.ActionCompensate: true, models.ActionNoteOnly: true},
	models.ReasonStuckProcessing:      {models.ActionAdvanceSucceeded: true, models.ActionAdvanceFailed: true, models.ActionNoteOnly: true},
	models.ReasonUnknownProviderState: {models.ActionAdvanceSucceeded: true, models.ActionAdvanceFailed: true, models.ActionNoteOnly: true},
	models.ReasonLedgerMismatch:       {models.ActionNoteOnly: true},
	models.ReasonClawbackShortfall:    {models.ActionNoteOnly: true},
	models.ReasonUnmatchedWebhook:     {models.ActionNoteOnly: true},
}

type Decision struct {
	Permitted bool
	Reason    string
}

type Engine struct {
	maxCompensateMinor int64
}

func New(maxCompensateMinor int64) *Engine {
	return &Engine{maxCompensateMinor: maxCompensateMinor}
}

// Evaluate is pure and side-effect-free: same inputs, same answer, forever.
func (e *Engine) Evaluate(reason models.Reason, action models.Action, amountMinor int64) Decision {
	allowed, ok := permittedActions[reason]
	if !ok || !allowed[action] {
		return Decision{Permitted: false, Reason: fmt.Sprintf("action %s is not a permitted resolution for reason %s", action, reason)}
	}
	if action == models.ActionCompensate && amountMinor > e.maxCompensateMinor {
		return Decision{Permitted: false, Reason: fmt.Sprintf("compensate amount %d exceeds the policy ceiling %d", amountMinor, e.maxCompensateMinor)}
	}
	return Decision{Permitted: true}
}
