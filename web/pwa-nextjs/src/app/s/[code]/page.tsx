"use client";

// Public share page. It shows only what the gateway's share view returned
// (athlete: WOD, timeline, tiers; coach: adds warm-up, today, injuries, cues).
// No sign-in.

import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { PlanSections } from "@/components/PlanView";
import { ErrorNote, Loading, Screen } from "@/components/ui";
import { ApiError, resolveShare } from "@/lib/api";
import type { Plan } from "@/lib/contract/plan";
import type { ShareLink } from "@/lib/gateway-types";
import { planTitle } from "@/lib/plan";

export default function SharePage() {
  const { code } = useParams<{ code: string }>();
  const [data, setData] = useState<{ share: ShareLink; plan: Partial<Plan> } | null>(null);
  const [error, setError] = useState<ApiError | null>(null);

  const load = () => {
    setError(null);
    resolveShare(code)
      .then(setData)
      .catch((e) => setError(e instanceof ApiError ? e : new ApiError(0, String(e))));
  };
  useEffect(load, [code]);

  if (!data) {
    return (
      <Screen>
        <header className="rule-B px-4 py-3">
          <span className="head text-[22px] tracking-wide">Coach Buddy</span>
        </header>
        {error ? (
          <div className="px-4">
            <ErrorNote message={error.message} onRetry={error.status === 404 ? undefined : load} />
          </div>
        ) : (
          <Loading label="Opening the plan" />
        )}
      </Screen>
    );
  }

  const { plan, share } = data;
  const coachView = share.includeCoachNotes;
  return (
    <Screen>
      <header className="rule-B px-4 pb-3 pt-4">
        <span className="head block text-base text-sub">Coach Buddy · {coachView ? "coach view" : "athlete view"}</span>
        <h1 className="head m-0 text-[36px] leading-none">{planTitle(plan)}</h1>
        {plan.timeline && <span className="head text-lg font-semibold text-sub">{plan.timeline.totalMinutes} min class</span>}
      </header>
      <main className="px-4 pb-12">
        <PlanSections plan={plan} audience={coachView ? "coach-share" : "athlete"} />
        <p className="mt-8 text-sm text-sub">Shared by your coach with Coach Buddy.</p>
      </main>
    </Screen>
  );
}
