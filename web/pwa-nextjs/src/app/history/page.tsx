"use client";

import Link from "next/link";
import { BrandMark } from "@/components/Brand";
import { useEffect, useMemo, useState } from "react";
import { ErrorNote, Loading, Screen, SettingsIcon } from "@/components/ui";
import { ApiError, listPlans } from "@/lib/api";
import type { Plan } from "@/lib/contract/plan";
import { isGenerating, isFailed, SECTION_ORDER } from "@/lib/plan";
import { useCoach } from "@/lib/useCoach";

function shortDate(plan: Plan): string {
  const m = plan.wod?.date?.match(/^\d{4}-(\d{2})-(\d{2})$/);
  if (m) return `${m[2]}.${m[1]}`;
  const c = plan.createdAt ? new Date(plan.createdAt) : null;
  return c && !Number.isNaN(c.getTime()) ? `${String(c.getDate()).padStart(2, "0")}.${String(c.getMonth() + 1).padStart(2, "0")}` : "--.--";
}

function summary(plan: Plan): string {
  if (plan.wod) return plan.wod.parts.map((p) => p.prescription).join(" · ");
  return isFailed(plan, "wod") ? "Could not read the screenshot" : "Reading the screenshot";
}

function status(plan: Plan): string {
  const bits = [`v${plan.revision}`];
  if (isGenerating(plan)) bits.push("building");
  else if (SECTION_ORDER.some((s) => isFailed(plan, s))) bits.push("a section failed");
  return bits.join(" · ");
}

export default function HistoryPage() {
  const { coach } = useCoach();
  const [plans, setPlans] = useState<Plan[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const load = () => {
    setError(null);
    listPlans()
      .then(setPlans)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };
  useEffect(() => {
    if (coach) load();
  }, [coach]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!plans || !q) return plans;
    return plans.filter((p) =>
      [shortDate(p), p.wod?.title ?? "", p.wod?.date ?? "", summary(p)].some((s) => s.toLowerCase().includes(q)),
    );
  }, [plans, query]);

  return (
    <Screen>
      <header className="rule-B flex h-14 items-center pl-4 pr-1">
        <Link href="/" className="flex flex-1 items-center self-stretch no-underline">
          <BrandMark />
        </Link>
        <Link href="/settings" aria-label="Settings" className="tap flex items-center justify-center">
          <SettingsIcon />
        </Link>
      </header>
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <div className="flex items-baseline justify-between">
          <h1 className="head m-0 text-[40px] leading-[0.95]">History</h1>
          <Link href="/" className="lnk tap flex items-center text-[15px]">
            + New plan
          </Link>
        </div>
        <label htmlFor="search" className="sr-only">
          Search plans
        </label>
        <input
          id="search"
          type="search"
          placeholder="Search movements or dates"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="field mt-4"
        />
        {error && <ErrorNote message={error} onRetry={load} />}
        {!plans && !error && <Loading label="Loading plans" />}
        {shown && shown.length === 0 && (
          <p className="mt-6 text-base text-sub">{plans?.length ? "No plan matches the search." : "No plans yet. Start with a WOD screenshot."}</p>
        )}
        {shown && shown.length > 0 && (
          <ol className="m-0 mt-4 list-none border-t-2 border-ink p-0">
            {shown.map((p) => (
              <li key={p.planId}>
                <Link href={`/plans/${p.planId}`} className="rule-b pair min-h-[60px] items-baseline gap-y-1 py-3 no-underline [--side:4rem]">
                  <span className="head text-2xl">{shortDate(p)}</span>
                  <span>
                    <span className="block text-base leading-snug">{summary(p)}</span>
                    <span className="text-sm text-sub">{status(p)}</span>
                  </span>
                </Link>
              </li>
            ))}
          </ol>
        )}
      </main>
    </Screen>
  );
}
