// PNG app icons rendered from the branding icon (Phase 4 step 1).
//
// Android wants PNG icons of 192 and 512 px in the manifest, and a separate
// maskable icon whose picture sits inside the middle 80% (the launcher crops
// it to a circle, a squircle or a rounded square). iOS uses a 180 px
// apple-touch-icon and fills any transparency with black, so that one is put
// on the paper colour.
//
// The source is the branding file's icon (SVG, PNG, JPEG or WebP), or the
// built-in public/icon.svg. Each size is rendered once per process: the
// branding is read once too, and a restart applies a changed file.
//
// Server only.

import { readFile } from "node:fs/promises";
import path from "node:path";
import sharp from "sharp";
import { getBranding, type Branding } from "./branding";

export const ICON_SIZES = {
  "icon-192.png": { size: 192, kind: "any" },
  "icon-512.png": { size: 512, kind: "any" },
  "icon-maskable.png": { size: 512, kind: "maskable" },
  "apple-icon.png": { size: 180, kind: "apple" },
} as const;

export type IconName = keyof typeof ICON_SIZES;

/** Share of the maskable icon the picture fills; the rest is paper. W3C's safe zone is a circle of 80%. */
export const MASKABLE_SCALE = 0.8;

export function isIconName(name: string): name is IconName {
  return Object.prototype.hasOwnProperty.call(ICON_SIZES, name);
}

const BUILT_IN_ICON = path.join(process.cwd(), "public", "icon.svg");

function sourcePath(b: Branding): string {
  return b.iconPath ?? BUILT_IN_ICON;
}

function rgb(hex: string): { r: number; g: number; b: number } {
  let h = hex.slice(1);
  if (h.length === 3) h = h.replace(/./g, (c) => c + c);
  return { r: parseInt(h.slice(0, 2), 16), g: parseInt(h.slice(2, 4), 16), b: parseInt(h.slice(4, 6), 16) };
}

/** Renders one icon from the source image. Exported for the check script. */
export async function renderIcon(source: Buffer, name: IconName, paper: string): Promise<Buffer> {
  const { size, kind } = ICON_SIZES[name];
  const bg = rgb(paper);
  // density: an SVG with a small viewBox is rasterised sharp enough for 512 px.
  const input = () => sharp(source, { density: 384 });

  if (kind === "any") {
    // Kept as the box drew it, transparency included.
    return input()
      .resize(size, size, { fit: "contain", background: { ...bg, alpha: 0 } })
      .png()
      .toBuffer();
  }

  const inner = kind === "maskable" ? Math.round(size * MASKABLE_SCALE) : size;
  const picture = await input()
    .resize(inner, inner, { fit: "contain", background: { ...bg, alpha: 0 } })
    .png()
    .toBuffer();
  const composed = await sharp({ create: { width: size, height: size, channels: 3, background: bg } })
    .composite([{ input: picture, gravity: "centre" }])
    .png()
    .toBuffer();
  // Compositing adds an alpha channel; these two are opaque on purpose.
  return sharp(composed).removeAlpha().png().toBuffer();
}

const rendered = new Map<IconName, Promise<Buffer>>();

/** The icon for this server's branding, rendered on first use. */
export function brandIcon(name: IconName): Promise<Buffer> {
  let p = rendered.get(name);
  if (!p) {
    const b = getBranding();
    // The file is the mounted branding image, not part of the build output.
    p = readFile(/*turbopackIgnore: true*/ sourcePath(b)).then((src) => renderIcon(src, name, b.colours.paper));
    // A failed render is not kept, so the next request tries again.
    p.catch(() => rendered.delete(name));
    rendered.set(name, p);
  }
  return p;
}
