# Agents (`services/agents-python`): rules

`agents-python` (`:8081`) builds class plans with Google ADK (decision D4), LiteLLM and the mcp-rust tools. Python 3.12, FastAPI. The gateway calls it; nothing else does.

## 1. Layout

- `app/main.py`: FastAPI app. `GET /health`, `GET /api/v1/topology`, `POST /api/v1/orchestrate` (internal token required). Publishes section events to Redis for the gateway's SSE endpoint.
- `app/pipeline.py`: builds the ADK `Workflow` per request and assembles the plan document.
- `app/agents/sections.py`: one `SectionAgent` per plan section. It runs the section's `LlmAgent`, checks the answer, asks for one repair, or marks the section failed.
- `app/agents/prompts.py`: the instructions. `app/agents/outputs.py`: what each agent answers, built from the contract types. `app/agents/schema_for_model.py`: the schema sent to the model.
- `app/llm.py`: routes (provider, model, key per role, from the gateway's `routingConfig`) and the `LiteLlm` factory.
- `app/mcp_tools.py`: the mcp-rust tools through ADK's `McpToolset` (Streamable HTTP).
- `app/contract/`: generated from `schema/` (do not edit), plus `rules.py`, the cross-field rules.
- `app/models.py`: the request the gateway sends today; replaced by the contract's `CreatePlanRequest` in Phase 2 step 10.
- `tests/`: `test_pipeline.py` (pipeline with recorded answers), `test_wiring.py` (LiteLlm, MCP results, endpoint), `test_contract.py` (shared examples). Recorded answers in `tests/recordings/`.

## 2. Pipeline

```
START -> WodParserAgent -> WarmupPlannerAgent --+
                        -> ScalingAgent       --+-> TimelineAgent
```

- ADK `Workflow` (2.14.1 deprecates `SequentialAgent`/`ParallelAgent`; capability report, decision 5.1). Warm-up and Scaling run in parallel.
- One `LlmAgent` per role, each with its own `LiteLlm(model, api_key=...)` built for this request. Keys never go into `os.environ`.
- Only the parser sees the screenshot. The others get earlier sections from session state as JSON.
- The pipeline calls the mcp-rust tools itself (decision 5.2): `get_recent_coaching_history` before the warm-up, `verify_timeline_math` after the timeline answer.
- The Injury agent and the "today" groups come in step 9; until then `sectionStatus.injuries` is `skipped`.

## 3. Checks, repair and failure (D3)

1. The model gets a relaxed schema (structure, enums, required fields; limits written as descriptions). Gemini rejects the full contract schema as too complex for constrained decoding.
2. The answer is validated against the full contract model, then `app/contract/rules.py`, then (timeline) `verify_timeline_math`.
3. Any problem goes back to the model once, listed in the instruction. A second failure fails the section with the reasons.
4. A provider error or an MCP tool error (unreachable, or `isError: true`) fails the section at once, without retry.
5. A section that needs the WOD fails when the parser failed. Without a warm-up, the timeline reserves 12 minutes and is marked `provisional`.
6. Never fill a section with local content. Tests use recorded answers in `tests/`, never fallback code in `app/`.
7. Errors shown to the coach are at most 300 characters and never contain the key.

## 4. Commands

```bash
pip install --require-hashes -r requirements.txt -r requirements-dev.txt
pytest -v                                   # no key, no network
MCP_TEST_URL=http://localhost:8082 pytest   # also against a running mcp-rust
uvicorn app.main:app --host 0.0.0.0 --port 8081
```

Dependencies: edit `requirements.in`, then `uv pip compile requirements.in --python-version 3.12 --generate-hashes -o requirements.txt`. `google-adk`, `litellm` and `mcp` are pinned and move together.
