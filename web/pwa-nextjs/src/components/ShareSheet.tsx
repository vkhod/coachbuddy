"use client";

// Share: QR code and link from the gateway, with the Athlete / Coach toggle.
// New links default to the athlete view (decided in #21). The QR SVG is drawn
// by mcp-rust and shown as an image, so no markup from it reaches the page.

import { useEffect, useRef, useState } from "react";
import { ApiError, createShare } from "@/lib/api";
import type { ShareLink } from "@/lib/gateway-types";
import { ErrorNote } from "./ui";

type View = "athlete" | "coach";

export function ShareSheet({ planId, onClose }: { planId: string; onClose: () => void }) {
  const [view, setView] = useState<View>("athlete");
  const [links, setLinks] = useState<Partial<Record<View, ShareLink>>>({});
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    dialogRef.current?.showModal();
  }, []);

  const link = links[view];

  const make = (v: View) => {
    setError(null);
    createShare(planId, v === "coach")
      .then((l) => setLinks((m) => ({ ...m, [v]: l })))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  };

  useEffect(() => {
    if (!links[view]) make(view);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view]);

  async function copy() {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link.shareUrl);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  }

  return (
    <dialog
      ref={dialogRef}
      onClose={onClose}
      aria-labelledby="share-h"
      className="m-0 mt-auto w-full max-w-[480px] border-t-2 border-ink bg-paper p-0 text-ink backdrop:bg-ink/40 sm:mx-auto"
    >
      <div className="flex flex-col gap-3 px-4 pb-6 pt-4">
        <div className="flex items-baseline justify-between">
          <h2 id="share-h" className="head m-0 text-2xl">Share</h2>
          <button type="button" onClick={() => dialogRef.current?.close()} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
            Close
          </button>
        </div>
        <div role="radiogroup" aria-label="Who the link is for" className="grid grid-cols-2 border border-ink">
          {(["athlete", "coach"] as View[]).map((v, i) => (
            <button
              key={v}
              type="button"
              role="radio"
              aria-checked={view === v}
              onClick={() => setView(v)}
              className={`head h-[46px] text-lg ${i > 0 ? "border-l border-ink" : ""} ${view === v ? "bg-ink text-paper" : "bg-transparent text-ink"}`}
            >
              {v === "athlete" ? "Athlete" : "Coach"}
            </button>
          ))}
        </div>
        <p className="m-0 text-[15px] text-sub">
          {view === "athlete"
            ? "WOD, timeline and tiers only. No comment, injuries, warm-up or notes."
            : "Adds the warm-up, today's athletes, injuries and cues. Never your comment or the screenshot."}
        </p>
        {error && <ErrorNote message={error} onRetry={() => make(view)} />}
        {!link && !error && <p className="head m-0 py-6 text-lg text-sub">Making the link…</p>}
        {link && (
          <>
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(link.qrSvg)}`}
              alt={`QR code for ${link.shareUrl}`}
              className="mx-auto aspect-square w-full max-w-[260px] bg-white p-2"
            />
            <p className="m-0 break-all text-center text-[15px]">{link.shareUrl}</p>
            <div className="flex gap-6">
              <button type="button" onClick={copy} className="lnk tap bg-transparent p-0 text-[15px] text-ink">
                {copied ? "Copied" : "Copy link"}
              </button>
              <a href={link.shareUrl} target="_blank" rel="noreferrer" className="lnk tap flex items-center text-[15px]">
                Open
              </a>
            </div>
          </>
        )}
      </div>
    </dialog>
  );
}
