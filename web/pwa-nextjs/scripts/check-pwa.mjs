// Checks the PWA manifest and icons on the production build (Phase 4 step 1).
//
// Run after `npm run build`: `npm run check:pwa`. It starts .next/standalone
// twice, with the built-in branding and with config/branding/attakashaka, and
// checks for each:
//   - the manifest has id, scope, start_url, display and the three PNG icons;
//   - every icon is a PNG of the size the manifest says;
//   - the maskable icon has only the paper colour outside its middle 80%;
//   - the apple-touch-icon is 180 px with no transparency (iOS fills it black);
//   - /sw.js (Phase 4 step 2) answers as JavaScript, carries .next/BUILD_ID in
//     its version, and every build file it precaches answers 200.
// Exits 1 with the reasons when a check fails.

import { spawn } from "node:child_process";
import { cpSync, existsSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const standalone = path.join(root, ".next", "standalone");
const repo = path.resolve(root, "..", "..");

const BRANDINGS = [
  { label: "built-in", file: null, paper: "#f6f4ee" },
  { label: "attakashaka", file: path.join(repo, "config", "branding", "attakashaka", "branding.json"), paper: "#ffffff" },
];

if (!existsSync(path.join(standalone, "server.js"))) {
  console.error("no standalone build; run npm run build first");
  process.exit(1);
}
// next build leaves these out of the standalone folder (the Dockerfile copies them too).
// Remove the old copy first: a file left from an earlier build would be precached.
rmSync(path.join(standalone, ".next", "static"), { recursive: true, force: true });
cpSync(path.join(root, "public"), path.join(standalone, "public"), { recursive: true });
cpSync(path.join(root, ".next", "static"), path.join(standalone, ".next", "static"), { recursive: true });

const failures = [];
const fail = (label, msg) => failures.push(`${label}: ${msg}`);

function rgb(hex) {
  const h = hex.slice(1);
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
}

async function startServer(port, brandingFile) {
  const env = { ...process.env, PORT: String(port), HOSTNAME: "127.0.0.1", NODE_ENV: "production" };
  delete env.BRANDING_FILE;
  if (brandingFile) env.BRANDING_FILE = brandingFile;
  const proc = spawn(process.execPath, ["server.js"], { cwd: standalone, env, stdio: ["ignore", "pipe", "pipe"] });
  let log = "";
  proc.stdout.on("data", (d) => (log += d));
  proc.stderr.on("data", (d) => (log += d));
  const base = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    if (proc.exitCode !== null) throw new Error(`server exited: ${log}`);
    try {
      const r = await fetch(`${base}/manifest.webmanifest`);
      if (r.ok) return { proc, base };
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  proc.kill();
  throw new Error(`server did not start: ${log}`);
}

async function checkBranding({ label, file, paper }, port) {
  const { proc, base } = await startServer(port, file);
  try {
    const manifest = await (await fetch(`${base}/manifest.webmanifest`)).json();
    for (const key of ["id", "scope", "start_url", "display", "name", "short_name"]) {
      if (!manifest[key]) fail(label, `manifest has no ${key}`);
    }
    if (manifest.display !== "standalone") fail(label, `display is ${manifest.display}`);
    const icons = manifest.icons ?? [];
    const want = [
      ["192x192", "any"],
      ["512x512", "any"],
      ["512x512", "maskable"],
    ];
    for (const [sizes, purpose] of want) {
      if (!icons.some((i) => i.sizes === sizes && i.purpose === purpose && i.type === "image/png")) {
        fail(label, `manifest has no ${purpose} PNG icon of ${sizes}`);
      }
    }

    const toCheck = [...icons.map((i) => ({ ...i, src: i.src })), { src: "/brand/apple-icon.png", sizes: "180x180", purpose: "apple" }];
    for (const icon of toCheck) {
      const res = await fetch(base + icon.src);
      if (!res.ok || res.headers.get("content-type") !== "image/png") {
        fail(label, `${icon.src}: HTTP ${res.status} ${res.headers.get("content-type")}`);
        continue;
      }
      const buf = Buffer.from(await res.arrayBuffer());
      const meta = await sharp(buf).metadata();
      const [w, h] = icon.sizes.split("x").map(Number);
      if (meta.format !== "png" || meta.width !== w || meta.height !== h) {
        fail(label, `${icon.src}: ${meta.format} ${meta.width}x${meta.height}, manifest says ${icon.sizes}`);
      }
      if (icon.purpose === "apple" && meta.hasAlpha) fail(label, `${icon.src} has transparency`);
      if (icon.purpose === "maskable") {
        const { data, info } = await sharp(buf).removeAlpha().raw().toBuffer({ resolveWithObject: true });
        const [pr, pg, pb] = rgb(paper);
        const lo = Math.floor(info.width * 0.1);
        const hi = info.width - lo;
        let outside = 0;
        for (let y = 0; y < info.height; y++) {
          for (let x = 0; x < info.width; x++) {
            if (x >= lo && x < hi && y >= lo && y < hi) continue;
            const o = (y * info.width + x) * info.channels;
            if (Math.abs(data[o] - pr) > 2 || Math.abs(data[o + 1] - pg) > 2 || Math.abs(data[o + 2] - pb) > 2) outside++;
          }
        }
        if (outside) fail(label, `${icon.src}: ${outside} pixels outside the middle 80% are not the paper colour ${paper}`);
      }
    }

    const html = await (await fetch(`${base}/login`)).text();
    if (!html.includes('rel="manifest"')) fail(label, "page has no manifest link");
    if (!html.includes("/brand/apple-icon.png")) fail(label, "page has no apple-touch-icon link");
    const assets = await checkWorker(label, base);
    console.log(`${label}: manifest, ${toCheck.length} icons and the service worker (${assets} files) checked`);
  } finally {
    proc.kill();
  }
}

const buildId = readFileSync(path.join(root, ".next", "BUILD_ID"), "utf8").trim();

async function checkWorker(label, base) {
  const res = await fetch(`${base}/sw.js`);
  const type = res.headers.get("content-type") ?? "";
  if (!res.ok || !/^(application|text)\/javascript/.test(type)) {
    fail(label, `/sw.js: HTTP ${res.status} ${type}`);
    return 0;
  }
  const script = await res.text();
  const version = JSON.parse(script.match(/^const VERSION = (".*");$/m)?.[1] ?? "null");
  if (!version?.startsWith(`${buildId}-`)) fail(label, `/sw.js version ${version} does not start with BUILD_ID ${buildId}`);
  const assets = JSON.parse(script.match(/^const ASSETS = (\[.*\]);$/m)?.[1] ?? "[]");
  if (!assets.length) fail(label, "/sw.js precaches no build files");
  for (const a of assets) {
    const r = await fetch(base + a);
    if (!r.ok) fail(label, `/sw.js lists ${a}, which answers HTTP ${r.status}`);
  }
  const pages = JSON.parse(script.match(/^const PAGES = (\[.*\]);$/m)?.[1] ?? "[]");
  for (const p of pages) {
    const r = await fetch(base + p, { redirect: "manual" });
    if (r.status !== 200) fail(label, `/sw.js keeps ${p}, which answers HTTP ${r.status}`);
  }
  return assets.length;
}

let port = 3910;
for (const b of BRANDINGS) await checkBranding(b, port++);

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("PWA checks passed");
