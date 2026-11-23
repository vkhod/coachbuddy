// Shapes of gateway-only responses (auth, profile, keys, models, share links).
// They are not part of the plan contract in schema/; plan payloads use the
// generated types in ./contract instead.

export interface Coach {
  id: string;
  email: string;
  displayName: string;
  boxName: string;
  defaultClassDurationMin: number;
  createdAt: string;
}

export interface AuthResponse {
  token: string;
  coach: Coach;
}

export interface ApiKey {
  id: string;
  provider: string;
  keyHint: string;
  isPrimary: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Provider {
  id: string;
  label: string;
}

export interface ModelCatalog {
  providers: Provider[];
  models: { id: string; provider: string; label: string; vision?: boolean; default?: boolean }[];
}

export interface ShareLink {
  shortCode: string;
  shareUrl: string;
  planId: string;
  includeCoachNotes: boolean;
  qrSvg: string;
  createdAt: string;
}

export interface CreatePlanResponse {
  planId: string;
  revision: number;
  planUrl: string;
  eventsUrl: string;
}

/** One saved model choice (GET/PUT /api/v1/settings/agents). */
export interface AgentPref {
  /** "default-<role>" when the coach has saved nothing and the gateway lists its defaults. */
  id: string;
  agentRole: AgentRole;
  provider: string;
  /** Empty in a PUT clears the role's choice. */
  modelIdentifier: string;
}

/** "global" is the coach's model; the others override it for one agent. */
export type AgentRole = "global" | "parser" | "warmup" | "scaling" | "injury" | "timeline";

/** What one agent did for one section (agents-python's AgentRun). */
export interface AgentRunEntry {
  agent: string;
  section: string;
  provider: string;
  model: string;
  attempts: number;
  latencyMs: number;
  state: string;
  error: string | null;
  tools: string[];
}

/** One revision's runs (GET /api/v1/settings/runs). */
export interface AgentRunsRecord {
  planId: string;
  revision: number;
  createdAt: string;
  planTitle: string;
  wodDate: string;
  runs: AgentRunEntry[];
  orchestrationLatencyMs: number | null;
}
