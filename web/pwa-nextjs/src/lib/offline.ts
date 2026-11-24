"use client";

// Plans kept on the phone for reading offline (Phase 4 step 3).
//
// When a plan is opened and finished (no section pending or running), its
// current revision, exactly as the gateway returned it, is saved in IndexedDB
// under the signed-in coach. Offline, /plans/[id] and History show the saved
// copy and say so. Nothing here builds or changes a plan: a saved plan is
// replaced only by a newer revision from the gateway (AGENTS.md: no
// client-side fallbacks).
//
// Kept: the latest revision of up to MAX_PLANS plans, the most recently opened
// first. Not kept: versions, share links, screenshots. Sign out, or another
// coach signing in on the same phone, clears it.

import type { Plan } from "./contract/plan";
import type { Coach } from "./gateway-types";
import { isGenerating } from "./plan";

const DB = "coachbuddy-offline";
const STORE = "plans";
const COACH_KEY = "cb.coach";
export const MAX_PLANS = 100;

interface Saved {
  planId: string;
  coachId: string;
  savedAt: number;
  plan: Plan;
}

let dbPromise: Promise<IDBDatabase> | null = null;

function open(): Promise<IDBDatabase> {
  if (!dbPromise) {
    dbPromise = new Promise((resolve, reject) => {
      if (typeof indexedDB === "undefined") return reject(new Error("no IndexedDB"));
      const req = indexedDB.open(DB, 1);
      req.onupgradeneeded = () => {
        const store = req.result.createObjectStore(STORE, { keyPath: "planId" });
        store.createIndex("savedAt", "savedAt");
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    dbPromise.catch(() => (dbPromise = null));
  }
  return dbPromise;
}

function done<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function all(): Promise<Saved[]> {
  const db = await open();
  return done(db.transaction(STORE).objectStore(STORE).getAll() as IDBRequest<Saved[]>);
}

// --- The signed-in coach, for offline use ---

/** Remembers the coach the gateway returned. A different coach than before clears the saved plans. */
export function rememberCoach(coach: Coach): void {
  const before = cachedCoach();
  if (before && before.id !== coach.id) void clearOffline();
  try {
    localStorage.setItem(COACH_KEY, JSON.stringify(coach));
  } catch {
    /* storage full or blocked: offline reading just will not work */
  }
}

/** The coach last returned by the gateway on this phone, or null. */
export function cachedCoach(): Coach | null {
  try {
    const raw = localStorage.getItem(COACH_KEY);
    return raw ? (JSON.parse(raw) as Coach) : null;
  } catch {
    return null;
  }
}

// --- Plans ---

/** Saves a finished plan for the current coach; a plan still being built is not saved. */
export async function savePlan(plan: Plan): Promise<boolean> {
  const coach = cachedCoach();
  if (!coach || isGenerating(plan)) return false;
  try {
    const db = await open();
    const store = db.transaction(STORE, "readwrite").objectStore(STORE);
    const prev = (await done(store.get(plan.planId))) as Saved | undefined;
    if (prev && prev.plan.revision > plan.revision) return true;
    await done(store.put({ planId: plan.planId, coachId: coach.id, savedAt: Date.now(), plan } satisfies Saved));
    await trim();
    void askPersistent();
    return true;
  } catch {
    return false;
  }
}

async function trim(): Promise<void> {
  const saved = await all();
  if (saved.length <= MAX_PLANS) return;
  const db = await open();
  const store = db.transaction(STORE, "readwrite").objectStore(STORE);
  const old = saved.sort((a, b) => b.savedAt - a.savedAt).slice(MAX_PLANS);
  await Promise.all(old.map((s) => done(store.delete(s.planId))));
}

/** The saved copy of a plan for the current coach, or null. */
export async function savedPlan(planId: string): Promise<Plan | null> {
  const coach = cachedCoach();
  if (!coach) return null;
  try {
    const db = await open();
    const s = (await done(db.transaction(STORE).objectStore(STORE).get(planId))) as Saved | undefined;
    return s && s.coachId === coach.id ? s.plan : null;
  } catch {
    return null;
  }
}

/** Saved plans of the current coach, newest WOD first (as History lists them). */
export async function savedPlans(): Promise<Plan[]> {
  const coach = cachedCoach();
  if (!coach) return [];
  try {
    return (await all())
      .filter((s) => s.coachId === coach.id)
      .map((s) => s.plan)
      .sort((a, b) => (b.createdAt ?? "").localeCompare(a.createdAt ?? ""));
  } catch {
    return [];
  }
}

/** Ids of the saved plans, for the "Available offline" badge. */
export async function savedPlanIds(): Promise<Set<string>> {
  return new Set((await savedPlans()).map((p) => p.planId));
}

/** Drops one saved plan (the gateway says it no longer exists). */
export async function forgetPlan(planId: string): Promise<void> {
  try {
    const db = await open();
    await done(db.transaction(STORE, "readwrite").objectStore(STORE).delete(planId));
  } catch {
    /* nothing saved */
  }
}

/** Sign out: every saved plan and the remembered coach go. */
export async function clearOffline(): Promise<void> {
  try {
    localStorage.removeItem(COACH_KEY);
  } catch {
    /* ignore */
  }
  try {
    const db = await open();
    await done(db.transaction(STORE, "readwrite").objectStore(STORE).clear());
  } catch {
    /* nothing saved */
  }
}

let asked = false;

/** Asks the browser not to evict the saved plans when space runs low (once per page load). */
async function askPersistent(): Promise<void> {
  if (asked || !navigator.storage?.persist) return;
  asked = true;
  try {
    if (!(await navigator.storage.persisted())) await navigator.storage.persist();
  } catch {
    /* not supported: the browser may still keep them */
  }
}
