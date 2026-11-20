"""One plan section per agent: run the LlmAgent, check its answer, repair once, or fail.

A model answer goes through three checks: the output schema (generated contract types),
the cross-field rules (app/contract/rules.py) and, for the timeline, mcp-rust's
verify_timeline_math. Any failure goes back to the model once with the reasons. A second
failure, a provider error or a tool error marks the section failed with the reason, and
the other sections carry on (D3: nothing is filled in locally).
"""

from __future__ import annotations

import json
import logging
import time
from dataclasses import asdict, dataclass, field
from typing import Any, AsyncGenerator, Awaitable, Callable, Dict, List, Optional

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.events import Event, EventActions
from pydantic import BaseModel, ConfigDict, PrivateAttr, ValidationError

from app.agents import prompts
from app.agents.outputs import InjuriesOut, ParserOut, ScalingOut, TimelineDraft, WarmupOut
from app.agents.schema_for_model import schema_for_model
from app.contract.plan import (
    ClassInput,
    Injury,
    PartScaling,
    Plan,
    SectionState,
    SectionStatusMap,
    Timeline,
    TimelineStatus,
    Warmup,
    Wod,
)
from app.contract.rules import check_plan_rules
from app.llm import ModelFactory, Route, supports_vision
from app.mcp_tools import ToolError, Tools

logger = logging.getLogger("coachbuddy.sections")

MAX_ERROR = 300
# Warm-up minutes the Timeline agent reserves while the warm-up section has failed.
PROVISIONAL_WARMUP_MINUTES = 12


# Fixed wording under every injury block (plan 2.2: conservative, suggestion only).
INJURY_CAUTION = "Suggestion only. Check with the athlete; stop on pain."


class SectionFailed(Exception):
    """Stops a section with a reason the coach can read."""


class SectionSkipped(Exception):
    """The section has nothing to do for this plan (for example no injuries in the comment)."""


def as_json(value: Any) -> str:
    if isinstance(value, BaseModel):
        value = value.model_dump(mode="json", exclude_none=True)
    return json.dumps(value, ensure_ascii=False)


def _short(text: str, secret: str = "") -> str:
    if secret:
        text = text.replace(secret, "***")
    text = " ".join(text.split())
    return text if len(text) <= MAX_ERROR else text[: MAX_ERROR - 3] + "..."


def _validation_problems(exc: ValidationError) -> List[str]:
    out = []
    for err in exc.errors()[:8]:
        loc = ".".join(str(p) for p in err.get("loc", ()))
        out.append(f"{loc or 'answer'}: {err.get('msg', 'invalid')}")
    return out


def _rules_plan(
    wod: Wod,
    parts: Optional[List[PartScaling]] = None,
    warmup: Optional[Warmup] = None,
    injuries: Optional[List[Injury]] = None,
) -> Plan:
    """A minimal Plan so the contract rules can check sections against the WOD."""
    pending = SectionState(state="pending")
    return Plan(
        planId="check",
        coachId="check",
        revision=1,
        input=ClassInput(imageRef="check", classMinutes=60),
        wod=wod,
        warmup=warmup,
        parts=parts,
        injuries=injuries,
        sectionStatus=SectionStatusMap(wod=pending, warmup=pending, parts=pending, injuries=pending, timeline=pending),
    )


@dataclass
class AgentRun:
    """What actually ran, for the agent traces (D3)."""

    agent: str
    section: str
    provider: str
    model: str
    attempts: int = 0
    latencyMs: int = 0
    state: str = "pending"
    error: Optional[str] = None
    tools: List[str] = field(default_factory=list)


# (section, state, error, content): content is the section's output on "done".
Notify = Callable[[str, str, Optional[str], Optional[Dict[str, Any]]], Awaitable[None]]


