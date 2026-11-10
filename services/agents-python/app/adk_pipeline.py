from __future__ import annotations

import asyncio
import base64
from datetime import datetime, timezone
import json
import logging
import os
import re
import time
from typing import Any, Callable, Coroutine, Dict, List, Optional, Tuple

from app.mcp_client import RustMcpClient
from app.models import (
    EphemeralAgentRoute,
    ExerciseCoachingEntry,
    OrchestrateRequest,
    ParsedWodOutput,
    ScalingAndCuesOutput,
    SessionInput,
    TimelineOutput,
    WarmupPhaseSpec,
    WarmupPlanOutput,
    WorkoutBlockSpec,
)

logger = logging.getLogger("coachbuddy.adk_pipeline")

# Optional Google ADK + LiteLLM imports with availability flag
try:
    from google.adk.agents import LlmAgent
    from google.adk.models.lite_llm import LiteLlm
    from google.adk.runners import Runner
    from google.adk.sessions import InMemorySessionService
    from google.genai import types as genai_types

    ADK_AVAILABLE = True
except Exception as import_exc:  # pragma: no cover
    logger.warning("google-adk or litellm not fully importable (%s); using shim classes.", import_exc)
    ADK_AVAILABLE = False
    LlmAgent = None  # type: ignore
    LiteLlm = None  # type: ignore
    Runner = None  # type: ignore
    InMemorySessionService = None  # type: ignore
    genai_types = None  # type: ignore


EventPublisher = Callable[[str, str, Dict[str, Any]], Coroutine[Any, Any, None]]


# ============================================================================
# Known CrossFit Movement Taxonomy & Joint Mapping for Smart Parsing
# ============================================================================

MOVEMENT_PATTERNS: List[Tuple[str, re.Pattern[str], List[str]]] = [
    ("Front Squat", re.compile(r"\bfront\s+squats?\b", re.I), ["hips", "knees", "ankles", "thoracic_spine", "wrists"]),
    ("Back Squat", re.compile(r"\bback\s+squats?\b", re.I), ["hips", "knees", "ankles", "thoracic_spine"]),
    ("Overhead Squat", re.compile(r"\b(overhead\s+squats?|ohs)\b", re.I), ["shoulders", "thoracic_spine", "hips", "knees", "ankles", "wrists"]),
    ("Thruster", re.compile(r"\b(thrusters?|clusters?)\b", re.I), ["shoulders", "thoracic_spine", "hips", "knees", "wrists"]),
    ("Bar Muscle-Up", re.compile(r"\b(bar\s+muscle[\s-]?ups?|bmu|ring\s+muscle[\s-]?ups?|rmu|muscle[\s-]?ups?)\b", re.I), ["shoulders", "thoracic_spine", "hips", "wrists"]),
    ("Chest-to-Bar Pull-Up", re.compile(r"\b(chest[\s-]?to[\s-]?bar|c2b)\b", re.I), ["shoulders", "thoracic_spine", "wrists"]),
    ("Pull-Up", re.compile(r"\b(pull[\s-]?ups?|chin[\s-]?ups?)\b", re.I), ["shoulders", "thoracic_spine", "wrists"]),
    ("Toes-to-Bar", re.compile(r"\b(toes[\s-]?to[\s-]?bar|t2b|ttb|knees[\s-]?to[\s-]?elbows?)\b", re.I), ["shoulders", "hips", "core", "lower_back"]),
    ("Handstand Push-Up", re.compile(r"\b(handstand\s+push[\s-]?ups?|hspu|wall\s+walks?)\b", re.I), ["shoulders", "wrists", "thoracic_spine", "core"]),
    ("Power Clean", re.compile(r"\b(power\s+cleans?|squat\s+cleans?|hang\s+cleans?|cleans?)\b", re.I), ["hips", "knees", "ankles", "shoulders", "wrists"]),
    ("Snatch", re.compile(r"\b(power\s+snatch(?:es)?|squat\s+snatch(?:es)?|snatch(?:es)?)\b", re.I), ["shoulders", "thoracic_spine", "hips", "knees", "ankles", "wrists"]),
    ("Deadlift", re.compile(r"\b(deadlifts?|rdls?|romanian\s+deadlifts?)\b", re.I), ["hips", "lower_back", "knees", "shoulders"]),
    ("Push Press", re.compile(r"\b(push\s+press(?:es)?|push\s+jerks?|split\s+jerks?|shoulder[\s-]?to[\s-]?overhead|s2oh)\b", re.I), ["shoulders", "thoracic_spine", "hips", "knees", "wrists"]),
    ("Wall Ball", re.compile(r"\b(wall\s*balls?)\b", re.I), ["hips", "knees", "shoulders", "ankles"]),
    ("Box Jump", re.compile(r"\b(box\s+jumps?|box\s+jump[\s-]?overs?|bjo|box\s+step[\s-]?ups?)\b", re.I), ["hips", "knees", "ankles"]),
    ("Burpee", re.compile(r"\b(burpees?|bar[\s-]?facing\s+burpees?|devil\s+press(?:es)?)\b", re.I), ["shoulders", "wrists", "hips", "knees"]),
    ("Kettlebell Swing", re.compile(r"\b(kettlebell\s+swings?|kb\s+swings?|russian\s+swings?|american\s+swings?)\b", re.I), ["hips", "lower_back", "shoulders"]),
    ("Double-Under", re.compile(r"\b(double[\s-]?unders?|dus|single[\s-]?unders?)\b", re.I), ["ankles", "knees", "wrists", "shoulders"]),
    ("Walking Lunge", re.compile(r"\b(walking\s+lunges?|overhead\s+lunges?|lunges?)\b", re.I), ["hips", "knees", "ankles"]),
    ("Rowing / Erg", re.compile(r"\b(cal(?:orie)?\s+row|row(?:ing)?|assault\s+bike|echo\s+bike|ski\s*erg)\b", re.I), ["hips", "knees", "lower_back", "shoulders"]),
]


