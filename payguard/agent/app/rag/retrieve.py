"""Cosine-similarity retrieval over policy_chunks. pgvector's `<=>` operator is cosine
*distance*, so similarity = 1 - distance; we ask Postgres to do the ranking (it has the HNSW
index) rather than pulling every row back and sorting in Python.
"""

from app.config import settings
from app.db import get_connection, vector_literal
from app.rag.embeddings import Embedder
from app.schemas import PolicyCitation


def retrieve(embedder: Embedder, query: str, top_k: int | None = None) -> list[PolicyCitation]:
    top_k = top_k or settings.rag_top_k
    query_vec = vector_literal(embedder.embed(query))

    conn = get_connection()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT doc_name, chunk_index, content, 1 - (embedding <=> %s::vector) AS similarity "
                "FROM policy_chunks ORDER BY embedding <=> %s::vector LIMIT %s",
                (query_vec, query_vec, top_k),
            )
            rows = cur.fetchall()
    finally:
        conn.close()

    return [
        PolicyCitation(doc_name=r[0], chunk_index=r[1], content=r[2], similarity=float(r[3]))
        for r in rows
    ]
