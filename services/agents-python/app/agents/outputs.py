"""What each agent answers. Built from the generated contract types, so a model answer
is checked against the same schema the gateway and the frontend use."""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field, constr

from app.contract.plan import PartScaling, TimelineBlock, Warmup, Wod


class ParserOut(BaseModel):
    model_config = ConfigDict(extra="forbid")
    wod: Wod
    warmupWishes: constr(max_length=200) | None = Field(
        None, description="What the coach's comment asks of the warm-up, in the coach's words. Null if nothing."
    )


WarmupOut = Warmup


class ScalingOut(BaseModel):
    model_config = ConfigDict(extra="forbid")
    parts: list[PartScaling] = Field(..., min_length=1, max_length=6)


class TimelineDraft(BaseModel):
    model_config = ConfigDict(extra="forbid")
    blocks: list[TimelineBlock] = Field(..., min_length=1, max_length=15)
