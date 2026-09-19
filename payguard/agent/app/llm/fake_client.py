"""A deterministic stand-in for Gemini: same input always produces the same output, with no
network call and no API key. This is what tests and the offline demo run against, and it is
also what a fresh clone of this repo runs against by default (GEMINI_API_KEY unset) -- the
whole agent, RAG retrieval included, is exercised end to end without spending a cent.
"""

from app.schemas import PolicyCitation, RootCauseAnalysis


class FakeLLMClient:
    def analyze(self, context: dict) -> RootCauseAnalysis:
        reason = context.get("reason", "")
        ev = context.get("evidence", {}) or {}
        citations = [PolicyCitation(**c) for c in context.get("policy_citations", [])]
        payment_id = context.get("payment_id")

        if reason == "LOCAL_AHEAD":
            conclusion = (
                f"Payment {payment_id} is still {ev.get('local_status')} locally, but the "
                f"provider now authoritatively reports {ev.get('provider_status')!r}. No webhook "
                "for this reversal ever reached us, so local state is stale, not the provider's."
            )
            recommended, confidence = "COMPENSATE", (
                "high" if ev.get("authoritative_terminal") else "medium"
            )
        elif reason == "PROVIDER_AHEAD":
            conclusion = (
                f"Provider terminal state {ev.get('provider_status')!r} for payment {payment_id} "
                f"disagrees with our local {ev.get('local_status')!r}; the provider is authoritative."
            )
            recommended, confidence = "ADVANCE_SUCCEEDED" if ev.get("provider_status") == "succeeded" else "ADVANCE_FAILED", "high"
        elif reason == "UNKNOWN_PROVIDER_STATE":
            conclusion = (
                f"Provider returned status {ev.get('provider_status') or '(empty)'!r} for payment "
                f"{payment_id}, which this version of the core has no mapping for. Needs a human "
                "to confirm what that status means before anything is advanced."
            )
            recommended, confidence = "NOTE_ONLY", "low"
        elif reason == "CLAWBACK_SHORTFALL":
            conclusion = (
                "The wallet balance available for clawback is less than the grant that needs "
                "reversing; only partial recovery is possible without going negative."
            )
            recommended, confidence = "NOTE_ONLY", "medium"
        elif reason == "LEDGER_MISMATCH":
            conclusion = "The ledger's running balance disagrees with the account's stored balance for this tenant/user."
            recommended, confidence = "NOTE_ONLY", "medium"
        else:
            conclusion = f"Reason {reason!r} has no fake-LLM heuristic; deferring to a human with the raw evidence."
            recommended, confidence = "NOTE_ONLY", "low"

        missing = [] if ev.get("provider_payment_id") else ["no provider_payment_id recorded on this case"]

        return RootCauseAnalysis(
            conclusion=conclusion,
            evidence=[f"{k}={v}" for k, v in ev.items() if v not in (None, "", 0, False)],
            confidence=confidence,
            missing_information=missing,
            recommended_action=recommended,
            policy_citations=citations,
        )
