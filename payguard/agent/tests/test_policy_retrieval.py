"""Integration test against the real Postgres/pgvector instance -- skipped if it's unreachable,
same convention as the Go test suite's DB-dependent tests. Uses the fake embedder so it never
makes a network call, but exercises the real HNSW cosine-similarity query end to end.
"""

import psycopg
import pytest

from app.config import settings
from app.rag.embeddings import FakeEmbedder
from app.rag.retrieve import retrieve


def _db_available() -> bool:
    try:
        psycopg.connect(settings.database_dsn, connect_timeout=2).close()
        return True
    except Exception:
        return False


pytestmark = pytest.mark.skipif(not _db_available(), reason="postgres unavailable")


def test_retrieve_returns_relevant_chunks_for_refund_query():
    citations = retrieve(FakeEmbedder(), "provider reports payment refunded but local record still succeeded")
    assert len(citations) > 0
    assert all(0.0 <= c.similarity <= 1.0 for c in citations)


def test_retrieve_respects_top_k():
    citations = retrieve(FakeEmbedder(), "unknown provider status", top_k=1)
    assert len(citations) == 1
