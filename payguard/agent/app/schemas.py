"""Every shape the agent produces or consumes lives here. RootCauseAnalysis is the one thing
the LLM is allowed to output -- it never contains a field like "approved" or "execute", because
the LLM has no authority to move money. A human (via /v1/reconciliation-cases/{id}/approve) or
Go's own deterministic policy.Engine is what decides that, always.
"""

from typing import Literal

from pydantic import BaseModel, Field


class PolicyCitation(BaseModel):
    doc_name: str
    chunk_index: int
    content: str
    similarity: float


class RootCauseAnalysis(BaseModel):
    conclusion: str
    evidence: list[str] = Field(default_factory=list)
    confidence: Literal["low", "medium", "high"]
    missing_information: list[str] = Field(default_factory=list)
    recommended_action: str = ""
    policy_citations: list[PolicyCitation] = Field(default_factory=list)


class InvestigateResponse(BaseModel):
    case_id: int
    run_id: int
    analysis: RootCauseAnalysis
