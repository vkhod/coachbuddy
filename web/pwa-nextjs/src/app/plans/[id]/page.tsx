"use client";

// The plan, read on the phone. Only the title row and the jump bar stay fixed;
// while sections are still being built, the progress list sits on top and the
// sections appear as they finish.

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { ChangeSheet, type ChangeMode } from "@/components/ChangeSheet";
import { PlanSections, Progress, type PlanActions, type Refinable } from "@/components/PlanView";
import { ShareSheet } from "@/components/ShareSheet";
import { BackIcon, ErrorNote, Loading, Screen, ShareIcon } from "@/components/ui";
import { isGenerating, partAnchor, partLabel, planTitle, todaySummary } from "@/lib/plan";
import { useCoach } from "@/lib/useCoach";
import { usePlan } from "@/lib/usePlan";

function useActiveAnchor(ids: string[]): string | null {
  const [active, setActive] = useState<string | null>(ids[0] ?? null);
  const key = ids.join(",");
  useEffect(() => {
    const els = ids.map((id) => document.getElementById(id)).filter((e): e is HTMLElement => !!e);
    if (!els.length) return;
    const onScroll = () => {
      let current = els[0].id;
      for (const el of els) if (el.getBoundingClientRect().top <= 120) current = el.id;
      setActive(current);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return active;
}

export default function PlanPage() {
  const { id } = useParams<{ id: string }>();
  useCoach();
  const { plan, error, reload, saved } = usePlan(id);
  const [sharing, setSharing] = useState(false);
  const [sheet, setSheet] = useState<{ section: Refinable; mode: ChangeMode } | null>(null);

  const jumps = useMemo(() => {
    const parts = plan?.wod?.parts ?? [];
    return [
      { id: "time", label: "Time" },
      ...(plan?.warmup || plan?.sectionStatus.warmup.state === "failed" ? [{ id: "warmup", label: "Warm-up" }] : []),
      ...parts.map((p) => ({ id: partAnchor(p.id), label: partLabel(p, parts) })),
    ];
  }, [plan]);
  const active = useActiveAnchor(jumps.map((j) => j.id));

  if (!plan) {
    return (
      <Screen>
        <header className="rule-B flex h-14 items-center pl-1">
          <Link href="/history" aria-label="Back to history" className="tap flex items-center justify-center">
            <BackIcon />
          </Link>
        </header>
        {error ? (
          <div className="px-4">
            <ErrorNote message={error.status === 404 ? "This plan does not exist or is not yours." : error.message} onRetry={reload} />
          </div>
        ) : (
          <Loading label="Opening the plan" />
        )}
      </Screen>
    );
  }

  const generating = isGenerating(plan);
  const context = todaySummary(plan);
  // Changes wait until the plan is settled; the gateway answers 409 otherwise.
  // A saved copy is read only: changes need the gateway.
  const actions: PlanActions | undefined =
    generating || !plan.wod || saved
      ? undefined
      : {
          change: (section) => setSheet({ section, mode: "change" }),
          retry: (section) => setSheet({ section, mode: "retry" }),
          addInjury: () => setSheet({ section: "injuries", mode: "add-injury" }),
        };

  return (
    <Screen>
      <header className="rule-B sticky top-0 z-10 bg-paper">
        <div className="flex min-h-14 flex-wrap items-center pl-1 pr-1">
          <Link href="/history" aria-label="Back to history" className="tap flex items-center justify-center">
            <BackIcon />
          </Link>
          <h1 className="head m-0 min-w-0 flex-1 py-1 text-2xl leading-tight [overflow-wrap:anywhere] narrow:order-last narrow:basis-full narrow:px-3 narrow:pb-2 narrow:pt-0">{planTitle(plan)}</h1>
          <Link
            href={`/plans/${plan.planId}/versions`}
            aria-label={`Version ${plan.revision}, see all versions`}
            className="head tap flex items-center justify-center text-lg normal-case underline underline-offset-4 narrow:ml-auto"
          >
            v{plan.revision}
          </Link>
          <button
            type="button"
            aria-label="Share"
            onClick={() => setSharing(true)}
            disabled={!plan.wod || saved}
            className="tap flex items-center justify-center bg-transparent text-ink disabled:opacity-30"
          >
            <ShareIcon />
          </button>
        </div>
        <nav aria-label="Jump to section" className="flex overflow-x-auto px-2">
          {jumps.map((j) => (
            <a
              key={j.id}
              href={`#${j.id}`}
              aria-current={active === j.id ? "true" : undefined}
              className={`head flex h-11 shrink-0 items-center px-2.5 text-[17px] font-semibold tracking-wide no-underline ${
                active === j.id ? "shadow-[inset_0_-3px_0_var(--ink)]" : ""
              }`}
            >
              {j.label}
            </a>
          ))}
        </nav>
      </header>

      <main className="px-4 pb-16">
        {(context || plan.input.classMinutes) && (
          <section aria-label="Today, from your comment" className="rule-b py-3">
            {context && <p className="head m-0 text-xl text-accent">{context}</p>}
            <p className="m-0 text-[15px] text-sub">{plan.input.classMinutes} min class</p>
          </section>
        )}
        {saved && (
          <p role="status" className="mb-0 mt-4 border-l-2 border-ink pl-3 text-base">
            Offline. This is the copy saved on this phone, v{plan.revision}. Changes and sharing need a connection.
          </p>
        )}
        {error && <ErrorNote message={error.message} onRetry={reload} />}
        {generating && <Progress plan={plan} />}
        <PlanSections plan={plan} actions={actions} />
      </main>

      {sharing && <ShareSheet planId={plan.planId} onClose={() => setSharing(false)} />}
      {sheet && (
        <ChangeSheet
          planId={plan.planId}
          revision={plan.revision}
          section={sheet.section}
          mode={sheet.mode}
          onStarted={() => {
            void reload();
            window.scrollTo({ top: 0, behavior: "smooth" });
          }}
          onClose={() => setSheet(null)}
        />
      )}
    </Screen>
  );
}
