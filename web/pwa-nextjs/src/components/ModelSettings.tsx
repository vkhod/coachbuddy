"use client";

// Settings > Model: the model every agent uses, and under Advanced a different
// model for one agent. Only models of providers the coach has a key for are
// offered. The gateway decides what actually runs (buildRoutes): a choice whose
// provider has no key falls back to the primary key's default model, and the
// agent runs page shows what ran.

import Link from "next/link";
import { useEffect, useState } from "react";
import { ErrorNote } from "@/components/ui";
import { ApiError, getAgentPrefs, saveAgentPrefs } from "@/lib/api";
import type { AgentPref, AgentRole, ApiKey, ModelCatalog } from "@/lib/gateway-types";

const msg = (e: unknown) => (e instanceof ApiError ? e.message : String(e));

/** Per-agent roles in pipeline order, with what each one does in the coach's words. */
export const AGENT_ROLES: { role: Exclude<AgentRole, "global">; label: string; does: string }[] = [
  { role: "parser", label: "Board", does: "reads the WOD screenshot and your comment" },
  { role: "warmup", label: "Warm-up", does: "plans the warm-up" },
  { role: "scaling", label: "Scaling", does: "writes the tiers for each part" },
  { role: "injury", label: "Injuries", does: "writes the injury rows" },
  { role: "timeline", label: "Timeline", does: "times the class" },
];

const FOLLOW = ""; // option value: no saved choice for this role

export function ModelSettings({ keys, catalog }: { keys: ApiKey[]; catalog: ModelCatalog }) {
  const [saved, setSaved] = useState<Partial<Record<AgentRole, AgentPref>> | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<string | null>(null);

  const load = () => {
    setError(null);
    getAgentPrefs()
      .then((list) => {
        // The gateway lists its defaults as "default-<role>" when nothing is saved.
        const own = list.filter((p) => !p.id.startsWith("default-"));
        setSaved(Object.fromEntries(own.map((p) => [p.agentRole, p])));
      })
      .catch((e) => setError(msg(e)));
  };
  useEffect(load, []);

  const keyed = new Set(keys.map((k) => k.provider));
  const primary = keys.find((k) => k.isPrimary) ?? keys[0];
  const offered = catalog.models.filter((m) => keyed.has(m.provider));
  const label = (id: string) => catalog.models.find((m) => m.id === id)?.label ?? id;
  const providerLabel = (id: string) => catalog.providers.find((p) => p.id === id)?.label ?? id;
  const keyDefault = primary ? catalog.models.find((m) => m.provider === primary.provider && m.default) : undefined;

  async function choose(role: AgentRole, modelId: string) {
    setError(null);
    setStatus(null);
    const model = catalog.models.find((m) => m.id === modelId);
    try {
      const list = await saveAgentPrefs([{ agentRole: role, provider: model?.provider ?? "", modelIdentifier: modelId }]);
      setSaved(Object.fromEntries(list.filter((p) => !p.id.startsWith("default-")).map((p) => [p.agentRole, p])));
      setStatus("Saved. Applies to the next plan or change.");
    } catch (e) {
      setError(msg(e));
    }
  }

  if (!primary) return null;
  if (error && !saved) return <ErrorNote message={error} onRetry={load} />;
  if (!saved) return <p className="m-0 py-3 text-base text-sub">Loading models…</p>;

  const globalId = saved.global?.modelIdentifier ?? FOLLOW;
  const mainLabel = globalId ? label(globalId) : keyDefault?.label ?? "the default";
  const overrides = AGENT_ROLES.filter((r) => saved[r.role]).length;

  const options = (role: AgentRole, followText: string) => {
    const list = role === "parser" ? offered.filter((m) => m.vision) : offered;
    const current = saved[role]?.modelIdentifier;
    // A saved model whose key was removed stays visible, so the coach sees why it is not used.
    const stale = current && !list.some((m) => m.id === current) ? current : null;
    return (
      <>
        <option value={FOLLOW}>{followText}</option>
        {list.map((m) => (
          <option key={m.id} value={m.id}>
            {m.label}
          </option>
        ))}
        {stale && <option value={stale}>{label(stale)} (no key, not used)</option>}
      </>
    );
  };

  return (
    <div>
      <div className="rule-b py-2">
        <label htmlFor="model-global" className="head block pb-1.5 text-lg">
          Model for all agents
        </label>
        <select
          id="model-global"
          value={globalId}
          onChange={(e) => choose("global", e.target.value)}
          className="field h-12"
        >
          {options("global", `Default for ${providerLabel(primary.provider)} (${keyDefault?.label ?? "list default"})`)}
        </select>
      </div>
      {error && <ErrorNote message={error} />}
      {status && (
        <p className="m-0 mt-1 text-sm text-sub" role="status">
          {status}
        </p>
      )}

      <details className="group mt-3">
        <summary className="lnk tap flex cursor-pointer list-none items-center text-[15px]">
          Advanced{overrides ? ` (${overrides} agent${overrides > 1 ? "s" : ""} on another model)` : ""}
        </summary>
        <p className="m-0 mt-1 text-sm text-sub">A different model for one agent. The others use {mainLabel}.</p>
        {AGENT_ROLES.map(({ role, label: name, does }) => (
          <div key={role} className="rule-b py-2">
            <label htmlFor={`model-${role}`} className="block pb-1.5">
              <span className="head text-lg">{name}</span>
              <span className="text-sm text-sub"> {does}</span>
            </label>
            <select
              id={`model-${role}`}
              value={saved[role]?.modelIdentifier ?? FOLLOW}
              onChange={(e) => choose(role, e.target.value)}
              className="field h-12"
            >
              {options(role, `Same as all agents (${mainLabel})`)}
            </select>
          </div>
        ))}
        <Link href="/settings/runs" className="lnk tap mt-2 inline-flex items-center text-[15px] text-ink">
          Agent runs: what ran for each plan
        </Link>
      </details>
    </div>
  );
}
