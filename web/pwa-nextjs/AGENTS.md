# CoachBuddy Next.js PWA — Agent Rules (`web/pwa-nextjs`)

This directory contains `pwa-nextjs` (`:3000`), the TypeScript + Next.js 14 (App Router) + Tailwind CSS Progressive Web App for CrossFit and functional fitness coaches.

---

## 1. Directory Layout

- `src/app/page.tsx`: Main multi-tab Coaching Console (WOD Planner, Active Session & Coach Floor Mode, Session History Library, BYOK Encrypted Key Vault).
- `src/app/s/[code]/page.tsx`: Public QR Share view supporting both `Co-Coach View` (full timeline + cues + scaling) and `Athlete View` (workout + scaling tiers).
- `src/app/layout.tsx` & `src/app/globals.css`: App shell, metadata, PWA manifest links, and Tactical Athletic Box dark theme styling.
- `src/lib/contract/`: TypeScript types generated from `schema/` by `scripts/gen-schema` (do not edit).
- `src/lib/`: API client utilities, TypeScript interfaces matching the Go Gateway & Python Orchestrator contracts, and demo fallback helpers.
- `public/manifest.json` & `public/icon.svg`: Installable PWA manifest and vector assets.

---

## 2. UI & Functional Guardrails

1. **Tactical Athletic Box Aesthetic**:
   - High-contrast dark slate (`#090D16` / `slate-950`) surfaces with high-visibility athletic accents (Volt/Lime `#CCFF00`, Amber, Cyan, Rose) for effortless legibility from across a gym floor.
2. **Coach Floor Mode & Screen WakeLock**:
   - Floor Mode must maintain a large countdown timer with `Play/Pause`, `Next Block`, and `Reset` controls and safely request/release `navigator.wakeLock` (with graceful fallback on unsupported browsers).
3. **BYOK Vault Safety**:
   - Never store raw LLM API keys in `localStorage` after saving to the vault; send them to `gateway-go` (`:8080`) for AES-256-GCM encryption and display only masked key badges returned by the API.
4. **No Client-Side Fallbacks**:
   - If `gateway-go` (`NEXT_PUBLIC_API_URL`, default `http://localhost:8080`) is unreachable or returns an error, show the error with a retry action. Never build, preview or store a locally generated plan, API key or share link.
   - The existing fallbacks in `src/lib/api.ts` are removed during the redesign; do not add new ones.

---

## 3. Commands

```bash
# Install dependencies
npm install

# Start development server on :3000
npm run dev

# Type-check and build production bundle
npm run build
```
