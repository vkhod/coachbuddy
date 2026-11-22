# CoachBuddy Next.js PWA — Agent Rules (`web/pwa-nextjs`)

This directory contains `pwa-nextjs` (`:3000`), the TypeScript + Next.js 14 (App Router) + Tailwind CSS Progressive Web App for CrossFit and functional fitness coaches.

---

## 1. Directory Layout

- `src/app/page.tsx`: Main multi-tab Coaching Console (WOD Planner, Active Session Plan, Session History Library, BYOK Encrypted Key Vault). Replaced in Phase 3 by the routes `/`, `/plans/[id]`, `/history`, `/settings`.
- `src/app/s/[code]/page.tsx`: Public QR Share view supporting both `Co-Coach View` (full timeline + cues + scaling) and `Athlete View` (workout + scaling tiers).
- `src/app/layout.tsx` & `src/app/globals.css`: App shell, metadata, PWA manifest links, and Tactical Athletic Box dark theme styling.
- `src/lib/contract/`: TypeScript types generated from `schema/` by `scripts/gen-schema` (do not edit).
- `src/lib/`: API client utilities, TypeScript interfaces matching the Go Gateway & Python Orchestrator contracts, and demo fallback helpers.
- `public/manifest.json` & `public/icon.svg`: Installable PWA manifest and vector assets.

---

## 2. UI & Functional Guardrails

1. **A Plan to Read, Not a Live Class Tool**:
   - The app builds a WOD plan the coach reads on the phone. There is no class timer, Floor Mode or screen wake lock (removed in Phase 3); do not add them back.
2. **Look**:
   - The current dark slate and Volt/Lime (`#CCFF00`) styling is replaced in Phase 3 by the approved "programme sheet" look: Barlow Condensed and Barlow, hairline rules, one accent colour for athletes who need attention today. Do not extend the old styling.
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
