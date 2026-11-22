# Agents (`services/agents-python`): rules

`agents-python` (`:8081`) builds class plans with Google ADK (decision D4), LiteLLM and the mcp-rust tools. Python 3.12, FastAPI. The gateway calls it; nothing else does.

## 1. Layout

- `app/main.py`: FastAPI app. `GET /health`, `GET /api/v1/topology`, `POST /api/v1/orchestrate` and `POST /api/v1/regenerate` (internal token required; the gateway calls both in the background after answering the coach with 202). Publishes section events to Redis for the gateway.
- `app/pipeline.py`: builds the ADK `Workflow` per request (`run` for a new plan, `regenerate` for one section) and assembles the plan document. `Progress` publishes the snapshots.
- `app/agents/sections.py`: one `SectionAgent` per plan section. It runs the section's `LlmAgent`, checks the answer, asks for one repair, or marks the section failed.
- `app/agents/prompts.py`: the instructions (Parser, Warm-up, Scaling, Injury, Timeline). `app/agents/outputs.py`: what each agent answers, built from the contract types. `app/agents/schema_for_model.py`: the schema sent to the model.
- `app/llm.py`: routes (provider, model, key per role, from the gateway's `routingConfig`) and the `LiteLlm` factory.
- `app/mcp_tools.py`: the mcp-rust tools through ADK's `McpToolset` (Streamable HTTP).
- `app/contract/`: generated from `schema/` (do not edit), plus `rules.py`, the cross-field rules.
- `app/models.py`: the internal requests. Orchestrate: `planId`, `coachId`, `imageRef`, `createdAt` (all set by the gateway), `input` (the contract's `CreatePlanRequest`) and `routingConfig` (decrypted keys, so never in the public schema). Regenerate: `planId`, `coachId`, `revision` (the new one), `section`, `instruction`, `plan` (the stored plan at the revision before) and `routingConfig`.
- `tests/`: `test_pipeline.py` (pipeline with recorded answers), `test_refine.py` (refinement), `test_wiring.py` (LiteLlm, MCP results, endpoints), `test_contract.py` (shared examples). Recorded answers in `tests/recordings/`.

## 2. Pipeline

```
START -> WodParserAgent -> WarmupPlannerAgent --+           +-> InjuryAgent (only with injuries)
                        -> ScalingAgent       --+-> join  --+-> TimelineAgent
```

- ADK `Workflow` (2.14.1 deprecates `SequentialAgent`/`ParallelAgent`; capability report, decision 5.1). Warm-up and Scaling run in parallel.
- One `LlmAgent` per role, each with its own `LiteLlm(model, api_key=...)` built for this request. Keys never go into `os.environ`.
- Only the parser sees the screenshot. The others get earlier sections from session state as JSON.
- The pipeline calls the mcp-rust tools itself (decision 5.2): `get_recent_coaching_history` before the warm-up, `verify_timeline_math` after the timeline answer.
- The parser also reads the comment: `today` (who needs attention, mapped to a tier row) and one injury mention per injury (label, the comment's words, a return note). No names.
- Warm-up and Scaling see the `today` groups. The Injury agent sees the WOD, the warm-up (or "not planned yet") and the mentions, and writes one warm-up line and per-part suggestions per injury.
- The pipeline, not the model, fills each injury's `label`, `fromComment`, `returnNote` and the fixed `caution` ("Suggestion only. Check with the athlete; stop on pain."). The model's entries must match the mentions in order.
- No injuries in the comment: `sectionStatus.injuries` is `skipped` and the Injury agent is not called.
- Events: every section change (`running`, `done`, `failed`, `skipped`) publishes a `PlanEvent` with `plan`, the plan as it stands, validated as a `Plan`. Publishing is serialised, so a later snapshot never lacks a section an earlier one had. The gateway stores the snapshots, saves the returned plan, and sends the closing `plan` event itself; agents-python never sends it.

### Refinement (step 11)

```
START -> one SectionAgent (warmup, parts, injuries or timeline)
warm-up change: WarmupPlannerAgent --+-> InjuryAgent (new warm-up lines only)
                                     +-> TimelineAgent (only when the minutes changed)
```

- The stored plan goes into session state; the section being changed is removed from it and shown to the model as "current", with the coach's instruction and the comment the plan was built from. The parser never runs again (the gateway refuses `wod`).
- A retry of a failed section adds "The previous attempt at the ... failed: <reason>" to the prompt, so a bare "Try again" does not repeat the same attempt blind.
- After a warm-up change two sections follow, in parallel. The Injury section writes one new warm-up line per stored injury (`InjuryWarmupLinesOut`: label and line, same labels and order); the pipeline keeps everything else in each injury as stored. The Timeline section rebuilds only when the warm-up minutes changed, or the old timeline was provisional or missing. Either raises `SectionKept` when it has nothing to do (no injuries, same minutes) or when the new warm-up failed: the stored section and its state stay, with no model call. A failed new warm-up also marks the kept timeline `provisional`.
- Injuries changed on request answer with their labels (`InjuriesRefineOut`), so the coach can add or drop one ("Add an injury: sore knee"). An injury that stays keeps its `fromComment`; a new one gets the instruction as `fromComment`.
- Events carry the new revision. A section that starts again loses its content in the snapshots until it is done, as in a new plan.

### Session state

- Keys: `wod`, `warmup`, `parts`, `injuries`, `timeline` (final values), `draft:<section>` (the model's answer), `repair:<section>` (reasons for the retry), `status:<section>`, `run:<section>` (trace). The parser also writes `today` and `injuryMentions`. Refinement adds `refine` (section, instruction, comment, current value) and `kept:<section>`.
- ADK copies agent nodes, so per-request data goes through session state, never through fields on the agents (a shared list on the agents came back empty).

## 3. Checks, repair and failure (D3)

1. The model gets a relaxed schema (structure, enums, required fields; limits written as descriptions). Gemini rejects the full contract schema as too complex for constrained decoding.
2. The answer is validated against the full contract model, then `app/contract/rules.py`, then (timeline) `verify_timeline_math`.
3. Any problem goes back to the model once, listed in the instruction. A second failure fails the section with the reasons.
4. A provider error or an MCP tool error (unreachable, or `isError: true`) fails the section at once, without retry.
5. A section that needs the WOD fails when the parser failed. Without a warm-up, the timeline reserves 12 minutes and is marked `provisional`. The timeline's warm-up blocks must add up to the warm-up minutes (or the 12 reserved); otherwise it goes back for repair.
6. Never fill a section with local content. Tests use recorded answers in `tests/`, never fallback code in `app/`.
7. Errors shown to the coach are at most 300 characters and never contain the key.
8. Fields an agent must always decide (the parser's `today`, `injuries`, `warmupWishes`) are required in its output model, empty allowed. An optional field with a default can be left out by the model and then reads as "nothing" (seen live with Gemini, 2026-11-20).

## 4. Tests

- `tests/fakes.py`: `ReplayModels` (recorded answers per role) and `FakeTools` (mcp-rust).
- End to end without live calls (not a script in the repo): run this app with `PlanPipeline(tools=FakeTools(), model_factory=...)` swapped in, against real Postgres, MongoDB and Redis containers and a built gateway binary.
- `ReplayLlm` copies its answer list, so a second request built from the same `ReplayModels` starts again from the first answer. Give each request its own script, as `test_refine.py` does. For several requests in one run against a real gateway, the model factory must hand out answers in order across requests.

## 5. Commands

```bash
pip install --require-hashes -r requirements.txt -r requirements-dev.txt
pytest -v                                   # no key, no network
MCP_TEST_URL=http://localhost:8082 pytest   # also against a running mcp-rust
uvicorn app.main:app --host 0.0.0.0 --port 8081
```

Dependencies: edit `requirements.in`, then `uv pip compile requirements.in --python-version 3.12 --generate-hashes -o requirements.txt`. `google-adk`, `litellm` and `mcp` are pinned and move together. `litellm==1.109.1` is above ADK 2.14.1's declared extras range (`<1.106.3`); check ADK's release notes before the next bump.
