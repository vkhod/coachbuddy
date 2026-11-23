// The signed-in coach's token. Kept in localStorage until Phase 5 moves it to an
// httpOnly cookie (plan item S7). Never holds an LLM API key.

const TOKEN_KEY = "cb.token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string): void {
  try {
    window.localStorage.setItem(TOKEN_KEY, token);
  } catch {
    // Private mode without storage: the session lasts for this page only.
  }
}

export function clearToken(): void {
  try {
    window.localStorage.removeItem(TOKEN_KEY);
  } catch {
    // nothing stored
  }
}