def parse_wod_deterministically(session_input: SessionInput) -> ParsedWodOutput:
    """Deterministic regex & domain heuristic WOD parser."""
    raw = (session_input.rawText or "").strip()
    lines = [ln.strip() for ln in raw.splitlines() if ln.strip()]

    title = (session_input.title or "").strip()
    if not title:
        if lines:
            first_line = re.sub(r"^[#*\-\s]+", "", lines[0]).strip()
            title = first_line[:72] if len(first_line) > 3 else "Daily Box Conditioning & Strength Plan"
        else:
            title = "Vision WOD Analysis & Coaching Plan"

    movements: List[str] = []
    joints: List[str] = []
    for canonical_name, pattern, mov_joints in MOVEMENT_PATTERNS:
        if pattern.search(raw):
            if canonical_name == "Pull-Up" and "Chest-to-Bar Pull-Up" in movements:
                continue
            movements.append(canonical_name)
            for j in mov_joints:
                if j not in joints:
                    joints.append(j)

    if not movements:
        movements = ["Front Squat", "Thruster", "Pull-Up"]
        joints = ["shoulders", "thoracic_spine", "hips", "knees", "wrists"]

    blocks: List[WorkoutBlockSpec] = []
    raw_lc = raw.lower()

    has_strength = bool(
        re.search(r"\b(part\s*a|block\s*a|a\)|a\.|strength|every\s+\d|e2mom|e3mom|5x5|5x3|heavy)\b", raw_lc)
    )
    has_metcon = bool(
        re.search(r"\b(part\s*b|block\s*b|b\)|b\.|metcon|for\s+time|amrap|emom|21[\s-]*15[\s-]*9|rounds?\s+for\s+time|rft|cap)\b", raw_lc)
    )

    amrap_match = re.search(r"(?:amrap|emom|cap[:\s]*)\s*(\d{1,2})\s*(?:min|m\b)?", raw_lc)
    metcon_minutes = int(amrap_match.group(1)) if amrap_match else 15
    metcon_minutes = max(min(metcon_minutes, 30), 8)

    if has_strength and has_metcon:
        blocks.append(
            WorkoutBlockSpec(
                type="strength",
                name="Block A: Strength & Positional Progression",
                allocatedMinutes=15,
                notes="Build across working sets with crisp positional pauses; prioritize mechanics over max load.",
            )
        )
        blocks.append(
            WorkoutBlockSpec(
                type="metcon",
                name="Block B: Primary Conditioning (Metcon)",
                allocatedMinutes=metcon_minutes,
                notes="High-output conditioning; coach external cues, pacing strategy, and lumbar alignment under fatigue.",
            )
        )
    elif has_strength and not has_metcon:
        blocks.append(
            WorkoutBlockSpec(
                type="strength",
                name="Primary Heavy Strength & Skill Session",
                allocatedMinutes=24,
                notes="Progressive loading waves with 2-3 min rest between heavy efforts.",
            )
        )
    else:
        if len(movements) >= 3:
            blocks.append(
                WorkoutBlockSpec(
                    type="skill",
                    name="Skill & Barbell Touch-and-Go Primer",
                    allocatedMinutes=12,
                    notes="Dial in cycling efficiency, breathing cadence, and scaling rehearsal before the clock starts.",
                )
            )
        blocks.append(
            WorkoutBlockSpec(
                type="metcon",
                name="Primary Conditioning (Engine / Metcon)",
                allocatedMinutes=metcon_minutes,
                notes="Maintain unbroken mechanics on early sets; keep transitions under 10 seconds.",
            )
        )

    stimulus = (
        f"High-neurological-demand session combining {', '.join(movements[:3])} — "
        f"loads {', '.join(joints[:4])} while demanding midline bracing and sustainable aerobic/glycolytic pacing."
    )

    return ParsedWodOutput(
        title=title,
        extractedText=raw or title,
        blocks=blocks,
        movements=movements,
        primaryJoints=joints,
        stimulus=stimulus,
    )


# ============================================================================
# Dynamic BYOK LiteLLM Model Resolution (with SecretStr protection)
# ============================================================================


def resolve_agent_route(
    role: str,
    routing_config: Dict[str, EphemeralAgentRoute],
) -> Tuple[str, str, str]:
    """Resolves `(provider, model_identifier, plain_api_key)` for a specific agent role:
    `parser` | `warmup` | `scaling` | `timeline`, falling back to `global` and environment defaults.
    Unwraps `SecretStr` via `.get_secret_value()` only when needed in memory.
    """
    role_route = routing_config.get(role)
    global_route = routing_config.get("global")

    provider = (
        (role_route.provider if role_route and role_route.provider else None)
        or (global_route.provider if global_route and global_route.provider else None)
        or "gemini"
    ).lower()

    default_models = {
        "gemini": "gemini/gemini-3.8-flash",
        "openai": "openai/gpt-4o",
        "anthropic": "anthropic/claude-3-7-sonnet-latest",
        "groq": "groq/llama-3.3-70b-versatile",
    }
    model = (
        (role_route.model if role_route and role_route.model else None)
        or (global_route.model if global_route and global_route.model else None)
        or default_models.get(provider, "gemini/gemini-3.8-flash")
    )
    if "gemini-2.5-flash" in model:
        model = model.replace("gemini-2.5-flash", "gemini-3.8-flash")

    if "/" not in model:
        model = f"{provider}/{model}"

    role_key_str = role_route.apiKey.get_secret_value().strip() if role_route and role_route.apiKey else ""
    global_key_str = global_route.apiKey.get_secret_value().strip() if global_route and global_route.apiKey else ""
    api_key = role_key_str or global_key_str

    if not api_key:
        if provider == "gemini":
            api_key = os.getenv("DEFAULT_GEMINI_API_KEY", "").strip()
        elif provider == "openai":
            api_key = os.getenv("DEFAULT_OPENAI_API_KEY", "").strip()
        elif provider == "anthropic":
            api_key = os.getenv("DEFAULT_ANTHROPIC_API_KEY", "").strip()
        elif provider == "groq":
            api_key = os.getenv("DEFAULT_GROQ_API_KEY", "").strip()

    return provider, model, api_key


def create_adk_litellm_model(provider: str, model: str, api_key: str) -> Any:
    """Creates an isolated Google ADK `LiteLlm` model instance bound to the coach's decrypted BYOK key.
    Never mutates global `os.environ` so concurrent coach sessions remain strictly isolated.
    """
    if not ADK_AVAILABLE or LiteLlm is None:
        return None
    return LiteLlm(model=model, api_key=api_key)


def _is_live_api_key(api_key: str) -> bool:
    """Returns True when a real user-supplied BYOK key is present (not a unit-test placeholder)."""
    cleaned = (api_key or "").strip()
    if not cleaned:
        return False
    test_prefixes = ("test-", "mock-", "local-", "sk-parser-secret", "sk-global-secret")
    return not cleaned.startswith(test_prefixes)


