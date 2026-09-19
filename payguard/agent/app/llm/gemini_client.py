"""The real LLM client. Only imported/used when GEMINI_API_KEY is set (see app/llm/__init__
usage in agent_graph.py); everything else in the agent is written against the LLMClient
Protocol so this file can be swapped for FakeLLMClient without touching any other module.
"""

import json

from google import genai
from google.genai import types

from app.config import settings
from app.schemas import RootCauseAnalysis


class GeminiLLMClient:
    def __init__(self) -> None:
        self._client = genai.Client(api_key=settings.gemini_api_key)

    def analyze(self, context: dict) -> RootCauseAnalysis:
        response = self._client.models.generate_content(
            model=settings.gemini_model,
            contents=_build_prompt(context),
            config=types.GenerateContentConfig(
                response_mime_type="application/json",
                response_schema=RootCauseAnalysis,
                temperature=0.1,
            ),
        )
        return RootCauseAnalysis.model_validate_json(response.text)


def _build_prompt(context: dict) -> str:
    return (
        "You are a payments reconciliation analyst reviewing a case a deterministic detector "
        "already flagged. You are given fresh evidence gathered from read-only tools and cited "
        "policy passages. Produce a root-cause analysis.\n\n"
        "You do not decide what action is taken. You only propose a recommended_action; a "
        "separate deterministic policy engine and a human both have to agree before anything "
        "executes. Be conservative -- if evidence is incomplete or contradictory, say so in "
        "missing_information rather than guessing, and lower your confidence accordingly.\n\n"
        f"CASE CONTEXT:\n{json.dumps(context, indent=2, default=str)}"
    )
