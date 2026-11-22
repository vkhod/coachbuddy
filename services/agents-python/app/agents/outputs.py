"""What each agent answers. Built from the generated contract types, so a model answer
is checked against the same schema the gateway and the frontend use."""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field, constr

from app.contract.plan import InjuryPart, PartScaling, TimelineBlock, TodayGroup, Warmup, Wod


class InjuryMention(BaseModel):
    """One injury as the comment states it. Becomes the label, fromComment and returnNote
    of a contract Injury; the Injury agent adds the advice."""

    model_config = ConfigDict(extra="forbid")
    label: constr(min_length=1, max_length=40) = Field(..., description='Body area, for example "Ankle", "Shoulder", "Lower back".')
    fromComment: constr(max_length=200) = Field(..., description="The words of the comment this injury comes from.")
    returnNote: constr(max_length=60) | None = Field(None, description='For an athlete coming back, for example "returning after 2 weeks". Null otherwise.')


class ParserOut(BaseModel):
    """Every field is required, empty lists included. Live on 2026-11-20 Gemini left out an
    optional `injuries` key for "Today injured ankle will join after two weeks leave", and a
    default of [] turned that into "no injuries". A missing key is now a validation error
    and goes back to the model as a repair."""

    model_config = ConfigDict(extra="forbid")
    wod: Wod
    warmupWishes: constr(max_length=200) | None = Field(
        ..., description="What the coach's comment asks of the warm-up, in the coach's words. Null if nothing."
    )
    today: list[TodayGroup] = Field(..., max_length=6, description="Empty list when the comment names nobody.")
    injuries: list[InjuryMention] = Field(..., max_length=6, description="Empty list when the comment names no injury.")


class InjuryAdvice(BaseModel):
    model_config = ConfigDict(extra="forbid")
    label: constr(min_length=1, max_length=40) = Field(..., description="Same label as the injury in the input, same order.")
    warmup: constr(min_length=1, max_length=100)
    parts: list[InjuryPart] = Field(..., max_length=6)


class InjuriesOut(BaseModel):
    model_config = ConfigDict(extra="forbid")
    injuries: list[InjuryAdvice] = Field(..., min_length=1, max_length=6)


WarmupOut = Warmup


class ScalingOut(BaseModel):
    model_config = ConfigDict(extra="forbid")
    parts: list[PartScaling] = Field(..., min_length=1, max_length=6)


class TimelineDraft(BaseModel):
    model_config = ConfigDict(extra="forbid")
    blocks: list[TimelineBlock] = Field(..., min_length=1, max_length=15)


class InjuryRefineAdvice(BaseModel):
    """An injury in a refinement turn: the coach may add or drop injuries in the
    instruction, so the label and return note come from the answer here."""

    model_config = ConfigDict(extra="forbid")
    label: constr(min_length=1, max_length=40) = Field(..., description='Body area, for example "Ankle". Keep the label of an injury that stays.')
    returnNote: constr(max_length=60) | None = Field(..., description='For an athlete coming back, for example "returning after 2 weeks". Null otherwise.')
    warmup: constr(min_length=1, max_length=100)
    parts: list[InjuryPart] = Field(..., max_length=6)


class InjuriesRefineOut(BaseModel):
    model_config = ConfigDict(extra="forbid")
    injuries: list[InjuryRefineAdvice] = Field(..., max_length=6, description="Empty list when the coach removes every injury.")


class InjuryWarmupLine(BaseModel):
    model_config = ConfigDict(extra="forbid")
    label: constr(min_length=1, max_length=40) = Field(..., description="The injury's label, as in the current list.")
    warmup: constr(min_length=1, max_length=100)


class InjuryWarmupLinesOut(BaseModel):
    """After a warm-up change: one new warm-up line per stored injury. The part
    suggestions and everything else stay as stored."""

    model_config = ConfigDict(extra="forbid")
    injuries: list[InjuryWarmupLine] = Field(..., max_length=6, description="One entry per injury, same labels and order as the current list.")
