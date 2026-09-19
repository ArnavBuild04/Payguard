"""End-to-end: real Go core (its /internal/tools/* endpoints), real Postgres/pgvector, fake LLM
and fake embedder. Skipped if the core isn't running -- same convention as the Go suite's
DB-dependent tests, since this needs a live process on :8080, not just a live database.
"""

import httpx
import pytest

from app.config import settings
from app.investigate import investigate_case


def _core_available() -> bool:
    try:
        httpx.get(f"{settings.core_base_url}/health", timeout=2)
        return True
    except Exception:
        return False


pytestmark = pytest.mark.skipif(not _core_available(), reason="payguard-core not running on :8080")


def test_investigate_produces_a_recommendation_never_an_approval():
    # Any OPEN LEDGER_MISMATCH case works for this check; the fixture-seeding path a real
    # deployment uses is the detector itself, not this test.
    conn_cases = httpx.get(f"{settings.core_base_url}/v1/reconciliation-cases").json()
    open_cases = [c for c in conn_cases if c["status"] == "OPEN"]
    if not open_cases:
        pytest.skip("no open cases available to investigate")

    case_id = open_cases[0]["id"]
    response = investigate_case(case_id)

    assert response.case_id == case_id
    assert response.run_id > 0
    assert response.analysis.confidence in {"low", "medium", "high"}
    assert not hasattr(response.analysis, "approved")

    # The case itself must still be OPEN after investigation -- the agent never touches
    # /v1/reconciliation-cases/{id}/approve.
    after = httpx.get(f"{settings.core_base_url}/v1/reconciliation-cases/{case_id}").json()
    assert after["status"] == "OPEN"
