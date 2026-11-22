"use client";

import React from "react";
import {
  Clock,
  Flame,
  Users,
  Share2,
  CheckCircle2,
  AlertTriangle,
  Sparkles,
  HeartPulse,
  Wrench,
  Activity,
  RefreshCw,
} from "lucide-react";
import { TimelineBlock, WodSession } from "@/lib/api";

export function formatMinRange(startMin: number, endMin: number): string {
  const fmt = (m: number) => {
    const hrs = Math.floor(m / 60);
    const mins = m % 60;
    return `${String(hrs).padStart(2, "0")}:${String(mins).padStart(2, "0")}`;
  };
  return `${fmt(startMin)}-${fmt(endMin)}`;
}

export function blockTypeColor(type: TimelineBlock["type"]): {
  bg: string;
  border: string;
  text: string;
  bar: string;
} {
  switch (type) {
    case "brief":
      return {
        bg: "bg-sky-500/15",
        border: "border-sky-500/40",
        text: "text-sky-300",
        bar: "bg-sky-400",
      };
    case "warmup":
      return {
        bg: "bg-emerald-500/15",
        border: "border-emerald-500/40",
        text: "text-emerald-300",
        bar: "bg-emerald-400",
      };
    case "strength":
      return {
        bg: "bg-[#CCFF00]/15",
        border: "border-[#CCFF00]/45",
        text: "text-[#CCFF00]",
        bar: "bg-[#CCFF00]",
      };
    case "metcon":
      return {
        bg: "bg-[#F97316]/15",
        border: "border-[#F97316]/45",
        text: "text-[#F97316]",
        bar: "bg-[#F97316]",
      };
    case "transition":
      return {
        bg: "bg-amber-500/15",
        border: "border-amber-500/40",
        text: "text-amber-300",
        bar: "bg-amber-400",
      };
    default:
      return {
        bg: "bg-purple-500/15",
        border: "border-purple-500/40",
        text: "text-purple-300",
        bar: "bg-purple-400",
      };
  }
}

export interface ActiveSessionTabProps {
  activeSession: WodSession;
  onUpdateSession: (updated: WodSession) => void;
  exerciseSubTab: Record<number, "both" | "cues" | "scaling">;
  setExerciseSubTab: React.Dispatch<
    React.SetStateAction<Record<number, "both" | "cues" | "scaling">>
  >;
  scalingFilter: "all" | "rxPlus" | "scaled" | "seniorsMasters" | "injured";
  setScalingFilter: React.Dispatch<
    React.SetStateAction<
      "all" | "rxPlus" | "scaled" | "seniorsMasters" | "injured"
    >
  >;
  onOpenShare: (includeNotes: boolean) => void;
}

