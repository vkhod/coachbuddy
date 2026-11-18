"""The JSON schema we send to the model, derived from the contract model.

Gemini rejects schemas whose length and count constraints make constrained decoding too
large ("The specified schema produces a constraint that has too many states for
serving", seen live on 2026-11-18 with the Scaling schema). So the model gets the
structure (types, enums, required fields) with each limit written into the field's
description, and the full contract model checks the answer afterwards. A broken limit
goes back to the model through the repair retry, like any other validation error.
"""

from __future__ import annotations

from typing import Any, Dict, Type

from pydantic import BaseModel

# keyword -> how the limit is described to the model
_LIMITS = {
    "maxLength": "At most {} characters.",
    "minLength": "At least {} characters.",
    "maxItems": "At most {} items.",
    "minItems": "At least {} items.",
    "maximum": "At most {}.",
    "minimum": "At least {}.",
}
_DROP = ("default", "title")


def _relax(node: Any) -> Any:
    if isinstance(node, list):
        return [_relax(n) for n in node]
    if not isinstance(node, dict):
        return node
    out: Dict[str, Any] = {}
    notes = []
    for key, value in node.items():
        if key == "minLength" and value == 1:
            notes.append("Not empty.")
        elif key in _LIMITS and not isinstance(value, (dict, list)):
            notes.append(_LIMITS[key].format(value))
        elif key == "pattern":
            notes.append(f"Must match {value}.")
        elif key in _DROP and not isinstance(value, dict):
            continue
        elif key == "properties":
            out[key] = {name: _relax(prop) for name, prop in value.items()}
        else:
            out[key] = _relax(value)
    if notes:
        out["description"] = " ".join(filter(None, [out.get("description", ""), *notes]))
    return out


def schema_for_model(model: Type[BaseModel]) -> Dict[str, Any]:
    return _relax(model.model_json_schema())
