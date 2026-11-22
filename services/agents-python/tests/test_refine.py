"""Refinement (step 11): one section built again with the coach's instruction.

Recorded model answers only. Each test names the failure it guards against."""

from __future__ import annotations

import copy

import pytest

from app.contract.plan import Plan
from app.contract.plan_event import PlanEvent
from app.models import EphemeralAgentRoute, RegenerateRequest
from app.pipeline import InputError, PlanPipeline
from tests.fakes import FakeTools, ReplayModels, load_recording, mcp_down, request

TWO_INJURIES = "senior athlete today. ankle injury returning after 2 weeks, one shoulder tear"
ROUTES = {"global": EphemeralAgentRoute(provider="gemini", model="gemini/gemini-3.8-flash", apiKey="test-key-123")}


def recording(name="build-engine-60.json"):
    return copy.deepcopy(load_recording(name))


async def built_plan(name="build-engine-60.json", comment=""):
    """A stored plan at revision 1, as the gateway keeps it (no agentRuns)."""
    doc = await PlanPipeline(tools=FakeTools(), model_factory=ReplayModels(recording(name))).run(request(comment=comment))
    return {k: v for k, v in doc.items() if k not in ("agentRuns", "orchestrationLatencyMs")}


def shorter_warmup(minutes):
    """The recorded warm-up, cut to `minutes` (the blocks still add up)."""
    w = recording()["warmup"][0]
    w["blocks"][1]["minutes"] = minutes - w["blocks"][0]["minutes"]
    w["minutes"] = minutes
    w["blocks"][0]["name"] = "Band pull-apart game"
    return w


def timeline_for(warmup_minutes):
    """The recorded timeline with the warm-up block at `warmup_minutes`; the cool-down
    takes up the difference, so the blocks still fill the class."""
    t = recording()["timeline"][0]
    shift = warmup_minutes - 12
    for b in t["blocks"][1:]:
        if b.get("section") != "warmup":
            b["startMin"] += shift
        if b is not t["blocks"][-1]:
            b["endMin"] += shift
    return t


async def refine(plan, section, instruction, script, tools=None, events=None, revision=2):
    async def publish(_plan_id, payload):
        if events is not None:
            events.append(payload)

    req = RegenerateRequest(
        planId=plan["planId"], coachId=plan["coachId"], revision=revision, section=section,
        instruction=instruction, plan=plan, routingConfig=ROUTES,
    )
    return await PlanPipeline(tools=tools or FakeTools(), model_factory=script).regenerate(req, publish=publish)


def states(doc):
    return {k: v["state"] for k, v in doc["sectionStatus"].items()}


def calls(models):
    return {role: len(m.requests) for role, m in models.models.items() if m.requests}


async def test_warmup_change_with_the_same_minutes_changes_only_the_warmup():
    """Done-when check: a warm-up refinement changes only the warm-up."""
    before = await built_plan()
    new = shorter_warmup(before["warmup"]["minutes"])
    models = ReplayModels({"warmup": [new]})
    tools = FakeTools()
    doc = await refine(before, "warmup", "Use bands", models, tools=tools)

    Plan.model_validate({k: v for k, v in doc.items() if k not in ("agentRuns", "orchestrationLatencyMs")})
    assert doc["revision"] == 2
    assert doc["warmup"]["blocks"][0]["name"] == "Band pull-apart game"
    for key in ("wod", "parts", "timeline", "input", "createdAt", "planId", "coachId"):
        assert doc[key] == before[key], key
    assert doc["sectionStatus"] == before["sectionStatus"]
    # The timeline model is never built a request: the minutes did not change.
    assert calls(models) == {"warmup": 1}
    assert [n for n, _ in tools.calls] == ["get_recent_coaching_history"]
    assert [r["section"] for r in doc["agentRuns"]] == ["warmup"]


async def test_the_warmup_prompt_has_the_current_warmup_the_instruction_and_the_comment():
    before = await built_plan(comment="game-like warmup please")
    models = ReplayModels({"warmup": [shorter_warmup(before["warmup"]["minutes"])]})
    await refine(before, "warmup", "Less jumping. No equipment", models)
    prompt = models.models["warmup"].instructions()[0]
    assert "The coach's change: Less jumping. No equipment" in prompt
    assert "Current warm-up: " in prompt and "Partner mirror game" in prompt
    assert "Coach's comment when the plan was built: game-like warmup please" in prompt


