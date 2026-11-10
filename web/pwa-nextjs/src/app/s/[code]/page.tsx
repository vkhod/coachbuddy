"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  Dumbbell,
  Clock,
  Flame,
  ShieldAlert,
  Sparkles,
  CheckCircle2,
  ArrowLeft,
  Users,
  Activity,
  HeartPulse,
  Wrench,
  Calendar,
} from "lucide-react";
import {
  resolveSharedSession,
  SharedSessionPayload,
  TimelineBlock,
} from "@/lib/api";

function formatMinRange(startMin: number, endMin: number): string {
  const fmt = (m: number) => {
    const hrs = Math.floor(m / 60);
    const mins = m % 60;
    return `${String(hrs).padStart(2, "0")}:${String(mins).padStart(2, "0")}`;
  };
  return `${fmt(startMin)}-${fmt(endMin)}`;
}

function blockBadgeStyle(type: TimelineBlock["type"]): string {
  switch (type) {
    case "brief":
      return "bg-sky-500/15 text-sky-300 border-sky-500/30";
    case "warmup":
      return "bg-emerald-500/15 text-emerald-300 border-emerald-500/30";
    case "strength":
      return "bg-[#CCFF00]/15 text-[#CCFF00] border-[#CCFF00]/40";
    case "metcon":
      return "bg-[#F97316]/15 text-[#F97316] border-[#F97316]/40";
    case "transition":
      return "bg-amber-500/15 text-amber-300 border-amber-500/30";
    default:
      return "bg-purple-500/15 text-purple-300 border-purple-500/30";
  }
}

