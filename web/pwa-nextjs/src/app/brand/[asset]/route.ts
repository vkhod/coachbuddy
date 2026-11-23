// /brand/logo and /brand/icon: the images named in the branding file. Without a
// logo, /brand/logo is 404 (the header shows the name instead); without an icon,
// /brand/icon is the built-in one.

import { readFile } from "node:fs/promises";
import path from "node:path";
import { getBranding, IMAGE_TYPES } from "@/lib/branding";

export const dynamic = "force-dynamic";

const BUILT_IN_ICON = path.join(process.cwd(), "public", "icon.svg");

export async function GET(_req: Request, { params }: { params: Promise<{ asset: string }> }) {
  const { asset } = await params;
  const b = getBranding();
  let file: string | null;
  if (asset === "logo") file = b.logoPath;
  else if (asset === "icon") file = b.iconPath ?? BUILT_IN_ICON;
  else return new Response("not found", { status: 404 });
  if (!file) return new Response("no logo configured", { status: 404 });

  // The file is the mounted branding image, not part of the build output.
  const body = await readFile(/*turbopackIgnore: true*/ file);
  return new Response(new Uint8Array(body), {
    headers: {
      "Content-Type": IMAGE_TYPES[path.extname(file).toLowerCase()] ?? "application/octet-stream",
      // The URL carries ?v= of the file's size and time, so a changed file gets a new URL.
      "Cache-Control": "public, max-age=86400",
      "X-Content-Type-Options": "nosniff",
      // An SVG opened directly must not run scripts.
      "Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; sandbox",
    },
  });
}