def _extract_json_dict(raw_text: str) -> Dict[str, Any]:
    """Extracts and parses a JSON object from an LLM response string."""
    text = raw_text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*", "", text, flags=re.I)
        text = re.sub(r"\s*```$", "", text)
    start = text.find("{")
    end = text.rfind("}")
    if start != -1 and end != -1 and end > start:
        text = text[start : end + 1]
    return json.loads(text)


async def call_live_llm_json(
    provider: str,
    model: str,
    api_key: str,
    system_prompt: str,
    user_prompt: str,
    image_bytes: Optional[bytes] = None,
    mime_type: Optional[str] = None,
) -> Dict[str, Any]:
    """Calls the configured LLM provider (Gemini, OpenAI, Anthropic, or Groq) using the
    decrypted per-request BYOK key and returns a parsed JSON dictionary.
    """
    import httpx

    prov = provider.lower().strip()
    short_model = model.split("/", 1)[1] if "/" in model else model

    # If the attached image is an SVG data URI, include its XML text in the prompt
    # because vision APIs only accept raster MIME types (png/jpeg/webp) in inline binary blobs.
    if image_bytes and mime_type and "svg" in mime_type.lower():
        svg_text = image_bytes.decode("utf-8", errors="ignore")
        user_prompt = f"{user_prompt}\n\nAttached Whiteboard SVG Content:\n{svg_text}"
        image_bytes = None
        mime_type = None

    async with httpx.AsyncClient(timeout=30.0) as client:
        if prov == "gemini":
            parts: List[Dict[str, Any]] = [{"text": f"{system_prompt}\n\n{user_prompt}"}]
            if image_bytes and mime_type:
                parts.append(
                    {
                        "inlineData": {
                            "mimeType": mime_type,
                            "data": base64.b64encode(image_bytes).decode("ascii"),
                        }
                    }
                )
            payload = {
                "contents": [{"role": "user", "parts": parts}],
                "generationConfig": {
                    "temperature": 0.3,
                    "responseMimeType": "application/json",
                },
            }

            candidate_models = [short_model]
            for fallback_m in ("gemini-3.8-flash", "gemini-flash-latest", "gemini-2.0-flash"):
                if fallback_m not in candidate_models:
                    candidate_models.append(fallback_m)

            resp = None
            last_timeout_err: Optional[Exception] = None
            idx = 0
            while idx < len(candidate_models):
                curr_model = candidate_models[idx]
                idx += 1
                url = f"https://generativelanguage.googleapis.com/v1beta/models/{curr_model}:generateContent"
                timed_out = False
                for attempt in range(2):
                    try:
                        resp = await client.post(url, params={"key": api_key}, json=payload)
                    except httpx.TimeoutException as tex:
                        last_timeout_err = tex
                        timed_out = True
                        logger.warning("Gemini model %s timed out (attempt %d); trying next fallback", curr_model, attempt + 1)
                        break
                    if resp.status_code in (503, 429, 500, 502, 504) and attempt < 1:
                        # Transient high demand / rate limit spike: wait briefly and retry
                        await asyncio.sleep(1.5 * (attempt + 1))
                        continue
                    break

                if timed_out:
                    continue
                if resp is not None and resp.status_code == 404:
                    # If Google's 404 message suggests a newer model (e.g. "use models/gemini-..."), prioritize it next
                    suggested = re.findall(r"models/(gemini-[a-zA-Z0-9._-]+)", resp.text)
                    for s_mod in suggested:
                        if s_mod != curr_model and s_mod not in candidate_models:
                            candidate_models.insert(idx, s_mod)
                    continue
                if resp is not None and resp.status_code in (503, 429) and idx < len(candidate_models):
                    # Current model is overloaded; try the next fallback Gemini model
                    continue
                break

            if resp is None:
                raise RuntimeError(f"Gemini API timeout (504): request timed out across models ({type(last_timeout_err).__name__})")
            if resp.status_code >= 400:
                status_c = resp.status_code
                err_txt = resp.text[:300]
                raise RuntimeError(f"Gemini API error ({status_c}): {err_txt}")
            data = resp.json()
            candidates = data.get("candidates") or []
            if not candidates:
                raise RuntimeError(f"Gemini returned no candidates: {data}")
            text_out = candidates[0].get("content", {}).get("parts", [{}])[0].get("text", "{}")
            return _extract_json_dict(text_out)

        elif prov in ("openai", "groq"):
            url = (
                "https://api.groq.com/openai/v1/chat/completions"
                if prov == "groq"
                else "https://api.openai.com/v1/chat/completions"
            )
            user_content: Any = user_prompt
            if image_bytes and mime_type and prov == "openai":
                b64_img = base64.b64encode(image_bytes).decode("ascii")
                user_content = [
                    {"type": "text", "text": user_prompt},
                    {"type": "image_url", "image_url": {"url": f"data:{mime_type};base64,{b64_img}"}},
                ]
            payload = {
                "model": short_model,
                "temperature": 0.3,
                "response_format": {"type": "json_object"},
                "messages": [
                    {"role": "system", "content": system_prompt + "\nRespond strictly with valid JSON."},
                    {"role": "user", "content": user_content},
                ],
            }
            resp = await client.post(
                url,
                headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
                json=payload,
            )
            if resp.status_code >= 400:
                raise RuntimeError(f"{prov.upper()} API error ({resp.status_code}): {resp.text[:300]}")
            data = resp.json()
            text_out = data["choices"][0]["message"]["content"]
            return _extract_json_dict(text_out)

        elif prov == "anthropic":
            url = "https://api.anthropic.com/v1/messages"
            payload = {
                "model": short_model,
                "max_tokens": 2500,
                "temperature": 0.3,
                "system": system_prompt + "\nRespond strictly with a single valid JSON object and no markdown prose.",
                "messages": [{"role": "user", "content": user_prompt}],
            }
            resp = await client.post(
                url,
                headers={
                    "x-api-key": api_key,
                    "anthropic-version": "2023-06-01",
                    "Content-Type": "application/json",
                },
                json=payload,
            )
            if resp.status_code >= 400:
                raise RuntimeError(f"Anthropic API error ({resp.status_code}): {resp.text[:300]}")
            data = resp.json()
            text_out = data["content"][0]["text"]
            return _extract_json_dict(text_out)

        else:
            raise RuntimeError(f"Unsupported LLM provider: {provider}")


# ============================================================================
# Multimodal Vision Content Builder (TODO 3.1)
# ============================================================================


