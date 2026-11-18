"""The real wiring around the pipeline: LiteLlm with the coach's key, MCP results, the
HTTP endpoint. LiteLLM's network call is replaced by a spy client; nothing leaves the box."""

from __future__ import annotations

import os

import litellm
import pytest
from fastapi import HTTPException
from google.adk.models.lite_llm import LiteLlm, LiteLLMClient

from app import main
from app.llm import Route, resolve_route
from app.mcp_tools import McpTools, ToolError, tool_result
from app.models import EphemeralAgentRoute
from app.pipeline import PlanPipeline
from tests.fakes import FakeTools, load_recording, request



class SpyClient(LiteLLMClient):
    """Records what LiteLLM would send and answers from the recording."""

    def __init__(self, answers):
        super().__init__()
        self.answers = answers
        self.calls = []

    async def acompletion(self, model, messages, tools, **kwargs):
        import json

        self.calls.append({"model": model, "messages": messages, "kwargs": kwargs})
        role = {"WodParserAgent": "parser", "WarmupPlannerAgent": "warmup", "ScalingAgent": "scaling", "TimelineAgent": "timeline"}
        system = messages[0]["content"] if messages and messages[0]["role"] == "system" else ""
        name = next(n for n in role if f'"{n}"' in str(system))
        answer = self.answers[role[name]].pop(0)
        return litellm.ModelResponse(choices=[{"message": {"role": "assistant", "content": json.dumps(answer)}}])


async def test_litellm_gets_the_key_per_call_and_the_image_as_data_uri():
    spy = SpyClient(load_recording("build-engine-60.json"))

    def factory(route: Route):
        return LiteLlm(model=route.model, api_key=route.api_key, llm_client=spy)

    doc = await PlanPipeline(tools=FakeTools(), model_factory=factory).run(request())
    assert doc["sectionStatus"]["timeline"]["state"] == "done"
    assert {c["kwargs"]["api_key"] for c in spy.calls} == {"test-key-123"}
    assert all(c["kwargs"]["response_format"] for c in spy.calls)
    parser_call = spy.calls[0]
    user = [m for m in parser_call["messages"] if m["role"] == "user"][0]["content"]
    assert any(p.get("type") == "image_url" and p["image_url"]["url"].startswith("data:image/png;base64,") for p in user)
    # The key never reaches the process environment.
    assert "test-key-123" not in "".join(os.environ.values())


def test_route_repr_hides_the_key():
    route = resolve_route("parser", {"parser": EphemeralAgentRoute(provider="gemini", model="gemini-3.8-flash", apiKey="AIzaSyD-Secret999")})
    assert route.model == "gemini/gemini-3.8-flash"
    assert "Secret999" not in repr(route)
    assert "Secret999" not in repr(EphemeralAgentRoute(provider="gemini", model="m", apiKey="AIzaSyD-Secret999"))


def test_tool_result_shapes():
    assert tool_result("t", {"content": [{"type": "text", "text": "{}"}], "structuredContent": {"ok": 1}}) == {"ok": 1}
    assert tool_result("t", {"content": [{"type": "text", "text": '{"ok": 2}'}]}) == {"ok": 2}
    with pytest.raises(ToolError, match="expected u32"):
        tool_result("verify_timeline_math", {"content": [{"type": "text", "text": "Invalid arguments: expected u32"}], "isError": True})
    with pytest.raises(ToolError, match="not JSON"):
        tool_result("t", {"content": [{"type": "text", "text": "oops"}]})


async def test_mcp_unreachable_is_a_tool_error():
    tools = McpTools(url="http://127.0.0.1:9", timeout_sec=1)
    with pytest.raises(ToolError, match="unreachable"):
        await tools.call("verify_timeline_math", {"blocks": []})
    await tools.close()


@pytest.mark.skipif(not os.getenv("MCP_TEST_URL"), reason="set MCP_TEST_URL to a running mcp-rust")
async def test_real_mcp_rust():
    tools = McpTools(url=os.environ["MCP_TEST_URL"])
    ok = await tools.call("verify_timeline_math", {"totalMinutes": 60, "blocks": [{"startMin": 0, "endMin": 60, "label": "WOD"}]})
    assert ok["feasibilityStatus"] == "OPTIMAL"
    with pytest.raises(ToolError, match="expected u32"):
        await tools.call("verify_timeline_math", {"totalMinutes": 60, "blocks": [{"startMin": -1, "endMin": 60, "label": "WOD"}]})
    hist = await tools.call("get_recent_coaching_history", {"coachId": "nobody", "limit": 5})
    assert hist["count"] == 0
    await tools.close()


async def test_endpoint_checks_the_internal_token():
    with pytest.raises(HTTPException) as exc:
        await main.orchestrate(request(), x_internal_gateway_token="wrong")
    assert exc.value.status_code == 403


async def test_endpoint_turns_bad_input_into_400(monkeypatch):
    with pytest.raises(HTTPException) as exc:
        await main.orchestrate(request(image="data:image/gif;base64,R0lG"), x_internal_gateway_token=main.INTERNAL_GATEWAY_TOKEN)
    assert exc.value.status_code == 400 and "unsupported image type" in exc.value.detail


async def test_health():
    data = await main.health_check()
    assert data["status"] == "ok" and data["mcpRustUrl"].endswith("/mcp")
