// Client for gateway-go. Every call either returns what the gateway sent or
// throws ApiError with the gateway's reason. Nothing is built locally when a
// call fails (AGENTS.md: no client-side fallbacks).

import type { CreatePlanRequest } from "./contract/create-plan.request";
import type { Plan } from "./contract/plan";
import type { PlanRevision } from "./contract/plan-revision";
import type {
  ApiKey,
  AuthResponse,
  Coach,
  CreatePlanResponse,
  ModelCatalog,
  ShareLink,
} from "./gateway-types";
import { clearToken, getToken } from "./session";

export const API_BASE = (process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080").replace(/\/$/, "");

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

type Options = { method?: string; body?: unknown; auth?: boolean };

async function request<T>(path: string, { method = "GET", body, auth = true }: Options = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (auth) {
    const token = getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
    });
  } catch {
    throw new ApiError(0, "Could not reach the server. Check the connection and try again.");
  }
  const text = await res.text();
  let data: unknown = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = null;
  }
  if (!res.ok) {
    const body = data as { error?: unknown } | null;
    const reason = typeof body?.error === "string" && body.error ? body.error : `The server answered ${res.status}.`;
    if (res.status === 401 && auth) {
      clearToken();
      if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
        const next = encodeURIComponent(window.location.pathname);
        window.location.assign(`/login?next=${next}`);
      }
    }
    throw new ApiError(res.status, reason);
  }
  return data as T;
}

// --- Auth and profile ---

export const login = (email: string, password: string) =>
  request<AuthResponse>("/api/v1/auth/login", { method: "POST", body: { email, password }, auth: false });

export const register = (body: {
  email: string;
  password: string;
  displayName: string;
  boxName: string;
  defaultClassDurationMin: number;
}) => request<AuthResponse>("/api/v1/auth/register", { method: "POST", body, auth: false });

export const getMe = () => request<{ coach: Coach }>("/api/v1/auth/me").then((r) => r.coach);

export const updateProfile = (body: { displayName: string; boxName: string; defaultClassDurationMin: number }) =>
  request<{ coach: Coach }>("/api/v1/auth/profile", { method: "PUT", body }).then((r) => r.coach);

// --- Keys and models ---

export const listKeys = () => request<{ keys: ApiKey[] | null }>("/api/v1/settings/keys").then((r) => r.keys ?? []);

export const saveKey = (provider: string, apiKey: string) =>
  request<{ key: ApiKey }>("/api/v1/settings/keys", {
    method: "POST",
    body: { provider, apiKey, isPrimary: true },
  }).then((r) => r.key);

export const deleteKey = (id: string) => request<unknown>(`/api/v1/settings/keys/${id}`, { method: "DELETE" });

export const listModels = () => request<ModelCatalog>("/api/v1/settings/models");

// --- Plans ---

export const createPlan = (body: CreatePlanRequest) =>
  request<CreatePlanResponse>("/api/v1/plans", { method: "POST", body });

export const listPlans = () => request<{ plans: Plan[] }>("/api/v1/plans").then((r) => r.plans);

export const getPlan = (id: string) => request<{ plan: Plan }>(`/api/v1/plans/${id}`).then((r) => r.plan);

export const deletePlan = (id: string) => request<unknown>(`/api/v1/plans/${id}`, { method: "DELETE" });

export const listRevisions = (id: string) =>
  request<{ current: number; revisions: PlanRevision[] }>(`/api/v1/plans/${id}/revisions`);

/** SSE URL for a plan's events. EventSource cannot send headers, so the token
 * goes in the query until Phase 5 moves it to a cookie. */
export function planEventsUrl(id: string): string {
  const token = getToken() ?? "";
  return `${API_BASE}/api/v1/plans/${id}/events?token=${encodeURIComponent(token)}`;
}

// --- Sharing ---

export const createShare = (planId: string, includeCoachNotes: boolean) =>
  request<ShareLink>(`/api/v1/plans/${planId}/share`, { method: "POST", body: { includeCoachNotes } });

export const resolveShare = (code: string) =>
  request<{ share: ShareLink; plan: Partial<Plan> }>(`/api/v1/share/${encodeURIComponent(code)}`, { auth: false });
