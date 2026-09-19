"""A single, boring psycopg connection helper. No ORM: the agent only ever does a handful of
simple reads/writes (policy_chunks, agent_runs), so raw SQL is clearer than a framework here.
"""

import psycopg

from app.config import settings


def get_connection() -> psycopg.Connection:
    return psycopg.connect(settings.database_dsn)


def vector_literal(values: list[float]) -> str:
    """pgvector accepts a text literal like '[0.1,0.2,0.3]' cast with ::vector -- this avoids
    pulling in the separate pgvector-python adapter package for a single call site."""
    return "[" + ",".join(repr(float(v)) for v in values) + "]"
