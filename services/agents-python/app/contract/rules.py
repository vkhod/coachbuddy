"""Cross-field rules of the plan contract that JSON Schema cannot express.

The generated models (plan.py and friends) check each field on its own: types, enums,
lengths and item counts. The rules here check fields against each other. Agents run
both on every model answer; a failure goes back to the model once as a repair
instruction, then fails the section (D3).

Timeline arithmetic (gaps, overlaps, the total) is not checked here: the mcp-rust tool
verify_timeline_math is the only implementation of it.
"""

from __future__ import annotations

from app.contract.plan import PartKind, PartLayout, Plan

TIER_KEYS = ("rx", "rxPlus", "scaled", "senior")


def check_plan_rules(plan: Plan) -> list[str]:
    """Returns one readable message per broken rule; an empty list means the plan passes."""
    errors: list[str] = []
    wod_parts = {p.id: p for p in plan.wod.parts} if plan.wod else {}

    for part in plan.parts or []:
        where = f"parts[{part.partId}]"
        if wod_parts and part.partId not in wod_parts:
            errors.append(f"{where}: no WOD part with id {part.partId}")
            continue

        if part.layout == PartLayout.grid:
            if not part.columns:
                errors.append(f"{where}: layout is grid but columns is missing")
            else:
                for key in TIER_KEYS:
                    tier = getattr(part.tiers, key)
                    if tier is None:
                        continue
                    cells = len(tier.cells or [])
                    if cells != len(part.columns):
                        errors.append(f"{where}.tiers.{key}: {cells} cells for {len(part.columns)} columns")
        else:
            if part.columns:
                errors.append(f"{where}: columns are only allowed when layout is grid")
            for key in TIER_KEYS:
                tier = getattr(part.tiers, key)
                if tier is not None and tier.cells:
                    errors.append(f"{where}.tiers.{key}: cells are only allowed when layout is grid")

        wod_part = wod_parts.get(part.partId)
        if part.tiers.rxPlus is not None and wod_part is not None and wod_part.kind != PartKind.conditioning:
            errors.append(f"{where}.tiers.rxPlus: RX+ is only for conditioning parts, {part.partId} is {wod_part.kind.value}")

    if wod_parts and plan.parts is not None:
        scaled_ids = [p.partId for p in plan.parts]
        for part_id in wod_parts:
            if scaled_ids.count(part_id) != 1:
                errors.append(f"parts: WOD part {part_id} needs exactly one entry, found {scaled_ids.count(part_id)}")

    for i, injury in enumerate(plan.injuries or []):
        for ip in injury.parts:
            if wod_parts and ip.partId not in wod_parts:
                errors.append(f"injuries[{i}] ({injury.label}).parts: no WOD part with id {ip.partId}")

    return errors
