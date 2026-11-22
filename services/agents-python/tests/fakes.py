"""Test doubles: a model that replays recorded answers and an MCP tools stand-in.

Only used by the tests (D3: no fakes in app code)."""

from __future__ import annotations

import asyncio
import json
from pathlib import Path
from typing import Any, AsyncGenerator, Callable, Dict, List, Optional

from google.adk.models.base_llm import BaseLlm
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types

from app.llm import Route
from app.mcp_tools import ToolError
from app.contract.create_plan_request import CreatePlanRequest
from app.models import EphemeralAgentRoute, OrchestrateRequest

RECORDINGS = Path(__file__).parent / "recordings"
TINY_PNG = (
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk"
    "YPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
)


def load_recording(name: str) -> Dict[str, List[Any]]:
    data = json.loads((RECORDINGS / name).read_text(encoding="utf-8"))
    return {k: v for k, v in data.items() if not k.startswith("_")}


class ReplayLlm(BaseLlm):
    """Answers each call with the next recorded answer (a dict, a raw string, or an Exception)."""

    answers: List[Any]
    requests: List[LlmRequest] = []

    async def generate_content_async(self, llm_request: LlmRequest, stream: bool = False) -> AsyncGenerator[LlmResponse, None]:
        self.requests.append(llm_request)
        if not self.answers:
            raise AssertionError(f"{self.model}: no recorded answer left")
        answer = self.answers.pop(0)
        await asyncio.sleep(0)
        if isinstance(answer, Exception):
            raise answer
        text = answer if isinstance(answer, str) else json.dumps(answer)
        yield LlmResponse(content=types.Content(role="model", parts=[types.Part(text=text)]))

    def instructions(self) -> List[str]:
        return [str(r.config.system_instruction) for r in self.requests]

    def inline_parts(self) -> List[str]:
        return [p.inline_data.mime_type for r in self.requests for c in r.contents for p in (c.parts or []) if p.inline_data]


class ReplayModels:
    """Model factory for PlanPipeline: one ReplayLlm per agent role."""

    def __init__(self, script: Dict[str, List[Any]]) -> None:
        self.script = {k: list(v) for k, v in script.items()}
        self.models: Dict[str, ReplayLlm] = {}

    def __call__(self, route: Route) -> BaseLlm:
        model = ReplayLlm(model=route.model, answers=self.script.get(route.role, []), requests=[])
        self.models[route.role] = model
        return model


class FakeTools:
    """Answers like mcp-rust. handlers maps a tool name to a function(args) -> result or raises."""

    def __init__(self, handlers: Optional[Dict[str, Callable[[Dict[str, Any]], Dict[str, Any]]]] = None) -> None:
        self.calls: List[tuple[str, Dict[str, Any]]] = []
        self.handlers = {
            "get_recent_coaching_history": lambda args: {"coachId": args["coachId"], "recentWarmups": [], "count": 0, "summary": "none"},
            "verify_timeline_math": verify_ok,
            **(handlers or {}),
        }

    async def call(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        self.calls.append((name, args))
        return self.handlers[name](args)


def verify_ok(args: Dict[str, Any]) -> Dict[str, Any]:
    return {"isValid": True, "totalMinutes": args["totalMinutes"], "scheduledMinutes": args["totalMinutes"], "discrepancyMinutes": 0,
            "feasibilityStatus": "OPTIMAL", "coachWarnings": ["Timeline verified"], "issues": [], "blocks": args["blocks"]}


def mcp_down(_args: Dict[str, Any]) -> Dict[str, Any]:
    raise ToolError("mcp-rust unreachable at http://mcp-rust:8082/mcp: All connection attempts failed")


def request(comment: str = "", image: str = TINY_PNG, minutes: int = 60, routes: Optional[Dict[str, EphemeralAgentRoute]] = None) -> OrchestrateRequest:
    return OrchestrateRequest(
        planId="pl_test01",
        coachId="c_01",
        imageRef="img_test01",
        createdAt="2026-11-20T09:00:00+00:00",
        input=CreatePlanRequest(imageBase64=image, comment=comment or None, classMinutes=minutes),
        routingConfig=routes if routes is not None else {"global": EphemeralAgentRoute(provider="gemini", model="gemini/gemini-3.8-flash", apiKey="test-key-123")},
    )
