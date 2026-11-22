"""The plan pipeline on Google ADK (D4), and refinement of one section (step 11).

Built per request, because every agent's model is bound to that request's key:

    START -> Parser -> Warm-up  --+          +-> Injury (only when the comment names injuries)
                    -> Scaling  --+-> join --+-> Timeline

Warm-up and Scaling run in parallel, then Injury and Timeline (the timeline does not need
the injury blocks). The parser also reads who attends today (`today`) and the injuries from
the comment. ADK's Workflow replaces the deprecated SequentialAgent/ParallelAgent
(capability report, decision 5.1).

Refinement runs one section again with the stored plan in session state and the coach's
instruction. After a warm-up change the Timeline section follows, and rebuilds only when
the warm-up minutes changed (plan 2.3).
"""

from __future__ import annotations

import asyncio
import base64
import binascii
import copy
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
from app.models import OrchestrateRequest, RegenerateRequest

logger = logging.getLogger("coachbuddy.pipeline")

APP_NAME = "coachbuddy"
SECTIONS = ("wod", "warmup", "parts", "injuries", "timeline")
# Plan fields a section can produce (the parser also produces `today`).
PLAN_FIELDS = ("today", "wod", "warmup", "parts", "injuries", "timeline")
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


def _user_message(req: OrchestrateRequest) -> types.Content:
    lines = [f"Class length: {req.input.classMinutes} minutes."]
    comment = (req.input.comment or "").strip()
    lines.append(f"Coach's comment: {comment}" if comment else "Coach's comment: none")
    raw, mime = decode_image(req.input.imageBase64)
    return types.Content(role="user", parts=[types.Part(text="\n".join(lines)), types.Part.from_bytes(data=raw, mime_type=mime)])


