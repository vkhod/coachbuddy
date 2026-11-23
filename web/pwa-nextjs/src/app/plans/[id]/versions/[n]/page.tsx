"use client";

// An earlier version, read only. Restoring happens from Versions.

import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { PlanSections } from "@/components/PlanView";
import { BackHeader, ErrorNote, Loading, Screen } from "@/components/ui";
import { ApiError, getRevision } from "@/lib/api";
import type { Plan } from "@/lib/contract/plan";
import { planTitle } from "@/lib/plan";
import { useCoach } from "@/lib/useCoach";

export default function RevisionPage() {
  const { id, n } = useParams<{ id: string; n: string }>();
  const { coach } = useCoach();
  const [plan, setPlan] = useState<Plan | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setError(null);
    getRevision(id, Number(n))
      .then((r) => setPlan(r.plan))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };
  useEffect(() => {
    if (coach) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [coach, id, n]);

  return (
    <Screen>
      <BackHeader href={`/plans/${id}/versions`} label="Back to versions" title={plan ? `${planTitle(plan)} · v${n}` : `v${n}`} />
      <main className="px-4 pb-16">
        <p className="rule-b m-0 py-3 text-[15px] text-sub">An earlier version, read only. Restore it from Versions to use it again.</p>
        {error && <ErrorNote message={error} onRetry={load} />}
        {!plan && !error && <Loading label="Opening the version" />}
        {plan && <PlanSections plan={plan} />}
      </main>
    </Screen>
  );
}
