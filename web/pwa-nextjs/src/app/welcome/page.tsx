"use client";

import { useRouter } from "next/navigation";
import { KeyForm } from "@/components/KeyForm";
import { AppHeader, Screen } from "@/components/ui";
import { useCoach } from "@/lib/useCoach";

export default function WelcomePage() {
  const router = useRouter();
  const { coach } = useCoach();

  return (
    <Screen>
      <AppHeader history={false} settings={false} />
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <h1 className="head m-0 text-[40px] leading-[0.95]">Welcome, {coach?.displayName || "coach"}</h1>
        <p className="mb-0 mt-2 text-base text-sub">One step before your first plan.</p>
        <ol className="m-0 mt-4 list-none border-t-2 border-ink p-0">
          <li className="rule-b grid grid-cols-[28px_1fr_auto] items-baseline py-3">
            <span className="head text-xl">1</span>
            <span className="text-base">Account created</span>
            <span className="head text-base text-sub">Done</span>
          </li>
          <li className="rule-b grid grid-cols-[28px_1fr] items-baseline py-3">
            <span className="head text-xl">2</span>
            <div>
              <span className="head block text-xl">Add your AI key</span>
              <span className="text-[15px] text-sub">
                Plans are built with your own AI provider key. It is stored encrypted and only ever shown masked.
              </span>
            </div>
          </li>
        </ol>
        <div className="pt-4">
          <KeyForm submitLabel="Save key and continue" onSaved={() => router.replace("/")} />
        </div>
      </main>
    </Screen>
  );
}