async def test_warmup_with_other_minutes_rebuilds_the_timeline():
    """Done-when check: ...plus the timeline if the minutes changed."""
    before = await built_plan()
    models = ReplayModels({"warmup": [shorter_warmup(10)], "timeline": [timeline_for(10)]})
    tools = FakeTools()
    doc = await refine(before, "warmup", "Shorter", models, tools=tools)

    assert doc["warmup"]["minutes"] == 10
    assert states(doc)["timeline"] == "done"
    assert calls(models) == {"warmup": 1, "timeline": 1}
    assert "Warm-up: 10 minutes." in models.models["timeline"].instructions()[0]
    assert [n for n, _ in tools.calls] == ["get_recent_coaching_history", "verify_timeline_math"]
    assert [r["section"] for r in doc["agentRuns"]] == ["warmup", "timeline"]
    for key in ("wod", "parts"):
        assert doc[key] == before[key]


async def test_a_timeline_that_keeps_the_old_warmup_minutes_is_repaired():
    """A rebuilt timeline must give the warm-up its new minutes."""
    before = await built_plan()
    models = ReplayModels({"warmup": [shorter_warmup(10)], "timeline": [recording()["timeline"][0], timeline_for(10)]})
    doc = await refine(before, "warmup", "Shorter", models)
    assert states(doc)["timeline"] == "done"
    assert 'the warm-up blocks (section "warmup") take 12 minutes, the warm-up is 10 minutes' in models.models["timeline"].instructions()[1]
    warm = [b for b in doc["timeline"]["blocks"] if b.get("section") == "warmup"][0]
    assert warm["endMin"] - warm["startMin"] == 10


async def test_a_provisional_timeline_is_rebuilt_once_the_warmup_exists():
    before = await built_plan()
    before["timeline"]["provisional"] = True
    models = ReplayModels({"warmup": [shorter_warmup(before["warmup"]["minutes"])], "timeline": [recording()["timeline"][0]]})
    doc = await refine(before, "warmup", "Simpler", models)
    assert calls(models) == {"warmup": 1, "timeline": 1}
    assert doc["timeline"]["provisional"] is False


async def test_a_failed_warmup_change_keeps_the_timeline_marked_provisional():
    """Nothing is computed in place of the warm-up; the old timeline stays, marked provisional."""
    before = await built_plan()
    bad = dict(shorter_warmup(10), minutes="ten")
    models = ReplayModels({"warmup": [bad, copy.deepcopy(bad)]})
    doc = await refine(before, "warmup", "Shorter", models)

    assert states(doc)["warmup"] == "failed" and "warmup" not in doc
    assert "minutes: Input should be a valid integer" in doc["sectionStatus"]["warmup"]["error"]
    assert states(doc)["timeline"] == "done" and doc["timeline"]["provisional"] is True
    assert doc["timeline"]["blocks"] == before["timeline"]["blocks"]
    assert calls(models) == {"warmup": 2}


async def test_scaling_change_leaves_every_other_section():
    before = await built_plan()
    parts = recording()["scaling"][0]
    parts["parts"][0]["tiers"]["senior"] = {"line": "Goblet squat 5x5, no jumping"}
    models = ReplayModels({"scaling": [parts]})
    doc = await refine(before, "parts", "Less jumping", models)
    assert doc["parts"][0]["tiers"]["senior"]["line"] == "Goblet squat 5x5, no jumping"
    for key in ("wod", "warmup", "timeline"):
        assert doc[key] == before[key]
    assert calls(models) == {"scaling": 1}


async def test_timeline_change_is_checked_by_mcp_rust_and_fails_visibly_when_it_is_down():
    before = await built_plan()
    models = ReplayModels({"timeline": [recording()["timeline"][0]]})
    doc = await refine(before, "timeline", "Longer cool-down", models, tools=FakeTools({"verify_timeline_math": mcp_down}))
    assert states(doc)["timeline"] == "failed" and "timeline" not in doc
    assert "mcp-rust unreachable" in doc["sectionStatus"]["timeline"]["error"]
    assert doc["warmup"] == before["warmup"]


