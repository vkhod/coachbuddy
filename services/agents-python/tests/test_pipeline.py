"""The ADK pipeline with recorded model answers and a stand-in for mcp-rust.

Each test names the failure it guards against. No live key, no network."""

from __future__ import annotations

import copy

import pytest

from app.contract.plan import Plan
from app.llm import RouteError
from app.mcp_tools import ToolError
from app.models import EphemeralAgentRoute
from app.pipeline import InputError, PlanPipeline
from tests.fakes import FakeTools, ReplayModels, load_recording, mcp_down, request, verify_ok



def script(**overrides):
    rec = load_recording("build-engine-60.json")
    rec.update({k: v for k, v in overrides.items()})
    return copy.deepcopy(rec)


async def run(models, tools=None, req=None, events=None):
    async def publish(plan_id, payload):
        if events is not None:
            events.append(payload)

    return await PlanPipeline(tools=tools or FakeTools(), model_factory=models).run(req or request(), publish=publish)


def states(doc):
    return {k: v["state"] for k, v in doc["sectionStatus"].items()}


async def test_full_plan_is_valid_against_the_contract():
    events = []
    models = ReplayModels(script())
    doc = await run(models, events=events)

    plan = Plan.model_validate({k: v for k, v in doc.items() if k not in ("agentRuns", "orchestrationLatencyMs")})
    assert states(doc) == {"wod": "done", "warmup": "done", "parts": "done", "injuries": "skipped", "timeline": "done"}
    assert [p.id for p in plan.wod.parts] == ["A", "B", "C"]
    assert plan.timeline.status.value == "OPTIMAL" and plan.timeline.provisional is False
    assert plan.input.comment is None and plan.input.classMinutes == 60
    # Nulls the model may send for optional fields are dropped (the JSON Schema has no nulls).
    assert "rxPlus" not in doc["parts"][0]["tiers"]
    assert {e["section"] for e in events} >= {"wod", "warmup", "parts", "timeline", "plan"}
    assert [r["agent"] for r in doc["agentRuns"]][0] == "WodParserAgent"


async def test_screenshot_goes_to_the_parser_only():
    models = ReplayModels(script())
    await run(models)
    assert models.models["parser"].inline_parts() == ["image/png"]
    for role in ("warmup", "scaling", "timeline"):
        assert models.models[role].inline_parts() == [], role


async def test_warmup_and_timeline_use_the_mcp_tools():
    tools = FakeTools()
    await run(ReplayModels(script()), tools=tools)
    names = [n for n, _ in tools.calls]
    assert names == ["get_recent_coaching_history", "verify_timeline_math"]
    _, args = tools.calls[1]
    assert args["totalMinutes"] == 60 and args["blocks"][0] == {"startMin": 0, "endMin": 3, "label": "Brief", "section": "brief"}


async def test_scaling_rule_error_is_repaired_once():
    good = script()["scaling"][0]
    bad = copy.deepcopy(good)
    bad["parts"][2]["tiers"]["scaled"]["cells"] = ["6", "14", "20"]  # 3 cells for 4 columns
    models = ReplayModels(script(scaling=[bad, good]))
    doc = await run(models)
    assert states(doc)["parts"] == "done"
    second = models.models["scaling"].instructions()[1]
    assert "parts[C].tiers.scaled: 3 cells for 4 columns" in second


async def test_scaling_fails_after_one_repair_and_the_rest_survives():
    bad = script()["scaling"][0]
    bad["parts"][0]["tiers"]["rxPlus"] = {"line": "Heavier"}  # RX+ on a strength part
    models = ReplayModels(script(scaling=[bad, copy.deepcopy(bad)]))
    doc = await run(models)
    assert states(doc) == {"wod": "done", "warmup": "done", "parts": "failed", "injuries": "skipped", "timeline": "done"}
    assert "RX+ is only for conditioning parts" in doc["sectionStatus"]["parts"]["error"]
    assert "parts" not in doc


async def test_schema_error_is_repaired_once():
    good = script()["warmup"][0]
    bad = dict(good, minutes="twelve")
    models = ReplayModels(script(warmup=[bad, good]))
    doc = await run(models)
    assert states(doc)["warmup"] == "done"
    assert "minutes: Input should be a valid integer" in models.models["warmup"].instructions()[1]


async def test_stopped_mcp_rust_fails_warmup_and_timeline_visibly():
    """Done-when check: no canned or locally computed substitute appears."""
    tools = FakeTools({"get_recent_coaching_history": mcp_down, "verify_timeline_math": mcp_down})
    models = ReplayModels(script())
    doc = await run(models, tools=tools)
    assert states(doc) == {"wod": "done", "warmup": "failed", "parts": "done", "injuries": "skipped", "timeline": "failed"}
    assert "mcp-rust unreachable" in doc["sectionStatus"]["timeline"]["error"]
    assert "timeline" not in doc and "warmup" not in doc
    # The warm-up model is never called when its history is missing.
    assert models.models["warmup"].requests == []


async def test_tool_is_error_fails_the_timeline_without_retry():
    def is_error(_args):
        raise ToolError("verify_timeline_math: Invalid arguments for verify_timeline_math: invalid value: integer `-1`, expected u32")

    models = ReplayModels(script())
    doc = await run(models, tools=FakeTools({"verify_timeline_math": is_error}))
    assert states(doc)["timeline"] == "failed"
    assert "expected u32" in doc["sectionStatus"]["timeline"]["error"]
    assert len(models.models["timeline"].requests) == 1


