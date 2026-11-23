// Read-only helpers over the contract Plan. They arrange what the backend sent;
// they never compute plan content (D3).

import type {
  Injury,
  Plan,
  SectionStateName,
  SectionStatusMap,
  TierKey,
  TodayGroup,
  WodPart,
} from "./contract/plan";

export type SectionName = keyof SectionStatusMap;

export const SECTION_ORDER: SectionName[] = ["wod", "warmup", "parts", "injuries", "timeline"];

const BUSY: SectionStateName[] = ["pending", "running"];

export function isBusy(state: SectionStateName | undefined): boolean {
  return state !== undefined && BUSY.includes(state);
}

/** True while any section is still pending or running. */
export function isGenerating(plan: Pick<Plan, "sectionStatus">): boolean {
  return SECTION_ORDER.some((s) => isBusy(plan.sectionStatus?.[s]?.state));
}

export function isFailed(plan: Pick<Plan, "sectionStatus">, section: SectionName): boolean {
  return plan.sectionStatus?.[section]?.state === "failed";
}

export function failReason(plan: Pick<Plan, "sectionStatus">, section: SectionName): string {
  return plan.sectionStatus?.[section]?.error || "No reason was given.";
}

/** "2026-11-18" -> "18.11.26". */
export function wodDate(date?: string): string | null {
  const m = date?.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  return m ? `${m[3]}.${m[2]}.${m[1].slice(2)}` : null;
}

/** Plan title for headers: "WOD 18.11.26", or the creation date while the WOD is not read yet. */
export function planTitle(plan: Pick<Plan, "wod" | "createdAt">): string {
  const d = wodDate(plan.wod?.date);
  if (d) return `WOD ${d}`;
  if (plan.createdAt) {
    const c = new Date(plan.createdAt);
    if (!Number.isNaN(c.getTime())) {
      return `Plan ${String(c.getDate()).padStart(2, "0")}.${String(c.getMonth() + 1).padStart(2, "0")}`;
    }
  }
  return "Plan";
}

export const two = (n: number) => String(n).padStart(2, "0");

/** Jump-bar label for a part: its name when that name is short and unique ("Engine"), else its id ("A"). */
export function partLabel(part: WodPart, all: WodPart[]): string {
  const sameName = all.filter((p) => p.name.toLowerCase() === part.name.toLowerCase()).length;
  return sameName === 1 && part.name.length <= 8 ? part.name : part.id;
}

/** Section anchor id for a part. */
export const partAnchor = (id: string) => `part-${id}`;

/** Where a timeline block jumps to: the warm-up, a part, or nowhere. */
export function blockAnchor(section: string | undefined, plan: Pick<Plan, "wod">): string | null {
  if (!section) return null;
  if (section === "warmup") return "warmup";
  return plan.wod?.parts.some((p) => p.id === section) ? partAnchor(section) : null;
}

/** Tier rows in display order. RX+ only when present (engine parts). */
export const TIER_ORDER: TierKey[] = ["rx", "scaled", "senior", "rxPlus"];

export const TIER_LABEL: Record<TierKey, string> = {
  rx: "RX",
  scaled: "Scaled",
  senior: "Senior",
  rxPlus: "RX+",
};

/** "1 today" note for a tier row when the comment named such athletes. */
export function todayNote(today: TodayGroup[] | undefined, tier: TierKey): string | null {
  const groups = (today ?? []).filter((g) => g.tier === tier);
  if (groups.length === 0) return null;
  const count = groups.reduce((n, g) => n + (g.count ?? 0), 0);
  return count > 0 ? `${count} today` : "today";
}

/** Short state shown under an injury label: "returning" when there is a return note. */
export function injuryNote(injury: Injury): string | null {
  return injury.returnNote ? "returning" : null;
}

/** The injury suggestion for one part, if the Injury agent gave one. */
export function injuryForPart(injury: Injury, partId: string): string | null {
  return injury.parts.find((p) => p.partId === partId)?.suggestion ?? null;
}

/** One line for the context row: "Today: Senior ×1 · Ankle, returning after 2 weeks". */
export function todaySummary(plan: Pick<Plan, "today" | "injuries">): string | null {
  const bits: string[] = [];
  for (const g of plan.today ?? []) bits.push(g.count ? `${g.label} ×${g.count}` : g.label);
  for (const i of plan.injuries ?? []) bits.push(i.returnNote ? `${i.label}, ${i.returnNote}` : i.label);
  return bits.length ? `Today: ${bits.join(" · ")}` : null;
}
