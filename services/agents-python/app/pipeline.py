"""The plan pipeline on Google ADK (D4).

Built per request, because every agent's model is bound to that request's key:

    START -> Parser -> Warm-up  --+          +-> Injury (only when the comment names injuries)
                    -> Scaling  --+-> join --+-> Timeline

Warm-up and Scaling run in parallel, then Injury and Timeline (the timeline does not need
the injury blocks). The parser also reads who attends today (`today`) and the injuries from
the comment. ADK's Workflow replaces the deprecated SequentialAgent/ParallelAgent
(capability report, decision 5.1).
"""

from __future__ import annotations

import base64
import binascii
from datetime import datetime, timezone
import logging
import re
import time
from typing import Any, Awaitable, Callable, Dict, List, Optional, Tuple

from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.workflow import START, JoinNode, Workflow
from google.genai import types

from app.agents.sections import build_section, role_for
from app.contract.plan import Plan
from app.contract.plan_event import PlanEvent
from app.llm import ModelFactory, RouteError, litellm_model, resolve_route
from app.mcp_tools import Tools
from app.models import OrchestrateRequest

logger = logging.getLogger("coachbuddy.pipeline")

APP_NAME = "coachbuddy"
SECTIONS = ("wod", "warmup", "parts", "injuries", "timeline")
RASTER_TYPES = {"image/png", "image/jpeg", "image/webp", "image/heic", "image/heif"}

# (planId, payload) -> publishes one PlanEvent
EventPublisher = Callable[[str, Dict[str, Any]], Awaitable[None]]


class InputError(ValueError):
    """The request cannot be turned into a plan (shown to the coach as a 400)."""


def decode_image(data: str) -> Tuple[bytes, str]:
    """Accepts plain base64 or a data URI. Returns the bytes and the MIME type."""
    mime = "image/jpeg"
    match = re.match(r"^data:([\w/+.-]+);base64,", data)
    if match:
        mime = match.group(1).lower()
        data = data[match.end():]
    if mime not in RASTER_TYPES:
        raise InputError(f"unsupported image type {mime}; send a PNG, JPEG or WebP screenshot")
    try:
        raw = base64.b64decode(data, validate=False)
    except (binascii.Error, ValueError) as exc:
        raise InputError("the image is not valid base64") from exc
    if not raw:
        raise InputError("the image is empty")
    return raw, mime


def _user_message(req: OrchestrateRequest) -> Tuple[types.Content, bool]:
    lines = [f"Class length: {req.input.targetDurationMinutes} minutes."]
    comment = req.input.coachConstraints.strip()
    lines.append(f"Coach's comment: {comment}" if comment else "Coach's comment: none")
    if req.input.rawText.strip():
        lines.append("WOD as text:\n" + req.input.rawText.strip())
    parts = [types.Part(text="\n".join(lines))]
    has_image = bool((req.input.imageBase64 or "").strip())
    if has_image:
        raw, mime = decode_image(req.input.imageBase64 or "")
        parts.append(types.Part.from_bytes(data=raw, mime_type=mime))
    if not has_image and not req.input.rawText.strip():
        raise InputError("send a WOD screenshot")
    return types.Content(role="user", parts=parts), has_image


class PlanPipeline:
    def __init__(self, tools: Tools, model_factory: ModelFactory = litellm_model) -> None:
        self.tools = tools
        self.model_factory = model_factory

    async def run(self, req: OrchestrateRequest, publish: Optional[EventPublisher] = None) -> Dict[str, Any]:
        """Runs every section and returns the plan plus agentRuns. Section failures are in
        sectionStatus; only bad input or missing routes raise (InputError, RouteError)."""
        plan_id = req.sessionId
        message, has_image = _user_message(req)
        routes = {s: resolve_route(role_for(s), req.routingConfig) for s in ("wod", "warmup", "parts", "injuries", "timeline")}

        async def notify(section: str, state: str, error: Optional[str]) -> None:
            if publish is None:
                return
            event = PlanEvent(planId=plan_id, revision=1, section=section, state=state, error=error)
            await publish(plan_id, event.model_dump(mode="json", exclude_none=True))

        node = {s: build_section(s, routes[s], self.model_factory, self.tools, notify) for s in routes}
        join = JoinNode(name="CoachingDone")
        workflow = Workflow(
            name="PlanWorkflow",
            edges=[
                (START, node["wod"]),
                (node["wod"], node["warmup"]),
                (node["wod"], node["parts"]),
                ((node["warmup"], node["parts"]), join),
                (join, node["injuries"]),
                (join, node["timeline"]),
            ],
        )

        sessions = InMemorySessionService()
        session = await sessions.create_session(
            app_name=APP_NAME,
            user_id=req.coachId,
            session_id=plan_id,
            state={"coachId": req.coachId, "classMinutes": req.input.targetDurationMinutes, "hasImage": has_image},
        )
        runner = Runner(app_name=APP_NAME, agent=workflow, session_service=sessions)
        started = time.perf_counter()
        async for _ in runner.run_async(user_id=req.coachId, session_id=session.id, new_message=message):
            pass
        session = await sessions.get_session(app_name=APP_NAME, user_id=req.coachId, session_id=session.id)
        state = session.state if session else {}

        now = datetime.now(timezone.utc).isoformat()
        status: Dict[str, Dict[str, Any]] = {}
        for section in SECTIONS:
            status[section] = state.get(f"status:{section}") or {"state": "failed", "error": "the section did not run"}

        comment = req.input.coachConstraints.strip()
        doc: Dict[str, Any] = {
            "planId": plan_id,
            "coachId": req.coachId,
            "revision": 1,
            # The screenshot is stored under this reference from Phase 2 step 10 on.
            "input": {"imageRef": f"img_{plan_id}"[:64], "classMinutes": req.input.targetDurationMinutes, **({"comment": comment} if comment else {})},
            "sectionStatus": status,
            "createdAt": now,
            "updatedAt": now,
        }
        if status["wod"]["state"] == "done" and state.get("today"):
            doc["today"] = state["today"]
        for section in ("wod", "warmup", "parts", "injuries", "timeline"):
            if status[section]["state"] == "done" and state.get(section) is not None:
                doc[section] = state[section]

        # Our own output must satisfy the contract; anything else is a bug here, not a model error.
        Plan.model_validate(doc)
        doc["agentRuns"] = [state[f"run:{s}"] for s in ("wod", "warmup", "parts", "injuries", "timeline") if state.get(f"run:{s}")]
        doc["orchestrationLatencyMs"] = int((time.perf_counter() - started) * 1000)
        await notify("plan", "done", None)
        return doc