def build_multimodal_user_content(session_input: SessionInput) -> Dict[str, Any]:
    """Constructs a structured multimodal payload (and Google GenAI `Content` if ADK is loaded)
    for `WodParserAgent`.

    Returns a dictionary with:
    - `"text_prompt"`: str (the formatted prompt combining rawText, coachConstraints, duration)
    - `"has_image"`: bool (True if `session_input.imageBase64` contained valid base64 image data)
    - `"mime_type"`: Optional[str] (e.g., `"image/png"`, `"image/jpeg"`, or `None` if no image)
    - `"image_bytes"`: Optional[bytes] (decoded raw image bytes, or `None` if no image)
    - `"genai_content"`: Optional[Any] (`genai_types.Content` with text + `Part.from_bytes` if `genai_types` is available)
    """
    text_prompt = (
        f"WOD Raw Text:\n{session_input.rawText or '(See attached WOD whiteboard image)'}\n\n"
        f"Coach Constraints: {session_input.coachConstraints or 'Broomstick reaction game + joint mobility'}\n"
        f"Target Class Duration Minutes: {session_input.targetDurationMinutes}\n"
        f"Equipment Focus: {', '.join(session_input.equipmentFocus or ['pvc_broomstick'])}"
    )

    raw_b64 = (session_input.imageBase64 or "").strip()
    has_image = False
    mime_type: Optional[str] = None
    image_bytes: Optional[bytes] = None

    if raw_b64:
        mime_type = "image/jpeg"
        if raw_b64.startswith("data") and "," in raw_b64:
            header, b64_payload = raw_b64.split(",", 1)
            mime_type = header.split(";")[0][5:]   
        else: 
            b64_payload = raw_b64
        
        image_bytes = base64.b64decode(b64_payload)
        has_image = True

    genai_content: Optional[Any] = None
    if genai_types is not None:
        parts = [genai_types.Part.from_text(text=text_prompt)]
        if has_image and image_bytes and mime_type:
            parts.append(genai_types.Part.from_bytes(data=image_bytes, mime_type=mime_type))
        genai_content = genai_types.Content(role="user", parts=parts)

    return {
        "text_prompt": text_prompt,
        "has_image": has_image,
        "mime_type": mime_type,
        "image_bytes": image_bytes,
        "genai_content": genai_content,
    }


# ============================================================================
# Stage Synthesizers (Warmup, Scaling & Cues, Timeline)
# ============================================================================


def synthesize_warmup_plan(
    parsed_wod: ParsedWodOutput,
    session_input: SessionInput,
    history_res: Dict[str, Any],
) -> WarmupPlanOutput:
    """Synthesizes a 2-phase warm-up (Phase 1 Reaction Game + Phase 2 Joint Mobility Flow),
    rotating games if the coach recently used one in `history_res`, and providing `alternativeGames`
    for 1-click UI game swapping.
    """
    recent_games = [
        str(w.get("gameTitle", "")).lower()
        for w in (history_res.get("recentWarmups") or [])
    ]
    constraints_lc = (session_input.coachConstraints or "").lower()
    wants_partner = "partner" in constraints_lc or "mirror" in constraints_lc

    all_games: List[WarmupPhaseSpec] = [
        WarmupPhaseSpec(
            name="Phase 1: PVC Broomstick Circle Drop & Switch (Reaction Game)",
            durationMinutes=4,
            instructions=(
                "Athletes stand in a 2-meter circle, each balancing a vertical PVC broomstick on the floor with one "
                "palm on top. On Coach's cue ('LEFT!', 'RIGHT!', 'CLAP-CATCH!', '360 SPIN!'), athletes leave their "
                "stick upright and lunge to catch their neighbor's stick before it hits the floor. "
                "Penalty for any drop: whole class performs 3 synchronized tempo jump squats!"
            ),
            coachingGoal="Break post-workday mental fog, spike lateral hip/ankle reaction, and get the class laughing.",
            drills=[],
        ),
        WarmupPhaseSpec(
            name="Phase 1: Partner Mirror Footwork & Plank Shoulder-Tap Duel",
            durationMinutes=4,
            instructions=(
                "Partners face each other 1.5m apart in an athletic quarter-squat stance. Partner A leads lateral "
                "shuffles and hip taps while Partner B mirrors in real time for 30s. On Coach's whistle ('PLANK!'), "
                "both drop to a high plank for a 15-sec shoulder-tap duel while resisting hip rotation."
            ),
            coachingGoal="Warm up wrists, rotator cuffs, anti-rotational core stability, and active knee/hip flexion.",
            drills=[],
        ),
        WarmupPhaseSpec(
            name="Phase 1: Odd/Even Med-Ball & Cone Sprint Reaction Tag",
            durationMinutes=4,
            instructions=(
                "Partners line up facing each other across a medicine ball. Coach calls math cues or colors; "
                "even = grab the ball, odd = defensive squat shuffle to the outer cone."
            ),
            coachingGoal="Ignite central nervous system speed and hip-hinge readiness before touching a barbell.",
            drills=[],
        ),
    ]

    # Pick primary game avoiding recent history if possible
    ordered_games = list(all_games)
    if wants_partner:
        ordered_games = [all_games[1], all_games[0], all_games[2]]

    chosen_game = ordered_games[0]
    for candidate in ordered_games:
        if not any(rg and rg in candidate.name.lower() for rg in recent_games):
            chosen_game = candidate
            break

    alternatives = [g for g in ordered_games if g.name != chosen_game.name]

    drills = [
        {
            "name": "PVC / Broomstick Pass-Throughs + Around-the-Worlds",
            "reps": "10 Pass-Throughs + 5/side Around-the-Worlds",
            "focus": "Scapulohumeral rhythm, pectoral opening, and locked elbows with ribs tucked down",
        },
        {
            "name": "Broomstick Sotts Press + Overhead Squat Therapy",
            "reps": "6 slow reps (3-sec pause in bottom)",
            "focus": "Thoracic spine extension, external shoulder rotation, and upright bottom-squat posture",
        },
        {
            "name": "Broomstick Kang Squats (Good Morning -> Deep Squat -> Stand)",
            "reps": "8 controlled reps",
            "focus": "Hamstring & glute hinge activation transitioning smoothly into deep hip/ankle flexion",
        },
        {
            "name": "Scapular Pulls + Hollow/Arch Beat Swings or Empty-Bar Primer",
            "reps": "10 Scap Pulls + 5 Skill Primers",
            "focus": "Lat engagement, shoulder girdle control, and crisp triple-extension timing",
        },
    ]

    phase_2 = WarmupPhaseSpec(
        name="Phase 2: Targeted Biomechanical Joint & Movement Prep",
        durationMinutes=8,
        instructions=(
            "Transition athletes into a semi-circle facing the coach so everyone has clear sightlines. "
            "Call reps on cadence to dial in fault-free positions before touching working loads."
        ),
        coachingGoal=f"Mobilize primary joints ({', '.join(parsed_wod.primaryJoints)}) and groove WOD mechanics.",
        drills=drills,  # type: ignore[arg-type]
    )

    style = (
        session_input.coachConstraints.strip()
        if session_input.coachConstraints.strip()
        else "Broomstick (PVC) Reaction Game + Joint Mobility Flow"
    )

    return WarmupPlanOutput(
        gameTitle=chosen_game.name,
        style=style,
        durationMinutes=12,
        phases=[chosen_game, phase_2],
        alternativeGames=alternatives,
        drillNames=[d["name"] for d in drills],
    )


