"""The LangGraph investigation flow. Four steps, always run in this order, always read-only
until the very last write (agent_runs, in investigate.py after this graph returns):

    gather_case -> gather_evidence -> retrieve_policy -> analyze

Nothing in this graph calls /v1/reconciliation-cases/{id}/approve, or anything else that could
move money or change a payment's status. That is a structural guarantee, not a runtime check:
the only client this module holds is CoreToolsClient, whose every method is a GET.
"""

import json
from typing import TypedDict

from langgraph.graph import END, START, StateGraph

from app.llm.base import LLMClient
from app.rag.embeddings import Embedder
from app.rag.retrieve import retrieve
from app.schemas import RootCauseAnalysis
from app.tools_client import CoreToolsClient


class InvestigateState(TypedDict, total=False):
    case_id: int
    case: dict | None
    evidence: dict
    payment: dict | None
    provider_payment: dict | None
    wallet_balance: dict | None
    policy_citations: list[dict]
    analysis: RootCauseAnalysis | None
    error: str | None


def build_graph(tools: CoreToolsClient, embedder: Embedder, llm: LLMClient):
    def gather_case(state: InvestigateState) -> dict:
        case = tools.get_case(state["case_id"])
        if case is None:
            return {"case": None, "error": f"case {state['case_id']} not found"}
        evidence = json.loads(case.get("evidence_json") or "{}")
        return {"case": case, "evidence": evidence}

    def gather_evidence(state: InvestigateState) -> dict:
        if state.get("error"):
            return {}
        evidence = state["evidence"]
        payment_id = state["case"].get("payment_id")
        payment = tools.get_payment(payment_id) if payment_id else None
        provider_payment = tools.get_provider_payment(evidence.get("provider_payment_id", ""))
        wallet_balance = None
        if evidence.get("tenant_id") and evidence.get("user_id"):
            wallet_balance = tools.get_wallet_balance(evidence["tenant_id"], evidence["user_id"])
        return {"payment": payment, "provider_payment": provider_payment, "wallet_balance": wallet_balance}

    def retrieve_policy(state: InvestigateState) -> dict:
        if state.get("error"):
            return {}
        reason = state["case"]["reason"]
        query = f"{reason} {json.dumps(state['evidence'])}"
        citations = retrieve(embedder, query)
        return {"policy_citations": [c.model_dump() for c in citations]}

    def analyze(state: InvestigateState) -> dict:
        if state.get("error"):
            return {
                "analysis": RootCauseAnalysis(
                    conclusion=state["error"],
                    confidence="low",
                    missing_information=[state["error"]],
                )
            }
        context = {
            "case_id": state["case_id"],
            "payment_id": state["case"].get("payment_id"),
            "reason": state["case"]["reason"],
            "evidence": state["evidence"],
            "payment": state.get("payment"),
            "provider_payment": state.get("provider_payment"),
            "wallet_balance": state.get("wallet_balance"),
            "policy_citations": state.get("policy_citations", []),
        }
        analysis = llm.analyze(context)
        return {"analysis": analysis}

    graph = StateGraph(InvestigateState)
    graph.add_node("gather_case", gather_case)
    graph.add_node("gather_evidence", gather_evidence)
    graph.add_node("retrieve_policy", retrieve_policy)
    graph.add_node("analyze", analyze)

    graph.add_edge(START, "gather_case")
    graph.add_edge("gather_case", "gather_evidence")
    graph.add_edge("gather_evidence", "retrieve_policy")
    graph.add_edge("retrieve_policy", "analyze")
    graph.add_edge("analyze", END)

    return graph.compile()
