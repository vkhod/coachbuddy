export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export interface Coach {
  id: string;
  email: string;
  displayName: string;
  boxName: string;
  defaultClassDurationMin: number;
  createdAt?: string;
}

export interface LLMAPIKey {
  id: string;
  coachId: string;
  provider: "gemini" | "openai" | "anthropic" | "groq" | string;
  label?: string;
  keyHint: string;
  isPrimary?: boolean;
  isDefault?: boolean;
  createdAt: string;
  updatedAt?: string;
}

export type AgentRole = "global" | "parser" | "warmup" | "scaling" | "timeline";

export interface AgentModelPref {
  id?: string;
  coachId?: string;
  agentRole: AgentRole;
  apiKeyId?: string;
  provider: string;
  modelIdentifier: string;
  updatedAt?: string;
}

export interface SessionInput {
  title?: string;
  rawText: string;
  imageBase64?: string;
  coachConstraints: string;
  targetDurationMinutes: number;
  equipmentFocus?: string[];
}

export interface WarmupDrill {
  name: string;
  reps: string;
  focus: string;
}

export interface WarmupPhase {
  name: string;
  durationMinutes: number;
  instructions: string;
  coachingGoal: string;
  drills?: WarmupDrill[];
}

export interface FaultFix {
  fault: string;
  fix: string;
}

export interface InjuredScaling {
  wristOrShoulder: string;
  kneePain: string;
  lowerBack: string;
}

export interface ExerciseScaling {
  rxPlus: string;
  scaled: string;
  seniorsMasters: string;
  injured: InjuredScaling;
}

export interface ExerciseCues {
  external: string[];
  internal: string[];
  commonFaults: FaultFix[];
}

export interface SessionExercise {
  name: string;
  category: string;
  cues: ExerciseCues;
  scaling: ExerciseScaling;
}

export interface TimelineBlock {
  startMin: number;
  endMin: number;
  label: string;
  type: "brief" | "warmup" | "transition" | "strength" | "metcon" | "cooldown" | string;
  notes: string;
}

export interface SessionTimeline {
  totalMinutes: number;
  feasibilityStatus: "OPTIMAL" | "TIGHT" | "OVERFLOW" | string;
  coachWarnings?: string[];
  blocks: TimelineBlock[];
}

export interface AgentTrace {
  agent: string;
  provider: string;
  model: string;
  mcpTool?: string;
  status: string;
}

export interface WodSession {
  _id?: string;
  sessionId: string;
  coachId: string;
  status: string;
  title: string;
  createdAt: string;
  input: SessionInput;
  parsedWod: {
    blocks: Array<{ type: string; name: string; allocatedMinutes: number }>;
    movements: string[];
    primaryJoints: string[];
    stimulus: string;
  };
  warmup: {
    gameTitle?: string;
    style: string;
    durationMinutes: number;
    phases: WarmupPhase[];
    alternativeGames?: WarmupPhase[];
    drillNames?: string[];
  };
  exercises: SessionExercise[];
  timeline: SessionTimeline;
  agentTraces: AgentTrace[];
}

export interface SharedLink {
  shortCode: string;
  shareUrl: string;
  sessionId: string;
  coachId: string;
  includeCoachNotes: boolean;
  qrSvg: string;
  viewCount: number;
  createdAt: string;
}

export interface SharedSessionPayload {
  share: SharedLink;
  session: WodSession;
}

export const WOD_PRESETS: Array<{
  id: string;
  label: string;
  tag: string;
  title: string;
  rawText: string;
  coachConstraints: string;
  duration: number;
}> = [
    {
      id: "preset-fran-fs",
      label: "Heavy Front Squat + Fran-style Thruster/Pull-up Metcon",
      tag: "STRENGTH + BENCHMARK SPRINT",
      title: "Heavy Front Squat + Fran-style Thruster/Pull-up Metcon",
      duration: 60,
      coachConstraints:
        "Mostly Broomstick / PVC warm-up with a fun reaction game so athletes don't get bored. Emphasize front rack thoracic & wrist prep.",
      rawText: `PART A — STRENGTH (15 MIN)
Every 2:30 x 6 Sets:
3 Front Squats @ 80-85% 1RM (Build to a heavy triple, 2-sec pause in bottom on rep 1)

PART B — METCON: "FRAN RELOADED" (10 MIN CAP)
21 - 15 - 9 Reps For Time:
- Thrusters (95/65 lb | Rx+ 115/80 lb)
- Pull-Ups (Rx+ Chest-to-Bar Pull-Ups)
Immediately into:
400m Run (or 500m Row) Buy-Out`,
    },
    {
      id: "preset-snatch-emom",
      label: "Snatch Skill + 20m Engine EMOM",
      tag: "OLYMPIC SKILL + AEROBIC ENGINE",
      title: "Snatch Skill + 20m Engine EMOM",
      duration: 60,
      coachConstraints:
        "Thoracic & Overhead Mobility focus with PVC broomstick drills. Gamified reaction drill to wake up fast-twitch hip extension.",
      rawText: `PART A — OLYMPIC BARBELL SKILL (12 MIN)
Every 90s x 8 Sets:
1 Hang Power Snatch + 1 Squat Snatch + 1 Overhead Squat
Focus on crisp turnover and active overhead shoulder stability (65-75% 1RM).

PART B — 20 MINUTE ENGINE EMOM (5 ROUNDS)
Min 1: 15/12 Cal Assault Bike or Row
Min 2: 12 Power Snatches (75/55 lb | Rx+ 95/65 lb)
Min 3: 12 Toes-to-Bar (Rx+ 15 Toes-to-Bar)
Min 4: Rest & Chalk Check`,
    },
    {
      id: "preset-deadlift-partner",
      label: "Deadlift & Box Jump Partner Grinder",
      tag: "POSTERIOR CHAIN + TEAM GRINDER",
      title: "Deadlift & Box Jump Partner Grinder",
      duration: 60,
      coachConstraints:
        "Partner Warm-up with Broomstick hinge & hamstring prep. Extra attention to lower-back bracing and Achilles/knee safety on box jumps.",
      rawText: `PART A — HEAVY HINGE (14 MIN)
5 - 5 - 3 - 3 - 3 Touch-and-Go Deadlifts
Build to a crisp heavy 3-rep set (leave 2 reps in the tank, zero lumbar rounding).

PART B — PARTNER GRINDER (18 MIN AMRAP)
In Teams of 2 (You Go, I Go — split reps evenly):
- 20 Deadlifts (225/155 lb | Rx+ 275/185 lb)
- 24 Box Jumps (24/20 in — step down mandatory)
- 28 Wall Ball Shots (20/14 lb to 10/9 ft target)`,
    },
  ];