def synthesize_scaling_and_cues(parsed_wod: ParsedWodOutput) -> ScalingAndCuesOutput:
    """Synthesizes external/internal cues, common faults, and 4-tier scaling for each movement."""
    movements = parsed_wod.movements or ["Front Squat", "Thruster", "Pull-Up"]
    entries: List[ExerciseCoachingEntry] = []

    for raw_name in movements:
        q = raw_name.strip().lower()
        if "thruster" in q or "cluster" in q:
            data = {
                "name": raw_name.strip(),
                "category": "Weightlifting — Compound Squat-to-Press",
                "primaryJoints": ["shoulders", "thoracic_spine", "hips", "knees", "wrists"],
                "cues": {
                    "external": [
                        "Point your elbow lasers at the front wall on the squat, then launch the bar into the ceiling",
                        "Pop the barbell off your shoulders with violent hip extension before your arms press",
                        "Poke your head through the window at lockout so the bar stacks over your heels",
                    ],
                    "internal": [
                        "Breathe in at the top lockout, hold 360-degree core brace through the bottom turnaround",
                        "Squeeze glutes and quads hard at the top of the leg drive to transfer force into the bar",
                    ],
                    "commonFaults": [
                        {
                            "fault": "Pressing with the arms too early while still rising out of the squat",
                            "fix": "Cue: 'Patient arms! Jump the bar off your shoulders—legs first, arms second'",
                        },
                        {
                            "fault": "Elbows dropping in the squat, crashing the bar forward onto the wrists",
                            "fix": "Cue: 'Lead with your fingertips and drive elbows up out of the hole'",
                        },
                    ],
                },
                "scaling": {
                    "rxPlus": "Heavier barbell (115/80 lb) or unbroken sets across all rounds",
                    "scaled": "Lighter Barbell (65/45 lb) or Dual Dumbbell Thrusters (30/20 lb) for smooth cycle speed",
                    "seniorsMasters": "Squat to 15\" Box target + Dual Dumbbell Push Press (neutral grip protects wrists & shoulders)",
                    "injured": {
                        "wristOrShoulder": "Single-Arm Dumbbell Thruster (healthy side) or Goblet Squat + Russian KB Swing",
                        "kneePain": "Push Press from Rack (eliminates deep knee flexion) or Thruster to high 18\" Box",
                        "lowerBack": "Light Dual Dumbbell Thruster to Box with strict upright torso",
                    },
                },
            }
        elif "squat" in q:
            data = {
                "name": raw_name.strip(),
                "category": "Weightlifting — Squat",
                "primaryJoints": ["hips", "knees", "ankles", "thoracic_spine", "wrists"],
                "cues": {
                    "external": [
                        "Point your elbow lasers straight at the front wall throughout the entire squat",
                        "Spread the floor apart with your shoes as you sit down between your heels",
                    ],
                    "internal": [
                        "Inhale a big 360-degree belly breath into your lifting belt before unlocking hips",
                    ],
                    "commonFaults": [
                        {
                            "fault": "Elbows dropping at the bottom, pitching the barbell forward onto the wrists",
                            "fix": "Cue: 'Punch elbows toward the ceiling—lead with your fingertips!'",
                        },
                    ],
                },
                "scaling": {
                    "rxPlus": "Pause Front Squat (2-sec rock-bottom pause without losing thoracic tension) or +10% load",
                    "scaled": "Lighter Barbell Front Squat or Goblet Squat with Kettlebell/Dumbbell held at chest",
                    "seniorsMasters": "Goblet Squat or Cross-Arm Squat to a 15\" Box target",
                    "injured": {
                        "wristOrShoulder": "Zombie Front Squat (arms straight out), Cross-Arm Squat, or Belt Squat",
                        "kneePain": "Box Squat to parallel with vertical shins (push hips back first)",
                        "lowerBack": "Dual Kettlebell Front-Rack Split Squat or Goblet Squat to Box with upright torso",
                    },
                },
            }
        else:
            data = {
                "name": raw_name.strip(),
                "category": "Functional Conditioning & Gymnastics",
                "primaryJoints": ["shoulders", "hips", "knees", "core"],
                "cues": {
                    "external": [
                        "Drive forcefully through the floor while maintaining a neutral head-to-heel line",
                        "Push the bar away at the top so you flow directly into the next rep",
                    ],
                    "internal": [
                        "Brace your abdominal wall 360 degrees before initiating every rep",
                    ],
                    "commonFaults": [
                        {
                            "fault": "Loss of midline stability under metabolic fatigue",
                            "fix": "Cue: 'Ribs locked to belt buckle—reset posture before the next rep'",
                        },
                    ],
                },
                "scaling": {
                    "rxPlus": "Chest-to-Bar / heavier load or unbroken sets with strict tempo",
                    "scaled": "Banded/Jumping variation or 75% working weight for unbroken mechanics",
                    "seniorsMasters": "Strict Ring Rows or controlled tempo (3-1-1) with joint-friendly range of motion",
                    "injured": {
                        "wristOrShoulder": "Neutral-grip Ring Rows or single-arm dumbbell variation on healthy side",
                        "kneePain": "Strict upper-body focus or step-box dismount (no jumping impact)",
                        "lowerBack": "Unilateral supported variation with zero spinal shear",
                    },
                },
            }
        entries.append(ExerciseCoachingEntry.model_validate(data))

    return ScalingAndCuesOutput(exercises=entries)