export default function SharedSessionPage() {
  const params = useParams<{ code: string }>();
  const code = params?.code || "cb-demo";
  const [payload, setPayload] = useState<SharedSessionPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [selectedTier, setSelectedTier] = useState<
    "all" | "rxPlus" | "scaled" | "seniorsMasters" | "injured"
  >("all");

  useEffect(() => {
    let active = true;
    setLoading(true);
    resolveSharedSession(code)
      .then((res) => {
        if (active) {
          setPayload(res);
          setLoading(false);
        }
      })
      .catch(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [code]);

  if (loading) {
    return (
      <div className="min-h-screen bg-[#0B0F17] text-slate-100 flex flex-col items-center justify-center p-6">
        <div className="w-12 h-12 rounded-2xl border-2 border-[#CCFF00] border-t-transparent animate-spin mb-4" />
        <p className="text-sm font-mono uppercase tracking-widest text-[#CCFF00]">
          Loading Shared Box Session ({code})...
        </p>
      </div>
    );
  }

  if (!payload) {
    return (
      <div className="min-h-screen bg-[#0B0F17] text-slate-100 flex flex-col items-center justify-center p-6 text-center">
        <ShieldAlert className="w-12 h-12 text-[#F97316] mb-3" />
        <h1 className="text-2xl font-bold mb-2">Shared Session Not Found</h1>
        <p className="text-slate-400 mb-6 max-w-md">
          The short link <span className="font-mono text-slate-200">{code}</span>{" "}
          may have expired or been removed.
        </p>
        <Link
          href="/"
          className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl bg-[#CCFF00] text-[#0B0F17] font-bold hover:bg-[#d8ff33] transition"
        >
          <ArrowLeft className="w-4 h-4" />
          Open CoachBuddy Planner
        </Link>
      </div>
    );
  }

  const { share, session } = payload;
  const includeCoachNotes = share.includeCoachNotes;

  return (
    <div className="min-h-screen bg-[#0B0F17] text-slate-100 pb-16">
      {/* Top Banner */}
      <header className="sticky top-0 z-30 border-b border-slate-800/80 bg-[#0B0F17]/95 backdrop-blur-md">
        <div className="max-w-5xl mx-auto px-4 py-3.5 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-[#CCFF00]/15 border border-[#CCFF00]/40 flex items-center justify-center">
              <Dumbbell className="w-5 h-5 text-[#CCFF00]" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-black tracking-tight text-lg text-white">
                  COACH<span className="text-[#CCFF00]">BUDDY</span>
                </span>
                <span
                  className={`text-[11px] font-mono uppercase px-2 py-0.5 rounded-full border ${
                    includeCoachNotes
                      ? "bg-[#CCFF00]/15 text-[#CCFF00] border-[#CCFF00]/40"
                      : "bg-emerald-500/15 text-emerald-300 border-emerald-500/30"
                  }`}
                >
                  {includeCoachNotes
                    ? "Co-Coach View (Full Cues & Notes)"
                    : "Athlete Whiteboard View"}
                </span>
              </div>
              <p className="text-xs text-slate-400 font-mono">
                Share Code: <span className="text-slate-200">{share.shortCode}</span>
              </p>
            </div>
          </div>

          <Link
            href="/"
            className="inline-flex items-center gap-2 px-3.5 py-2 rounded-xl bg-slate-900 hover:bg-slate-800 border border-slate-700 text-xs font-semibold text-slate-200 transition"
          >
            <ArrowLeft className="w-3.5 h-3.5 text-[#CCFF00]" />
            Open Full Coach App
          </Link>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 pt-6 space-y-6">
        {/* Session Hero Card */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 md:p-6 shadow-xl">
          <div className="flex flex-wrap items-start justify-between gap-4 mb-4">
            <div>
              <div className="flex flex-wrap items-center gap-2 mb-2">
                <span className="px-2.5 py-0.5 rounded-md bg-[#F97316]/20 border border-[#F97316]/40 text-[#F97316] text-xs font-mono font-bold uppercase">
                  WOD SESSION BRIEF
                </span>
                <span className="px-2.5 py-0.5 rounded-md bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 text-xs font-mono">
                  <Clock className="w-3.5 h-3.5 inline mr-1" />
                  {session.timeline?.totalMinutes || 60} MIN CLASS
                </span>
                <span className="px-2.5 py-0.5 rounded-md bg-slate-800 text-slate-300 text-xs font-mono">
                  <Calendar className="w-3.5 h-3.5 inline mr-1" />
                  {new Date(session.createdAt).toLocaleDateString()}
                </span>
              </div>
              <h1 className="text-2xl md:text-3xl font-black tracking-tight text-white">
                {session.title}
              </h1>
            </div>
          </div>

          <p className="text-sm text-slate-300 bg-slate-950/70 border border-slate-800/90 rounded-xl p-3.5 mb-4">
            <strong className="text-[#CCFF00] font-mono uppercase text-xs mr-2">
              Intended Stimulus:
            </strong>
            {session.parsedWod?.stimulus}
          </p>

          {session.input?.rawText && (
            <div className="rounded-xl bg-[#0B0F17] border border-slate-800 p-4 font-mono text-xs md:text-sm text-slate-200 whitespace-pre-wrap leading-relaxed">
              {session.input.rawText}
            </div>
          )}
        </section>

        {/* Minute-by-Minute Class Timeline */}
        {session.timeline && (
          <section className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 md:p-6">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-bold flex items-center gap-2">
                <Clock className="w-5 h-5 text-[#CCFF00]" />
                Class Timeline ({session.timeline.totalMinutes} Min)
              </h2>
              <span className="px-2.5 py-1 rounded-full text-xs font-mono font-bold bg-emerald-500/15 text-emerald-300 border border-emerald-500/30">
                {session.timeline.feasibilityStatus}
              </span>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
              {session.timeline.blocks.map((b, idx) => (
                <div
                  key={idx}
                  className={`rounded-xl border p-3.5 ${blockBadgeStyle(b.type)}`}
                >
                  <div className="text-xs font-mono font-bold opacity-90 mb-1">
                    {formatMinRange(b.startMin, b.endMin)}
                  </div>
                  <div className="font-bold text-sm text-white mb-1">
                    {b.label}
                  </div>
                  {includeCoachNotes && b.notes && (
                    <p className="text-xs text-slate-300/90 leading-snug">
                      {b.notes}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Warm-up Flow */}
        {session.warmup && (
          <section className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 md:p-6">
            <div className="flex flex-wrap items-center justify-between gap-2 mb-4">
              <h2 className="text-lg font-bold flex items-center gap-2">
                <Flame className="w-5 h-5 text-emerald-400" />
                Warm-Up & Mobility Flow ({session.warmup.durationMinutes} Min)
              </h2>
              <span className="text-xs font-mono px-2.5 py-1 rounded-lg bg-emerald-500/15 text-emerald-300 border border-emerald-500/30">
                {session.warmup.style}
              </span>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {session.warmup.phases.map((phase, idx) => (
                <div
                  key={idx}
                  className="rounded-xl border border-slate-800 bg-[#0B0F17]/90 p-4 flex flex-col justify-between"
                >
                  <div>
                    <div className="flex items-center justify-between gap-2 mb-2">
                      <span className="font-bold text-sm text-[#CCFF00]">
                        {phase.name}
                      </span>
                      <span className="text-xs font-mono px-2 py-0.5 rounded bg-slate-800 text-slate-300">
                        {phase.durationMinutes} min
                      </span>
                    </div>
                    <p className="text-xs md:text-sm text-slate-300 leading-relaxed mb-3">
                      {phase.instructions}
                    </p>

                    {phase.drills && phase.drills.length > 0 && (
                      <div className="space-y-2 mb-3">
                        {phase.drills.map((d, dIdx) => (
                          <div
                            key={dIdx}
                            className="rounded-lg bg-slate-900/90 border border-slate-800/80 p-2.5 text-xs"
                          >
                            <div className="flex items-center justify-between font-semibold text-white">
                              <span>{d.name}</span>
                              <span className="font-mono text-[#CCFF00]">
                                {d.reps}
                              </span>
                            </div>
                            <div className="text-slate-400 mt-0.5">
                              Focus: {d.focus}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="pt-2 border-t border-slate-800/80 text-xs text-emerald-300 flex items-center gap-1.5">
                    <Sparkles className="w-3.5 h-3.5 shrink-0" />
                    <span>Goal: {phase.coachingGoal}</span>
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 4-Tier Scaling Matrix & Coaching Cues */}
        <section className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 md:p-6">
          <div className="flex flex-wrap items-center justify-between gap-3 mb-5">
            <div>
              <h2 className="text-lg font-bold flex items-center gap-2">
                <Users className="w-5 h-5 text-[#F97316]" />
                4-Tier Movement Scaling{" "}
                {includeCoachNotes && "& Tactical Coaching Cues"}
              </h2>
              <p className="text-xs text-slate-400">
                Select your scaling division below to highlight your personalized
                modification.
              </p>
            </div>

            <div className="flex flex-wrap gap-1.5">
              {[
                { id: "all", label: "All Tiers" },
                { id: "rxPlus", label: "Rx+ (Firebreather)" },
                { id: "scaled", label: "Scaled" },
                { id: "seniorsMasters", label: "Masters / Seniors" },
                { id: "injured", label: "Injury Subs" },
              ].map((tab) => (
                <button
                  key={tab.id}
                  onClick={() =>
                    setSelectedTier(
                      tab.id as
                        | "all"
                        | "rxPlus"
                        | "scaled"
                        | "seniorsMasters"
                        | "injured"
                    )
                  }
                  className={`px-3 py-1.5 rounded-lg text-xs font-bold transition ${
                    selectedTier === tab.id
                      ? "bg-[#CCFF00] text-[#0B0F17]"
                      : "bg-slate-800/90 text-slate-300 hover:bg-slate-700"
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
          </div>

          <div className="space-y-5">
            {session.exercises.map((ex, idx) => (
              <div
                key={idx}
                className="rounded-xl border border-slate-800 bg-[#0B0F17] p-4 md:p-5 space-y-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800 pb-3">
                  <h3 className="text-base md:text-lg font-extrabold text-white">
                    {ex.name}
                  </h3>
                  <span className="text-xs font-mono px-2.5 py-0.5 rounded-md bg-slate-800 text-slate-300">
                    {ex.category}
                  </span>
                </div>

                {/* 4-Tier Scaling Cards */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {(selectedTier === "all" || selectedTier === "rxPlus") && (
                    <div className="rounded-xl border border-[#F97316]/40 bg-[#F97316]/10 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-[#F97316] mb-1">
                        Rx+ (Firebreather)
                      </div>
                      <p className="text-xs md:text-sm text-slate-100">
                        {ex.scaling.rxPlus}
                      </p>
                    </div>
                  )}

                  {(selectedTier === "all" || selectedTier === "scaled") && (
                    <div className="rounded-xl border border-sky-500/40 bg-sky-500/10 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-sky-300 mb-1">
                        Scaled (Intermediate)
                      </div>
                      <p className="text-xs md:text-sm text-slate-100">
                        {ex.scaling.scaled}
                      </p>
                    </div>
                  )}

                  {(selectedTier === "all" ||
                    selectedTier === "seniorsMasters") && (
                    <div className="rounded-xl border border-emerald-500/40 bg-emerald-500/10 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-emerald-300 mb-1">
                        Masters / Seniors (Joint-Friendly)
                      </div>
                      <p className="text-xs md:text-sm text-slate-100">
                        {ex.scaling.seniorsMasters}
                      </p>
                    </div>
                  )}

                  {(selectedTier === "all" || selectedTier === "injured") && (
                    <div className="rounded-xl border border-amber-500/40 bg-amber-500/10 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-amber-300 mb-1.5 flex items-center gap-1">
                        <HeartPulse className="w-3.5 h-3.5" />
                        Injured Alternatives (Zero-Pain Subs)
                      </div>
                      <ul className="text-xs text-slate-200 space-y-1">
                        <li>
                          <strong className="text-amber-300">
                            Wrist/Shoulder:
                          </strong>{" "}
                          {ex.scaling.injured.wristOrShoulder}
                        </li>
                        <li>
                          <strong className="text-amber-300">Knee Pain:</strong>{" "}
                          {ex.scaling.injured.kneePain}
                        </li>
                        <li>
                          <strong className="text-amber-300">Lower Back:</strong>{" "}
                          {ex.scaling.injured.lowerBack}
                        </li>
                      </ul>
                    </div>
                  )}
                </div>

                {/* Co-Coach Cues (if enabled on the shared link) */}
                {includeCoachNotes && ex.cues && (
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-2 border-t border-slate-800/80">
                    <div className="rounded-lg bg-slate-900/90 p-3 border border-slate-800">
                      <div className="text-xs font-mono font-bold text-[#CCFF00] uppercase mb-1.5 flex items-center gap-1">
                        <Activity className="w-3.5 h-3.5" />
                        External Cues
                      </div>
                      <ul className="text-xs text-slate-300 space-y-1.5">
                        {ex.cues.external.map((c, i) => (
                          <li key={i} className="flex items-start gap-1.5">
                            <CheckCircle2 className="w-3.5 h-3.5 text-[#CCFF00] shrink-0 mt-0.5" />
                            <span>{c}</span>
                          </li>
                        ))}
                      </ul>
                    </div>

                    <div className="rounded-lg bg-slate-900/90 p-3 border border-slate-800">
                      <div className="text-xs font-mono font-bold text-emerald-300 uppercase mb-1.5">
                        Internal Cues
                      </div>
                      <ul className="text-xs text-slate-300 space-y-1.5">
                        {ex.cues.internal.map((c, i) => (
                          <li key={i} className="flex items-start gap-1.5">
                            <span className="text-emerald-400 font-bold">•</span>
                            <span>{c}</span>
                          </li>
                        ))}
                      </ul>
                    </div>

                    <div className="rounded-lg bg-slate-900/90 p-3 border border-slate-800">
                      <div className="text-xs font-mono font-bold text-[#F97316] uppercase mb-1.5 flex items-center gap-1">
                        <Wrench className="w-3.5 h-3.5" />
                        Fault → Rapid Fix
                      </div>
                      <ul className="text-xs text-slate-300 space-y-2">
                        {ex.cues.commonFaults.map((ff, i) => (
                          <li key={i}>
                            <div className="text-rose-300 font-medium">
                              ✗ {ff.fault}
                            </div>
                            <div className="text-emerald-300 mt-0.5">
                              ✓ {ff.fix}
                            </div>
                          </li>
                        ))}
                      </ul>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      </main>
    </div>
  );
}