class SectionAgent(BaseAgent):
    """Wraps one LlmAgent. Subclasses define the inputs, the checks and the final value."""

    model_config = ConfigDict(arbitrary_types_allowed=True)

    section: str
    inner: LlmAgent
    route: Route
    tools: Any = None
    notify: Any = None
    _run: Optional[AgentRun] = PrivateAttr(default=None)

    def used_tool(self, name: str) -> None:
        if self._run is not None:
            self._run.tools.append(name)

    @property
    def draft_key(self) -> str:
        return f"draft:{self.section}"

    @property
    def repair_key(self) -> str:
        return f"repair:{self.section}"

    async def prepare(self, state: Dict[str, Any]) -> Dict[str, Any]:
        """Extra state for the instruction, or SectionFailed when the section cannot run."""
        return {}

    async def check(self, state: Dict[str, Any], draft: Dict[str, Any]) -> tuple[Any, List[str]]:
        """The final section value and a list of problems; problems trigger the repair."""
        return draft, []

    def output(self, value: Any) -> Dict[str, Any]:
        return {self.section: value}

    async def _emit(self, state: str, error: Optional[str] = None, content: Optional[Dict[str, Any]] = None) -> None:
        if self.notify is not None:
            try:
                await self.notify(self.section, state, error, content)
            except Exception as exc:  # events are best-effort; the plan is the record
                logger.debug("event for %s not published: %s", self.section, exc)

    def _state_event(self, delta: Dict[str, Any]) -> Event:
        return Event(author=self.name, actions=EventActions(state_delta=delta))

    async def _run_async_impl(self, ctx: InvocationContext) -> AsyncGenerator[Event, None]:
        run = AgentRun(agent=self.inner.name, section=self.section, provider=self.route.provider, model=self.route.model)
        self._run = run
        started = time.perf_counter()
        await self._emit("running")
        state = ctx.session.state

        def finish(state_name: str, error: Optional[str] = None) -> Event:
            run.state, run.error = state_name, error
            run.latencyMs = int((time.perf_counter() - started) * 1000)
            return self._state_event(
                {
                    f"status:{self.section}": {"state": state_name, **({"error": error} if error else {})},
                    f"run:{self.section}": asdict(run),
                }
            )

        try:
            extra = await self.prepare(state)
        except SectionSkipped:
            yield finish("skipped")
            await self._emit("skipped")
            return
        except (SectionFailed, ToolError) as exc:
            reason = _short(str(exc))
            yield finish("failed", reason)
            await self._emit("failed", reason)
            return
        if extra:
            yield self._state_event(extra)

        problems: List[str] = []
        for attempt in (1, 2):
            run.attempts = attempt
            yield self._state_event({self.repair_key: problems if attempt == 2 else []})
            try:
                async for event in self.inner.run_async(ctx):
                    yield event
                draft = ctx.session.state.get(self.draft_key)
                if not isinstance(draft, dict):
                    raise ValueError("the answer is not a JSON object")
                value, problems = await self.check(ctx.session.state, draft)
            except ValidationError as exc:
                problems = _validation_problems(exc)
            except ValueError as exc:
                problems = [_short(str(exc), self.route.api_key)]
            except (SectionFailed, ToolError) as exc:
                reason = _short(str(exc))
                yield finish("failed", reason)
                await self._emit("failed", reason)
                return
            except Exception as exc:  # provider or network error: no silent retry
                reason = _short(f"{self.route.model}: {exc or type(exc).__name__}", self.route.api_key)
                logger.warning("%s failed: %s", self.inner.name, reason)
                yield finish("failed", reason)
                await self._emit("failed", reason)
                return
            if not problems:
                out = self.output(value)
                yield self._state_event(out)
                yield finish("done")
                await self._emit("done", content=out)
                return
            logger.info("%s answer rejected (attempt %d): %s", self.inner.name, attempt, problems)

        reason = _short("answer still invalid after one repair: " + "; ".join(problems))
        yield finish("failed", reason)
        await self._emit("failed", reason)


def _instruction(base: str, inputs: Callable[[Dict[str, Any]], str], repair_key: str) -> Callable[[ReadonlyContext], str]:
    def build(ctx: ReadonlyContext) -> str:
        state = ctx.state
        parts = [base, inputs(state)]
        repair = state.get(repair_key) or []
        if repair:
            parts.append(
                "Your previous answer was rejected for these reasons. Answer again in the same schema "
                "and fix every one:\n- " + "\n- ".join(repair)
            )
        return "\n\n".join(p for p in parts if p)

    return build


def _llm_agent(name: str, section: str, route: Route, factory: ModelFactory, base: str, inputs, schema, sees_input: bool) -> LlmAgent:
    return LlmAgent(
        name=name,
        model=factory(route),
        instruction=_instruction(base, inputs, f"repair:{section}"),
        # Structure for the model; the full contract model checks the answer in check().
        output_schema=schema_for_model(schema),
        output_key=f"draft:{section}",
        include_contents="default" if sees_input else "none",
    )


