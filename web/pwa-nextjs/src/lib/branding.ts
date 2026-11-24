// Branding from a mounted configuration file (Phase 3 step 5).
//
// BRANDING_FILE names a JSON file (see config/branding/README.md). When it is not
// set, the app is Coach Buddy with the programme sheet colours. When it is set, the
// file must exist and be valid: the server refuses to start otherwise
// (src/instrumentation.ts), so a box never runs with half its branding.
//
// Server only: it reads the file system. Client components get the result through
// BrandProvider (src/components/Brand.tsx).

import { readFileSync, statSync } from "node:fs";
import path from "node:path";

export type ColourName = "paper" | "ink" | "sub" | "rule" | "accent" | "card";

export interface Branding {
  name: string;
  shortName: string;
  colours: Record<ColourName, string>;
  /** Absolute path of the wide logo shown in the header, or null for the name in text. */
  logoPath: string | null;
  /** Absolute path of the square app icon, or null for the built-in icon. */
  iconPath: string | null;
}

/** What the client needs: no file paths. */
export interface PublicBranding {
  name: string;
  shortName: string;
  hasLogo: boolean;
  /** Changes when the logo or icon file changes, so browsers fetch the new one. */
  version: string;
}

export const DEFAULT_BRANDING: Branding = {
  name: "Coach Buddy",
  shortName: "Coach Buddy",
  colours: {
    paper: "#f6f4ee",
    ink: "#121212",
    sub: "#4a4a48",
    rule: "#cbc6ba",
    accent: "#b33a0b",
    card: "#ffffff",
  },
  logoPath: null,
  iconPath: null,
};

export const IMAGE_TYPES: Record<string, string> = {
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
};

const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;
const MAX_IMAGE_BYTES = 1024 * 1024;

export class BrandingError extends Error {
  constructor(file: string, reason: string) {
    super(`branding file ${file}: ${reason}`);
    this.name = "BrandingError";
  }
}

// --- Contrast (WCAG 2.x relative luminance) ---

function luminance(hex: string): number {
  let h = hex.slice(1);
  if (h.length === 3) h = h.replace(/./g, (c) => c + c);
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** Text colours must be readable on both backgrounds: 4.5:1 is WCAG AA for body text. */
const TEXT_ON_BACKGROUND: [ColourName, ColourName][] = [
  ["ink", "paper"],
  ["ink", "card"],
  ["sub", "paper"],
  ["accent", "paper"],
]; // ink on paper also covers the primary buttons (paper text on ink)
const MIN_CONTRAST = 4.5;

// --- Parsing ---

function imageFile(file: string, dir: string, key: string, value: unknown): string | null {
  if (value === undefined || value === null) return null;
  if (typeof value !== "string" || value === "" || value !== path.basename(value)) {
    throw new BrandingError(file, `"${key}" must be a file name next to the branding file, without folders`);
  }
  const ext = path.extname(value).toLowerCase();
  if (!IMAGE_TYPES[ext]) {
    throw new BrandingError(file, `"${key}" must be one of ${Object.keys(IMAGE_TYPES).join(", ")}`);
  }
  const full = path.join(dir, value);
  let size: number;
  try {
    const st = statSync(full);
    if (!st.isFile()) throw new Error("not a file");
    size = st.size;
  } catch {
    throw new BrandingError(file, `"${key}" file ${value} not found next to the branding file`);
  }
  if (size > MAX_IMAGE_BYTES) {
    throw new BrandingError(file, `"${key}" file ${value} is larger than 1 MB`);
  }
  return full;
}

function text(file: string, key: string, value: unknown, max: number): string {
  if (typeof value !== "string" || value.trim() === "" || value.trim().length > max) {
    throw new BrandingError(file, `"${key}" must be text of 1 to ${max} characters`);
  }
  return value.trim();
}

/** Reads and checks a branding file. Throws BrandingError with the reason. */
export function parseBranding(file: string): Branding {
  let raw: unknown;
  try {
    raw = JSON.parse(readFileSync(file, "utf8"));
  } catch (e) {
    const reason = (e as NodeJS.ErrnoException).code === "ENOENT" ? "not found" : `not valid JSON (${(e as Error).message})`;
    throw new BrandingError(file, reason);
  }
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new BrandingError(file, "must be a JSON object");
  }
  const cfg = raw as Record<string, unknown>;
  const known = new Set(["name", "shortName", "logo", "icon", "colours"]);
  const unknown = Object.keys(cfg).filter((k) => !known.has(k));
  if (unknown.length) throw new BrandingError(file, `unknown field(s): ${unknown.join(", ")}`);

  const name = text(file, "name", cfg.name, 40);
  const shortName = cfg.shortName === undefined ? name.slice(0, 12) : text(file, "shortName", cfg.shortName, 12);

  const colours = { ...DEFAULT_BRANDING.colours };
  if (cfg.colours !== undefined) {
    if (typeof cfg.colours !== "object" || cfg.colours === null || Array.isArray(cfg.colours)) {
      throw new BrandingError(file, `"colours" must be an object`);
    }
    for (const [k, v] of Object.entries(cfg.colours)) {
      if (!(k in colours)) throw new BrandingError(file, `unknown colour "${k}" (use ${Object.keys(colours).join(", ")})`);
      if (typeof v !== "string" || !HEX.test(v)) throw new BrandingError(file, `colour "${k}" must be a hex value like #1a2b3c`);
      colours[k as ColourName] = v.toLowerCase();
    }
  }
  for (const [fg, bg] of TEXT_ON_BACKGROUND) {
    const ratio = contrast(colours[fg], colours[bg]);
    if (ratio < MIN_CONTRAST) {
      throw new BrandingError(file, `"${fg}" on "${bg}" has contrast ${ratio.toFixed(2)}:1; at least ${MIN_CONTRAST}:1 is needed to read it`);
    }
  }

  const dir = path.dirname(file);
  return {
    name,
    shortName,
    colours,
    logoPath: imageFile(file, dir, "logo", cfg.logo),
    iconPath: imageFile(file, dir, "icon", cfg.icon),
  };
}

let cached: Branding | null = null;

/** The branding for this server process, read once. Restart to apply a changed file. */
export function getBranding(): Branding {
  if (!cached) {
    const file = process.env.BRANDING_FILE?.trim();
    cached = file ? parseBranding(path.resolve(file)) : DEFAULT_BRANDING;
  }
  return cached;
}

function fileVersion(p: string | null): string {
  if (!p) return "0";
  const st = statSync(p);
  return `${st.size.toString(36)}${Math.floor(st.mtimeMs).toString(36)}`;
}

export function publicBranding(b: Branding = getBranding()): PublicBranding {
  return {
    name: b.name,
    shortName: b.shortName,
    hasLogo: b.logoPath !== null,
    version: `${fileVersion(b.logoPath)}-${fileVersion(b.iconPath)}`,
  };
}

/** The colours as CSS custom properties, for a <style> in the root layout. */
export function cssVariables(b: Branding = getBranding()): string {
  const vars = (Object.keys(b.colours) as ColourName[]).map((k) => `--${k}:${b.colours[k]};`).join("");
  return `:root{${vars}}`;
}