async def test_invalid_timeline_gets_one_repair_with_the_rust_issues():
    good = script()["timeline"][0]
    gap = copy.deepcopy(good)
    gap["blocks"][3]["startMin"] = 25

    def verify(args):
        if args["blocks"][3]["startMin"] == 25:
            return {**verify_ok(args), "isValid": False, "feasibilityStatus": "INVALID",
                    "issues": [{"blockIndex": 3, "blockLabel": "B. Pendlay row", "issueType": "GAP", "description": "Unscheduled gap of 1 min between 24 and 25"}]}
        return verify_ok(args)

    models = ReplayModels(script(timeline=[gap, good]))
    doc = await run(models, tools=FakeTools({"verify_timeline_math": verify}))
    assert states(doc)["timeline"] == "done"
    assert "blocks[3] GAP: Unscheduled gap of 1 min" in models.models["timeline"].instructions()[1]


async def test_missing_warmup_makes_the_timeline_provisional():
    bad = dict(script()["warmup"][0], minutes="x")
    models = ReplayModels(script(warmup=[bad, bad]))
    doc = await run(models)
    assert states(doc)["warmup"] == "failed" and states(doc)["timeline"] == "done"
    assert doc["timeline"]["provisional"] is True
    assert "Reserve 12 minutes" in models.models["timeline"].instructions()[0]


async def test_provider_error_on_the_parser_fails_every_section_that_needs_the_wod():
    models = ReplayModels(script(parser=[RuntimeError("litellm.ServiceUnavailableError: GeminiException - overloaded (key test-key-123)")]))
    doc = await run(models)
    assert states(doc) == {"wod": "failed", "warmup": "failed", "parts": "failed", "injuries": "skipped", "timeline": "failed"}
    assert "overloaded" in doc["sectionStatus"]["wod"]["error"]
    assert "test-key-123" not in doc["sectionStatus"]["wod"]["error"]
    assert doc["sectionStatus"]["warmup"]["error"] == "the WOD could not be read, so this section was not built"
    assert len(models.models["parser"].requests) == 1  # no silent retry


async def test_parser_refuses_a_model_without_vision(monkeypatch):
    monkeypatch.setattr("app.agents.sections.supports_vision", lambda model: False)
    models = ReplayModels(script())
    doc = await run(models)
    assert "cannot read images" in doc["sectionStatus"]["wod"]["error"]
    assert models.models["parser"].requests == []


async def test_comment_is_kept_and_passed_to_the_parser():
    models = ReplayModels(script())
    doc = await run(models, req=request(comment="game-like warmup with broomsticks"))
    assert doc["input"]["comment"] == "game-like warmup with broomsticks"
    contents = models.models["parser"].requests[0].contents
    assert any("game-like warmup with broomsticks" in (p.text or "") for c in contents for p in c.parts)


async def test_per_role_routes_and_no_key_is_a_route_error():
    routes = {
        "global": EphemeralAgentRoute(provider="gemini", model="gemini/gemini-3.8-flash", apiKey="g-key"),
        "scaling": EphemeralAgentRoute(provider="anthropic", model="anthropic/claude-haiku-5-5", apiKey="a-key"),
    }
    models = ReplayModels(script())
    await run(models, req=request(routes=routes))
    assert models.models["scaling"].model == "anthropic/claude-haiku-5-5"
    assert models.models["parser"].model == "gemini/gemini-3.8-flash"

    with pytest.raises(RouteError, match="no API key"):
        await run(ReplayModels(script()), req=request(routes={"global": EphemeralAgentRoute(provider="gemini", model="gemini/gemini-3.8-flash")}))


@pytest.mark.parametrize("image,msg", [("data:image/svg+xml;base64,PHN2Zy8+", "unsupported image type"), ("", "send a WOD screenshot")])
async def test_bad_input_is_rejected(image, msg):
    with pytest.raises(InputError, match=msg):
        await run(ReplayModels(script()), req=request(image=image))


def test_model_schema_keeps_structure_and_describes_limits():
    import json

    from app.agents.outputs import ScalingOut
    from app.agents.schema_for_model import schema_for_model

    sent = schema_for_model(ScalingOut)
    text = json.dumps(sent)
    for keyword in ('"maxLength"', '"maxItems"', '"minItems"', '"pattern"', '"default"'):
        assert keyword not in text
    tier = sent["$defs"]["Tier"]
    assert tier["required"] == ["line"] and tier["properties"]["line"]["description"] == "At most 100 characters. Not empty."
    assert sent["$defs"]["PartLayout"]["enum"] == ["lines", "grid"]


async def test_limit_the_model_was_not_forced_to_keep_is_repaired():
    good = script()["scaling"][0]
    bad = copy.deepcopy(good)
    bad["parts"][1]["tiers"]["senior"]["line"] = "x" * 140
    models = ReplayModels(script(scaling=[bad, good]))
    doc = await run(models)
    assert states(doc)["parts"] == "done"
    assert "parts.1.tiers.senior.line: String should have at most 100 characters" in models.models["scaling"].instructions()[1]


async def test_live_gemini_recording_replays_into_a_valid_plan():
    """Real answers from gemini-3.8-flash for the 18/11 screenshot pass every check unchanged."""
    models = ReplayModels(load_recording("live-gemini-3.8-flash-2026-11-18.json"))
    doc = await run(models, req=request(comment="I have a mood for a game like warmup"))
    assert states(doc) == {"wod": "done", "warmup": "done", "parts": "done", "injuries": "skipped", "timeline": "done"}
    assert [p["minutes"] for p in doc["wod"]["parts"]] == [9, 10, 20]
    engine = [p for p in doc["parts"] if p["partId"] == "C"][0]
    assert engine["layout"] == "grid" and len(engine["columns"]) == 4
    assert all(r["attempts"] == 1 for r in doc["agentRuns"])
