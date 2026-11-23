"use client";

// Capture: screenshot, comment, class length, Build plan. One action per screen.

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { AppHeader, CameraIcon, ClassLength, ErrorNote, Screen } from "@/components/ui";
import { ApiError, createPlan, listKeys } from "@/lib/api";
import { toUploadDataUrl } from "@/lib/image";
import { useCoach } from "@/lib/useCoach";

function today(): string {
  const d = new Date();
  const day = d.toLocaleDateString("en-GB", { weekday: "short" });
  return `${day} ${String(d.getDate()).padStart(2, "0")}.${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export default function CapturePage() {
  const router = useRouter();
  const { coach, error: coachError } = useCoach();
  const fileRef = useRef<HTMLInputElement>(null);
  const [image, setImage] = useState<{ dataUrl: string; name: string } | null>(null);
  const [comment, setComment] = useState("");
  const [minutes, setMinutes] = useState<number | null>(null);
  const [hasKey, setHasKey] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!coach) return;
    setMinutes((m) => m ?? coach.defaultClassDurationMin ?? 60);
    listKeys()
      .then((keys) => setHasKey(keys.length > 0))
      .catch(() => setHasKey(null));
  }, [coach]);

  async function pick(file: File | undefined) {
    if (!file) return;
    setError(null);
    try {
      setImage({ dataUrl: await toUploadDataUrl(file), name: file.name });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function build() {
    if (!image || !minutes) return;
    setBusy(true);
    setError(null);
    try {
      const res = await createPlan({
        imageBase64: image.dataUrl,
        comment: comment.trim() || undefined,
        classMinutes: minutes,
      });
      router.push(`/plans/${res.planId}`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setBusy(false);
    }
  }

  const fileInput = (
    <input
      ref={fileRef}
      type="file"
      accept="image/*"
      className="hidden"
      onChange={(e) => {
        void pick(e.target.files?.[0]);
        e.target.value = "";
      }}
    />
  );

  return (
    <Screen>
      <AppHeader />
      <main className="flex flex-1 flex-col px-4 py-[22px]">
        <div className="flex items-baseline justify-between">
          <h1 className="head m-0 text-[40px] leading-[0.95]">New plan</h1>
          <span className="head text-lg font-semibold text-sub">{today()}</span>
        </div>
        {fileInput}

        {coachError && <ErrorNote message={coachError} onRetry={() => window.location.reload()} />}
        {hasKey === false && (
          <p className="mb-0 mt-4 border-l-2 border-ink pl-3 text-base">
            Add your AI key before the first plan.{" "}
            <Link href="/welcome" className="lnk">
              Add key
            </Link>
          </p>
        )}

        {!image ? (
          <>
            <button
              type="button"
              onClick={() => fileRef.current?.click()}
              className="mt-4 flex min-h-[260px] flex-col items-center justify-center gap-3 border-2 border-dashed border-ink bg-transparent px-4 text-ink"
            >
              <CameraIcon />
              <span className="head text-[32px] leading-none">
                Add WOD
                <br />
                screenshot
              </span>
              <span className="text-[15px] text-sub">From your gallery or the camera</span>
            </button>
            <p className="mb-0 mt-4 text-base text-sub">Then add a comment about today&apos;s athletes and the class length.</p>
          </>
        ) : (
          <>
            <section aria-label="WOD screenshot" className="rule-b mt-4 grid grid-cols-[88px_minmax(0,1fr)] gap-3.5 border-t-2 border-ink pb-4 pt-[18px]">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src={image.dataUrl} alt="Chosen WOD screenshot" className="h-[136px] w-[88px] border border-ink object-cover object-top" />
              <div className="flex flex-col gap-1">
                <span className="head text-xl">WOD screenshot</span>
                <span className="break-all text-[15px] text-sub">{image.name}</span>
                <div className="mt-auto flex gap-[18px]">
                  <button type="button" onClick={() => fileRef.current?.click()} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                    Replace
                  </button>
                  <button type="button" onClick={() => setImage(null)} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                    Remove
                  </button>
                </div>
              </div>
            </section>

            <div className="flex flex-col gap-1.5 pt-4">
              <label htmlFor="comment" className="head text-lg">
                Comment <span className="font-sans text-[15px] font-normal normal-case text-sub">optional</span>
              </label>
              <textarea
                id="comment"
                rows={4}
                maxLength={1000}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="Senior athlete today, ankle returning after 2 weeks; game-like warm-up"
                className="field resize-none leading-snug"
              />
              <span className="text-sm text-sub">Describe athletes and injuries, not names.</span>
            </div>

            {minutes !== null && <ClassLength value={minutes} onChange={setMinutes} />}

            <div className="min-h-6 flex-1" />
            {error && <ErrorNote message={error} />}
            <button type="button" onClick={build} disabled={busy || !minutes} className="btn-primary mt-4">
              {busy ? "Sending" : "Build plan"}
            </button>
            <p className="mb-0 mt-2.5 text-sm text-sub">Building needs a connection.</p>
          </>
        )}
        {!image && error && <ErrorNote message={error} />}
      </main>
    </Screen>
  );
}
