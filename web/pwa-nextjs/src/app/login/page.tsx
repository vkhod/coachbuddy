"use client";

// Sign in, or create an account. Not in the Phase 1 mockups (Welcome assumes an
// accepted invite); invite-only sign-up replaces "Create account" in Phase 5.

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState, type FormEvent } from "react";
import { AppHeader, ErrorNote, Screen } from "@/components/ui";
import { ApiError, login, register } from "@/lib/api";
import { setToken } from "@/lib/session";

function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
}

function LoginForm() {
  const router = useRouter();
  const params = useSearchParams();
  const [mode, setMode] = useState<"signin" | "create">("signin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [box, setBox] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (mode === "signin") {
        const res = await login(email.trim(), password);
        setToken(res.token);
        router.replace(safeNext(params.get("next")));
      } else {
        const res = await register({
          email: email.trim(),
          password,
          displayName: name.trim() || "Coach",
          boxName: box.trim(),
          defaultClassDurationMin: 60,
        });
        setToken(res.token);
        router.replace("/welcome");
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setBusy(false);
    }
  }

  const creating = mode === "create";
  return (
    <main className="flex flex-1 flex-col px-4 py-[22px]">
      <h1 className="head m-0 text-[40px] leading-[0.95]">{creating ? "Create account" : "Sign in"}</h1>
      <form onSubmit={submit} className="mt-4 flex flex-col gap-4 border-t-2 border-ink pt-4">
        <div className="flex flex-col gap-1.5">
          <label htmlFor="email" className="head text-lg">Email</label>
          <input id="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} className="field" />
        </div>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="password" className="head text-lg">Password</label>
          <input
            id="password"
            type="password"
            autoComplete={creating ? "new-password" : "current-password"}
            required
            minLength={creating ? 8 : undefined}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="field"
          />
        </div>
        {creating && (
          <>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="name" className="head text-lg">Your name</label>
              <input id="name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} className="field" />
            </div>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="box" className="head text-lg">Box</label>
              <input id="box" autoComplete="organization" value={box} onChange={(e) => setBox(e.target.value)} className="field" />
            </div>
          </>
        )}
        {error && <ErrorNote message={error} />}
        <button type="submit" disabled={busy} className="btn-primary mt-2">
          {busy ? "One moment" : creating ? "Create account" : "Sign in"}
        </button>
      </form>
      <button
        type="button"
        onClick={() => {
          setMode(creating ? "signin" : "create");
          setError(null);
        }}
        className="lnk tap mt-4 self-start bg-transparent p-0 text-[15px] text-ink"
      >
        {creating ? "I have an account: sign in" : "New coach? Create an account"}
      </button>
    </main>
  );
}

export default function LoginPage() {
  return (
    <Screen>
      <AppHeader history={false} settings={false} />
      <Suspense>
        <LoginForm />
      </Suspense>
    </Screen>
  );
}
