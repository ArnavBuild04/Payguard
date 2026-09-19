import json

from app.db import get_connection
from app.schemas import RootCauseAnalysis


def save_run(case_id: int, analysis: RootCauseAnalysis) -> int:
    conn = get_connection()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "INSERT INTO agent_runs "
                "(case_id, conclusion, confidence, recommended_action, evidence_json, "
                " missing_information_json, policy_citations_json) "
                "VALUES (%s, %s, %s, %s, %s, %s, %s) RETURNING id",
                (
                    case_id,
                    analysis.conclusion,
                    analysis.confidence,
                    analysis.recommended_action,
                    json.dumps(analysis.evidence),
                    json.dumps(analysis.missing_information),
                    json.dumps([c.model_dump() for c in analysis.policy_citations]),
                ),
            )
            run_id = cur.fetchone()[0]
        conn.commit()
        return run_id
    finally:
        conn.close()


def get_latest_run(case_id: int) -> dict | None:
    conn = get_connection()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT id, case_id, conclusion, confidence, recommended_action, "
                "evidence_json, missing_information_json, policy_citations_json, created_at "
                "FROM agent_runs WHERE case_id = %s ORDER BY created_at DESC LIMIT 1",
                (case_id,),
            )
            row = cur.fetchone()
    finally:
        conn.close()

    if row is None:
        return None
    return {
        "id": row[0],
        "case_id": row[1],
        "conclusion": row[2],
        "confidence": row[3],
        "recommended_action": row[4],
        "evidence": json.loads(row[5]),
        "missing_information": json.loads(row[6]),
        "policy_citations": json.loads(row[7]),
        "created_at": row[8].isoformat(),
    }


def list_latest_runs_by_case() -> dict[int, dict]:
    """One row per case_id (its most recent run) -- what the approval UI's list view needs."""
    conn = get_connection()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT DISTINCT ON (case_id) id, case_id, conclusion, confidence, "
                "recommended_action, evidence_json, missing_information_json, "
                "policy_citations_json, created_at "
                "FROM agent_runs ORDER BY case_id, created_at DESC"
            )
            rows = cur.fetchall()
    finally:
        conn.close()

    result = {}
    for row in rows:
        result[row[1]] = {
            "id": row[0],
            "case_id": row[1],
            "conclusion": row[2],
            "confidence": row[3],
            "recommended_action": row[4],
            "evidence": json.loads(row[5]),
            "missing_information": json.loads(row[6]),
            "policy_citations": json.loads(row[7]),
            "created_at": row[8].isoformat(),
        }
    return result
