# CoachBuddy Next.js TypeScript PWA (`web/pwa-nextjs`)

High-contrast **Tactical Athletic Box** Next.js 14 App Router PWA for CrossFit & functional fitness coaches.

## Key Features
- **Tab 1: WOD Session Planner & Multi-Agent Stepper**:
  - One-click CrossFit WOD presets (*"Heavy Front Squat + Fran-style Thruster/Pull-up Metcon"*, *"Snatch Skill + 20m Engine EMOM"*, *"Deadlift & Box Jump Partner Grinder"*).
  - Drag-and-drop / paste WOD Whiteboard Screenshot (multimodal vision input) + text input.
  - Quick-select Warm-up & Coaching Cue style chips (*"Mostly Broomstick / PVC"*, *"Gamified / Reaction Drill"*, *"Partner Warm-up"*, *"Thoracic & Overhead Mobility"*, *"No Running / Row & Bike Only"*).
  - Class Duration Selector (`45`, `60`, `75`, `90 min` — default `60 min`).
  - Live Multi-Agent Execution Stepper tracking `WodParserAgent` $\rightarrow$ `WarmupPlannerAgent` + `ScalingAndCuesAgent` $\rightarrow$ `TimelineLogisticsAgent`.
- **Tab 2: Active Session Plan & "Coach Floor Mode"**:
  - Proportional minute-by-minute class timeline bar with feasibility badges (`OPTIMAL`, `TIGHT`, `OVERFLOW`) and logistics warnings.
  - Toggleable **Coach Floor Mode** with a giant gym-floor countdown timer (`Play`, `Pause`, `Next Block`, `Reset`) and Browser Screen `navigator.wakeLock` integration.
  - Non-repetitive 2-phase Warm-up card (Reaction Game + PVC Broomstick Mobility Drills).
  - Interactive **4-Tier Movement Scaling Matrix** (`Rx+`, `Scaled`, `Masters / Seniors`, and `Injured Alternatives` for Wrist/Shoulder, Knee Pain, and Lower Back) + External/Internal Cues & Common Fault $\rightarrow$ Fix pairs.
  - One-click **Share Session & Rust SVG QR Code Modal** (`Co-Coach View` vs `Athlete View`).
- **Tab 3: Session History Library**:
  - Searchable history of generated WOD sessions with one-click load into Floor Mode.
- **Tab 4: BYOK Encrypted Key Vault & Per-Agent LLM Router**:
  - Manage AES-256-GCM encrypted API keys for `Google Gemini`, `OpenAI`, `Anthropic Claude`, and `Groq`.
  - Route all agents through a single Global model or configure per-agent provider/model overrides.
- **Public Share Route (`/s/[code]`)**:
  - Mobile-friendly SSR/CSR shared WOD & 4-tier scaling page for co-coaches and athletes scanning the QR code at the whiteboard.
