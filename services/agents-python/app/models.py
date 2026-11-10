from __future__ import annotations

from typing import Any, Dict, List, Literal, Optional
from pydantic import BaseModel, ConfigDict, Field, SecretStr


class EphemeralAgentRoute(BaseModel):
    model_config = ConfigDict(extra="ignore")

    provider: str = Field(default="gemini", description="LLM provider: gemini | openai | anthropic | groq")
    model: str = Field(default="gemini/gemini-3.8-flash", description="LiteLLM model string")
    apiKey: SecretStr = Field(default=SecretStr(""), description="Ephemeral decrypted BYOK API key for this request")


class SessionInput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    title: Optional[str] = Field(default="", description="Optional custom session title")
    rawText: str = Field(default="", description="Raw WOD text pasted by the coach")
    imageBase64: Optional[str] = Field(default="", description="Optional base64 WOD whiteboard/app screenshot")
    coachConstraints: str = Field(
        default="",
        description="Coach's constraints or warm-up preference, e.g., 'today I want to use mostly a broomstick for a warmup'",
    )
    targetDurationMinutes: int = Field(default=60, ge=15, le=180, description="Total class duration in minutes")
    equipmentFocus: List[str] = Field(default_factory=list, description="Preferred warm-up or class equipment")


class OrchestrateRequest(BaseModel):
    model_config = ConfigDict(extra="ignore")

    sessionId: str = Field(..., description="Unique session identifier")
    coachId: str = Field(..., description="Coach UUID")
    input: SessionInput
    routingConfig: Dict[str, EphemeralAgentRoute] = Field(default_factory=dict)


class WorkoutBlockSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    type: Literal["strength", "skill", "metcon", "engine"] | str = "metcon"
    name: str
    allocatedMinutes: int = Field(default=15, ge=1, le=120)
    repScheme: Optional[str] = None
    notes: Optional[str] = None


class ParsedWodOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    title: str = "Daily Box WOD Session"
    extractedText: str = Field(default="", description="Clean OCR/normalized WOD text extracted from image or raw input")
    blocks: List[WorkoutBlockSpec] = Field(default_factory=list)
    movements: List[str] = Field(default_factory=list)
    primaryJoints: List[str] = Field(default_factory=list)
    stimulus: str = "High-output functional conditioning and positional strength."


class WarmupDrill(BaseModel):
    model_config = ConfigDict(extra="ignore", coerce_numbers_to_str=True)

    name: str
    reps: str = "8-10 reps"
    focus: str = "Joint prep & activation"


class WarmupPhaseSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    name: str
    durationMinutes: int = 5
    instructions: str = ""
    coachingGoal: str = ""
    drills: List[WarmupDrill] = Field(default_factory=list)


class WarmupPlanOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    gameTitle: str = Field(default="PVC Broomstick Circle Drop & Switch")
    style: str
    durationMinutes: int = 12
    phases: List[WarmupPhaseSpec]
    alternativeGames: List[WarmupPhaseSpec] = Field(default_factory=list)
    drillNames: List[str] = Field(default_factory=list)


class CommonFaultSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    fault: str = "Loss of positional mechanics under fatigue"
    fix: str = "Reset posture and use crisp external cue"


class MovementCuesSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    external: List[str] = Field(default_factory=list)
    internal: List[str] = Field(default_factory=list)
    commonFaults: List[CommonFaultSpec] = Field(default_factory=list)


class InjuredScalingSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    wristOrShoulder: str = "Single-arm or neutral-grip variation"
    kneePain: str = "Box-supported or reduced knee-flexion variation"
    lowerBack: str = "Unilateral supported variation with zero spinal shear"


class ScalingTiersSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    rxPlus: str = "Competition standard / unbroken sets"
    scaled: str = "Reduced load or volume to preserve intended stimulus"
    seniorsMasters: str = "Controlled tempo and joint-friendly range of motion"
    injured: InjuredScalingSpec = Field(default_factory=InjuredScalingSpec)


class ExerciseCoachingEntry(BaseModel):
    model_config = ConfigDict(extra="ignore")

    name: str
    category: str = "Primary Movement"
    primaryJoints: List[str] = Field(default_factory=list)
    cues: MovementCuesSpec = Field(default_factory=MovementCuesSpec)
    scaling: ScalingTiersSpec = Field(default_factory=ScalingTiersSpec)


class ScalingAndCuesOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    exercises: List[ExerciseCoachingEntry]


class TimelineBlockSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    startMin: int
    endMin: int
    label: str
    type: str = "metcon"
    notes: Optional[str] = ""


class TimelineOutput(BaseModel):
    model_config = ConfigDict(extra="ignore")

    totalMinutes: int
    feasibilityStatus: Literal["OPTIMAL", "TIGHT", "INVALID", "OVERFLOW"] | str = "OPTIMAL"
    coachWarnings: List[str] = Field(default_factory=list)
    blocks: List[TimelineBlockSpec]


class AgentTrace(BaseModel):
    model_config = ConfigDict(extra="ignore")

    agent: str
    provider: str
    model: str
    executionMode: Literal["google-adk-litellm", "deterministic-stage"] | str = "google-adk-litellm"
    mcpTool: Optional[str] = None
    latencyMs: Optional[int] = None
    status: str = "completed"
