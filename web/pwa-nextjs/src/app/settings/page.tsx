"use client";

// Settings: profile, AI key, sign out. Model choice and agent runs ("Advanced")
// come in Phase 3 step 5.

import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { useBrand } from "@/components/Brand";
import { KeyForm } from "@/components/KeyForm";
import { BackHeader, ClassLength, ErrorNote, Screen } from "@/components/ui";
import { ApiError, deleteKey, listKeys, listModels, updateProfile } from "@/lib/api";
import type { ApiKey, Provider } from "@/lib/gateway-types";
import { clearToken } from "@/lib/session";
import { useCoach } from "@/lib/useCoach";

const msg = (e: unknown) => (e instanceof ApiError ? e.message : String(e));

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rule-b grid min-h-11 grid-cols-[96px_minmax(0,1fr)] items-center gap-2 py-2">
      <span className="head text-lg">{label}</span>
      <span className="text-base">{children}</span>
    </div>
  );
}

export default function SettingsPage() {
  const router = useRouter();
  const brand = useBrand();
  const { coach, setCoach, error: coachError } = useCoach();
  const [name, setName] = useState("");
  const [box, setBox] = useState("");
  const [minutes, setMinutes] = useState(60);
  const [saved, setSaved] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);

  const [keys, setKeys] = useState<ApiKey[] | null>(null);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [replacing, setReplacing] = useState(false);
  const [keyError, setKeyError] = useState<string | null>(null);

  useEffect(() => {
    if (!coach) return;
    setName(coach.displayName);
    setBox(coach.boxName);
    setMinutes(coach.defaultClassDurationMin || 60);
  }, [coach]);

  const loadKeys = () => {
    setKeyError(null);
    Promise.all([listKeys(), listModels()])
      .then(([k, c]) => {
        setKeys(k);
        setProviders(c.providers);
      })
      .catch((e) => setKeyError(msg(e)));
  };
  useEffect(() => {
    if (coach) loadKeys();
  }, [coach]);

  async function saveProfile(e: FormEvent) {
    e.preventDefault();
    setProfileError(null);
    setSaved(false);
    try {
      setCoach(await updateProfile({ displayName: name.trim(), boxName: box.trim(), defaultClassDurationMin: minutes }));
      setSaved(true);
    } catch (err) {
      setProfileError(msg(err));
    }
  }

  async function removeKey(id: string) {
    setKeyError(null);
    try {
      await deleteKey(id);
      loadKeys();
    } catch (err) {
      setKeyError(msg(err));
    }
  }

  const primary = keys?.find((k) => k.isPrimary) ?? keys?.[0];
  const providerLabel = (id: string) => providers.find((p) => p.id === id)?.label ?? id;

  return (
    <Screen>
      <BackHeader href="/" label="Back" title={brand.name} />
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <h1 className="head m-0 text-[40px] leading-[0.95]">Settings</h1>
        {coachError && <ErrorNote message={coachError} onRetry={() => window.location.reload()} />}

        <section aria-labelledby="p-h" className="pt-5">
          <h2 id="p-h" className="head rule-B m-0 pb-1.5 text-2xl">Profile</h2>
          <form onSubmit={saveProfile}>
            <div className="rule-b grid min-h-11 grid-cols-[96px_minmax(0,1fr)] items-center gap-2 py-2">
              <label htmlFor="name" className="head text-lg">Name</label>
              <input id="name" value={name} onChange={(e) => setName(e.target.value)} className="field py-2" />
            </div>
            {coach && <Row label="Email">{coach.email}</Row>}
            <div className="rule-b grid min-h-11 grid-cols-[96px_minmax(0,1fr)] items-center gap-2 py-2">
              <label htmlFor="box" className="head text-lg">Box</label>
              <input id="box" value={box} onChange={(e) => setBox(e.target.value)} className="field py-2" />
            </div>
            <ClassLength value={minutes} onChange={setMinutes} legend="Class length" />
            {profileError && <ErrorNote message={profileError} />}
            <div className="mt-3 flex items-center gap-4">
              <button type="submit" disabled={!coach} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                Save profile
              </button>
              {saved && <span className="text-[15px] text-sub" role="status">Saved</span>}
            </div>
          </form>
        </section>

        <section aria-labelledby="k-h" className="pt-6">
          <h2 id="k-h" className="head rule-B m-0 pb-1.5 text-2xl">AI key</h2>
          {keyError && <ErrorNote message={keyError} onRetry={loadKeys} />}
          {keys && primary && !replacing && (
            <>
              <Row label="Provider">{providerLabel(primary.provider)}</Row>
              <Row label="Key">{primary.keyHint}</Row>
              <div className="mt-2 flex gap-6">
                <button type="button" onClick={() => setReplacing(true)} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                  Replace key
                </button>
                <button type="button" onClick={() => removeKey(primary.id)} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                  Remove key
                </button>
              </div>
              <p className="m-0 mt-1 text-sm text-sub">Stored encrypted on the server. The full key is never shown again.</p>
            </>
          )}
          {keys && (!primary || replacing) && (
            <div className="pt-3">
              {!primary && <p className="mb-3 mt-0 text-base">No key yet. Plans are built with your own AI provider key.</p>}
              <KeyForm
                submitLabel="Save key"
                onSaved={() => {
                  setReplacing(false);
                  loadKeys();
                }}
              />
              {replacing && (
                <button type="button" onClick={() => setReplacing(false)} className="lnk tap mt-2 bg-transparent p-0 text-[15px] text-ink">
                  Cancel
                </button>
              )}
            </div>
          )}
        </section>

        <div className="min-h-8 flex-1" />
        <button
          type="button"
          onClick={() => {
            clearToken();
            router.replace("/login");
          }}
          className="head tap mt-6 h-12 w-full rounded-sm border border-ink bg-transparent text-lg text-ink"
        >
          Sign out
        </button>
      </main>
    </Screen>
  );
}
