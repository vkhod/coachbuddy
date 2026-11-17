# Python Multi-Agent Orchestrator — Agent Rules (`services/agents-python`)

This directory contains `agents-python` (`:8081`), the Python 3.12+ FastAPI service orchestrating CoachBuddy's multi-agent pipeline with **Google Agent Development Kit (ADK)**, **LiteLLM**, and the **Rust MCP Server** (`mcp-rust`).

---

## 1. Directory Layout

- `app/main.py`: FastAPI application (`/health`, `/api/v1/orchestrate`, `/orchestrate`).
- `app/adk_pipeline.py`: Multi-agent orchestration pipeline implementing the 3-stage `SequentialAgent` + `ParallelAgent` topology.
- `app/mcp_client.py`: Async JSON-RPC 2.0 client (`RustMcpClient`) calling `mcp-rust` (`http://localhost:8082/mcp`). Tool errors are raised to the caller; MCP tools are never re-implemented locally.
- `app/contract/`: Pydantic models generated from `schema/` by `scripts/gen-schema` (do not edit), plus `rules.py`, the hand-written cross-field rules. These models are the agents' output schemas.
- `app/models.py`: Strict Pydantic v2 models matching the cross-service `camelCase` JSON contract.
- `tests/test_orchestrator.py`: Pytest suite verifying end-to-end orchestration, MCP tool integration, timeline math, and 4-tier scaling outputs.

---

## 2. Multi-Agent Pipeline Topology & Rules

1. **Stage 1 — `WodParserAgent`**: Extracts workout blocks (`strength`, `skill`, `metcon`, `engine`), movement taxonomy, `primaryJoints`, and physiological `stimulus` from raw WOD text or multimodal whiteboard screenshots.
2. **Stage 2 — `ParallelCoachingStage` (`ParallelAgent`)**:
   - **`WarmupPlannerAgent`**: Calls `mcp-rust` tool `get_recent_coaching_history` (`coachId`, `limit=5`) so warm-up games are never repeated on consecutive days, producing a 2-phase warm-up (Phase 1: Reaction/Partner/Broomstick Game; Phase 2: Biomechanical PVC/joint prep).
   - **`ScalingAndCuesAgent`**: Produces `cues.external`, `cues.internal`, `cues.commonFaults` (`[{"fault", "fix"}]`), and 4-tier scaling (`rxPlus`, `scaled`, `seniorsMasters`, and `injured` with `wristOrShoulder`, `kneePain`, `lowerBack`).
3. **Stage 3 — `TimelineLogisticsAgent`**: Generates a contiguous `0..totalMinutes` class timeline and validates it via the `mcp-rust` tool `verify_timeline_math`.
4. **Single Execution Mode (No Fallback Content)**:
   - Every agent runs against a real LLM, bound per agent or globally to the coach's BYOK credentials (`gemini`, `openai`, `anthropic`, `groq`).
   - If an LLM call fails (timeout, provider error, or invalid JSON after one repair retry) or an MCP tool call fails, the stage reports `status: "failed"` with the reason. It never returns substitute content, and agent traces must report what actually ran.
   - Tests use recorded LLM responses under `tests/`, not live keys and not fallback code in `app/`.
   - The current `app/adk_pipeline.py` and `app/mcp_client.py` still contain fallbacks; they are removed during the redesign.

---

## 3. Commands

```bash
# Run test suite
pytest -v

# Start FastAPI server locally on :8081
uvicorn app.main:app --host 0.0.0.0 --port 8081 --reload
```
