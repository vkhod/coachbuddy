// /sw.js: the service worker for this build and branding (src/lib/sw-script.ts).
// Only in a production build; `next dev` answers 404, so a worker never caches
// development chunks.

import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { getBranding, publicBranding } from "@/lib/branding";
import { workerScript } from "@/lib/sw-script";

export const dynamic = "force-dynamic";

/** Pages kept from the start: they hold no coach data (every page is a client page). */
const PAGES = ["/", "/history", "/login", "/welcome"];

let script: string | null = null;

function listStatic(dir: string, prefix: string): string[] {
  const out: string[] = [];
  for (const e of readdirSync(/*turbopackIgnore: true*/ dir, { withFileTypes: true })) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...listStatic(full, `${prefix}${e.name}/`));
    // .woff is the old-browser copy of each .woff2 font; every browser with a service worker reads woff2.
    else if (!e.name.endsWith(".woff") && !e.name.endsWith(".map")) out.push(`${prefix}${e.name}`);
  }
  return out;
}

function build(): string {
  const next = path.join(process.cwd(), ".next");
  const buildId = readFileSync(/*turbopackIgnore: true*/ path.join(next, "BUILD_ID"), "utf8").trim();
  const b = getBranding();
  const brand = createHash("sha256")
    .update(JSON.stringify({ ...publicBranding(b), colours: b.colours }))
    .digest("hex")
    .slice(0, 10);
  return workerScript({
    version: `${buildId}-${brand}`,
    assets: listStatic(path.join(next, "static"), "/_next/static/").sort(),
    pages: PAGES,
    paper: b.colours.paper,
    ink: b.colours.ink,
  });
}

export function GET() {
  if (process.env.NODE_ENV !== "production") return new Response("no service worker in development", { status: 404 });
  script ??= build();
  return new Response(script, {
    headers: {
      "Content-Type": "text/javascript; charset=utf-8",
      // The browser checks for a new worker on every page load.
      "Cache-Control": "no-cache",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
