"use client";

// Change, Retry and Add an injury: one sheet. The fixed chips (the same for
// every WOD and section) only add text to the instruction; the backend gets
// the instruction text alone. Only the chosen section is rebuilt.

import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, regenerateSection, type RefinableSection } from "@/lib/api";
import { ErrorNote } from "./ui";
import { OFFLINE_BUILD_NOTE, useOnline } from "@/lib/online";

export const CHIPS = [
  "Shorter",
  "Longer",
  "Simpler",
  "Less jumping",
  "No equipment",
  "More mobility",
  "Partner work",
  "Game-like",
  "Tabata style",
];

const MAX = 500;

/** Adds a chip's text to the instruction as its own sentence. */
export function addChip(text: string, chip: string): string {
  const t = text.trim();
  if (!t) return `${chip}.`;
  return `${/[.!?]$/.test(t) ? t : `${t}.`} ${chip}.`;
}

export type ChangeMode = "change" | "retry" | "add-injury";

const NAME: Record<RefinableSection, string> = {
  warmup: "warm-up",
  parts: "scaling",
  injuries: "injury notes",
  timeline: "timeline",
};

export function ChangeSheet({
  planId,
  revision,
  section,
  mode,
  onStarted,
  onClose,
}: {
  planId: string;
  revision: number;
  section: RefinableSection;
  mode: ChangeMode;
  onStarted: () => void;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [text, setText] = useState(mode === "retry" ? "Try again" : "");
  const [busy, setBusy] = useState(false);
  const online = useOnline();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    dialogRef.current?.showModal();
  }, []);

  const name = NAME[section];
  const title =
    mode === "add-injury" ? "Add an injury" : mode === "retry" ? `Retry the ${name}` : `Change the ${name}`;
  const action = mode === "add-injury" ? "Add injury" : mode === "retry" ? "Retry" : `Rebuild ${name}`;
  const placeholder =
    mode === "add-injury"
      ? "e.g. shoulder tear, avoid overhead work"
      : mode === "retry"
        ? "Say what to change, e.g. shorter, no equipment"
        : "Tap above or type, e.g. scaled-down engine moves, tabata style";

  async function submit(e: FormEvent) {
    e.preventDefault();
    const instruction = text.trim();
    if (!instruction) return;
    setBusy(true);
    setError(null);
    try {
      await regenerateSection(planId, section, instruction);
      dialogRef.current?.close();
      onStarted();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={dialogRef}
      onClose={onClose}
      aria-labelledby="change-h"
      className="m-0 mt-auto w-full max-w-[480px] border-t-2 border-ink bg-paper p-0 text-ink backdrop:bg-ink/40 sm:mx-auto"
    >
      <form onSubmit={submit} className="flex flex-col gap-3 px-4 pb-6 pt-4">
        <h2 id="change-h" className="head m-0 text-2xl">{title}</h2>
        <p className="m-0 text-[15px] text-sub">
          {mode === "add-injury"
            ? "The injury gets its own row in the warm-up and in every part. Nothing else is rebuilt."
            : `Only the ${name} is rebuilt. v${revision} stays in Versions.`}
          {mode === "retry" && " Add what to change, or retry as is."}
        </p>
        {mode !== "add-injury" && (
          <div role="group" aria-label="Quick additions, the same for every WOD" className="flex flex-wrap gap-2">
            {CHIPS.map((c) => (
              <button
                key={c}
                type="button"
                onClick={() => setText((t) => addChip(t, c).slice(0, MAX))}
                className="tap rounded-full border border-ink bg-transparent px-3.5 text-[15px] font-medium text-ink"
              >
                {c}
              </button>
            ))}
          </div>
        )}
        <div className="flex flex-col gap-1.5">
          <label htmlFor="instruction" className="head text-lg">
            {mode === "add-injury" ? "The injury" : "What should change"}
          </label>
          <textarea
            id="instruction"
            rows={3}
            maxLength={MAX}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={placeholder}
            className="field resize-none leading-snug"
          />
          {mode === "add-injury" && <span className="text-sm text-sub">Describe the injury, not the athlete.</span>}
        </div>
        {error && <ErrorNote message={error} />}
        {!online && <p className="m-0 border-l-2 border-ink pl-3 text-base">{OFFLINE_BUILD_NOTE}</p>}
        <div className="mt-1 flex items-center gap-6">
          <button type="submit" disabled={busy || !text.trim() || !online} className="btn-primary flex-1">
            {busy ? "Sending" : action}
          </button>
          <button type="button" onClick={() => dialogRef.current?.close()} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
            Cancel
          </button>
        </div>
        {section === "warmup" && mode !== "add-injury" && (
          <p className="m-0 text-sm text-sub">If the length changes, the timeline is updated too.</p>
        )}
      </form>
    </dialog>
  );
}
