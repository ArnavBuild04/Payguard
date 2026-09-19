package policy_test

import (
	"testing"

	"github.com/ArnavBuild04/payguard/core/internal/policy"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
)

func TestEvaluate_PermitsTheObviousPairs(t *testing.T) {
	e := policy.New(100000)
	cases := []struct {
		reason models.Reason
		action models.Action
	}{
		{models.ReasonProviderAhead, models.ActionAdvanceSucceeded},
		{models.ReasonMissingGrant, models.ActionRedriveGrant},
		{models.ReasonLocalAhead, models.ActionCompensate},
		{models.ReasonLedgerMismatch, models.ActionNoteOnly},
	}
	for _, c := range cases {
		d := e.Evaluate(c.reason, c.action, 100)
		if !d.Permitted {
			t.Errorf("reason=%s action=%s: got denied (%s), want permitted", c.reason, c.action, d.Reason)
		}
	}
}

func TestEvaluate_DeniesNonsensicalPairs(t *testing.T) {
	e := policy.New(100000)
	d := e.Evaluate(models.ReasonLedgerMismatch, models.ActionAdvanceSucceeded, 100)
	if d.Permitted {
		t.Fatal("expected LEDGER_MISMATCH + ADVANCE_SUCCEEDED to be denied")
	}
}

func TestEvaluate_DeniesCompensateOverCeiling(t *testing.T) {
	e := policy.New(1000)
	d := e.Evaluate(models.ReasonLocalAhead, models.ActionCompensate, 1001)
	if d.Permitted {
		t.Fatal("expected a compensate amount over the ceiling to be denied")
	}

	d = e.Evaluate(models.ReasonLocalAhead, models.ActionCompensate, 1000)
	if !d.Permitted {
		t.Fatal("expected a compensate amount at the ceiling to be permitted")
	}
}
