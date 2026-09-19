"""Applies migrations/*.sql in order, idempotently (every statement is CREATE ... IF NOT
EXISTS). Run standalone with `python -m app.migrate`, or it runs automatically on service
startup (see app/main.py) so a fresh clone needs no manual DB setup step beyond docker compose.
"""

from pathlib import Path

from app.db import get_connection

MIGRATIONS_DIR = Path(__file__).resolve().parent.parent / "migrations"


def run_migrations() -> None:
    conn = get_connection()
    try:
        with conn.cursor() as cur:
            for path in sorted(MIGRATIONS_DIR.glob("*.sql")):
                cur.execute(path.read_text())
        conn.commit()
    finally:
        conn.close()


if __name__ == "__main__":
    run_migrations()
    print("migrations applied")
