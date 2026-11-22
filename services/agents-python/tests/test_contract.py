"""The shared examples in schema/examples are the same data every service tests against.

valid/ must pass the generated models and the cross-field rules. invalid/schema-* must be
rejected by the generated models. invalid/rule-* pass the models and must be caught by
app/contract/rules.py, except rule-timeline-gap, which only mcp-rust's
verify_timeline_math checks (tested in services/mcp-rust).
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from pydantic import ValidationError

from app.contract.create_plan_request import CreatePlanRequest
from app.contract.plan import Plan
from app.contract.plan_event import PlanEvent
from app.contract.rules import check_plan_rules

EXAMPLES = Path(__file__).resolve().parents[3] / "schema" / "examples"

# Expected message for each rule example, so a rule cannot pass by failing for another reason.
RULE_ERRORS = {
    "rule-cells-mismatch.json": "parts[C].tiers.scaled: 3 cells for 4 columns",
    "rule-rxplus-on-strength.json": "parts[A].tiers.rxPlus: RX+ is only for conditioning parts, A is strength",
    "rule-unknown-part.json": "injuries[0] (Ankle).parts: no WOD part with id D",
    "rule-warmup-minutes.json": "warmup: blocks add up to 12 minutes, minutes is 14",
    "rule-timeline-gap.json": None,  # checked by mcp-rust verify_timeline_math
}


def _files(name: str, kind: str) -> list[Path]:
    return sorted((EXAMPLES / name / kind).glob("*.json"))


def _load(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def test_examples_are_found() -> None:
    assert _files("plan", "valid"), f"no plan examples under {EXAMPLES}"
    assert {p.name for p in _files("plan", "invalid") if p.name.startswith("rule-")} == set(RULE_ERRORS)


@pytest.mark.parametrize("path", _files("plan", "valid"), ids=lambda p: p.name)
def test_valid_plans_pass_models_and_rules(path: Path) -> None:
    plan = Plan.model_validate(_load(path))
    assert check_plan_rules(plan) == []


@pytest.mark.parametrize("path", [p for p in _files("plan", "invalid") if p.name.startswith("schema-")], ids=lambda p: p.name)
def test_schema_breaking_plans_are_rejected_by_models(path: Path) -> None:
    with pytest.raises(ValidationError):
        Plan.model_validate(_load(path))


@pytest.mark.parametrize("path", [p for p in _files("plan", "invalid") if p.name.startswith("rule-")], ids=lambda p: p.name)
def test_rule_breaking_plans_pass_models_and_fail_rules(path: Path) -> None:
    plan = Plan.model_validate(_load(path))
    expected = RULE_ERRORS[path.name]
    errors = check_plan_rules(plan)
    if expected is None:
        assert errors == []
    else:
        assert errors == [expected]


def test_plan_round_trips_without_adding_fields() -> None:
    raw = _load(_files("plan", "valid")[0])
    dumped = Plan.model_validate(raw).model_dump(mode="json", exclude_none=True)
    assert dumped == raw


def test_create_plan_request_examples() -> None:
    for path in _files("create-plan.request", "valid"):
        CreatePlanRequest.model_validate(_load(path))
    for path in _files("create-plan.request", "invalid"):
        with pytest.raises(ValidationError):
            CreatePlanRequest.model_validate(_load(path))


def test_plan_event_examples_carry_a_valid_plan() -> None:
    for path in _files("plan-event", "valid"):
        event = PlanEvent.model_validate(_load(path))
        if event.plan is not None:
            Plan.model_validate(event.plan)
    for path in _files("plan-event", "invalid"):
        with pytest.raises(ValidationError):
            PlanEvent.model_validate(_load(path))
