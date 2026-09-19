from typing import Protocol

from app.schemas import RootCauseAnalysis


class LLMClient(Protocol):
    """Same shape for the real Gemini client and the deterministic fake -- the rest of the
    agent (the graph, the FastAPI route, the tests) never knows or cares which one it's holding.
    """

    def analyze(self, context: dict) -> RootCauseAnalysis: ...
