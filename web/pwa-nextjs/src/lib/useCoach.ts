"use client";

// Signed-in coach for pages that need one. Without a token the page sends the
// coach to /login and comes back afterwards. When the gateway cannot be reached
// (offline), the coach it returned last time on this phone is used, so saved
// plans can be read (src/lib/offline.ts); `offline` is then true.

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ApiError, getMe } from "./api";
import type { Coach } from "./gateway-types";
import { cachedCoach, rememberCoach } from "./offline";
import { getToken } from "./session";

export function useCoach() {
  const router = useRouter();
  const [coach, setCoach] = useState<Coach | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [offline, setOffline] = useState(false);

  useEffect(() => {
    if (!getToken()) {
      router.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`);
      return;
    }
    getMe()
      .then((c) => {
        rememberCoach(c);
        setOffline(false);
        setCoach(c);
      })
      .catch((e) => {
        const cached = e instanceof ApiError && e.status === 0 ? cachedCoach() : null;
        if (cached) {
          setOffline(true);
          setCoach(cached);
        } else setError(e instanceof ApiError ? e.message : String(e));
      });
  }, [router]);

  return { coach, setCoach, error, offline };
}
