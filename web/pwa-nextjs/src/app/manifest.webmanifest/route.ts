// The PWA manifest, built from the branding file when the server runs.

import { getBranding, IMAGE_TYPES, publicBranding } from "@/lib/branding";
import path from "node:path";

export const dynamic = "force-dynamic";

export function GET() {
  const b = getBranding();
  const v = publicBranding(b).version;
  const iconType = b.iconPath ? IMAGE_TYPES[path.extname(b.iconPath).toLowerCase()] : "image/svg+xml";
  const manifest = {
    name: b.name,
    short_name: b.shortName,
    description: "WOD plans for CrossFit coaches: warm-up, scaling tiers, injuries and a class timeline from a screenshot.",
    start_url: "/",
    display: "standalone",
    background_color: b.colours.paper,
    theme_color: b.colours.paper,
    orientation: "any",
    icons: [{ src: `/brand/icon?v=${v}`, sizes: "any", type: iconType, purpose: "any" }],
  };
  return new Response(JSON.stringify(manifest, null, 2), {
    headers: { "Content-Type": "application/manifest+json", "Cache-Control": "no-cache" },
  });
}