class PlanPipeline:
    def __init__(self, tools: Tools, model_factory: ModelFactory = litellm_model) -> None:
        self.tools = tools
        self.model_factory = model_factory

    async def run(self, req: OrchestrateRequest, publish: Optional[EventPublisher] = None) -> Dict[str, Any]:
        """Runs every section and returns the plan plus agentRuns. Section failures are in
        sectionStatus; only bad input or missing routes raise (InputError, RouteError).

        Every section event carries the plan as it stands after it, so the gateway can store
        progress and the coach sees sections as they finish. The closing `plan` event is the
        gateway's: it is sent after the plan is saved."""
        plan_id = req.planId
        message = _user_message(req)
        routes = {s: resolve_route(role_for(s), req.routingConfig) for s in SECTIONS}

        comment = (req.input.comment or "").strip()
        doc: Dict[str, Any] = {
            "planId": plan_id,
            "coachId": req.coachId,
            "revision": 1,
            "input": {"imageRef": req.imageRef, "classMinutes": req.input.classMinutes, **({"comment": comment} if comment else {})},
            "sectionStatus": {s: {"state": "pending"} for s in SECTIONS},
            "createdAt": req.createdAt,
            "updatedAt": req.createdAt,
        }
        progress = Progress(doc, publish)
        node = {s: build_section(s, routes[s], self.model_factory, self.tools, progress.notify) for s in routes}
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
        state = {"coachId": req.coachId, "classMinutes": req.input.classMinutes, "hasImage": True}
        state, latency = await self._run_workflow(workflow, req.coachId, plan_id, state, message)

        # The final document comes from the session state, the record of what each section
        # produced; events are progress only.
        status: Dict[str, Dict[str, Any]] = {}
        for section in SECTIONS:
            status[section] = state.get(f"status:{section}") or {"state": "failed", "error": "the section did not run"}
        doc = {k: v for k, v in progress.doc.items() if k not in PLAN_FIELDS}
        doc["sectionStatus"] = status
        doc["updatedAt"] = _now()
        if status["wod"]["state"] == "done" and state.get("today"):
            doc["today"] = state["today"]
        for section in SECTIONS:
            if status[section]["state"] == "done" and state.get(section) is not None:
                doc[section] = state[section]

        # Our own output must satisfy the contract; anything else is a bug here, not a model error.
        Plan.model_validate(doc)
        doc["agentRuns"] = [state[f"run:{s}"] for s in SECTIONS if state.get(f"run:{s}")]
        doc["orchestrationLatencyMs"] = latency
        return doc

    async def regenerate(self, req: RegenerateRequest, publish: Optional[EventPublisher] = None) -> Dict[str, Any]:
        """Builds one section again with the coach's instruction and returns the plan at
        req.revision plus agentRuns. The other sections are kept as stored. A warm-up change
        is followed by the timeline when the warm-up minutes changed."""
        try:
            Plan.model_validate(req.plan)
            # The stored values as they are: a round trip would rewrite createdAt's format.
            before = copy.deepcopy(req.plan)
        except ValueError as exc:
            raise InputError(f"the stored plan is not valid: {exc}") from exc
        if before["planId"] != req.planId or before["coachId"] != req.coachId:
            raise InputError("the stored plan belongs to another plan or coach")
        if before["sectionStatus"]["wod"]["state"] != "done" or "wod" not in before:
            raise InputError("the WOD could not be read for this plan; build the plan again")

        section = req.section
        run_sections = [section, "timeline"] if section == "warmup" else [section]
        routes = {s: resolve_route(role_for(s), req.routingConfig) for s in run_sections}

        comment = before["input"].get("comment", "")
        mentions = [
            {"label": i["label"], "fromComment": i["fromComment"], **({"returnNote": i["returnNote"]} if i.get("returnNote") else {})}
            for i in before.get("injuries", [])
        ]
        state: Dict[str, Any] = {
            "coachId": req.coachId,
            "classMinutes": before["input"]["classMinutes"],
            "hasImage": False,
            "today": before.get("today", []),
            "injuryMentions": mentions,
            "warmupWishes": None,
            **{s: before[s] for s in ("wod", "warmup", "parts", "injuries", "timeline") if s in before and s != section},
            "refine": {
                "section": section,
                "instruction": req.instruction.strip(),
                "comment": comment,
                "current": before.get(section),
                "warmupMinutesBefore": (before.get("warmup") or {}).get("minutes"),
                "timelineBefore": before.get("timeline"),
            },
        }

        doc = copy.deepcopy(before)
        doc["revision"] = req.revision
        doc.pop(section, None)
        doc["sectionStatus"][section] = {"state": "pending"}
        doc["updatedAt"] = _now()
        progress = Progress(doc, publish, revision=req.revision)

        node = {s: build_section(s, routes[s], self.model_factory, self.tools, progress.notify, refine=True) for s in run_sections}
        edges = [(START, node[section])]
        if section == "warmup":
            edges.append((node["warmup"], node["timeline"]))
        workflow = Workflow(name="RefineWorkflow", edges=edges)
        message = types.Content(role="user", parts=[types.Part(text=f"Change the {section} section: {req.instruction.strip()}")])
        state, latency = await self._run_workflow(workflow, req.coachId, f"{req.planId}:{req.revision}", state, message)

        doc = progress.doc
        doc["updatedAt"] = _now()
        ran = []
        for s in run_sections:
            if state.get(f"kept:{s}"):
                continue
            ran.append(s)
            status = state.get(f"status:{s}") or {"state": "failed", "error": "the section did not run"}
            doc["sectionStatus"][s] = status
            doc.pop(s, None)
            if status["state"] == "done" and state.get(s) is not None:
                doc[s] = state[s]
        if doc["sectionStatus"]["warmup"]["state"] != "done" and "timeline" in doc:
            doc["timeline"] = {**doc["timeline"], "provisional": True}

        Plan.model_validate(doc)
        doc["agentRuns"] = [state[f"run:{s}"] for s in ran if state.get(f"run:{s}")]
        doc["orchestrationLatencyMs"] = latency
        return doc

    async def _run_workflow(
        self, workflow: Workflow, user_id: str, session_id: str, state: Dict[str, Any], message: types.Content
    ) -> Tuple[Dict[str, Any], int]:
        sessions = InMemorySessionService()
        session = await sessions.create_session(app_name=APP_NAME, user_id=user_id, session_id=session_id, state=state)
        runner = Runner(app_name=APP_NAME, agent=workflow, session_service=sessions)
        started = time.perf_counter()
        async for _ in runner.run_async(user_id=user_id, session_id=session.id, new_message=message):
            pass
        session = await sessions.get_session(app_name=APP_NAME, user_id=user_id, session_id=session.id)
        return (session.state if session else {}), int((time.perf_counter() - started) * 1000)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


class Progress:
    """The plan as it stands while sections run, published after every section change.

    Warm-up and Scaling finish concurrently: one at a time, so a snapshot sent later never
    lacks a section an earlier one had. A section that starts again (refinement) loses its
    old content until it is done, as in a new plan."""

    def __init__(self, doc: Dict[str, Any], publish: Optional[EventPublisher], revision: int = 1) -> None:
        self.doc = doc
        self.publish = publish
        self.revision = revision
        self.lock = asyncio.Lock()

    async def notify(self, section: str, state: str, error: Optional[str], content: Optional[Dict[str, Any]] = None) -> None:
        doc = self.doc
        async with self.lock:
            doc["sectionStatus"][section] = {"state": state, **({"error": error} if error else {})}
            if state != "done":
                doc.pop(section, None)
            if section == "warmup" and state == "failed" and "timeline" in doc:
                doc["timeline"] = {**doc["timeline"], "provisional": True}
            for key, value in (content or {}).items():
                if key in PLAN_FIELDS and value is not None:
                    if key == "today" and not value:
                        continue
                    doc[key] = value
            doc["updatedAt"] = _now()
            if self.publish is None:
                return
            snapshot = Plan.model_validate(doc).model_dump(mode="json", exclude_none=True)
            event = PlanEvent(planId=doc["planId"], revision=self.revision, section=section, state=state, error=error, plan=snapshot)
            await self.publish(doc["planId"], event.model_dump(mode="json", exclude_none=True))