export default function ActiveSessionTab({
  activeSession,
  onUpdateSession,
  exerciseSubTab,
  setExerciseSubTab,
  scalingFilter,
  setScalingFilter,
  onOpenShare,
}: ActiveSessionTabProps) {
  /**
   * Rotates `activeSession.warmup.phases[0]` (the Phase 1 Reaction Game) with the first
   * entry in `activeSession.warmup.alternativeGames`, placing the old Phase 1 game at the
   * end of `alternativeGames` so the coach can cycle through warm-up games infinitely.
   */
  const handleSwapWarmupGame = () => {
    const phases = activeSession.warmup?.phases || [];
    const altGames = activeSession.warmup?.alternativeGames || [];
    if (phases.length === 0 || altGames.length === 0) {
      return;
    }


    // 1. Grab the current game and the next alternative game:
    const currentGame = phases[0];
    const nextGame = altGames[0];
    // 2. Build the new `phases` array replacing `phases[0]` with `nextGame`:
    const updatedPhases = [nextGame, ...phases.slice(1)];
    // 3. Build the rotated `alternativeGames` queue (placing `currentGame` at the end):
    const updatedAlternatives = [...altGames.slice(1), currentGame];
    // 4. Call `onUpdateSession` with the updated `activeSession`:
    onUpdateSession({ ...activeSession, warmup: { ...activeSession.warmup, gameTitle: nextGame.name, phases: updatedPhases, alternativeGames: updatedAlternatives } });
    void onUpdateSession;
  };

  return (
    <div className="space-y-6">
      {/* Session Top Action Bar */}
      <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5 flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2 mb-1.5">
            <span className="px-2.5 py-0.5 rounded-md bg-[#CCFF00]/15 text-[#CCFF00] border border-[#CCFF00]/40 text-xs font-mono font-bold">
              {activeSession.timeline.totalMinutes} MIN CLASS PLAN
            </span>
            <span
              className={`px-2.5 py-0.5 rounded-md text-xs font-mono font-bold border ${activeSession.timeline.feasibilityStatus === "OPTIMAL"
                  ? "bg-emerald-500/15 text-emerald-300 border-emerald-500/40"
                  : "bg-amber-500/15 text-amber-300 border-amber-500/40"
                }`}
            >
              FEASIBILITY: {activeSession.timeline.feasibilityStatus}
            </span>
            <span className="text-xs font-mono text-slate-400">
              ID: {activeSession.sessionId}
            </span>
          </div>
          <h1 className="text-xl md:text-2xl font-black text-white">
            {activeSession.title}
          </h1>
          <p className="text-xs md:text-sm text-slate-300 mt-1">
            <strong className="text-[#CCFF00] font-mono uppercase text-xs mr-1.5">
              Stimulus:
            </strong>
            {activeSession.parsedWod?.stimulus}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          {/* Share Session Button */}
          <button
            type="button"
            onClick={() => onOpenShare(true)}
            className="inline-flex items-center gap-2 px-4 py-2.5 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-black text-xs uppercase tracking-wider shadow-lg shadow-[#CCFF00]/20 transition"
          >
            <Share2 className="w-4 h-4" />
            <span>Share WOD / QR Code</span>
          </button>
        </div>
      </div>

      {/* Minute-by-Minute Class Timeline Bar */}
      <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-bold text-white flex items-center gap-2">
            <Clock className="w-5 h-5 text-[#CCFF00]" />
            Minute-by-Minute Class Timeline (
            {activeSession.timeline.totalMinutes} Min Total)
          </h2>
        </div>

        {/* Proportional Visual Bar */}
        <div className="w-full h-4 rounded-full bg-[#0B0F17] overflow-hidden flex border border-slate-800 p-0.5 gap-0.5">
          {activeSession.timeline.blocks.map((blk, idx) => {
            const span = Math.max(1, blk.endMin - blk.startMin);
            const widthPct =
              (span / (activeSession.timeline.totalMinutes || 60)) * 100;
            const colors = blockTypeColor(blk.type);
            return (
              <div
                key={idx}
                style={{ width: `${widthPct}%` }}
                title={`${formatMinRange(blk.startMin, blk.endMin)} ${blk.label}`}
                className={`h-full first:rounded-l-full last:rounded-r-full ${colors.bar}`}
              />
            );
          })}
        </div>

        {/* Timeline Block Cards */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7 gap-2.5">
          {activeSession.timeline.blocks.map((blk, idx) => {
            const colors = blockTypeColor(blk.type);
            return (
              <div
                key={idx}
                className={`text-left rounded-xl border p-3 flex flex-col justify-between ${colors.bg} ${colors.border}`}
              >
                <div>
                  <div className="flex items-center justify-between gap-1 mb-1">
                    <span
                      className={`text-[11px] font-mono font-extrabold ${colors.text}`}
                    >
                      {formatMinRange(blk.startMin, blk.endMin)}
                    </span>
                    <span className="text-[10px] font-mono text-slate-400">
                      {blk.endMin - blk.startMin}m
                    </span>
                  </div>
                  <div className="font-bold text-xs text-white leading-snug mb-1.5">
                    {blk.label}
                  </div>
                </div>
                <p className="text-[11px] text-slate-300/90 line-clamp-3 leading-snug">
                  {blk.notes}
                </p>
              </div>
            );
          })}
        </div>

        {/* Coach Logistics Warnings */}
        {activeSession.timeline.coachWarnings &&
          activeSession.timeline.coachWarnings.length > 0 && (
            <div className="rounded-xl bg-amber-500/10 border border-amber-500/30 p-3.5 flex items-start gap-3">
              <AlertTriangle className="w-5 h-5 text-amber-400 shrink-0 mt-0.5" />
              <div className="space-y-1 text-xs text-amber-200">
                <div className="font-bold uppercase font-mono text-amber-300">
                  Rust MCP Timeline Notes
                </div>
                {activeSession.timeline.coachWarnings.map((w, i) => (
                  <p key={i}>• {w}</p>
                ))}
              </div>
            </div>
          )}
      </div>

      {/* Warm-Up Card (Phase 1 Reaction Game + Phase 2 Specific Mobility / Broomstick Drills) */}
      <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <span className="text-[11px] font-mono uppercase text-emerald-400 font-bold">
              WarmupPlannerAgent • MCP Tool: get_recent_coaching_history
            </span>
            <h2 className="text-lg font-extrabold text-white flex items-center gap-2">
              <Flame className="w-5 h-5 text-emerald-400" />
              Non-Repetitive Warm-Up & Broomstick Prep (
              {activeSession.warmup.durationMinutes} Min)
            </h2>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {activeSession.warmup.alternativeGames &&
              activeSession.warmup.alternativeGames.length > 0 && (
                <button
                  type="button"
                  onClick={handleSwapWarmupGame}
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-slate-800 hover:bg-slate-700 border border-[#CCFF00]/40 text-xs font-mono font-bold text-[#CCFF00] transition"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                  <span>Swap Warm-up Game</span>
                </button>
              )}
            <span className="px-3 py-1 rounded-xl bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 font-mono text-xs font-bold">
              {activeSession.warmup.style}
            </span>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {activeSession.warmup.phases.map((phase, idx) => (
            <div
              key={idx}
              className="rounded-xl border border-slate-800 bg-[#0B0F17] p-4 flex flex-col justify-between space-y-3"
            >
              <div className="space-y-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="px-2.5 py-0.5 rounded-md bg-[#CCFF00]/15 text-[#CCFF00] font-mono text-xs font-bold">
                    {phase.name}
                  </span>
                  <span className="text-xs font-mono text-slate-400">
                    {phase.durationMinutes} MIN
                  </span>
                </div>

                <p className="text-xs md:text-sm text-slate-200 leading-relaxed">
                  {phase.instructions}
                </p>

                {phase.drills && phase.drills.length > 0 && (
                  <div className="space-y-2 pt-1">
                    {phase.drills.map((drill, dIdx) => (
                      <div
                        key={dIdx}
                        className="rounded-lg bg-slate-900/90 border border-slate-800 p-2.5 flex flex-col sm:flex-row sm:items-center justify-between gap-1 text-xs"
                      >
                        <div>
                          <div className="font-bold text-white">
                            {drill.name}
                          </div>
                          <div className="text-slate-400 text-[11px]">
                            Biomechanical Focus: {drill.focus}
                          </div>
                        </div>
                        <span className="font-mono font-bold text-[#CCFF00] shrink-0">
                          {drill.reps}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div className="pt-2.5 border-t border-slate-800/80 text-xs text-emerald-300 flex items-center gap-1.5">
                <Sparkles className="w-3.5 h-3.5 shrink-0" />
                <span>
                  <strong>Coaching Goal:</strong> {phase.coachingGoal}
                </span>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Exercises, Coaching Cues & 4-Tier Scaling Matrix */}
      <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <span className="text-[11px] font-mono uppercase text-[#F97316] font-bold">
              ScalingAndCuesAgent • Biomechanical Cues & 4-Tier Matrix
            </span>
            <h2 className="text-lg font-extrabold text-white flex items-center gap-2">
              <Users className="w-5 h-5 text-[#F97316]" />
              Exercises, Tactical Coaching Cues & 4-Tier Scaling Matrix
            </h2>
          </div>

          {/* Global Scaling Tier Filter */}
          <div className="flex flex-wrap items-center gap-1.5">
            {[
              { id: "all", label: "All 4 Tiers" },
              { id: "rxPlus", label: "Rx+ (Firebreather)" },
              { id: "scaled", label: "Scaled" },
              { id: "seniorsMasters", label: "Masters / Seniors" },
              { id: "injured", label: "Injured Subs" },
            ].map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() =>
                  setScalingFilter(
                    t.id as
                    | "all"
                    | "rxPlus"
                    | "scaled"
                    | "seniorsMasters"
                    | "injured"
                  )
                }
                className={`px-3 py-1.5 rounded-lg text-xs font-bold transition ${scalingFilter === t.id
                    ? "bg-[#CCFF00] text-[#0B0F17]"
                    : "bg-[#0B0F17] text-slate-300 border border-slate-800 hover:border-slate-700"
                  }`}
              >
                {t.label}
              </button>
            ))}
          </div>
        </div>

        <div className="space-y-5">
          {activeSession.exercises.map((ex, idx) => {
            const mode = exerciseSubTab[idx] || "both";
            return (
              <div
                key={idx}
                className="rounded-2xl border border-slate-800 bg-[#0B0F17] p-5 space-y-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/90 pb-3">
                  <div>
                    <span className="text-[11px] font-mono uppercase text-[#CCFF00]">
                      {ex.category}
                    </span>
                    <h3 className="text-lg font-black text-white">{ex.name}</h3>
                  </div>

                  {/* Per-Movement Interactive View Switcher */}
                  <div className="flex items-center gap-1 bg-slate-900 p-1 rounded-xl border border-slate-800">
                    {[
                      { id: "both", label: "Cues + 4-Tier Scaling" },
                      { id: "cues", label: "Coaching Cues & Faults" },
                      { id: "scaling", label: "4-Tier Scaling Only" },
                    ].map((tab) => (
                      <button
                        key={tab.id}
                        type="button"
                        onClick={() =>
                          setExerciseSubTab((prev) => ({
                            ...prev,
                            [idx]: tab.id as "both" | "cues" | "scaling",
                          }))
                        }
                        className={`px-2.5 py-1 rounded-lg text-xs font-bold transition ${mode === tab.id
                            ? "bg-[#CCFF00] text-[#0B0F17]"
                            : "text-slate-400 hover:text-white"
                          }`}
                      >
                        {tab.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Cues Section: External, Internal, and Common Fault -> Fix */}
                {(mode === "both" || mode === "cues") && (
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-3.5">
                    {/* External Cues */}
                    <div className="rounded-xl bg-slate-900/80 border border-slate-800 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-[#CCFF00] mb-2 flex items-center gap-1.5">
                        <Activity className="w-4 h-4" />
                        External Cues (Actionable / Visual)
                      </div>
                      <ul className="space-y-2 text-xs text-slate-200">
                        {ex.cues.external.map((cue, cIdx) => (
                          <li
                            key={cIdx}
                            className="flex items-start gap-2 leading-snug"
                          >
                            <CheckCircle2 className="w-3.5 h-3.5 text-[#CCFF00] shrink-0 mt-0.5" />
                            <span>&ldquo;{cue}&rdquo;</span>
                          </li>
                        ))}
                      </ul>
                    </div>

                    {/* Internal Cues */}
                    <div className="rounded-xl bg-slate-900/80 border border-slate-800 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-emerald-300 mb-2 flex items-center gap-1.5">
                        <Sparkles className="w-4 h-4" />
                        Internal Cues (Tension / Breathing)
                      </div>
                      <ul className="space-y-2 text-xs text-slate-200">
                        {ex.cues.internal.map((cue, cIdx) => (
                          <li
                            key={cIdx}
                            className="flex items-start gap-2 leading-snug"
                          >
                            <span className="text-emerald-400 font-bold">•</span>
                            <span>{cue}</span>
                          </li>
                        ))}
                      </ul>
                    </div>

                    {/* Common Fault -> Fix */}
                    <div className="rounded-xl bg-slate-900/80 border border-slate-800 p-3.5">
                      <div className="text-xs font-mono font-bold uppercase text-[#F97316] mb-2 flex items-center gap-1.5">
                        <Wrench className="w-4 h-4" />
                        Common Fault → Instant Fix
                      </div>
                      <div className="space-y-2.5 text-xs">
                        {ex.cues.commonFaults.map((ff, fIdx) => (
                          <div
                            key={fIdx}
                            className="rounded-lg bg-[#0B0F17] p-2.5 border border-slate-800/80"
                          >
                            <div className="text-rose-300 font-semibold">
                              ✗ Fault: {ff.fault}
                            </div>
                            <div className="text-emerald-300 mt-1 font-medium">
                              ✓ Fix: {ff.fix}
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  </div>
                )}

                {/* 4-Tier Scaling Matrix */}
                {(mode === "both" || mode === "scaling") && (
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3.5">
                    {(scalingFilter === "all" ||
                      scalingFilter === "rxPlus") && (
                        <div className="rounded-xl border border-[#F97316]/45 bg-[#F97316]/10 p-3.5">
                          <div className="flex items-center justify-between mb-1.5">
                            <span className="text-xs font-mono font-black uppercase text-[#F97316]">
                              1. Rx+ (Firebreather)
                            </span>
                            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-[#F97316]/20 text-[#F97316]">
                              COMPETITOR
                            </span>
                          </div>
                          <p className="text-xs text-slate-100 leading-relaxed">
                            {ex.scaling.rxPlus}
                          </p>
                        </div>
                      )}

                    {(scalingFilter === "all" ||
                      scalingFilter === "scaled") && (
                        <div className="rounded-xl border border-sky-500/40 bg-sky-500/10 p-3.5">
                          <div className="flex items-center justify-between mb-1.5">
                            <span className="text-xs font-mono font-black uppercase text-sky-300">
                              2. Scaled (Intermediate)
                            </span>
                            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-sky-500/20 text-sky-300">
                              STIMULUS KEEP
                            </span>
                          </div>
                          <p className="text-xs text-slate-100 leading-relaxed">
                            {ex.scaling.scaled}
                          </p>
                        </div>
                      )}

                    {(scalingFilter === "all" ||
                      scalingFilter === "seniorsMasters") && (
                        <div className="rounded-xl border border-emerald-500/40 bg-emerald-500/10 p-3.5">
                          <div className="flex items-center justify-between mb-1.5">
                            <span className="text-xs font-mono font-black uppercase text-emerald-300">
                              3. Masters / Seniors
                            </span>
                            <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300">
                              LONGEVITY
                            </span>
                          </div>
                          <p className="text-xs text-slate-100 leading-relaxed">
                            {ex.scaling.seniorsMasters}
                          </p>
                        </div>
                      )}

                    {(scalingFilter === "all" ||
                      scalingFilter === "injured") && (
                        <div className="rounded-xl border border-amber-500/45 bg-amber-500/10 p-3.5">
                          <div className="flex items-center justify-between mb-1.5">
                            <span className="text-xs font-mono font-black uppercase text-amber-300 flex items-center gap-1">
                              <HeartPulse className="w-3.5 h-3.5" />
                              4. Injured Alternatives
                            </span>
                          </div>
                          <div className="space-y-1.5 text-xs text-slate-100">
                            <div>
                              <strong className="text-amber-300">
                                Wrist/Shoulder:
                              </strong>{" "}
                              {ex.scaling.injured.wristOrShoulder}
                            </div>
                            <div>
                              <strong className="text-amber-300">
                                Knee Pain:
                              </strong>{" "}
                              {ex.scaling.injured.kneePain}
                            </div>
                            <div>
                              <strong className="text-amber-300">
                                Lower Back:
                              </strong>{" "}
                              {ex.scaling.injured.lowerBack}
                            </div>
                          </div>
                        </div>
                      )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Agent Execution Trace Footer */}
      {activeSession.agentTraces && (
        <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-4 flex flex-wrap items-center justify-between gap-3">
          <div className="text-xs font-mono text-slate-400">
            Google ADK Multi-Agent Execution Trace:
          </div>
          <div className="flex flex-wrap gap-2">
            {activeSession.agentTraces.map((tr, idx) => (
              <span
                key={idx}
                className="px-2.5 py-1 rounded-lg bg-[#0B0F17] border border-slate-800 text-[11px] font-mono text-slate-300"
              >
                <strong className="text-[#CCFF00]">{tr.agent}</strong> →{" "}
                {tr.model}
                {tr.mcpTool ? ` (${tr.mcpTool})` : ""}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

