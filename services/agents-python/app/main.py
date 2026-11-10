from __future__ import annotations

from contextlib import asynccontextmanager
import json
import logging
import os
from typing import Any, AsyncGenerator, Dict, Optional

from fastapi import FastAPI, Header, HTTPException
from fastapi.middleware.cors import CORSMiddleware

try:
    import redis.asyncio as aioredis
except Exception:  # pragma: no cover
    aioredis = None  # type: ignore

from app.adk_pipeline import (
    ADK_AVAILABLE,
    CoachBuddyPipelineExecutor,
)
from app.mcp_client import RustMcpClient
from app.models import OrchestrateRequest

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("coachbuddy.main")

INTERNAL_GATEWAY_TOKEN = os.getenv(
    "INTERNAL_GATEWAY_TOKEN",
    "coachbuddy-internal-orchestration-secret",
)


class EventStreamManager:
    """Manages Redis Pub/Sub (`session:events:{sessionId}`) and Replay List (`session:buffered_events:{sessionId}`).

    Note: Go Gateway (`gateway-go`) is the single authoritative writer to MongoDB `wod_sessions`.
    """

    def __init__(self) -> None:
        self.redis_url = os.getenv("REDIS_URL", "redis://localhost:6379/0")
        self.redis_client: Optional[Any] = None

    async def connect(self) -> None:
        if aioredis is not None:
            try:
                self.redis_client = aioredis.from_url(
                    self.redis_url,
                    decode_responses=True,
                    socket_connect_timeout=2.0,
                )
                logger.info("Configured Redis Pub/Sub & Replay client for %s", self.redis_url)
            except Exception as exc:
                logger.warning("Redis client init skipped (%s)", exc)

    async def close(self) -> None:
        if self.redis_client is not None:
            try:
                await self.redis_client.aclose()
            except Exception:
                pass

    async def publish_step_event(
        self,
        session_id: str,
        step: str,
        payload: Dict[str, Any],
    ) -> None:
        if self.redis_client is None:
            return
        channel = f"session:events:{session_id}"
        buffer_key = f"session:buffered_events:{session_id}"
        encoded = json.dumps(payload)
        try:
            # Buffer in Redis list (with 5-min TTL) so late-connecting SSE clients replay missed steps
            await self.redis_client.rpush(buffer_key, encoded)
            await self.redis_client.expire(buffer_key, 300)
            await self.redis_client.publish(channel, encoded)
        except Exception as exc:
            logger.debug("Redis publish to %s skipped (%s)", channel, exc)


event_stream = EventStreamManager()
mcp_client = RustMcpClient()
pipeline_executor = CoachBuddyPipelineExecutor(mcp_client=mcp_client)


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncGenerator[None, None]:
    await event_stream.connect()
    yield
    await event_stream.close()


app = FastAPI(
    title="CoachBuddy Multi-Agent Orchestrator",
    description=(
        "Google ADK + LiteLLM + Rust MCP Multi-Agent Orchestrator for CrossFit Coaches. "
        "Coordinates WodParserAgent, Parallel(WarmupPlannerAgent, ScalingAndCuesAgent), "
        "and TimelineLogisticsAgent with per-agent BYOK SecretStr routing."
    ),
    version="1.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health_check() -> Dict[str, Any]:
    return {
        "status": "ok",
        "service": "agents-python",
        "adkAvailable": True,
        "adkRuntimeImported": ADK_AVAILABLE,
        "mcpRustUrl": mcp_client.base_url,
        "topology": [
            "WodParserAgent",
            "ParallelCoachingStage(WarmupPlannerAgent, ScalingAndCuesAgent)",
            "TimelineLogisticsAgent",
        ],
    }


@app.get("/api/v1/topology")
async def get_agent_topology() -> Dict[str, Any]:
    """Returns metadata describing the 3-stage Google ADK multi-agent hierarchy."""
    return {
        "rootAgent": "CoachBuddyOrchestrator",
        "framework": "google-adk",
        "modelRouter": "litellm",
        "adkImported": ADK_AVAILABLE,
        "stages": [
            {
                "order": 1,
                "type": "LlmAgent",
                "name": "WodParserAgent",
                "roleKey": "parser",
                "outputKey": "parsed_wod",
            },
            {
                "order": 2,
                "type": "ParallelAgent",
                "name": "ParallelCoachingStage",
                "subAgents": [
                    {
                        "type": "LlmAgent",
                        "name": "WarmupPlannerAgent",
                        "roleKey": "warmup",
                        "mcpTool": "get_recent_coaching_history",
                        "outputKey": "warmup_plan",
                    },
                    {
                        "type": "LlmAgent",
                        "name": "ScalingAndCuesAgent",
                        "roleKey": "scaling",
                        "outputKey": "scaling_and_cues",
                    },
                ],
            },
            {
                "order": 3,
                "type": "LlmAgent",
                "name": "TimelineLogisticsAgent",
                "roleKey": "timeline",
                "mcpTool": "verify_timeline_math",
                "outputKey": "class_timeline",
            },
        ],
    }


@app.post("/api/v1/orchestrate")
async def orchestrate_wod_session(
    req: OrchestrateRequest,
    x_internal_gateway_token: Optional[str] = Header(default=None),
) -> Dict[str, Any]:
    if x_internal_gateway_token != INTERNAL_GATEWAY_TOKEN:
        raise HTTPException(
            status_code=403,
            detail="Forbidden: invalid or missing X-Internal-Gateway-Token header.",
        )

    if not (req.input.rawText or "").strip() and not (req.input.imageBase64 or "").strip():
        raise HTTPException(
            status_code=400,
            detail="Either input.rawText or input.imageBase64 must be provided.",
        )

    try:
        session_doc = await pipeline_executor.execute(
            req=req,
            publish_event=event_stream.publish_step_event,
        )
        return session_doc
    except Exception as exc:
        err_detail = str(exc).strip() or f"{type(exc).__name__} (upstream LLM request timed out)"
        logger.exception("Orchestration failed for session %s: %s", req.sessionId, err_detail)
        await event_stream.publish_step_event(
            req.sessionId,
            "error",
            {
                "sessionId": req.sessionId,
                "step": "error",
                "status": "error",
                "message": err_detail,
            },
        )
        raise HTTPException(status_code=500, detail=f"Orchestration failed: {err_detail}") from exc
