"""The internal request for POST /api/v1/orchestrate, as the gateway sends it.

The coach's part (`input`) is the contract's CreatePlanRequest. The rest is internal and
stays out of the public schema: the decrypted keys must never appear in a public type.
"""

from __future__ import annotations

from typing import Dict

from pydantic import BaseModel, ConfigDict, Field, SecretStr

from app.contract.create_plan_request import CreatePlanRequest


class EphemeralAgentRoute(BaseModel):
    """Provider, model and decrypted key for one agent role, valid for one request."""

    model_config = ConfigDict(extra="ignore")

    provider: str = ""
    model: str = ""
    apiKey: SecretStr = Field(default=SecretStr(""))


class OrchestrateRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    planId: str = Field(..., min_length=1, max_length=64)
    coachId: str = Field(..., min_length=1, max_length=64)
    # Where the gateway stored the screenshot; written into the plan as input.imageRef.
    imageRef: str = Field(..., min_length=1, max_length=64)
    # The plan's createdAt, set by the gateway when it accepted the request.
    createdAt: str = Field(..., min_length=1)
    input: CreatePlanRequest
    routingConfig: Dict[str, EphemeralAgentRoute] = Field(default_factory=dict)
