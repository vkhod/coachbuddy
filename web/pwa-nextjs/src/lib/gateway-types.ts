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
