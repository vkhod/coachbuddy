# CoachBuddy coach app (`web/pwa-nextjs`)

The phone app a coach uses to build a WOD plan: Next.js 16 (App Router), React 19, TypeScript and Tailwind CSS. It builds a plan to read before and during class; there is no class timer.

Every plan comes from the agents with the coach's own key. When the gateway or a section fails, the app shows the reason and a retry. It never builds or stores anything locally.

## Screens

- **Capture (`/`)**: the WOD screenshot, an optional comment (who is in class today, injuries, warm-up wishes) and the class length (45, 60, 75 or 90 min). Build plan starts the generation.
- **Plan (`/plans/[id]`)**: the plan in class order, filled in section by section while it is built:
  - timeline with start and end minutes; each row jumps to its section;
  - warm-up: targets, blocks and minutes;
  - each WOD part with its tiers: RX, Scaled and Senior on every part, RX+ on engine parts. Engine parts with up to five movements are shown as a grid;
  - athletes today and injuries, marked in the accent colour, with an injury row in the warm-up and in every part.

  Change, Retry and Add an injury open a sheet with the fixed chips (Shorter, Longer, Simpler, and so on); only the instruction text is sent. A failed section shows the reason and a Retry button.
- **Versions (`/plans/[id]/versions`)**: what each revision changed, with restore. Earlier versions open read only.
- **History (`/history`)**: plans by date, with search.
- **Settings (`/settings`)**: profile and default class length, the AI key (stored encrypted by the gateway, shown masked), and **Model**: one model for all agents, or one per agent under Advanced (Board, Warm-up, Scaling, Injuries, Timeline). Only providers with a saved key are offered, and Board only offers models that read images. **Agent runs** (`/settings/runs`) lists what ran for each revision.
- **Share (`/s/[code]`)**: the public page behind a share link or QR code. The athlete view has the WOD, the tiers and the timeline; the coach view adds the warm-up, today's athletes, injuries and cues. The gateway decides what is in each view.
- **Sign in (`/login`)** and **first key (`/welcome`)**.

## Branding

A box's name, colours, logo and icon can replace Coach Buddy's without a rebuild: set `BRANDING_FILE` to a JSON file (with `BRANDING_DIR` as the mounted folder in Docker Compose). The server checks the file and the contrast of its colours at start and stops with the reason when it is invalid. Fields and setup: [`config/branding/README.md`](../../config/branding/README.md); a complete example: [`config/branding/attakashaka/`](../../config/branding/attakashaka/).

## Installing and offline

The app installs from the browser (Android Chrome: menu, Install app). A service worker keeps the app's own files and the Capture, History, Sign in and first-key pages on the phone, so the app opens without a connection. Every plan the coach opens is also saved on the phone (the last 100 opened), so it can be read in airplane mode: History lists the saved plans and the plan page says it is the copy saved on this phone. Signing out deletes them. Building or changing a plan always needs a connection; offline, those buttons are disabled with a note. When a new version is deployed, a bar at the bottom offers Reload; the app never switches versions under the coach by itself. The worker runs only in a production build (`npm run build`, then the standalone server), not under `npm run dev`. Browsers allow a service worker only on HTTPS or `localhost`.

## Accessibility

- Tap targets are at least 44 x 44 px, and the viewport allows zoom.
- Text and the accent colour have at least 4.5:1 contrast on the page background (WCAG AA). Branded colours are checked against the same limit at start.
- At 200% page zoom or font size, rows with a label column (tiers, injuries, warm-up targets, settings) put the text under the label instead of squeezing it, and nothing scrolls sideways. The engine grid is a table that scrolls sideways inside its own box.
- The `narrow` Tailwind screen (below 360 CSS px, which on a 390 px phone means about 110% zoom and more) moves the plan title to its own line and the timeline labels under their times.

## Commands

```bash
npm install
npm run dev     # :3000, against NEXT_PUBLIC_API_URL (default http://localhost:8080)
npm run build   # type check and production build
npm run check:pwa  # manifest, icons and service worker on that build
```

The gateway, its databases and mcp-rust must be running; see the root [README](../../README.md). Agent rules for this directory are in [AGENTS.md](AGENTS.md).
