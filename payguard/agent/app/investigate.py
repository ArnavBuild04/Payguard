"""Wires the graph to concrete dependencies (real or fake, decided once by config) and persists
the result. This is the only place that calls agent_runs_repo.save_run -- the one write this
whole service ever makes, and it writes a recommendation, never an approval.
"""

from app.agent_runs_repo import save_run
from app.agent_graph import build_graph
from app.config import settings
from app.llm.base import LLMClient
from app.rag.embeddings import Embedder, FakeEmbedder, GeminiEmbedder
from app.schemas import InvestigateResponse, RootCauseAnalysis
from app.tools_client import CoreToolsClient


def get_llm_client() -> LLMClient:
    if settings.use_real_llm:
        from app.llm.gemini_client import GeminiLLMClient

        return GeminiLLMClient()
    from app.llm.fake_client import FakeLLMClient

    return FakeLLMClient()


def get_embedder_instance() -> Embedder:
    if settings.use_real_llm:
        return GeminiEmbedder()
    return FakeEmbedder()


class CaseNotFoundError(Exception):
    pass


def investigate_case(case_id: int) -> InvestigateResponse:
    tools = CoreToolsClient()
    try:
        graph = build_graph(tools, get_embedder_instance(), get_llm_client())
        result = graph.invoke({"case_id": case_id})
    finally:
        tools.close()

    if result.get("error"):
        raise CaseNotFoundError(result["error"])

    analysis: RootCauseAnalysis = result["analysis"]
    run_id = save_run(case_id, analysis)
    return InvestigateResponse(case_id=case_id, run_id=run_id, analysis=analysis)
