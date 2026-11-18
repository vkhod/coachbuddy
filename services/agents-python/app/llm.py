"""Model routing: which provider, model and key each agent role uses for one request.

The gateway decides the routes (it owns the model list and the coach's keys) and sends
them in routingConfig. Keys stay in this request: they are passed to LiteLlm as a call
argument and never written to os.environ.
"""

from __future__ import annotations

from dataclasses import dataclass
import os
from typing import Callable, Dict

from google.adk.models.base_llm import BaseLlm
from google.adk.models.lite_llm import LiteLlm

from app.models import EphemeralAgentRoute

# LiteLLM fetches its model map from GitHub on import unless told not to. The service
# must start without GitHub, so use the map that ships with the pinned release.
os.environ.setdefault("LITELLM_LOCAL_MODEL_COST_MAP", "True")
# ADK warns on every Gemini model built through LiteLlm; LiteLlm is decision D4.
os.environ.setdefault("ADK_SUPPRESS_GEMINI_LITELLM_WARNINGS", "true")


class RouteError(ValueError):
    """The request has no usable model or key for a role."""


@dataclass(frozen=True)
class Route:
    role: str
    provider: str
    model: str
    api_key: str

    def __repr__(self) -> str:  # never print the key
        return f"Route(role={self.role!r}, provider={self.provider!r}, model={self.model!r})"


def resolve_route(role: str, routing: Dict[str, EphemeralAgentRoute]) -> Route:
    """The role's own route, else the global route. No defaults: the gateway sends them."""
    chosen = routing.get(role)
    if chosen is None or not chosen.model:
        chosen = routing.get("global")
    if chosen is None or not chosen.model:
        raise RouteError(f"no model configured for the {role} agent")
    key = chosen.apiKey.get_secret_value().strip()
    if not key:
        raise RouteError(f"no API key for {chosen.provider or 'the chosen provider'} ({role} agent)")
    model = chosen.model if "/" in chosen.model else f"{chosen.provider}/{chosen.model}"
    return Route(role=role, provider=chosen.provider, model=model, api_key=key)


def supports_vision(model: str) -> bool | None:
    """True or False when LiteLLM's model map knows the model, None when it does not."""
    import litellm

    try:
        info = litellm.get_model_info(model)
    except Exception:
        return None
    flag = info.get("supports_vision")
    return None if flag is None else bool(flag)


ModelFactory = Callable[[Route], BaseLlm]


def litellm_model(route: Route) -> BaseLlm:
    """One LiteLlm per agent per request, bound to that request's key."""
    return LiteLlm(model=route.model, api_key=route.api_key)
