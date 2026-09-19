"""Exercises the deterministic FakeLLMClient directly -- no network, no DB."""

from app.llm.fake_client import FakeLLMClient


def test_local_ahead_recommends_compensate():
    llm = FakeLLMClient()
    analysis = llm.analyze(
        {
            "reason": "LOCAL_AHEAD",
            "payment_id": 42,
            "evidence": {
                "local_status": "SUCCEEDED",
                "provider_status": "refunded",
                "authoritative_terminal": True,
                "provider_payment_id": "prov-1",
            },
        }
    )
    assert analysis.recommended_action == "COMPENSATE"
    assert analysis.confidence == "high"
    assert "42" in analysis.conclusion


def test_unknown_provider_state_is_always_note_only_and_low_confidence():
    llm = FakeLLMClient()
    analysis = llm.analyze(
        {
            "reason": "UNKNOWN_PROVIDER_STATE",
            "payment_id": 7,
            "evidence": {"provider_status": "under_review", "provider_payment_id": "prov-2"},
        }
    )
    assert analysis.recommended_action == "NOTE_ONLY"
    assert analysis.confidence == "low"


def test_missing_provider_payment_id_is_flagged_as_missing_information():
    llm = FakeLLMClient()
    analysis = llm.analyze({"reason": "LEDGER_MISMATCH", "payment_id": 1, "evidence": {}})
    assert any("provider_payment_id" in m for m in analysis.missing_information)


def test_never_produces_an_approve_or_execute_field():
    """Structural guarantee check: RootCauseAnalysis has no field that could be mistaken for
    an authorization to act -- confirms the schema itself enforces recommend-only behavior."""
    from app.schemas import RootCauseAnalysis

    fields = set(RootCauseAnalysis.model_fields.keys())
    assert "approved" not in fields
    assert "executed" not in fields
    assert "action_taken" not in fields
