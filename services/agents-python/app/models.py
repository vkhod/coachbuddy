"""Request models for POST /api/v1/orchestrate, as the gateway sends them today.

The plan itself is described by the generated contract in app/contract/. These request
models go when generation becomes asynchronous (Phase 2 step 10) and the gateway sends
the contract's CreatePlanRequest instead.
"""

from __future__ import annotations

from typing import Dict, Optional

from pydantic import BaseModel, ConfigDict, Field, SecretStr


class EphemeralAgentRoute(BaseModel):
    """Provider, model and decrypted key for one agent role, valid for one request."""

    model_config = ConfigDict(extra="ignore")

    provider: str = ""
    model: str = ""
    apiKey: SecretStr = Field(default=SecretStr(""))


class SessionInput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    title: Optional[str] = ""
    rawText: str = ""
    imageBase64: Optional[str] = ""
    # The coach's comment: who attends today, injuries, warm-up wishes.
    coachConstraints: str = Field(default="", max_length=1000)
    targetDurationMinutes: int = Field(default=60, ge=20, le=180)


class OrchestrateRequest(BaseModel):
    model_config = ConfigDict(extra="ignore")

    sessionId: str = Field(..., min_length=1, max_length=64)
    coachId: str = Field(..., min_length=1, max_length=64)
    input: SessionInput
    routingConfig: Dict[str, EphemeralAgentRoute] = Field(default_factory=dict)
