"""The agent's entire public surface is two endpoints: POST /cases/{id}/investigate (writes a
recommendation to agent_runs) and GET /cases/{id}/recommendation (reads the latest one back for
the approval UI). Neither can approve or reject a case -- that stays on the Go core's
/v1/reconciliation-cases/{id}/approve, called directly by a human clicking a button.
"""

from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware

from app.agent_runs_repo import get_latest_run, list_latest_runs_by_case
from app.investigate import CaseNotFoundError, investigate_case
from app.migrate import run_migrations


@asynccontextmanager
async def lifespan(_: FastAPI):
    run_migrations()
    yield


app = FastAPI(title="PayGuard Agent", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["GET", "POST"],
    allow_headers=["*"],
)


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


@app.post("/cases/{case_id}/investigate")
def investigate(case_id: int) -> dict:
    try:
        response = investigate_case(case_id)
    except CaseNotFoundError as e:
        raise HTTPException(status_code=404, detail=str(e))
    return response.model_dump()


@app.get("/cases/{case_id}/recommendation")
def recommendation(case_id: int) -> dict:
    run = get_latest_run(case_id)
    if run is None:
        raise HTTPException(status_code=404, detail="no recommendation for this case yet")
    return run


@app.get("/cases/recommendations")
def all_recommendations() -> dict:
    """Keyed by case_id -- the approval UI's list view fetches this once and joins it against
    the Go core's GET /v1/reconciliation-cases in the browser."""
    return list_latest_runs_by_case()
