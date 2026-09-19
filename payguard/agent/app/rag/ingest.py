"""Chunks the policy documents in app/policies/*.md, embeds each chunk, and upserts them into
pgvector's policy_chunks table. Run this once (or after editing a policy doc):

    python -m app.rag.ingest
"""

from pathlib import Path

from app.db import get_connection, vector_literal
from app.rag.embeddings import Embedder, get_embedder

POLICIES_DIR = Path(__file__).resolve().parent.parent / "policies"


def chunk_document(text: str, max_chars: int = 500) -> list[str]:
    """Splits on blank lines (paragraphs), then greedily packs paragraphs up to max_chars so a
    chunk is never mid-sentence and is short enough to cite cleanly in the agent's output."""
    paragraphs = [p.strip() for p in text.split("\n\n") if p.strip()]
    chunks: list[str] = []
    current = ""
    for p in paragraphs:
        if current and len(current) + len(p) + 2 > max_chars:
            chunks.append(current)
            current = p
        else:
            current = f"{current}\n\n{p}" if current else p
    if current:
        chunks.append(current)
    return chunks


def ingest_document(conn, embedder: Embedder, doc_path: Path) -> int:
    text = doc_path.read_text()
    chunks = chunk_document(text)
    doc_name = doc_path.stem
    with conn.cursor() as cur:
        cur.execute("DELETE FROM policy_chunks WHERE doc_name = %s", (doc_name,))
        for i, chunk in enumerate(chunks):
            vec = embedder.embed(chunk)
            cur.execute(
                "INSERT INTO policy_chunks (doc_name, chunk_index, content, embedding) "
                "VALUES (%s, %s, %s, %s::vector)",
                (doc_name, i, chunk, vector_literal(vec)),
            )
    conn.commit()
    return len(chunks)


def ingest_all() -> None:
    embedder = get_embedder()
    conn = get_connection()
    try:
        total = 0
        for doc_path in sorted(POLICIES_DIR.glob("*.md")):
            n = ingest_document(conn, embedder, doc_path)
            print(f"ingested {doc_path.name}: {n} chunks")
            total += n
        print(f"done: {total} chunks total")
    finally:
        conn.close()


if __name__ == "__main__":
    ingest_all()