async def synthesize_and_verify_timeline(
    total_minutes: int,
    warmup_plan: WarmupPlanOutput,
    parsed_wod: ParsedWodOutput,
    mcp_client: RustMcpClient,
) -> TimelineOutput:
    """Builds a contiguous 0..total_minutes class timeline and verifies it via Rust MCP `verify_timeline_math`."""
    total_min = max(total_minutes, 20)
    warmup_min = max(min(warmup_plan.durationMinutes, 18), 6)
    brief_min = 4
    cooldown_min = 5
    work_blocks = parsed_wod.blocks or [
        WorkoutBlockSpec(type="metcon", name="Primary Conditioning (Metcon)", allocatedMinutes=15)
    ]

    schedule: List[Dict[str, Any]] = []
    cursor = 0

    schedule.append(
        {
            "startMin": cursor,
            "endMin": cursor + brief_min,
            "label": "Whiteboard Brief, Stimulus & Injury/Scaling Check",
            "type": "brief",
            "notes": "Welcome athletes, explain the intended stimulus, and discreetly check for injury flags.",
        }
    )
    cursor += brief_min

    schedule.append(
        {
            "startMin": cursor,
            "endMin": cursor + warmup_min,
            "label": warmup_plan.style,
            "type": "warmup",
            "notes": f"{warmup_plan.gameTitle} followed by targeted joint prep flow.",
        }
    )
    cursor += warmup_min

    remaining_for_work = max(total_min - cursor - cooldown_min, len(work_blocks) * 6)
    per_block_total = remaining_for_work // len(work_blocks)

    for idx, wb in enumerate(work_blocks):
        trans_min = min(3, max(per_block_total // 4, 2))
        schedule.append(
            {
                "startMin": cursor,
                "endMin": cursor + trans_min,
                "label": f"Transition: Station Setup & Load Prep for {wb.name}",
                "type": "transition",
                "notes": "Assign lanes/racks and verify 5 unbroken rehearsal reps at each athlete's scaling tier.",
            }
        )
        cursor += trans_min

        work_end = (total_min - cooldown_min) if (idx + 1 == len(work_blocks)) else (cursor + (per_block_total - trans_min))
        schedule.append(
            {
                "startMin": cursor,
                "endMin": work_end,
                "label": wb.name,
                "type": wb.type,
                "notes": wb.notes or "Deliver crisp 5-word external cues and protect midline mechanics under fatigue.",
            }
        )
        cursor = work_end

    schedule.append(
        {
            "startMin": cursor,
            "endMin": total_min,
            "label": "Cooldown, Box Breathing, Fist Bumps & Equipment Reset",
            "type": "cooldown",
            "notes": "2 minutes parasympathetic nasal breathing on the floor + wipe down equipment.",
        }
    )

    verified = await mcp_client.verify_timeline_math(total_minutes=total_min, blocks=schedule)
    return TimelineOutput.model_validate(
        {
            "totalMinutes": verified.get("totalMinutes", total_min),
            "feasibilityStatus": verified.get("feasibilityStatus", "OPTIMAL"),
            "coachWarnings": verified.get("coachWarnings", []),
            "blocks": verified.get("blocks", schedule),
        }
    )


# ============================================================================
# Stage-by-Stage Pipeline Executor (TODO 3.2)
# ============================================================================


class CoachBuddyPipelineExecutor:
    """Executes the 3-Stage CoachBuddy workflow with real-time Redis SSE progress events:
    Stage 1: `WodParserAgent` (`parser`)
    Stage 2: `asyncio.gather(WarmupPlannerAgent, ScalingAndCuesAgent)` (`warmup` + `scaling`)
    Stage 3: `TimelineLogisticsAgent` (`timeline`)
    """

    def __init__(self, mcp_client: Optional[RustMcpClient] = None) -> None:
        self.mcp_client = mcp_client or RustMcpClient()

    async def run_parser_stage(self, req: OrchestrateRequest) -> ParsedWodOutput:
        """Runs Stage 1: Builds multimodal input and parses WOD structure into `ParsedWodOutput`."""
        multimodal = build_multimodal_user_content(req.input)
        prov, mod, key = resolve_agent_route("parser", req.routingConfig)
        if _is_live_api_key(key):
            sys_prompt = (
                "You are WodParserAgent, an elite CrossFit Level-3 Head Coach & Biomechanics Analyst.\n"
                "Analyze the WOD text (and any attached whiteboard image) and return a JSON object matching:\n"
                "{\n"
                '  "title": "string",\n'
                '  "extractedText": "string",\n'
                '  "blocks": [{"type": "strength|skill|metcon|accessory", "name": "string", "allocatedMinutes": 15, "notes": "string"}],\n'
                '  "movements": ["Front Squat", "Thruster"],\n'
                '  "primaryJoints": ["shoulders", "thoracic_spine", "hips", "knees", "wrists", "ankles"],\n'
                '  "stimulus": "string describing intended metabolic & neurological stimulus"\n'
                "}"
            )
            try:
                llm_json = await call_live_llm_json(
                    provider=prov,
                    model=mod,
                    api_key=key,
                    system_prompt=sys_prompt,
                    user_prompt=multimodal["text_prompt"],
                    image_bytes=multimodal["image_bytes"],
                    mime_type=multimodal["mime_type"],
                )
                if req.input.title and not llm_json.get("title"):
                    llm_json["title"] = req.input.title
                return ParsedWodOutput.model_validate(llm_json)
            except Exception as exc:
                err_s = str(exc)
                if any(code in err_s for code in ("(400)", "(401)", "(403)")):
                    raise
                logger.warning("WodParserAgent hit transient error/timeout; using deterministic parser: %s", repr(exc))

        parsed = parse_wod_deterministically(req.input)
        return ParsedWodOutput.model_validate(parsed.model_dump())

    async def run_warmup_stage(self, req: OrchestrateRequest, parsed_wod: ParsedWodOutput) -> WarmupPlanOutput:
        """Runs Stage 2a: Queries Rust MCP `get_recent_coaching_history` and synthesizes `WarmupPlanOutput`."""
        history_res = await self.mcp_client.get_recent_coaching_history(coach_id=req.coachId, limit=5)
        prov, mod, key = resolve_agent_route("warmup", req.routingConfig)
        if _is_live_api_key(key):
            recent_games = [w.get("gameTitle", "") for w in (history_res.get("recentWarmups") or [])]
            sys_prompt = (
                "You are WarmupPlannerAgent, an elite CrossFit Level-3 Coach.\n"
                "Design a 2-phase warm-up (Phase 1: 4-min Fun Reaction Game; Phase 2: 8-min Joint & PVC/Barbell Mobility Flow) "
                "plus 2 alternative Phase 1 reaction games in `alternativeGames` so the coach can swap games in 1 click.\n"
                "Avoid repeating these recent games if possible: " + json.dumps(recent_games) + "\n"
                "Return strictly JSON matching:\n"
                "{\n"
                '  "style": "string",\n'
                '  "gameTitle": "Phase 1: ...",\n'
                '  "durationMinutes": 12,\n'
                '  "phases": [\n'
                '    {"name": "Phase 1: ...", "durationMinutes": 4, "instructions": "...", "coachingGoal": "...", "drills": []},\n'
                '    {"name": "Phase 2: ...", "durationMinutes": 8, "instructions": "...", "coachingGoal": "...", "drills": [{"name": "...", "reps": "...", "focus": "..."}]}\n'
                "  ],\n"
                '  "alternativeGames": [\n'
                '    {"name": "Phase 1: ...", "durationMinutes": 4, "instructions": "...", "coachingGoal": "...", "drills": []}\n'
                "  ],\n"
                '  "drillNames": ["string"]\n'
                "}"
            )
            user_prompt = (
                f"Parsed WOD: {parsed_wod.model_dump_json()}\n"
                f"Coach Constraints: {req.input.coachConstraints}"
            )
            try:
                llm_json = await call_live_llm_json(prov, mod, key, sys_prompt, user_prompt)
                fallback_warmup = synthesize_warmup_plan(parsed_wod, req.input, history_res)
                if not llm_json.get("alternativeGames"):
                    llm_json["alternativeGames"] = [g.model_dump() for g in fallback_warmup.alternativeGames]
                if not llm_json.get("drillNames"):
                    drills_list: List[str] = []
                    for ph in llm_json.get("phases", []):
                        for d in ph.get("drills") or []:
                            if isinstance(d, dict) and d.get("name"):
                                drills_list.append(str(d["name"]))
                    llm_json["drillNames"] = drills_list or fallback_warmup.drillNames
                if not llm_json.get("gameTitle") and llm_json.get("phases"):
                    llm_json["gameTitle"] = llm_json["phases"][0].get("name", fallback_warmup.gameTitle)
                return WarmupPlanOutput.model_validate(llm_json)
            except Exception as exc:
                err_s = str(exc)
                if any(code in err_s for code in ("(400)", "(401)", "(403)")):
                    raise
                logger.warning("WarmupPlannerAgent hit transient error/timeout; using parsed_wod warmup synthesizer: %s", repr(exc))

        warmup = synthesize_warmup_plan(parsed_wod, req.input, history_res)
        return WarmupPlanOutput.model_validate(warmup.model_dump())

    async def run_scaling_stage(self, req: OrchestrateRequest, parsed_wod: ParsedWodOutput) -> ScalingAndCuesOutput:
        """Runs Stage 2b: Generates movement cues & 4-tier scaling validated via `ScalingAndCuesOutput`."""
        prov, mod, key = resolve_agent_route("scaling", req.routingConfig)
        if _is_live_api_key(key):
            # Stagger parallel call by 350ms to avoid burst 503/429 on free-tier keys
            await asyncio.sleep(0.35)
            sys_prompt = (
                "You are ScalingAndCuesAgent, an elite CrossFit Level-3 Coach & Physical Therapist.\n"
                "For every movement in `parsedWod.movements`, generate External cues, Internal cues, Fault->Fix pairs, "
                "and 4-tier scaling (`rxPlus`, `scaled`, `seniorsMasters`, and `injured` with `wristOrShoulder`, `kneePain`, `lowerBack`).\n"
                "Return strictly JSON matching:\n"
                "{\n"
                '  "exercises": [\n'
                "    {\n"
                '      "name": "string",\n'
                '      "category": "string",\n'
                '      "primaryJoints": ["shoulders", "hips"],\n'
                '      "cues": {\n'
                '        "external": ["punch the ceiling", "spread the floor"],\n'
                '        "internal": ["brace core 360"],\n'
                '        "commonFaults": [{"fault": "...", "fix": "..."}]\n'
                "      },\n"
                '      "scaling": {\n'
                '        "rxPlus": "...",\n'
                '        "scaled": "...",\n'
                '        "seniorsMasters": "...",\n'
                '        "injured": {"wristOrShoulder": "...", "kneePain": "...", "lowerBack": "..."}\n'
                "      }\n"
                "    }\n"
                "  ]\n"
                "}"
            )
            user_prompt = (
                f"Movements: {json.dumps(parsed_wod.movements)}\n"
                f"Stimulus: {parsed_wod.stimulus}\n"
                f"Coach Constraints: {req.input.coachConstraints}"
            )
            try:
                llm_json = await call_live_llm_json(prov, mod, key, sys_prompt, user_prompt)
                return ScalingAndCuesOutput.model_validate(llm_json)
            except Exception as exc:
                err_s = str(exc)
                if any(code in err_s for code in ("(400)", "(401)", "(403)")):
                    raise
                logger.warning("ScalingAndCuesAgent hit transient error/timeout; using parsed_wod scaling synthesizer: %s", repr(exc))

        scaling = synthesize_scaling_and_cues(parsed_wod)
        return ScalingAndCuesOutput.model_validate(scaling.model_dump())

    async def run_timeline_stage(
        self,
        req: OrchestrateRequest,
        warmup_plan: WarmupPlanOutput,
        parsed_wod: ParsedWodOutput,
    ) -> TimelineOutput:
        """Runs Stage 3: Builds schedule and verifies math via Rust MCP `verify_timeline_math`."""
        prov, mod, key = resolve_agent_route("timeline", req.routingConfig)
        total_min = max(req.input.targetDurationMinutes, 20)
        if _is_live_api_key(key):
            sys_prompt = (
                "You are TimelineLogisticsAgent, a CrossFit Box Logistics Coordinator.\n"
                f"Create a contiguous minute-by-minute class schedule from minute 0 to minute {total_min}.\n"
                "Every block's `startMin` MUST equal the previous block's `endMin`, starting at 0 and ending at "
                f"exactly {total_min}.\n"
                "Allowed block `type` values: `brief`, `warmup`, `transition`, `strength`, `skill`, `metcon`, `cooldown`.\n"
                "Return strictly JSON matching:\n"
                "{\n"
                f'  "totalMinutes": {total_min},\n'
                '  "blocks": [\n'
                '    {"startMin": 0, "endMin": 4, "label": "Whiteboard Brief", "type": "brief", "notes": "..."}\n'
                "  ]\n"
                "}"
            )
            user_prompt = (
                f"Target Duration: {total_min} minutes\n"
                f"Warmup Duration: {warmup_plan.durationMinutes} min ({warmup_plan.style} - {warmup_plan.gameTitle})\n"
                f"Workout Blocks: {json.dumps([b.model_dump() for b in parsed_wod.blocks])}"
            )
            try:
                llm_json = await call_live_llm_json(prov, mod, key, sys_prompt, user_prompt)
                raw_blocks = llm_json.get("blocks") or []
                verified = await self.mcp_client.verify_timeline_math(total_minutes=total_min, blocks=raw_blocks)
                return TimelineOutput.model_validate(
                    {
                        "totalMinutes": verified.get("totalMinutes", total_min),
                        "feasibilityStatus": verified.get("feasibilityStatus", "OPTIMAL"),
                        "coachWarnings": verified.get("coachWarnings", []),
                        "blocks": verified.get("blocks", raw_blocks),
                    }
                )
            except Exception as exc:
                err_s = str(exc)
                if any(code in err_s for code in ("(400)", "(401)", "(403)")):
                    raise
                logger.warning("TimelineLogisticsAgent hit transient error/timeout; using parsed_wod timeline synthesizer: %s", repr(exc))

        timeline = await synthesize_and_verify_timeline(
            total_minutes=total_min,
            warmup_plan=warmup_plan,
            parsed_wod=parsed_wod,
            mcp_client=self.mcp_client,
        )
        return TimelineOutput.model_validate(timeline.model_dump())

    async def execute(
        self,
        req: OrchestrateRequest,
        publish_event: Optional[EventPublisher] = None,
    ) -> Dict[str, Any]:
        start_ts = time.perf_counter()

        async def emit(step: str, status: str, message: str, extra: Optional[Dict[str, Any]] = None) -> None:
            if publish_event:
                payload: Dict[str, Any] = {
                    "sessionId": req.sessionId,
                    "step": step,
                    "status": status,
                    "message": message,
                    "timestamp": datetime.now(timezone.utc).isoformat(),
                }
                if extra:
                    payload.update(extra)
                await publish_event(req.sessionId, step, payload)

        p_prov, p_mod, p_key = resolve_agent_route("parser", req.routingConfig)
        w_prov, w_mod, w_key = resolve_agent_route("warmup", req.routingConfig)
        s_prov, s_mod, s_key = resolve_agent_route("scaling", req.routingConfig)
        t_prov, t_mod, t_key = resolve_agent_route("timeline", req.routingConfig)

        # =====================================================================
        # TODO(engineer) 3.2: Wire the 3 Sequential/Parallel Stages with SSE Events
        #
        # Instructions:
        # 1. Stage 1 (Parser):
        t_parser = time.perf_counter()
        await emit("parser", "running", "WodParserAgent analyzing WOD...")
        parsed_wod = await self.run_parser_stage(req)
        parser_latency_ms = int((time.perf_counter() - t_parser) * 1000)
        await emit("parser", "completed", f"Extracted {len(parsed_wod.movements)} movements")
        #
        # 2. Stage 2 (Parallel Warmup + Scaling):
        t_parallel = time.perf_counter()
        await emit("warmup", "running", "Running WarmupPlannerAgent & ScalingAndCuesAgent in parallel...")
        await emit("scaling", "running", "Generating 4-tier scaling & coaching cues...")
        #    - Run both concurrently with `asyncio.gather`:
        try:
            asyncio.get_running_loop()
            warmup_plan, scaling_out = await asyncio.gather(
                self.run_warmup_stage(req, parsed_wod),
                self.run_scaling_stage(req, parsed_wod),
            )
        except RuntimeError:
            warmup_plan = await self.run_warmup_stage(req, parsed_wod)
            scaling_out = await self.run_scaling_stage(req, parsed_wod)
        parallel_latency_ms = int((time.perf_counter() - t_parallel) * 1000)
        await emit("warmup", "completed", f"Warm-up ready: {warmup_plan.gameTitle}")
        await emit("scaling", "completed", f"Scaled {len(scaling_out.exercises)} exercises")
        
        # 3. Stage 3 (Timeline Logistics):
        t_timeline = time.perf_counter()
        await emit("timeline", "running", "Verifying timeline math via Rust MCP...")
        timeline_out = await self.run_timeline_stage(req, warmup_plan, parsed_wod)
        timeline_latency_ms = int((time.perf_counter() - t_timeline) * 1000)
        await emit("timeline", "completed", f"Timeline verified ({timeline_out.feasibilityStatus})")
        
        # 4. Final Completion Event:
        await emit("completed", "completed", "Multi-agent WOD session plan complete!")
        
        total_latency_ms = int((time.perf_counter() - start_ts) * 1000)

        agent_traces = [
            {
                "agent": "WodParserAgent",
                "provider": p_prov,
                "model": p_mod,
                "executionMode": "google-adk-litellm" if p_key else "deterministic-stage",
                "latencyMs": parser_latency_ms,
                "status": "completed",
            },
            {
                "agent": "WarmupPlannerAgent",
                "provider": w_prov,
                "model": w_mod,
                "executionMode": "google-adk-litellm" if w_key else "deterministic-stage",
                "mcpTool": "get_recent_coaching_history",
                "latencyMs": parallel_latency_ms,
                "status": "completed",
            },
            {
                "agent": "ScalingAndCuesAgent",
                "provider": s_prov,
                "model": s_mod,
                "executionMode": "google-adk-litellm" if s_key else "deterministic-stage",
                "latencyMs": parallel_latency_ms,
                "status": "completed",
            },
            {
                "agent": "TimelineLogisticsAgent",
                "provider": t_prov,
                "model": t_mod,
                "executionMode": "google-adk-litellm" if t_key else "deterministic-stage",
                "mcpTool": "verify_timeline_math",
                "latencyMs": timeline_latency_ms,
                "status": "completed",
            },
        ]

        input_dump = req.input.model_dump()
        if not (input_dump.get("rawText") or "").strip() and parsed_wod.extractedText:
            input_dump["rawText"] = parsed_wod.extractedText

        return {
            "_id": req.sessionId,
            "sessionId": req.sessionId,
            "coachId": req.coachId,
            "status": "completed",
            "title": parsed_wod.title,
            "createdAt": datetime.now(timezone.utc).isoformat(),
            "orchestrationLatencyMs": total_latency_ms,
            "input": input_dump,
            "parsedWod": parsed_wod.model_dump(),
            "warmup": warmup_plan.model_dump(),
            "exercises": [ex.model_dump() for ex in scaling_out.exercises],
            "timeline": timeline_out.model_dump(),
            "agentTraces": agent_traces,
        }
