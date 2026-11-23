# CoachBuddy Next.js PWA — Agent Rules (`web/pwa-nextjs`)

This directory contains `pwa-nextjs` (`:3000`), the TypeScript + Next.js 16 (App Router), React 19 + Tailwind CSS Progressive Web App for CrossFit and functional fitness coaches.

---

## 1. Directory Layout

- Routes (`src/app/`), each a client page:
  - `/` Capture: screenshot, comment, class length, Build plan.
  - `/plans/[id]` the plan in class order; progress while sections are built (SSE, `src/lib/usePlan.ts`). Change, Retry and Add an injury open `ChangeSheet`.
  - `/plans/[id]/versions` what each revision changed, with restore; `/plans/[id]/versions/[n]` an earlier version, read only.
  - `/history` plans by date, with search.
  - `/settings` profile, AI key, sign out.
  - `/s/[code]` public share page; shows only what the gateway's share view returns.
  - `/login`, `/welcome` sign in or create an account; add the first AI key.
- `src/components/`: `PlanView.tsx` (timeline, warm-up, parts, athletes today, failed sections), `ChangeSheet.tsx` (the fixed chips add text to the instruction; only the instruction is sent), `ShareSheet.tsx`, `KeyForm.tsx`, `ui.tsx` (headers, icons, class length, error note).
- `src/lib/api.ts`: the gateway client. It throws `ApiError` with the gateway's reason; it never builds anything locally.
- `src/lib/plan.ts`: read-only helpers over the contract `Plan` (labels, tier order, today and injury rows).
- `src/lib/contract/`: TypeScript types generated from `schema/` by `scripts/gen-schema` (do not edit). Plan payloads use only these.
- `src/lib/gateway-types.ts`: shapes of gateway-only responses (auth, keys, models, share links), which are not in the plan contract.
- `src/app/globals.css` and `tailwind.config.ts`: the programme sheet look. Colours are CSS variables (`--paper`, `--ink`, `--sub`, `--rule`, `--accent`) for branding from configuration.
- `public/manifest.json` and `public/icon.svg`: PWA manifest and icon.

---

## 2. UI & Functional Guardrails

1. **A Plan to Read, Not a Live Class Tool**:
   - The app builds a WOD plan the coach reads on the phone. There is no class timer, Floor Mode or screen wake lock (removed in Phase 3); do not add them back.
2. **Look**:
   - The approved "programme sheet" look: Barlow Condensed (headings, times, tier names) and Barlow (text), self-hosted with `@fontsource`; tabular figures; hairline rules instead of cards; square corners.
   - `--accent` is used only for rows about athletes who need attention today (the `today` groups and injuries). Nothing else gets the accent.
   - Tap targets are at least 44 px; the viewport allows zoom.
3. **BYOK Vault Safety**:
   - Never store raw LLM API keys in `localStorage` after saving to the vault; send them to `gateway-go` (`:8080`) for AES-256-GCM encryption and display only masked key badges returned by the API.
4. **No Client-Side Fallbacks**:
   - If `gateway-go` (`NEXT_PUBLIC_API_URL`, default `http://localhost:8080`) is unreachable or returns an error, show the error with a retry action. Never build, preview or store a locally generated plan, API key or share link.
   - The old `api.ts` with its local plan builder was removed in Phase 3 step 3; do not add fallbacks back.

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
