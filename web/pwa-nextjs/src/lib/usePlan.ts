"use client";

// Loads a plan and follows it while sections are still being built.
// Snapshots come over SSE (PlanEvent.plan). If the stream drops, the hook
// re-reads the stored plan every few seconds instead; both are real server
// state, never anything made up in the browser.
//
// A finished plan is saved on the phone (src/lib/offline.ts) and its page kept
// by the service worker. When the gateway cannot be reached and nothing came
// from it yet, the saved copy is shown and `saved` is true.

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, getPlan, planEventsUrl } from "./api";
import type { Plan } from "./contract/plan";
import type { PlanEvent } from "./contract/plan-event";
import { isGenerating } from "./plan";
import { forgetPlan, savePlan, savedPlan } from "./offline";
import { keepPageOffline } from "@/components/ServiceWorker";

const POLL_MS = 5000;

export function usePlan(planId: string) {
  const [plan, setPlan] = useState<Plan | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [saved, setSaved] = useState(false);
  const revisionRef = useRef(0);
  // True once anything came from the gateway; the saved copy never replaces it.
  const liveRef = useRef(false);

  const accept = useCallback((next: Plan) => {
    if (next.revision < revisionRef.current) return;
    revisionRef.current = next.revision;
    liveRef.current = true;
    setSaved(false);
    setPlan(next);
    if (!isGenerating(next)) {
      void savePlan(next).then((ok) => {
        if (ok) keepPageOffline(`/plans/${next.planId}`);
      });
    }
  }, []);

  const reload = useCallback(async () => {
    try {
      accept(await getPlan(planId));
      setError(null);
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError(0, String(e));
      if (err.status === 404) void forgetPlan(planId);
      if (err.status === 0 && !liveRef.current) {
        const copy = await savedPlan(planId);
        if (copy && !liveRef.current) {
          revisionRef.current = copy.revision;
          setSaved(true);
          setPlan(copy);
          setError(null);
          return;
        }
      }
      setError(err);
    }
  }, [planId, accept]);

  useEffect(() => {
    revisionRef.current = 0;
    liveRef.current = false;
    setSaved(false);
    setPlan(null);
    void reload();
  }, [reload]);

  // Back online: read the plan from the gateway again instead of the saved copy.
  useEffect(() => {
    if (!saved) return;
    const onOnline = () => void reload();
    window.addEventListener("online", onOnline);
    return () => window.removeEventListener("online", onOnline);
  }, [saved, reload]);

  const generating = plan ? isGenerating(plan) : false;
  const revision = plan?.revision;

  useEffect(() => {
    if (!generating) return;
    let poll: ReturnType<typeof setInterval> | null = null;
    const startPolling = () => {
      if (!poll) poll = setInterval(() => void reload(), POLL_MS);
    };
    let source: EventSource | null = null;
    try {
      source = new EventSource(planEventsUrl(planId));
    } catch {
      startPolling();
    }
    if (source) {
      source.onmessage = (msg) => {
        let evt: PlanEvent;
        try {
          evt = JSON.parse(msg.data) as PlanEvent;
        } catch {
          return;
        }
        if (evt.revision < revisionRef.current) return;
        if (evt.plan) accept(evt.plan as Plan);
        if (evt.section === "plan") {
          source?.close();
          void reload();
        }
      };
      source.onerror = () => {
        // The browser retries on its own; read the stored plan meanwhile.
        if (source?.readyState === EventSource.CLOSED) startPolling();
        void reload();
      };
    }
    return () => {
      source?.close();
      if (poll) clearInterval(poll);
    };
  }, [generating, revision, planId, reload, accept]);

  return { plan, error, reload, setPlan: accept, saved };
}
