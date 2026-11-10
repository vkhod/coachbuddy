from __future__ import annotations

import asyncio
import base64
from typing import Any, Dict, List
import pytest

from fastapi import HTTPException

from app.adk_pipeline import (
    CoachBuddyPipelineExecutor,
    build_multimodal_user_content,
    resolve_agent_route,
)
from app.main import (
    INTERNAL_GATEWAY_TOKEN,
    health_check,
    mcp_client,
    orchestrate_wod_session,
)
from app.models import EphemeralAgentRoute, OrchestrateRequest, SessionInput


def _run_sync(coro: Any) -> Any:
    """Drives a self-contained coroutine to completion without OS loopback sockets."""
    try:
        while True:
            coro.send(None)
    except StopIteration as stop:
        return stop.value
    finally:
        coro.close()


def test_health_endpoint() -> None:
    data = _run_sync(health_check())
    assert data["status"] == "ok"
    assert data["service"] == "agents-python"
    assert data["adkAvailable"] is True


def test_secret_str_masks_api_key_in_logs_and_repr() -> None:
    route = EphemeralAgentRoute(
        provider="gemini",
        model="gemini/gemini-3.8-flash",
        apiKey="AIzaSyD-TopSecretCoachKey999",  # type: ignore[arg-type]
    )
    # SecretStr must never leak raw key in repr() or str()
    assert "AIzaSyD-TopSecretCoachKey999" not in repr(route)
    assert "AIzaSyD-TopSecretCoachKey999" not in str(route)

    # resolve_agent_route unwraps SecretStr only when needed in memory
    prov, mod, key = resolve_agent_route("parser", {"parser": route})
    assert prov == "gemini"
    assert mod == "gemini/gemini-3.8-flash"
    assert key == "AIzaSyD-TopSecretCoachKey999"


def test_orchestrate_rejects_missing_internal_gateway_token() -> None:
    req = OrchestrateRequest(
        sessionId="wod-unauth",
        coachId="coach-1",
        input=SessionInput(rawText="21-15-9 Thrusters and Pull-Ups"),
    )
    with pytest.raises(HTTPException) as exc_info:
        _run_sync(orchestrate_wod_session(req, x_internal_gateway_token="wrong-token"))
    assert exc_info.value.status_code == 403


def test_todo_3_1_build_multimodal_user_content_with_data_uri() -> None:
    """Tests TODO(engineer) 3.1: decoding base64 image with data URI prefix."""
    fake_png_bytes = b"\x89PNG\r\n\x1a\nCoachBuddyWhiteboard"
    b64_str = base64.b64encode(fake_png_bytes).decode("ascii")
    data_uri = f"data:image/png;base64,{b64_str}"

    session_input = SessionInput(
        rawText="21-15-9 Thrusters & Pull-Ups",
        imageBase64=data_uri,
        coachConstraints="Use broomstick game",
        targetDurationMinutes=60,
    )

    result = build_multimodal_user_content(session_input)
    assert result["has_image"] is True, "Expected has_image=True when imageBase64 is provided"
    assert result["mime_type"] == "image/png", f"Expected mime_type='image/png', got {result['mime_type']}"
    assert result["image_bytes"] == fake_png_bytes, "Decoded bytes did not match original PNG bytes"


def test_todo_3_1_build_multimodal_user_content_text_only() -> None:
    session_input = SessionInput(
        rawText="5x5 Back Squat",
        imageBase64="",
        coachConstraints="Partner warmup",
        targetDurationMinutes=60,
    )
    result = build_multimodal_user_content(session_input)
    assert result["has_image"] is False
    assert result["image_bytes"] is None


def test_todo_3_2_pipeline_emits_step_events_and_validates_models(monkeypatch: pytest.MonkeyPatch) -> None:
    """Tests TODO(engineer) 3.2: 3-stage sequential/parallel execution & SSE event emission."""
    emitted_events: List[Dict[str, Any]] = []

    async def capture_event(_session_id: str, _step: str, payload: Dict[str, Any]) -> None:
        emitted_events.append(payload)

    executor = CoachBuddyPipelineExecutor()

    async def fast_mock_call_tool(tool_name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        return executor.mcp_client._local_fallback(tool_name, arguments)

    monkeypatch.setattr(executor.mcp_client, "call_tool", fast_mock_call_tool)

    req = OrchestrateRequest(
        sessionId="wod-events-002",
        coachId="coach-002",
        input=SessionInput(
            title="Tuesday Heavy Squat + Fran",
            rawText=(
                "Part A) Every 2:30 x 5 Sets: 3 Front Squats @ 82% 1RM\n"
                "Part B) 'Fran' - 21-15-9 For Time (Cap 12 min):\n"
                "- Thrusters (95/65 lb)\n"
                "- Pull-Ups"
            ),
            coachConstraints="use a broomstick warmup game",
            targetDurationMinutes=60,
        ),
    )

    doc = _run_sync(executor.execute(req, publish_event=capture_event))

    # 1. Verify SSE steps emitted (running + completed for parser, warmup, scaling, timeline, and final completed)
    steps = [e["step"] for e in emitted_events]
    assert "parser" in steps
    assert "warmup" in steps
    assert "scaling" in steps
    assert "timeline" in steps
    assert steps[-1] == "completed"

    # 2. Verify structured output fields from Phase 3 models
    assert doc["sessionId"] == "wod-events-002"
    assert doc["title"] == "Tuesday Heavy Squat + Fran"
    assert "alternativeGames" in doc["warmup"]
    assert len(doc["warmup"]["alternativeGames"]) >= 1
    assert len(doc["warmup"]["drillNames"]) >= 3
    assert doc["timeline"]["feasibilityStatus"] == "OPTIMAL"
    assert doc["timeline"]["totalMinutes"] == 60
    for trace in doc["agentTraces"]:
        assert isinstance(trace["latencyMs"], int)
        assert trace["latencyMs"] >= 0


def test_orchestrate_endpoint_full_pipeline(monkeypatch: pytest.MonkeyPatch) -> None:
    async def fast_mock_call_tool(tool_name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        return mcp_client._local_fallback(tool_name, arguments)

    monkeypatch.setattr(mcp_client, "call_tool", fast_mock_call_tool)

    req = OrchestrateRequest(
        sessionId="wod-test-001",
        coachId="coach-uuid-001",
        input=SessionInput(
            title="Thursday Olympic & Gymnastics Burner",
            rawText=(
                "A) Every 2:00 x 6: 2 Power Cleans + 1 Push Press\n"
                "B) 14 min AMRAP:\n"
                "  12 Thrusters (95/65)\n"
                "  12 Toes-to-Bar\n"
                "  12 Box Jumps (24/20)"
            ),
            coachConstraints="Today I want to use mostly a broomstick for a warmup game",
            targetDurationMinutes=60,
            equipmentFocus=["pvc_broomstick"],
        ),
        routingConfig={
            "global": EphemeralAgentRoute(
                provider="gemini",
                model="gemini/gemini-3.8-flash",
                apiKey="test-key",  # type: ignore[arg-type]
            ),
        },
    )

    doc = _run_sync(orchestrate_wod_session(req, x_internal_gateway_token=INTERNAL_GATEWAY_TOKEN))
    assert doc["sessionId"] == "wod-test-001"
    assert doc["status"] == "completed"
    assert len(doc["agentTraces"]) == 4
    assert doc["agentTraces"][0]["executionMode"] == "google-adk-litellm"
