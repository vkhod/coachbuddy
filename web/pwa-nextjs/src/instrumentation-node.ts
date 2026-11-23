// A BRANDING_FILE that is missing or invalid stops the server with the reason,
// the same way the services refuse to start without a required secret.

import { getBranding } from "./lib/branding";

try {
  const b = getBranding();
  console.log(`[pwa] branding: ${b.name}${process.env.BRANDING_FILE ? ` (${process.env.BRANDING_FILE})` : " (built-in)"}`);
} catch (e) {
  console.error(`[pwa] ${(e as Error).message}`);
  process.exit(1);
}
