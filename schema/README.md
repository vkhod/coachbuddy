# schema/: the contract

One JSON Schema (draft 2020-12) per payload is the source of truth for every service (decision D2).
The types in each service are generated from it and committed; nobody edits them by hand.

| File | What it describes |
|---|---|
| `plan.schema.json` | A class plan, as stored and returned by the gateway. Sections are absent until their agent finishes; `sectionStatus` says why. |
| `create-plan.request.schema.json` | `POST /api/v1/plans` |
| `regenerate-section.request.schema.json` | `POST /api/v1/plans/{id}/sections/{section}/regenerate` |
| `plan-event.schema.json` | One SSE event on `GET /api/v1/plans/{id}/events`. `plan` carries the plan as it stands; it is typed as an object because datamodel-code-generator cannot follow a reference into another file into one module, so agents-python and the gateway validate it as a `Plan`. |
| `plan-revision.schema.json` | One entry of `GET /api/v1/plans/{id}/revisions`: the revision number, what changed (create, regenerate with section and instruction, restore with the source revision), the sections it built, and its state. |
| `tools.schema.json` | Inputs and outputs of the mcp-rust tools. Optional tool inputs accept `null`, because a model fills them. |

## Changing the contract

1. Edit the schema.
2. Add or update an example under `examples/<schema>/valid/` or `examples/<schema>/invalid/`.
3. Run `scripts/gen-schema` from the repo root and commit the generated files with the schema change.

CI (`schema (contract)`) checks the examples against the schemas, regenerates the types, and fails if any generated file differs from what is committed.

## Generated files

| Service | File | Generator |
|---|---|---|
| pwa-nextjs | `web/pwa-nextjs/src/lib/contract/*.ts` | json-schema-to-typescript 16.0.0 |
| agents-python | `services/agents-python/app/contract/*.py` (except `rules.py`) | datamodel-code-generator 0.97.0, ruff 0.16.9 |
| gateway-go | `services/gateway-go/internal/contract/contract.go` | go-jsonschema v0.24.1 |
| mcp-rust | `services/mcp-rust/src/contract.rs` (tools only) | cargo-typify 0.10.1 |

Versions are pinned in `scripts/gen-schema`, `package.json` and `requirements-gen.txt`, because each one changes the output.
Local setup: `npm ci` in `schema/`, `pip install -r schema/requirements-gen.txt`, Go 1.27, Rust 1.97 and `cargo install cargo-typify --version 0.10.1 --locked`.

## Examples

Every service's tests load the same files, so all four languages are checked against the same data.

- `valid/`: accepted everywhere.
- `invalid/schema-*.json`: break the schema; the generated types reject them.
- `invalid/rule-*.json` (plan only): pass the schema on purpose. They break rules across fields that JSON Schema cannot express, for example an Engine grid tier with 3 cells for 4 columns. `services/agents-python/app/contract/rules.py` checks these, except the timeline arithmetic (`rule-timeline-gap`), which only mcp-rust's `verify_timeline_math` checks.
- Tool examples (`examples/tools/`) are wrapped as `{"$def": "<type>", "value": {...}}` because `tools.schema.json` holds several types.

What each generator enforces when decoding: Python and Go check lengths, item counts, enums and unknown fields. Rust checks types, enums and string lengths but ignores `maxItems`, which is why it only gets the tool schema. TypeScript types have no runtime checks; the reference check is `npm test` here (ajv).
