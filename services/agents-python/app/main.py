from __future__ import annotations

from contextlib import asynccontextmanager
import hmac
import json
import logging
import os
from typing import Any, AsyncGenerator, Dict, Optional

from fastapi import FastAPI, Header, HTTPException
from fastapi.middleware.cors import CORSMiddleware
import redis.asyncio as aioredis

from app.llm import RouteError
from app.mcp_tools import McpTools
from app.models import OrchestrateRequest
from app.pipeline import InputError, PlanPipeline

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("coachbuddy.main")

# Shared secret the gateway sends as X-Internal-Gateway-Token. Required; there is no default.
INTERNAL_GATEWAY_TOKEN = os.getenv("INTERNAL_GATEWAY_TOKEN", "").strip()


class EventStream:
    """Section events for the gateway's SSE endpoint: a replay list plus pub/sub.

    Redis key names are the ones the gateway reads (session:events:<id>,
    session:buffered_events:<id>). The gateway is the only writer of plan documents.
    """

    def __init__(self) -> None:
        self.redis_url = os.getenv("REDIS_URL", "redis://localhost:6379/0")
        self.client: Optional[Any] = None

    async def connect(self) -> None:
        self.client = aioredis.from_url(self.redis_url, decode_responses=True, socket_connect_timeout=2.0)

    async def close(self) -> None:
        if self.client is not None:
            await self.client.aclose()

    async def publish(self, plan_id: str, payload: Dict[str, Any]) -> None:
        if self.client is None:
            return
        encoded = json.dumps(payload)
        try:
            await self.client.rpush(f"session:buffered_events:{plan_id}", encoded)
            # Long enough to cover a slow parser; a finished plan is read from the gateway.
            await self.client.expire(f"session:buffered_events:{plan_id}", 600)
            await self.client.publish(f"session:events:{plan_id}", encoded)
        except Exception as exc:
            # Progress events are not the record; the returned plan is. Log and go on.
            logger.warning("event for %s not published: %s", plan_id, exc)


event_stream = EventStream()
mcp_tools = McpTools()
pipeline = PlanPipeline(tools=mcp_tools)


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncGenerator[None, None]:
    if not INTERNAL_GATEWAY_TOKEN:
        raise RuntimeError("INTERNAL_GATEWAY_TOKEN is not set (see .env.example)")
    await event_stream.connect()
    yield
    await mcp_tools.close()
    await event_stream.close()


app = FastAPI(
    title="Coach Buddy agents",
    description="Plan pipeline on Google ADK: Parser, then Warm-up and Scaling in parallel, then Injury and Timeline.",
    version="2.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

TOPOLOGY = {
    "framework": "google-adk",
    "modelRouter": "litellm",
    "root": "PlanWorkflow",
    "nodes": [
        {"name": "WodParserAgent", "role": "parser", "section": "wod", "after": []},
        {"name": "WarmupPlannerAgent", "role": "warmup", "section": "warmup", "after": ["WodParserAgent"], "mcpTool": "get_recent_coaching_history"},
        {"name": "ScalingAgent", "role": "scaling", "section": "parts", "after": ["WodParserAgent"]},
        {"name": "InjuryAgent", "role": "injury", "section": "injuries", "after": ["WarmupPlannerAgent", "ScalingAgent"], "onlyWhen": "the comment names injuries"},
        {"name": "TimelineAgent", "role": "timeline", "section": "timeline", "after": ["WarmupPlannerAgent", "ScalingAgent"], "mcpTool": "verify_timeline_math"},
    ],
}


@app.get("/health")
async def health_check() -> Dict[str, Any]:
    return {"status": "ok", "service": "agents-python", "mcpRustUrl": mcp_tools.url}


@app.get("/api/v1/topology")
async def get_agent_topology() -> Dict[str, Any]:
    return TOPOLOGY


def _check_token(token: Optional[str]) -> None:
    if not INTERNAL_GATEWAY_TOKEN or not hmac.compare_digest((token or "").encode(), INTERNAL_GATEWAY_TOKEN.encode()):
        raise HTTPException(status_code=403, detail="Forbidden: invalid or missing X-Internal-Gateway-Token header.")


@app.post("/api/v1/orchestrate")
async def orchestrate(req: OrchestrateRequest, x_internal_gateway_token: Optional[str] = Header(default=None)) -> Dict[str, Any]:
    """Builds a plan and returns it with agentRuns. Called by the gateway in the background
    after it answered the coach with 202. Failed sections are reported in sectionStatus
    with the reason; the response is still 200, so the coach sees the sections that did work."""
    _check_token(x_internal_gateway_token)
    try:
        return await pipeline.run(req, publish=event_stream.publish)
    except (InputError, RouteError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
