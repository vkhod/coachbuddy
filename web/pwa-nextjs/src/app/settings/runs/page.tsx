"use client";

// Settings > Agent runs: what actually ran for the latest plans and changes, one
// record per revision. Which agent built each section, on which model, how many
// attempts (2 means one repair), how long, the mcp-rust tools it called and the
// reason when it failed. Read only; it never reaches a share link.

import Link from "next/link";
import { useEffect, useState } from "react";
import { BackHeader, ErrorNote, Loading, Screen } from "@/components/ui";
import { ApiError, listAgentRuns } from "@/lib/api";
import type { AgentRunEntry, AgentRunsRecord } from "@/lib/gateway-types";
import { useCoach } from "@/lib/useCoach";

const SECTION_LABEL: Record<string, string> = {
  wod: "Board",
  warmup: "Warm-up",
  parts: "Scaling",
  injuries: "Injuries",
  timeline: "Timeline",
};

const seconds = (ms: number | null | undefined) => (typeof ms === "number" && ms > 0 ? `${(ms / 1000).toFixed(1)}\u00a0s` : "");

function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString("en-GB", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

/** The model id without its provider prefix: "gemini/gemini-3.8-flash" reads as "gemini-3.8-flash". */
const modelName = (m: string) => m.split("/").slice(1).join("/") || m;

function RunRow({ run }: { run: AgentRunEntry }) {
  const failed = run.state === "failed";
  const facts = [
    modelName(run.model),
    run.attempts > 1 ? `${run.attempts} attempts` : run.attempts === 1 ? "1 attempt" : "",
    seconds(run.latencyMs),
    run.tools?.length ? `tools: ${run.tools.join(", ")}` : "",
  ].filter(Boolean);
  return (
    <li className="rule-b grid grid-cols-[88px_minmax(0,1fr)] gap-2 py-2">
      <span className="head text-lg">{SECTION_LABEL[run.section] ?? run.section}</span>
      <span className="text-[15px]">
        <span className={failed ? "font-semibold" : ""}>{failed ? "Failed" : run.state === "done" ? "Done" : run.state}</span>
        <span className="text-sub"> · {facts.join(" · ")}</span>
        {failed && run.error && <span className="block text-sm">{run.error}</span>}
      </span>
    </li>
  );
}

export default function AgentRunsPage() {
  const { coach, error: coachError } = useCoach();
  const [records, setRecords] = useState<AgentRunsRecord[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setError(null);
    listAgentRuns(20)
      .then(setRecords)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };
  useEffect(() => {
    if (coach) load();
  }, [coach]);

  return (
    <Screen>
      <BackHeader href="/settings" label="Back to Settings" title="Settings" />
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <h1 className="head m-0 text-[40px] leading-[0.95]">Agent runs</h1>
        <p className="m-0 mt-2 text-base text-sub">What ran for your latest plans and changes, newest first.</p>
        {(coachError || error) && <ErrorNote message={(coachError || error)!} onRetry={coachError ? () => window.location.reload() : load} />}
        {!records && !error && !coachError && <Loading label="Loading runs" />}
        {records && records.length === 0 && <p className="mt-6 text-base">No runs yet. They appear after your first plan.</p>}
        {records?.map((r) => (
          <section key={`${r.planId}:${r.revision}`} className="pt-6" aria-label={`${r.planTitle || "Plan"}, version ${r.revision}`}>
            <div className="rule-B flex items-baseline justify-between gap-3 pb-1.5">
              <h2 className="head m-0 min-w-0 truncate text-2xl">
                <Link href={`/plans/${r.planId}`} className="no-underline">
                  {r.planTitle || "Untitled plan"}
                </Link>
              </h2>
              <span className="head shrink-0 text-lg text-sub">v{r.revision}</span>
            </div>
            <p className="m-0 pt-1 text-sm text-sub">
              {[r.wodDate, when(r.createdAt), seconds(r.orchestrationLatencyMs) && `total ${seconds(r.orchestrationLatencyMs)}`].filter(Boolean).join(" · ")}
            </p>
            <ul className="m-0 list-none p-0">
              {r.runs.map((run, i) => (
                <RunRow key={`${run.section}-${i}`} run={run} />
              ))}
            </ul>
          </section>
        ))}
      </main>
    </Screen>
  );
}