async def test_an_injury_can_be_added_to_a_plan_without_injuries():
    """Plan 2.1, "Add an injury": the Injury agent alone, with the stored plan."""
    before = await built_plan()
    assert before["sectionStatus"]["injuries"]["state"] == "skipped"
    answer = {"injuries": [{"label": "Knee", "returnNote": None, "warmup": "Bike instead of the shuffles",
                            "parts": [{"partId": "A", "suggestion": "Box squat to a pain-free depth"}]}]}
    models = ReplayModels({"injury": [answer]})
    doc = await refine(before, "injuries", "Add an injury: sore knee", models)

    assert states(doc)["injuries"] == "done"
    knee = doc["injuries"][0]
    assert (knee["label"], knee["fromComment"]) == ("Knee", "Add an injury: sore knee")
    assert knee["caution"] == "Suggestion only. Check with the athlete; stop on pain."
    assert "returnNote" not in knee
    prompt = models.models["injury"].instructions()[0]
    assert "There is no injury advice yet; build it." in prompt


async def test_injuries_that_stay_keep_the_words_of_the_comment():
    before = await built_plan("senior-two-injuries-60.json", comment=TWO_INJURIES)
    kept = copy.deepcopy(before["injuries"][0])
    answer = {"injuries": [{"label": "Ankle", "returnNote": "returning after 2 weeks", "warmup": kept["warmup"], "parts": kept["parts"]}]}
    models = ReplayModels({"injury": [answer]})
    doc = await refine(before, "injuries", "The shoulder athlete is not coming", models)
    assert [i["label"] for i in doc["injuries"]] == ["Ankle"]
    assert doc["injuries"][0]["fromComment"] == kept["fromComment"]
    assert doc["today"] == before["today"]


async def test_a_duplicate_injury_is_repaired():
    before = await built_plan("senior-two-injuries-60.json", comment=TWO_INJURIES)
    one = {"label": "Ankle", "returnNote": None, "warmup": "Bike", "parts": []}
    good = {"injuries": [one]}
    models = ReplayModels({"injury": [{"injuries": [one, dict(one, label="ankle")]}, good]})
    doc = await refine(before, "injuries", "Only the ankle today", models)
    assert states(doc)["injuries"] == "done"
    assert "injuries: Ankle is listed twice" in models.models["injury"].instructions()[1]


async def test_events_carry_the_new_revision_and_the_section_disappears_while_it_runs():
    before = await built_plan()
    events = []
    models = ReplayModels({"warmup": [shorter_warmup(10)], "timeline": [timeline_for(10)]})
    await refine(before, "warmup", "Shorter", models, events=events, revision=3)
    assert [(e["section"], e["state"]) for e in events] == [
        ("warmup", "running"), ("warmup", "done"), ("timeline", "running"), ("timeline", "done"),
    ]
    for e in events:
        PlanEvent.model_validate(e)
        Plan.model_validate(e["plan"])
        assert e["revision"] == 3 and e["plan"]["revision"] == 3
    assert "warmup" not in events[0]["plan"] and "timeline" not in events[2]["plan"]


async def test_a_plan_whose_wod_failed_cannot_be_refined():
    before = await built_plan()
    before["sectionStatus"]["wod"] = {"state": "failed", "error": "overloaded"}
    with pytest.raises(InputError, match="build the plan again"):
        await refine(before, "warmup", "Shorter", ReplayModels({}))


async def test_the_stored_plan_must_match_the_request():
    before = await built_plan()
    req = RegenerateRequest(planId="pl_other", coachId=before["coachId"], revision=2, section="parts",
                            instruction="Simpler", plan=before, routingConfig=ROUTES)
    with pytest.raises(InputError, match="another plan or coach"):
        await PlanPipeline(tools=FakeTools(), model_factory=ReplayModels({})).regenerate(req)


def test_the_parser_cannot_be_refined():
    with pytest.raises(ValueError):
        RegenerateRequest(planId="p", coachId="c", revision=2, section="wod", instruction="x", plan={}, routingConfig=ROUTES)