# --- Parser ---------------------------------------------------------------------------


class ParserSection(SectionAgent):
    async def prepare(self, state: Dict[str, Any]) -> Dict[str, Any]:
        if state.get("hasImage") and supports_vision(self.route.model) is False:
            raise SectionFailed(f"{self.route.model} cannot read images; choose a model with vision for the parser")
        return {}

    async def check(self, state, draft):
        out = ParserOut.model_validate(draft)
        ids = [p.id for p in out.wod.parts]
        problems = [f"wod.parts: part id {i} is used twice" for i in sorted(set(ids)) if ids.count(i) > 1]
        return out, problems

    def output(self, value: ParserOut) -> Dict[str, Any]:
        return {
            "wod": value.wod.model_dump(mode="json", exclude_none=True),
            "warmupWishes": value.warmupWishes or "",
            "today": [t.model_dump(mode="json", exclude_none=True) for t in value.today],
            "injuryMentions": [m.model_dump(mode="json", exclude_none=True) for m in value.injuries],
        }


def _parser_inputs(state: Dict[str, Any]) -> str:
    return f"Class length: {state['classMinutes']} minutes."


# --- Warm-up --------------------------------------------------------------------------


def _need_wod(state: Dict[str, Any]) -> Dict[str, Any]:
    wod = state.get("wod")
    if not wod:
        raise SectionFailed("the WOD could not be read, so this section was not built")
    return wod


class WarmupSection(SectionAgent):
    async def prepare(self, state):
        _need_wod(state)
        self.used_tool("get_recent_coaching_history")
        history = await self.tools.call("get_recent_coaching_history", {"coachId": state["coachId"], "limit": 5})
        recent = [
            {"title": w.get("gameTitle", ""), "drills": w.get("drillNames", [])[:8]}
            for w in history.get("recentWarmups", [])
        ]
        return {"recentWarmups": recent}

    async def check(self, state, draft):
        warmup = WarmupOut.model_validate(draft)
        problems = check_plan_rules(_rules_plan(Wod.model_validate(state["wod"]), warmup=warmup))
        return warmup.model_dump(mode="json", exclude_none=True), problems


def _today_line(state: Dict[str, Any]) -> str:
    today = state.get("today") or []
    return f"Athletes who need attention today: {as_json(today)}" if today else "Athletes who need attention today: none"


def _warmup_inputs(state: Dict[str, Any]) -> str:
    return "\n".join(
        [
            f"WOD: {as_json(state['wod'])}",
            f"Class length: {state['classMinutes']} minutes.",
            _today_line(state),
            f"Coach's warm-up wishes: {state.get('warmupWishes') or 'none'}",
            f"Recent warm-ups of this coach: {as_json(state.get('recentWarmups') or [])}",
        ]
    )


# --- Scaling --------------------------------------------------------------------------


class ScalingSection(SectionAgent):
    async def prepare(self, state):
        _need_wod(state)
        return {}

    async def check(self, state, draft):
        out = ScalingOut.model_validate(draft)
        wod = Wod.model_validate(state["wod"])
        problems = check_plan_rules(_rules_plan(wod, out.parts))
        return [p.model_dump(mode="json", exclude_none=True) for p in out.parts], problems

    def output(self, value):
        return {"parts": value}


def _scaling_inputs(state: Dict[str, Any]) -> str:
    return f"WOD: {as_json(state['wod'])}\n{_today_line(state)}"


# --- Injuries -------------------------------------------------------------------------


class InjurySection(SectionAgent):
    async def prepare(self, state):
        _need_wod(state)
        if not state.get("injuryMentions"):
            raise SectionSkipped()
        return {}

    async def check(self, state, draft):
        out = InjuriesOut.model_validate(draft)
        mentions = state["injuryMentions"]
        problems: List[str] = []
        got = [a.label for a in out.injuries]
        want = [m["label"] for m in mentions]
        if got != want:
            problems.append(f"injuries: expected one entry per injury in this order {want}, got {got}")
            return None, problems
        injuries = [
            Injury(
                label=m["label"],
                fromComment=m["fromComment"],
                returnNote=m.get("returnNote"),
                warmup=a.warmup,
                parts=a.parts,
                caution=INJURY_CAUTION,
            )
            for m, a in zip(mentions, out.injuries)
        ]
        problems += check_plan_rules(_rules_plan(Wod.model_validate(state["wod"]), injuries=injuries))
        return [i.model_dump(mode="json", exclude_none=True) for i in injuries], problems

    def output(self, value):
        return {"injuries": value}


