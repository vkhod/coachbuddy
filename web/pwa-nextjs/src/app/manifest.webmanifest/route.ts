// The PWA manifest, built from the branding file when the server runs.

import { getBranding, publicBranding } from "@/lib/branding";

export const dynamic = "force-dynamic";

export function GET() {
  const b = getBranding();
  const v = publicBranding(b).version;
  const manifest = {
    id: "/",
    name: b.name,
    short_name: b.shortName,
    description: "WOD plans for CrossFit coaches: warm-up, scaling tiers, injuries and a class timeline from a screenshot.",
    start_url: "/",
    scope: "/",
    display: "standalone",
    background_color: b.colours.paper,
    theme_color: b.colours.paper,
    // Not locked to portrait (WCAG 1.3.4; decided 2026-11-24): a foldable or a
    // phone propped sideways still shows the plan.
    orientation: "any",
    // PNGs rendered from the branding icon (src/lib/icons.ts).
    icons: [
      { src: `/brand/icon-192.png?v=${v}`, sizes: "192x192", type: "image/png", purpose: "any" },
      { src: `/brand/icon-512.png?v=${v}`, sizes: "512x512", type: "image/png", purpose: "any" },
      { src: `/brand/icon-maskable.png?v=${v}`, sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
  return new Response(JSON.stringify(manifest, null, 2), {
    headers: { "Content-Type": "application/manifest+json", "Cache-Control": "no-cache" },
  });
}
