"use client";

// Registers the service worker (/sw.js, src/lib/sw-script.ts) in a production
// build, and offers a new version instead of switching mid-use (Phase 4 plan,
// approved): when a new worker is waiting, a bar says "New version available"
// and Reload hands over to it.

import { useEffect, useState } from "react";

/** Asks the worker to keep a page for offline reading (used by step 3). */
export function keepPageOffline(path: string) {
  navigator.serviceWorker?.controller?.postMessage({ type: "cache-page", path });
}

/** Drops the kept plan pages (sign out). */
export function dropKeptPages() {
  navigator.serviceWorker?.controller?.postMessage({ type: "drop-pages" });
}

export function ServiceWorker() {
  const [waiting, setWaiting] = useState<ServiceWorker | null>(null);

  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) return;
    let reg: ServiceWorkerRegistration | undefined;

    const watch = (r: ServiceWorkerRegistration) => {
      // A waiting worker counts only when another one controls the page; the
      // first install takes over by itself and needs no prompt.
      const offer = () => {
        if (r.waiting && navigator.serviceWorker.controller) setWaiting(r.waiting);
      };
      offer();
      r.addEventListener("updatefound", () => {
        const w = r.installing;
        w?.addEventListener("statechange", () => {
          if (w.state === "installed") offer();
        });
      });
    };

    navigator.serviceWorker
      .register("/sw.js", { scope: "/", updateViaCache: "none" })
      .then((r) => {
        reg = r;
        watch(r);
      })
      .catch(() => {
        // No worker (private mode, an old browser): the app works online as before.
      });

    // A phone keeps the app open for days; look for a new version when it comes back.
    const onVisible = () => {
      if (document.visibilityState === "visible") void reg?.update().catch(() => undefined);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, []);

  const reload = () => {
    if (!waiting) return;
    navigator.serviceWorker.addEventListener("controllerchange", () => window.location.reload(), { once: true });
    waiting.postMessage({ type: "skip-waiting" });
  };

  if (!waiting) return null;
  return (
    <>
      {/* Room under the page, so the bar never covers its last lines. */}
      <div aria-hidden className="h-20" />
      <div role="status" className="fixed inset-x-0 bottom-0 z-50 border-t-2 border-ink bg-paper">
        <div className="mx-auto flex max-w-[480px] flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2">
          <p className="m-0 flex-1 text-base">New version available.</p>
          <button type="button" onClick={reload} className="head tap rounded-sm bg-ink px-5 text-lg tracking-wide text-paper">
            Reload
          </button>
        </div>
      </div>
    </>
  );
}