export const WARMUP_STYLE_CHIPS = [
  "Mostly Broomstick / PVC",
  "Gamified / Reaction Drill",
  "Partner Warm-up",
  "Thoracic & Overhead Mobility",
  "No Running / Row & Bike Only",
];

export const MODEL_CATALOG: Record<
  string,
  Array<{ id: string; label: string; badge: string }>
> = {
  gemini: [
    {
      id: "gemini/gemini-3.8-flash",
      label: "Gemini 3.8 Flash",
      badge: "Latest Fast Multimodal + ADK",
    },
    {
      id: "gemini/gemini-3.8-flash-lite",
      label: "Gemini 3.8 Flash-Lite",
      badge: "Ultra-Low Latency & High Availability",
    },
    {
      id: "gemini/gemini-3.8-pro",
      label: "Gemini 3.8 Pro",
      badge: "Deep Biomechanics Reasoning",
    },
  ],
  openai: [
    {
      id: "openai/gpt-4o",
      label: "GPT-4o",
      badge: "Omni Vision + Fast Tool Use",
    },
    {
      id: "openai/gpt-4o-mini",
      label: "GPT-4o Mini",
      badge: "Low-Latency Subagent",
    },
  ],
  anthropic: [
    {
      id: "anthropic/claude-3-7-sonnet-latest",
      label: "Claude 3.7 Sonnet",
      badge: "Precision Coaching Cues",
    },
    {
      id: "anthropic/claude-3-5-haiku-latest",
      label: "Claude 3.5 Haiku",
      badge: "Ultra-Fast Timeline Math",
    },
  ],
  groq: [
    {
      id: "groq/llama-3.3-70b-versatile",
      label: "Llama 3.3 70B (Groq LPU)",
      badge: "Sub-Second Inference",
    },
  ],
};

// --- Local Storage Keys for Resilient Offline / Demo Continuity ---
const LS_TOKEN = "cb_jwt_token";
const LS_COACH = "cb_coach_profile";
const LS_KEYS = "cb_byok_keys";
const LS_PREFS = "cb_agent_prefs";
const LS_SESSIONS = "cb_wod_sessions";
const LS_SHARES = "cb_shared_links";

export const DEFAULT_COACH: Coach = {
  id: "00000000-0000-0000-0000-000000000001",
  email: "coach@crossfitbox.com",
  displayName: "Coach Alex Mercer (CF-L3)",
  boxName: "CrossFit Iron Horizon",
  defaultClassDurationMin: 60,
};

const DEFAULT_AGENT_PREFS: AgentModelPref[] = [
  {
    agentRole: "global",
    provider: "gemini",
    modelIdentifier: "gemini/gemini-3.8-flash",
  },
  {
    agentRole: "parser",
    provider: "gemini",
    modelIdentifier: "gemini/gemini-3.8-flash",
  },
  {
    agentRole: "warmup",
    provider: "gemini",
    modelIdentifier: "gemini/gemini-3.8-flash",
  },
  {
    agentRole: "scaling",
    provider: "gemini",
    modelIdentifier: "gemini/gemini-3.8-flash",
  },
  {
    agentRole: "timeline",
    provider: "gemini",
    modelIdentifier: "gemini/gemini-3.8-flash",
  },
];

function isBrowser(): boolean {
  return typeof window !== "undefined";
}

export function getStoredToken(): string | null {
  if (!isBrowser()) return null;
  return localStorage.getItem(LS_TOKEN);
}

export function setStoredToken(token: string | null) {
  if (!isBrowser()) return;
  if (token) {
    localStorage.setItem(LS_TOKEN, token);
  } else {
    localStorage.removeItem(LS_TOKEN);
  }
}

export function getStoredCoach(): Coach {
  if (!isBrowser()) return DEFAULT_COACH;
  try {
    const raw = localStorage.getItem(LS_COACH);
    if (raw) return JSON.parse(raw);
  } catch {
    // ignore
  }
  return DEFAULT_COACH;
}

export function setStoredCoach(coach: Coach) {
  if (!isBrowser()) return;
  localStorage.setItem(LS_COACH, JSON.stringify(coach));
}

function getLocalKeys(): LLMAPIKey[] {
  if (!isBrowser()) return [];
  try {
    const raw = localStorage.getItem(LS_KEYS);
    if (raw) {
      const parsed = JSON.parse(raw) as LLMAPIKey[];
      return parsed.filter((k) => k.id !== "key-demo-gemini");
    }
  } catch {
    // ignore
  }
  return [];
}

function saveLocalKeys(keys: LLMAPIKey[]) {
  if (!isBrowser()) return;
  localStorage.setItem(LS_KEYS, JSON.stringify(keys));
}

function getLocalPrefs(): AgentModelPref[] {
  if (!isBrowser()) return DEFAULT_AGENT_PREFS;
  try {
    const raw = localStorage.getItem(LS_PREFS);
    if (raw) {
      const parsed = JSON.parse(raw) as AgentModelPref[];
      if (Array.isArray(parsed) && parsed.length > 0) {
        return parsed.map((p) => ({
          ...p,
          modelIdentifier: p.modelIdentifier
            .replace("gemini-2.5-flash", "gemini-3.8-flash")
            .replace("gemini-2.5-pro", "gemini-3.8-pro"),
        }));
      }
    }
  } catch {
    // ignore
  }
  return DEFAULT_AGENT_PREFS;
}

