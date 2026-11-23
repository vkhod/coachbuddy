"use client";

// Signed-in coach for pages that need one. Without a token the page sends the
// coach to /login and comes back afterwards.

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ApiError, getMe } from "./api";
import type { Coach } from "./gateway-types";
import { getToken } from "./session";

export function useCoach() {
  const router = useRouter();
  const [coach, setCoach] = useState<Coach | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!getToken()) {
      router.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`);
      return;
    }
    getMe()
      .then(setCoach)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [router]);

  return { coach, setCoach, error };
}
