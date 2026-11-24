"use client";

// Whether the browser thinks it is online. navigator.onLine is false only when
// there is no network at all; a connected phone whose network goes nowhere
// still reads as online, and then the gateway call itself fails with its own
// message (src/lib/api.ts).

import { useSyncExternalStore } from "react";

function subscribe(cb: () => void) {
  window.addEventListener("online", cb);
  window.addEventListener("offline", cb);
  return () => {
    window.removeEventListener("online", cb);
    window.removeEventListener("offline", cb);
  };
}

export function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
}

/** Shown wherever a plan is built or changed: the agents run on the server. */
export const OFFLINE_BUILD_NOTE = "You are offline. Building or changing a plan needs a connection.";
