"use client";

// Versions: what each revision changed. Restoring copies an old version into a
// new one, so nothing is lost.

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { BackHeader, ErrorNote, Loading, Screen } from "@/components/ui";
import { ApiError, getPlan, listRevisions, restoreRevision } from "@/lib/api";
import type { PlanRevision, RevisionSection } from "@/lib/contract/plan-revision";
import { planTitle } from "@/lib/plan";
import { useCoach } from "@/lib/useCoach";

const SECTION_NAME: Record<RevisionSection, string> = {
  wod: "WOD",
  warmup: "Warm-up",
  parts: "Scaling",
  injuries: "Injury notes",
  timeline: "Timeline",
};

function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getDate())}.${p(d.getMonth() + 1)} · ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** What a revision changed, in the coach's words where there are any. */
function describe(rev: PlanRevision): string[] {
  const c = rev.change;
  if (c.kind === "create") return ["First plan from the screenshot and your comment"];
  if (c.kind === "restore") return [`Restored from v${c.restoredFrom}`];
  const name = c.section ? SECTION_NAME[c.section] : "Section";
  const lines = [c.instruction ? `${name} rebuilt: “${c.instruction}”` : `${name} rebuilt`];
  const also = rev.sections.filter((s) => s !== c.section).map((s) => SECTION_NAME[s]);
  if (also.length) lines.push(`${also.join(", ")} updated too`);
  return lines;
}

export default function VersionsPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { coach } = useCoach();
  const [title, setTitle] = useState("Plan");
  const [data, setData] = useState<{ current: number; revisions: PlanRevision[] } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [restoring, setRestoring] = useState<number | null>(null);

  const load = () => {
    setError(null);
    Promise.all([listRevisions(id), getPlan(id)])
      .then(([r, p]) => {
        setData(r);
        setTitle(planTitle(p));
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };
  useEffect(() => {
    if (coach) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [coach, id]);

  async function restore(n: number) {
    setRestoring(n);
    setError(null);
    try {
      await restoreRevision(id, n);
      router.push(`/plans/${id}`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setRestoring(null);
    }
  }

  const busy = data?.revisions.some((r) => r.state === "running") ?? false;
  const revisions = data ? [...data.revisions].sort((a, b) => b.revision - a.revision) : [];

  return (
    <Screen>
      <BackHeader href={`/plans/${id}`} label="Back to the plan" title={title} />
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <h1 className="head m-0 text-[40px] leading-[0.95]">Versions</h1>
        {error && <ErrorNote message={error} onRetry={load} />}
        {!data && !error && <Loading label="Loading versions" />}
        {data && (
          <ol reversed className="m-0 mt-4 list-none border-t-2 border-ink p-0">
            {revisions.map((r) => {
              const current = r.revision === data.current;
              return (
                <li key={r.revision} className="rule-b grid grid-cols-[48px_minmax(0,1fr)] gap-1 py-3">
                  <span className="head text-2xl normal-case">v{r.revision}</span>
                  <div className="flex flex-col gap-1">
                    <span className="head text-base text-sub">
                      {current ? "Current · " : ""}
                      {when(r.createdAt)}
                      {r.state === "running" ? " · building" : r.state === "failed" ? " · a section failed" : ""}
                    </span>
                    {describe(r).map((line, i) => (
                      <span key={i} className={i === 0 ? "text-base leading-snug" : "text-[15px] text-sub"}>
                        {line}
                      </span>
                    ))}
                    <div className="mt-1 flex gap-6">
                      {current ? (
                        <Link href={`/plans/${id}`} className="lnk tap flex items-center text-[15px]">
                          Open
                        </Link>
                      ) : (
                        <>
                          <Link href={`/plans/${id}/versions/${r.revision}`} className="lnk tap flex items-center text-[15px]">
                            View
                          </Link>
                          {r.state !== "running" && (
                            <button
                              type="button"
                              disabled={busy || restoring !== null}
                              onClick={() => restore(r.revision)}
                              className="lnk tap bg-transparent p-0 text-[15px] text-ink disabled:opacity-40"
                            >
                              {restoring === r.revision ? "Restoring" : `Restore as v${data.current + 1}`}
                            </button>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        )}
        <p className="mb-0 mt-4 text-sm text-sub">Restoring copies an old version into a new one, so nothing is lost.</p>
      </main>
    </Screen>
  );
}
