"""Pluggable embeddings, same reasoning as the LLM client: a real Gemini embedder and a
deterministic fake behind one Protocol, so ingestion and retrieval both run offline in tests.
"""

import hashlib
from typing import Protocol

from app.config import settings


class Embedder(Protocol):
    def embed(self, text: str) -> list[float]: ...


class GeminiEmbedder:
    def __init__(self) -> None:
        from google import genai

        self._client = genai.Client(api_key=settings.gemini_api_key)

    def embed(self, text: str) -> list[float]:
        response = self._client.models.embed_content(
            model=settings.gemini_embedding_model,
            contents=text,
        )
        return list(response.embeddings[0].values)


class FakeEmbedder:
    """Deterministic, content-sensitive, and dependency-free: hashes overlapping shingles of
    the text into a fixed-size vector, then L2-normalizes it. It's not semantically meaningful
    the way a real embedding is, but two chunks about the same topic (sharing words) land closer
    together than two unrelated chunks -- enough to exercise cosine retrieval correctly in tests.
    """

    def __init__(self, dim: int | None = None):
        self.dim = dim or settings.embedding_dim

    def embed(self, text: str) -> list[float]:
        vec = [0.0] * self.dim
        words = text.lower().split()
        for w in words:
            h = int(hashlib.sha256(w.encode()).hexdigest(), 16)
            idx = h % self.dim
            sign = 1.0 if (h // self.dim) % 2 == 0 else -1.0
            vec[idx] += sign

        norm = sum(v * v for v in vec) ** 0.5
        if norm == 0:
            return vec
        return [v / norm for v in vec]


def get_embedder() -> Embedder:
    if settings.use_real_llm:
        return GeminiEmbedder()
    return FakeEmbedder()