def _injury_inputs(state: Dict[str, Any]) -> str:
    warmup = state.get("warmup")
    return "\n".join(
        [
            f"WOD: {as_json(state['wod'])}",
            f"Warm-up: {as_json(warmup)}" if warmup else "Warm-up: not planned yet; advise for a general warm-up.",
            f"Injuries: {as_json(state['injuryMentions'])}",
        ]
    )


# --- Timeline -------------------------------------------------------------------------


class TimelineSection(SectionAgent):
    async def prepare(self, state):
        _need_wod(state)
        return {"warmupProvisional": not state.get("warmup")}

    async def check(self, state, draft):
        out = TimelineDraft.model_validate(draft)
        wod_ids = {p["id"] for p in state["wod"]["parts"]}
        problems = [
            f"blocks[{i}].section: no WOD part {b.section}"
            for i, b in enumerate(out.blocks)
            if b.section and b.section not in ("warmup", "brief", "transition") and b.section not in wod_ids
        ]
        self.used_tool("verify_timeline_math")
        verified = await self.tools.call(
            "verify_timeline_math",
            {
                "totalMinutes": state["classMinutes"],
                "blocks": [b.model_dump(mode="json", exclude_none=True) for b in out.blocks],
            },
        )
        status = verified.get("feasibilityStatus")
        if status not in ("OPTIMAL", "TIGHT", "INVALID"):
            raise ToolError(f"verify_timeline_math: unknown status {status!r}")
        if status == "INVALID":
            for issue in verified.get("issues", [])[:6]:
                problems.append(f"blocks[{issue.get('blockIndex')}] {issue.get('issueType')}: {issue.get('description', '')}")
            if not verified.get("issues"):
                problems.append("verify_timeline_math: the timeline does not add up")
        warnings = [w[:120] for w in verified.get("coachWarnings", [])][:5] if status == "TIGHT" else None
        timeline = Timeline(
            totalMinutes=state["classMinutes"],
            blocks=out.blocks,
            status=TimelineStatus(status),
            warnings=warnings or None,
            provisional=bool(state.get("warmupProvisional")),
        )
        return timeline.model_dump(mode="json", exclude_none=True), problems


def _timeline_inputs(state: Dict[str, Any]) -> str:
    parts = [{"id": p["id"], "name": p["name"], "minutes": p["minutes"]} for p in state["wod"]["parts"]]
    warmup = state.get("warmup")
    if warmup:
        warm = f"Warm-up: {warmup['minutes']} minutes."
    else:
        warm = (
            f"The warm-up is not planned yet. Reserve {PROVISIONAL_WARMUP_MINUTES} minutes for it, "
            "labelled \"Warm-up\", section \"warmup\"."
        )
    return "\n".join([f"Class length: {state['classMinutes']} minutes.", warm, f"WOD parts: {as_json(parts)}"])


# --- Builders -------------------------------------------------------------------------

SECTIONS = {
    "wod": ("WodParserAgent", ParserSection, prompts.PARSER, _parser_inputs, ParserOut, True, "parser"),
    "warmup": ("WarmupPlannerAgent", WarmupSection, prompts.WARMUP, _warmup_inputs, WarmupOut, False, "warmup"),
    "parts": ("ScalingAgent", ScalingSection, prompts.SCALING, _scaling_inputs, ScalingOut, False, "scaling"),
    "injuries": ("InjuryAgent", InjurySection, prompts.INJURY, _injury_inputs, InjuriesOut, False, "injury"),
    "timeline": ("TimelineAgent", TimelineSection, prompts.TIMELINE, _timeline_inputs, TimelineDraft, False, "timeline"),
}


def build_section(section: str, route: Route, factory: ModelFactory, tools: Tools, notify: Optional[Notify]) -> SectionAgent:
    name, cls, base, inputs, schema, sees_input, _role = SECTIONS[section]
    inner = _llm_agent(name, section, route, factory, base, inputs, schema, sees_input)
    return cls(name=f"{name}Section", section=section, inner=inner, route=route, tools=tools, notify=notify)


def role_for(section: str) -> str:
    return SECTIONS[section][6]


# Re-exported for tests.
__all__ = ["SectionAgent", "SectionFailed", "SectionSkipped", "AgentRun", "build_section", "role_for", "Warmup"]
