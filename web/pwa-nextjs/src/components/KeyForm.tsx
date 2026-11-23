"use client";

// Provider and API key entry, used by Welcome and Settings. The key goes
// straight to the gateway vault; only the masked hint ever comes back.

import { useEffect, useState, type FormEvent } from "react";
import { ApiError, listModels, saveKey } from "@/lib/api";
import type { ApiKey, Provider } from "@/lib/gateway-types";
import { ErrorNote } from "./ui";

const KEY_HELP: Record<string, string> = {
  gemini: "https://aistudio.google.com/apikey",
  openai: "https://platform.openai.com/api-keys",
  anthropic: "https://console.anthropic.com/settings/keys",
};

export function KeyForm({ submitLabel, onSaved }: { submitLabel: string; onSaved: (key: ApiKey) => void }) {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [provider, setProvider] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadProviders = () => {
    setError(null);
    listModels()
      .then((c) => {
        setProviders(c.providers);
        setProvider((p) => p || c.providers[0]?.id || "");
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };
  useEffect(loadProviders, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const key = await saveKey(provider, apiKey.trim());
      setApiKey("");
      onSaved(key);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <label htmlFor="provider" className="head text-lg">Provider</label>
        <select id="provider" value={provider} onChange={(e) => setProvider(e.target.value)} className="field h-12" required>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
        </select>
      </div>
      <div className="flex flex-col gap-1.5">
        <label htmlFor="key" className="head text-lg">API key</label>
        <input
          id="key"
          type="password"
          autoComplete="off"
          spellCheck={false}
          placeholder="Paste your key"
          required
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          className="field"
        />
        {KEY_HELP[provider] && (
          <a href={KEY_HELP[provider]} target="_blank" rel="noreferrer" className="lnk tap flex items-center self-start text-[15px]">
            Where do I get a key?
          </a>
        )}
      </div>
      {error && <ErrorNote message={error} onRetry={providers.length ? undefined : loadProviders} />}
      <button type="submit" disabled={busy || !provider} className="btn-primary">
        {busy ? "Saving" : submitLabel}
      </button>
      <p className="m-0 text-sm text-sub">Stored encrypted on the server. The full key is never shown again.</p>
    </form>
  );
}