function saveLocalPrefs(prefs: AgentModelPref[]) {
  if (!isBrowser()) return;
  localStorage.setItem(LS_PREFS, JSON.stringify(prefs));
}

export function generateDeterministicQRSVG(data: string): string {
  // Deterministic high-contrast Tactical Volt & Slate SVG QR Code
  let hash = 2166136261;
  for (let i = 0; i < data.length; i++) {
    hash ^= data.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  const grid = 21;
  const cell = 10;
  const margin = 20;
  const size = grid * cell + margin * 2;
  const rects: string[] = [];

  const isFinder = (r: number, c: number) =>
    (r < 7 && c < 7) ||
    (r < 7 && c >= grid - 7) ||
    (r >= grid - 7 && c < 7);

  const drawFinder = (ox: number, oy: number) => {
    rects.push(
      `<rect x="${ox}" y="${oy}" width="70" height="70" rx="6" fill="none" stroke="#CCFF00" stroke-width="10"/>`
    );
    rects.push(
      `<rect x="${ox + 20}" y="${oy + 20}" width="30" height="30" rx="3" fill="#CCFF00"/>`
    );
  };

  drawFinder(margin, margin);
  drawFinder(margin + (grid - 7) * cell, margin);
  drawFinder(margin, margin + (grid - 7) * cell);

  let seed = Math.abs(hash) || 123456789;
  for (let r = 0; r < grid; r++) {
    for (let c = 0; c < grid; c++) {
      if (isFinder(r, c)) continue;
      seed = (seed * 1664525 + 1013904223) % 4294967296;
      const on = (seed & 1) === 1 || (r === 6 && c % 2 === 0) || (c === 6 && r % 2 === 0);
      if (on) {
        const x = margin + c * cell;
        const y = margin + r * cell;
        rects.push(
          `<rect x="${x + 1}" y="${y + 1}" width="8" height="8" rx="2" fill="#F8FAFC"/>`
        );
      }
    }
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="256" height="256"><rect width="100%" height="100%" rx="16" fill="#0B0F17"/>${rects.join(
    ""
  )}</svg>`;
}

export function buildLocalSessionPlan(
  input: SessionInput,
  prefs: AgentModelPref[] = DEFAULT_AGENT_PREFS,
  sessionIdOverride?: string
): WodSession {
  const sessionId =
    sessionIdOverride ||
    `wod-${Math.random().toString(36).substring(2, 8)}`;
  const rawLower = (input.rawText || "").toLowerCase();
  const constraintsLower = (input.coachConstraints || "").toLowerCase();
  const dur = input.targetDurationMinutes || 60;

  const getPref = (role: AgentRole) =>
    prefs.find((p) => p.agentRole === role) ||
    prefs.find((p) => p.agentRole === "global") || {
      provider: "gemini",
      modelIdentifier: "gemini/gemini-3.8-flash",
    };

  const isSnatch = rawLower.includes("snatch") || rawLower.includes("emom");
  const isDeadlift =
    rawLower.includes("deadlift") ||
    rawLower.includes("box jump") ||
    rawLower.includes("wall ball");

  const title =
    input.title?.trim() ||
    (isSnatch
      ? "Snatch Skill + 20m Engine EMOM"
      : isDeadlift
        ? "Deadlift & Box Jump Partner Grinder"
        : "Heavy Front Squat + Fran-style Thruster/Pull-up Metcon");

  const usesBroomstick =
    constraintsLower.includes("broom") ||
    constraintsLower.includes("pvc") ||
    !constraintsLower;
  const usesPartner =
    constraintsLower.includes("partner") || rawLower.includes("partner");

  const warmupStyle = usesPartner
    ? "Partner Broomstick Reaction Challenge + Synchronized Joint Prep"
    : usesBroomstick
      ? "Broomstick (PVC) Circle Drop Game + Targeted Mobility Flow"
      : "Gamified Cone/Ball Reaction Drill + Dynamic Barbell Prep";

  const exercises: SessionExercise[] = isSnatch
    ? [
      {
        name: "Snatch (Hang Power Snatch + Squat Snatch)",
        category: "Olympic Weightlifting",
        cues: {
          external: [
            "Sweep the bar into your hip crease with tight lats—keep knuckles pointed down",
            "Jump the floor away and punch the ceiling violently on turnover",
            "Show the bottom of your shoes to the platform before pulling under",
          ],
          internal: [
            "Lock your shoulder blades down and back before initiating from the hang",
            "Actively pull your body under the bar and press up into the bar overhead",
          ],
          commonFaults: [
            {
              fault: "Banging the bar horizontally away from the hips (looping bar path)",
              fix: "Cue: 'High elbows & scarecrow pull—keep the bar brushing your shirt'",
            },
            {
              fault: "Soft overhead lockout or forward bar drift in the bottom of the squat",
              fix: "Cue: 'Show your armpits to the front wall and press through the pinky side of the hand'",
            },
          ],
        },
        scaling: {
          rxPlus:
            "Unbroken touch-and-go Squat Snatch complex @ 80% 1RM + 95/65 lb in EMOM",
          scaled:
            "Hang Power Snatch + Overhead Squat to comfortable depth (55/35 lb or PVC/Technique Bar)",
          seniorsMasters:
            "Hang Muscle Snatch + Overhead Plate Hold or Single-Arm Dumbbell Power Snatch from hang",
          injured: {
            wristOrShoulder:
              "Single-Arm DB Snatch (uninjured arm) or Russian Kettlebell Swings (eye-level only)",
            kneePain:
              "Tall Muscle Snatch (no deep knee rebend) + Strict Behind-the-Neck PVC Press",
            lowerBack:
              "High-Hang Dumbbell Snatch from blocks/boxes with strict vertical torso",
          },
        },
      },
      {
        name: "Toes-to-Bar & Assault Bike Engine",
        category: "Gymnastics & Monostructural",
        cues: {
          external: [
            "Press the rig down toward the floor with straight arms to load the kip",
            "Flick your toes to the bar and actively pull your heels back behind the vertical plane",
          ],
          internal: [
            "Engage lats and hollow core together—avoid swinging loosely from the shoulders",
            "Breathe rhythmically on every kip swing",
          ],
          commonFaults: [
            {
              fault: "Tucking knees early and losing the pendulum swing after rep 3",
              fix: "Cue: 'Push away from the bar at the top—eyes on the crossbar'",
            },
          ],
        },
        scaling: {
          rxPlus: "15 Strict or Unbroken Kipping Toes-to-Bar per minute",
          scaled:
            "Hanging Knee Raises above hip crease or Kipping Toes-to-Target",
          seniorsMasters:
            "AbMat V-Ups / Lying Toes-to-Rig (anchors hands on rig upright, zero shoulder hang)",
          injured: {
            wristOrShoulder:
              "Hollow Body Rocks or AbMat Sit-Ups + Bike Erg (hands-free if needed)",
            kneePain:
              "Strict Hanging Knee Raises (step onto box to dismount) + SkiErg or Upper-Body Bike",
            lowerBack:
              "Deadbug Iso-Switches (lumbar pressed flat into floor) + steady Concept2 Row",
          },
        },
      },
    ]
    : isDeadlift
      ? [
        {
          name: "Barbell Deadlift",
          category: "Powerlifting / Posterior Chain",
          cues: {
            external: [
              "Bend the barbell around your shins to lock your lats in",
              "Push the gym floor down with your legs until the bar passes your knees",
              "Stand tall Like a soldier at the top—don't lean back",
            ],
            internal: [
              "Take a big 360-degree belly breath into your belt before pulling slack out of the bar",
              "Feel equal pressure across your mid-foot and heels",
            ],
            commonFaults: [
              {
                fault: "Hips shooting up faster than shoulders off the floor (stripper pull)",
                fix: "Cue: 'Chest and hips rise at the exact same speed—push the floor away'",
              },
              {
                fault: "Bouncing touch-and-go reps with a soft lower back",
                fix: "Cue: 'Kiss the plates to the floor—reset lat tension on every rep'",
              },
            ],
          },
          scaling: {
            rxPlus:
              "275/185 lb touch-and-go sets of 10 with zero belt or grip breaks",
            scaled:
              "155/105 lb Barbell Deadlift or Dual Kettlebell Sumo Deadlift",
            seniorsMasters:
              "Elevated Barbell Deadlift from 4\" Pulling Blocks or Trap-Bar Deadlift (joint-friendly hip angle)",
            injured: {
              wristOrShoulder:
                "Safety Squat Bar Good Mornings or Belt Squat / Glute Bridges",
              kneePain:
                "Romanian Deadlift (RDL) with vertical shins and high hip hinge",
              lowerBack:
                "Barbell Hip Thrusts or Light Kettlebell Sumo Deadlift from 12\" Box",
            },
          },
        },
        {
          name: "Box Jumps & Wall Ball Shots",
          category: "Plyometrics & Metabolic Conditioning",
          cues: {
            external: [
              "Land on the box as quiet as a ninja—knees tracking over toes",
              "Step down on every single rep to protect your Achilles tendon",
              "Keep the medicine ball right in front of your chin like a goblet squat",
            ],
            internal: [
              "Use hip drive to float onto the box instead of pulling your knees to your chest",
              "Exhale sharply as the ball leaves your hands",
            ],
            commonFaults: [
              {
                fault: "Landing on the box in a deep squat with caving knees (valgus)",
                fix: "Cue: 'Lower the box height by 4 inches and land in an athletic quarter-squat'",
              },
            ],
          },
          scaling: {
            rxPlus: "30/24 in Box Jumps (step down) + 30/20 lb Wall Ball",
            scaled:
              "20/16 in Box Jumps or Alternating Box Step-Ups + 14/10 lb Wall Ball",
            seniorsMasters:
              "16\" Sturdy Box Step-Ups + Squat-to-Target with Light MedBall Press",
            injured: {
              wristOrShoulder:
                "Box Step-Ups + MedBall Bear-Hug Squats (no overhead throw)",
              kneePain:
                "Low 12\" Box Step-Ups (slow eccentric) + Strict MedBall Push Press",
              lowerBack:
                "Alternating Reverse Lunges + Wall Ball Chest Passes to target",
            },
          },
        },
      ]
      : [
        {
          name: "Front Squat & Barbell Thruster",
          category: "Weightlifting & Compound Metcon",
          cues: {
            external: [
              "Point your elbow lasers straight at the front wall throughout the squat",
              "Drive the floor apart with your shoes and launch the bar off your shoulders",
              "Punch your head through the window at the top of every Thruster",
            ],
            internal: [
              "Brace your core 360 degrees before unlocking hips and knees",
              "Finish violent hip extension before your arms start pressing",
            ],
            commonFaults: [
              {
                fault: "Elbows dropping at the bottom of the Front Squat / Thruster",
                fix: "Cue: 'Lead with your elbows to the ceiling—chest proud!'",
              },
              {
                fault: "Pressing the Thruster early while still rising out of the squat",
                fix: "Cue: 'Pop the bar off your collarbones with your legs first, arms second'",
              },
            ],
          },
          scaling: {
            rxPlus:
              "115/80 lb Thrusters unbroken 21-15-9 + 85% 1RM Paused Front Squats",
            scaled:
              "65/45 lb Barbell Thrusters or 30/20 lb Dual Dumbbell Thrusters",
            seniorsMasters:
              "Front Squat to 15\" Box Target + Dual Dumbbell Push Press (joint-friendly wrist & knee angle)",
            injured: {
              wristOrShoulder:
                "Single-Arm DB Thruster (uninjured side) or Goblet Squat + Russian KB Swing",
              kneePain:
                "Push Press from Rack + Tempo Box Squat above parallel (vertical shins)",
              lowerBack:
                "Light Dual DB Front Squat to Box with strict upright torso",
            },
          },
        },
        {
          name: "Pull-Up (Kipping / Chest-to-Bar)",
          category: "Gymnastics Pulling",
          cues: {
            external: [
              "Push the bar away at the top so you swing directly into the next rep",
              "Glue your shoes together—snap from tight arch to tight hollow",
            ],
            internal: [
              "Initiate the pull by pressing the bar down with straight arms and active lats",
              "Keep your neck neutral—don't crane your chin over the bar",
            ],
            commonFaults: [
              {
                fault: "Dropping straight down from the top and losing kip rhythm",
                fix: "Cue: 'Push away from the rig at the top like a pendulum'",
              },
              {
                fault: "Bending knees wildly on the backswing (energy leak)",
                fix: "Cue: 'Squeeze glutes and point toes—swing from the shoulders, not the knees'",
              },
            ],
          },
          scaling: {
            rxPlus: "Chest-to-Bar Pull-Ups or Bar Muscle-Ups (15-12-9)",
            scaled:
              "Banded Kipping/Strict Pull-Ups or Jumping Pull-Ups with 2s negative",
            seniorsMasters:
              "Strict Ring Rows (feet below rings, pause 1 sec at chest)",
            injured: {
              wristOrShoulder:
                "Neutral-Grip Ring Rows or Single-Arm Dumbbell Bent-Over Rows",
              kneePain:
                "Strict/Banded Pull-Ups (step onto a plyo box to dismount—zero jumping impact)",
              lowerBack: "Chest-Supported Incline Bench Dumbbell Rows",
            },
          },
        },
      ];

  const feasibilityStatus =
    dur < 45 ? "OVERFLOW" : dur === 45 ? "TIGHT" : "OPTIMAL";
  const metconEnd = Math.max(46, dur - 8);

  return {
    _id: sessionId,
    sessionId,
    coachId: getStoredCoach().id,
    status: "completed",
    title,
    createdAt: new Date().toISOString(),
    input,
    parsedWod: {
      blocks: [
        {
          type: "strength",
          name: isSnatch
            ? "Olympic Snatch Complex"
            : isDeadlift
              ? "Heavy Deadlift Progression"
              : "Heavy Front Squat Triples",
          allocatedMinutes: 15,
        },
        {
          type: "metcon",
          name: isSnatch
            ? "20-Min Engine EMOM"
            : isDeadlift
              ? "18-Min Partner Grinder AMRAP"
              : "Fran Reloaded + 400m Buy-Out",
          allocatedMinutes: Math.max(12, metconEnd - 40),
        },
      ],
      movements: exercises.map((e) => e.name),
      primaryJoints: [
        "shoulders",
        "thoracic_spine",
        "hips",
        "knees",
        "wrists",
        "ankles",
      ],
      stimulus: isSnatch
        ? "Explosive Olympic barbell speed-strength followed by sustainable aerobic threshold pacing."
        : isDeadlift
          ? "High-tension posterior chain strength paired with high-turnover partner conditioning."
          : "Maximal front-rack core stability followed by a classic high-heart-rate glycolytic sprint.",
    },
    warmup: {
      gameTitle: "Phase 1: PVC Broomstick Circle Drop & Switch (Reaction Game)",
      style: warmupStyle,
      durationMinutes: 12,
      phases: [
        {
          name: "Phase 1: PVC Broomstick Circle Drop & Switch (Reaction Game)",
          durationMinutes: 4,
          instructions:
            "Athletes form a circle balancing PVC broomsticks vertically on the floor. Coach calls 'LEFT!', 'RIGHT!', or 'CLAP-SWITCH!'. Athletes must release their stick and catch the neighbor's stick before it hits the rubber floor. Any drop = whole class does 3 synchronized Tempo Air Squats with high-fives!",
          coachingGoal:
            "Break the ice, spark laughter and focus, elevate heart rate, and prime reaction speed without boring repetitive jogging.",
        },
        {
          name: "Phase 2: Targeted Broomstick Mobility & Barbell Primer",
          durationMinutes: 8,
          instructions:
            "Coach-led progressive PVC broomstick flow moving from general joint lubrication into WOD-specific positions.",
          coachingGoal:
            "Open thoracic spine, wrists, shoulders, and hips so every athlete hits full range of motion safely.",
          drills: [
            {
              name: "PVC Dislocates + Around-the-Worlds + Sotts Press",
              reps: "8 + 6 + 5 reps",
              focus:
                "Shoulder capsule external rotation & overhead thoracic extension",
            },
            {
              name: "Broomstick Kang Squats (Good Morning into Deep Squat)",
              reps: "8 slow tempo reps",
              focus:
                "Hamstring length, lumbar bracing, and upright bottom-squat posture",
            },
            {
              name: "Broomstick Front-Rack Elbow Rotations + Thruster Pop Drill",
              reps: "6 reps per side + 5 pops",
              focus:
                "Lat/tricep release for pain-free front rack & explosive hip-to-overhead timing",
            },
            {
              name: "Rig Scapular Pulls + Beat Swings",
              reps: "10 + 10 reps",
              focus:
                "Active shoulder depression and tight arch-to-hollow rhythm",
            },
          ],
        },
      ],
      alternativeGames: [
        {
          name: "Phase 1: Partner Mirror Footwork & Plank Shoulder-Tap Duel",
          durationMinutes: 4,
          instructions:
            "Partners face each other 1.5m apart in an athletic quarter-squat stance. Partner A leads lateral shuffles and hip taps while Partner B mirrors in real time for 30s. On Coach's whistle ('PLANK!'), both drop to a high plank for a 15-sec shoulder-tap duel while resisting hip rotation.",
          coachingGoal:
            "Warm up wrists, rotator cuffs, anti-rotational core stability, and active knee/hip flexion.",
        },
        {
          name: "Phase 1: Odd/Even Med-Ball & Cone Sprint Reaction Tag",
          durationMinutes: 4,
          instructions:
            "Partners line up facing each other across a medicine ball. Coach calls math cues or colors; even = grab the ball, odd = defensive squat shuffle to the outer cone.",
          coachingGoal:
            "Ignite central nervous system speed and hip-hinge readiness before touching a barbell.",
        },
      ],
    },
    exercises,
    timeline: {
      totalMinutes: dur,
      feasibilityStatus,
      coachWarnings: [
        "Stage PVC broomsticks and collars before class starts to keep the 04:00 transition seamless.",
        "During the 16:00-20:00 transition, pair athletes by similar barbell height (2 per rack) and confirm injured/Masters scaling options.",
      ],
      blocks: [
        {
          startMin: 0,
          endMin: 4,
          label: "Whiteboard Brief, Stimulus & Injury Check",
          type: "brief",
          notes:
            "Welcome athletes, explain the stimulus, and privately check for wrist, shoulder, knee, or lower-back issues.",
        },
        {
          startMin: 4,
          endMin: 16,
          label: "Broomstick Reaction Game & Mobility Prep",
          type: "warmup",
          notes:
            "4 min PVC Drop & Switch reaction game + 8 min targeted broomstick mobility & empty bar prep.",
        },
        {
          startMin: 16,
          endMin: 20,
          label: "Transition: Rack Assignment & Warm-up Sets",
          type: "transition",
          notes:
            "Assign 2 athletes per squat rack; complete 2 quick warm-up sets to opening weight.",
        },
        {
          startMin: 20,
          endMin: 35,
          label: "Strength / Skill Block",
          type: "strength",
          notes:
            "Run a running clock on the gym timer. Walk the floor coaching external cues and bracing.",
        },
        {
          startMin: 35,
          endMin: 40,
          label: "Setup: Metcon Loading & 1-Round Rehearsal",
          type: "transition",
          notes:
            "Athletes adjust barbell weights, test 3 Thrusters + 3 Pull-Up scaling reps, and chalk up.",
        },
        {
          startMin: 40,
          endMin: metconEnd,
          label: "Engine / Metcon Block",
          type: "metcon",
          notes:
            "3-2-1 GO! Watch lumbar posture under fatigue and cue breathing at transition points.",
        },
        {
          startMin: metconEnd,
          endMin: dur,
          label: "Cooldown, Fist Bumps & Scoreboard Notes",
          type: "cooldown",
          notes:
            "2 min supine box breathing, thoracic/lat stretch on rig, wipe down bars, and log scores.",
        },
      ],
    },
    agentTraces: [
      {
        agent: "WodParserAgent",
        provider: getPref("parser").provider,
        model: getPref("parser").modelIdentifier,
        status: "completed",
      },
      {
        agent: "WarmupPlannerAgent",
        provider: getPref("warmup").provider,
        model: getPref("warmup").modelIdentifier,
        mcpTool: "query_warmup_drills",
        status: "completed",
      },
      {
        agent: "ScalingAndCuesAgent",
        provider: getPref("scaling").provider,
        model: getPref("scaling").modelIdentifier,
        mcpTool: "get_movement_substitutions",
        status: "completed",
      },
      {
        agent: "TimelineLogisticsAgent",
        provider: getPref("timeline").provider,
        model: getPref("timeline").modelIdentifier,
        mcpTool: "validate_class_timeline",
        status: "completed",
      },
    ],
  };
}

function getLocalSessions(): WodSession[] {
  if (!isBrowser()) return [];
  try {
    const raw = localStorage.getItem(LS_SESSIONS);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed) && parsed.length > 0) return parsed;
    }
  } catch {
    // ignore
  }
  const initial = [
    buildLocalSessionPlan(
      {
        title: WOD_PRESETS[0].title,
        rawText: WOD_PRESETS[0].rawText,
        coachConstraints: WOD_PRESETS[0].coachConstraints,
        targetDurationMinutes: 60,
      },
      DEFAULT_AGENT_PREFS,
      "wod-seed-fran"
    ),
    buildLocalSessionPlan(
      {
        title: WOD_PRESETS[1].title,
        rawText: WOD_PRESETS[1].rawText,
        coachConstraints: WOD_PRESETS[1].coachConstraints,
        targetDurationMinutes: 60,
      },
      DEFAULT_AGENT_PREFS,
      "wod-seed-snatch"
    ),
  ];
  localStorage.setItem(LS_SESSIONS, JSON.stringify(initial));
  return initial;
}

function saveLocalSessions(sessions: WodSession[]) {
  if (!isBrowser()) return;
  localStorage.setItem(LS_SESSIONS, JSON.stringify(sessions));
}

async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
  timeoutMs = 8000
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const token = getStoredToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  try {
    const baseUrl =
      !isBrowser() && process.env.INTERNAL_GATEWAY_URL
        ? process.env.INTERNAL_GATEWAY_URL
        : API_BASE_URL;
    const res = await fetch(`${baseUrl}${path}`, {
      ...options,
      headers,
      signal: controller.signal,
    });
    if (!res.ok) {
      const errBody = await res.json().catch(() => ({}));
      throw new Error(errBody.error || `HTTP ${res.status}`);
    }
    return (await res.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}

export async function checkGatewayHealth(): Promise<{
  online: boolean;
  features?: string[];
}> {
  try {
    const data = await apiFetch<{ status: string; features?: string[] }>(
      "/health",
      { method: "GET" },
      2500
    );
    return { online: data.status === "ok", features: data.features };
  } catch {
    return { online: false };
  }
}

export async function registerCoach(payload: {
  email: string;
  password: string;
  displayName: string;
  boxName: string;
  defaultClassDurationMin: number;
}): Promise<{ token: string; coach: Coach; mode: "gateway" | "local" }> {
  try {
    const res = await apiFetch<{ token: string; coach: Coach }>(
      "/api/v1/auth/register",
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    setStoredToken(res.token);
    setStoredCoach(res.coach);
    return { ...res, mode: "gateway" };
  } catch {
    const fallbackCoach: Coach = {
      id: `coach-${Math.random().toString(36).slice(2, 9)}`,
      email: payload.email,
      displayName: payload.displayName || "Head Coach",
      boxName: payload.boxName || "IronVolt CrossFit",
      defaultClassDurationMin: payload.defaultClassDurationMin || 60,
      createdAt: new Date().toISOString(),
    };
    const token = `local-jwt-${Date.now()}`;
    setStoredToken(token);
    setStoredCoach(fallbackCoach);
    return { token, coach: fallbackCoach, mode: "local" };
  }
}

export async function loginCoach(payload: {
  email: string;
  password: string;
}): Promise<{ token: string; coach: Coach; mode: "gateway" | "local" }> {
  try {
    const res = await apiFetch<{ token: string; coach: Coach }>(
      "/api/v1/auth/login",
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    );
    setStoredToken(res.token);
    setStoredCoach(res.coach);
    return { ...res, mode: "gateway" };
  } catch {
    const existing = getStoredCoach();
    const coach: Coach = {
      ...existing,
      email: payload.email || existing.email,
    };
    const token = `local-jwt-${Date.now()}`;
    setStoredToken(token);
    setStoredCoach(coach);
    return { token, coach, mode: "local" };
  }
}

export async function listAPIKeys(): Promise<LLMAPIKey[]> {
  try {
    const res = await apiFetch<{ keys: LLMAPIKey[] }>("/api/v1/settings/keys");
    if (Array.isArray(res.keys)) {
      saveLocalKeys(res.keys);
      return res.keys;
    }
  } catch {
    // fallback to local keys
  }
  return getLocalKeys();
}

export async function saveAPIKey(payload: {
  provider: string;
  label?: string;
  apiKey: string;
  isPrimary?: boolean;
  isDefault?: boolean;
}): Promise<LLMAPIKey> {
  const isPrimary = payload.isPrimary ?? payload.isDefault ?? true;
  try {
    const res = await apiFetch<{ key: LLMAPIKey }>("/api/v1/settings/keys", {
      method: "POST",
      body: JSON.stringify({
        provider: payload.provider,
        apiKey: payload.apiKey,
        isPrimary,
      }),
    });
    const current = getLocalKeys().filter(
      (k) => k.id !== res.key.id && k.provider !== res.key.provider
    );
    saveLocalKeys([res.key, ...current]);
    return res.key;
  } catch {
    const trimmed = payload.apiKey.trim();
    const prefix = trimmed.length >= 4 ? trimmed.slice(0, 4) : "key";
    const suffix = trimmed.length >= 4 ? trimmed.slice(-4) : "1234";
    const newKey: LLMAPIKey = {
      id: `key-${Math.random().toString(36).slice(2, 9)}`,
      coachId: getStoredCoach().id,
      provider: payload.provider,
      label: payload.label || `${payload.provider.toUpperCase()} Key`,
      keyHint: `${prefix}••••${suffix}`,
      isPrimary,
      isDefault: isPrimary,
      createdAt: new Date().toISOString(),
    };
    const current = getLocalKeys().filter((k) => k.provider !== payload.provider);
    saveLocalKeys([newKey, ...current]);
    return newKey;
  }
}

export async function deleteAPIKey(id: string): Promise<void> {
  try {
    await apiFetch(`/api/v1/settings/keys/${id}`, { method: "DELETE" });
  } catch {
    // ignore
  }
  const updated = getLocalKeys().filter((k) => k.id !== id);
  saveLocalKeys(updated);
}

export async function getAgentPreferences(): Promise<AgentModelPref[]> {
  try {
    const res = await apiFetch<{ preferences: AgentModelPref[] }>(
      "/api/v1/settings/agents"
    );
    if (Array.isArray(res.preferences) && res.preferences.length > 0) {
      saveLocalPrefs(res.preferences);
      return res.preferences;
    }
  } catch {
    // fallback
  }
  return getLocalPrefs();
}

export async function saveAgentPreferences(
  preferences: AgentModelPref[]
): Promise<AgentModelPref[]> {
  saveLocalPrefs(preferences);
  try {
    const res = await apiFetch<{ preferences: AgentModelPref[] }>(
      "/api/v1/settings/agents",
      {
        method: "PUT",
        body: JSON.stringify({ preferences }),
      }
    );
    if (Array.isArray(res.preferences)) {
      saveLocalPrefs(res.preferences);
      return res.preferences;
    }
  } catch {
    // fallback saved in localStorage
  }
  return preferences;
}

export async function generateWodSession(
  input: SessionInput
): Promise<{ session: WodSession; mode: "gateway" | "local" }> {
  try {
    const res = await apiFetch<{
      sessionId: string;
      status: string;
      session: WodSession;
    }>(
      "/api/v1/sessions/generate",
      {
        method: "POST",
        body: JSON.stringify(input),
      },
      150000
    );
    const current = getLocalSessions().filter(
      (s) => s.sessionId !== res.session.sessionId
    );
    saveLocalSessions([res.session, ...current]);
    return { session: res.session, mode: "gateway" };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    // If the Gateway responded with an explicit application/LLM error (e.g. missing key, 400, 401, 502),
    // surface it directly so the coach sees the real error rather than silently generating mock data.
    if (
      !msg.toLowerCase().includes("failed to fetch") &&
      !msg.toLowerCase().includes("networkerror")
    ) {
      throw err;
    }
    const prefs = getLocalPrefs();
    const localSession = buildLocalSessionPlan(input, prefs);
    const current = getLocalSessions();
    saveLocalSessions([localSession, ...current]);
    return { session: localSession, mode: "local" };
  }
}

export async function listWodSessions(): Promise<WodSession[]> {
  try {
    const res = await apiFetch<{ sessions: WodSession[] }>("/api/v1/sessions");
    if (Array.isArray(res.sessions) && res.sessions.length > 0) {
      saveLocalSessions(res.sessions);
      return res.sessions;
    }
  } catch {
    // fallback
  }
  return getLocalSessions();
}

export async function deleteWodSession(sessionId: string): Promise<void> {
  try {
    await apiFetch(`/api/v1/sessions/${sessionId}`, { method: "DELETE" });
  } catch {
    // fallback
  }
  const remaining = getLocalSessions().filter((s) => s.sessionId !== sessionId);
  saveLocalSessions(remaining);
}

export async function createSessionShareLink(
  sessionId: string,
  includeCoachNotes: boolean,
  sessionFallback?: WodSession
): Promise<SharedLink> {
  try {
    const link = await apiFetch<SharedLink>(
      `/api/v1/sessions/${sessionId}/share`,
      {
        method: "POST",
        body: JSON.stringify({ includeCoachNotes }),
      }
    );
    // Also cache locally so /s/[code] works even if backend restarts
    if (isBrowser() && sessionFallback) {
      const mapRaw = localStorage.getItem(LS_SHARES);
      const map = mapRaw ? JSON.parse(mapRaw) : {};
      map[link.shortCode] = { share: link, session: sessionFallback };
      localStorage.setItem(LS_SHARES, JSON.stringify(map));
    }
    return link;
  } catch {
    const shortCode = `cb-${Math.random().toString(36).substring(2, 8)}`;
    const origin = isBrowser()
      ? window.location.origin
      : "http://localhost:3000";
    const shareUrl = `${origin}/s/${shortCode}`;
    const link: SharedLink = {
      shortCode,
      shareUrl,
      sessionId,
      coachId: getStoredCoach().id,
      includeCoachNotes,
      qrSvg: generateDeterministicQRSVG(shareUrl),
      viewCount: 1,
      createdAt: new Date().toISOString(),
    };
    if (isBrowser()) {
      const session =
        sessionFallback ||
        getLocalSessions().find((s) => s.sessionId === sessionId) ||
        getLocalSessions()[0];
      const mapRaw = localStorage.getItem(LS_SHARES);
      const map = mapRaw ? JSON.parse(mapRaw) : {};
      map[shortCode] = { share: link, session };
      localStorage.setItem(LS_SHARES, JSON.stringify(map));
    }
    return link;
  }
}

export async function resolveSharedSession(
  code: string
): Promise<SharedSessionPayload> {
  try {
    return await apiFetch<SharedSessionPayload>(`/api/v1/share/${code}`);
  } catch {
    if (isBrowser()) {
      try {
        const mapRaw = localStorage.getItem(LS_SHARES);
        if (mapRaw) {
          const map = JSON.parse(mapRaw);
          if (map[code]) {
            const entry = map[code] as SharedSessionPayload;
            const clonedSession = JSON.parse(
              JSON.stringify(entry.session)
            ) as WodSession;
            if (!entry.share.includeCoachNotes && clonedSession.timeline) {
              delete clonedSession.timeline.coachWarnings;
            }
            return { share: entry.share, session: clonedSession };
          }
        }
      } catch {
        // ignore
      }
    }
    // Deterministic fallback demo share so any QR scan `/s/cb-demo` works out of the box
    const fallbackSession = buildLocalSessionPlan(
      {
        title: WOD_PRESETS[0].title,
        rawText: WOD_PRESETS[0].rawText,
        coachConstraints: WOD_PRESETS[0].coachConstraints,
        targetDurationMinutes: 60,
      },
      DEFAULT_AGENT_PREFS,
      "wod-shared-demo"
    );
    const shareUrl = `http://localhost:3000/s/${code}`;
    return {
      share: {
        shortCode: code,
        shareUrl,
        sessionId: fallbackSession.sessionId,
        coachId: DEFAULT_COACH.id,
        includeCoachNotes: true,
        qrSvg: generateDeterministicQRSVG(shareUrl),
        viewCount: 1,
        createdAt: new Date().toISOString(),
      },
      session: fallbackSession,
    };
  }
}

export interface SessionStepEvent {
  sessionId: string;
  step: "parser" | "warmup" | "scaling" | "timeline" | "completed" | "error" | string;
  status: "running" | "completed" | "error" | string;
  message: string;
  timestamp?: string;
}

/**
 * Subscribes to real-time Redis SSE progress events for a session from the Go Gateway:
 * `GET /api/v1/sessions/{sessionId}/events?token=<jwt>`
 *
 * Returns an unsubscribe cleanup function `() => void` that closes the EventSource.
 */
export function subscribeToSessionEvents(
  sessionId: string,
  onEvent: (event: SessionStepEvent) => void
): () => void {
  if (!isBrowser() || typeof EventSource === "undefined") {
    return () => { };
  }

  const token = getStoredToken();
  const query = token ? `?token=${encodeURIComponent(token)}` : "";
  const url = `${API_BASE_URL}/api/v1/sessions/${encodeURIComponent(
    sessionId
  )}/events${query}`;

  const es = new EventSource(url);
  es.onmessage = (msg) => {
    try {
      const parsed = JSON.parse(msg.data) as SessionStepEvent;
      onEvent(parsed);
      if (parsed.step === "completed" || parsed.step === "error") {
        es.close();
      }
    } catch (error) {
      // ignore
      console.error("Error parsing session event", error);
      es.close();
    }
  };

  es.onerror = () => {
    es.close();
  };

  return () => {
    es.close();
  };
}

