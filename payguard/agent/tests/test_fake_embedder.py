"""FakeEmbedder isn't semantically meaningful the way a real embedding is, but it must at least
be deterministic and put related text closer together than unrelated text -- otherwise cosine
retrieval in tests would be exercising nothing."""

import math

from app.rag.embeddings import FakeEmbedder


def cosine(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(x * x for x in b))
    return dot / (na * nb) if na and nb else 0.0


def test_same_text_gives_identical_vector():
    e = FakeEmbedder(dim=64)
    v1 = e.embed("refund policy for local ahead cases")
    v2 = e.embed("refund policy for local ahead cases")
    assert v1 == v2


def test_shared_vocabulary_is_closer_than_unrelated_text():
    e = FakeEmbedder(dim=256)
    refund_a = e.embed("refund policy reversal compensate local ahead provider")
    refund_b = e.embed("compensate reversal refund local ahead")
    clawback = e.embed("clawback shortfall wallet balance debit collection")

    assert cosine(refund_a, refund_b) > cosine(refund_a, clawback)


def test_vector_is_unit_normalized():
    e = FakeEmbedder(dim=32)
    v = e.embed("some policy text")
    norm = math.sqrt(sum(x * x for x in v))
    assert math.isclose(norm, 1.0, abs_tol=1e-9)
