"use client";

import React, { useState, useEffect, useRef } from "react";
import Link from "next/link";
import {
  Dumbbell,
  Sparkles,
  Clock,
  Flame,
  History,
  Settings,
  ShieldCheck,
  KeyRound,
  Cpu,
  Upload,
  CheckCircle2,
  Trash2,
  Search,
  Copy,
  ExternalLink,
  UserCheck,
  LogIn,
  X,
  Zap,
  Camera,
  Layers,
} from "lucide-react";
import {
  Coach,
  LLMAPIKey,
  AgentModelPref,
  AgentRole,
  WodSession,
  SharedLink,
  WOD_PRESETS,
  WARMUP_STYLE_CHIPS,
  MODEL_CATALOG,
  DEFAULT_COACH,
  getStoredCoach,
  getStoredToken,
  checkGatewayHealth,
  loginCoach,
  registerCoach,
  listAPIKeys,
  saveAPIKey,
  deleteAPIKey,
  getAgentPreferences,
  saveAgentPreferences,
  generateWodSession,
  listWodSessions,
  deleteWodSession,
  createSessionShareLink,
  subscribeToSessionEvents,
} from "@/lib/api";
import ActiveSessionTab from "@/components/ActiveSessionTab";

type ActiveTab = "planner" | "session" | "history" | "settings";

export default function CoachBuddyApp() {
  // Navigation & Global State
  const [activeTab, setActiveTab] = useState<ActiveTab>("planner");
  const [gatewayOnline, setGatewayOnline] = useState<boolean>(false);
  const [coach, setCoach] = useState<Coach>(DEFAULT_COACH);
  const [keys, setKeys] = useState<LLMAPIKey[]>([]);
  const [agentPrefs, setAgentPrefs] = useState<AgentModelPref[]>([]);
  const [sessions, setSessions] = useState<WodSession[]>([]);
  const [activeSession, setActiveSession] = useState<WodSession | null>(null);

  // Auth Modal State
  const [showAuthModal, setShowAuthModal] = useState(false);
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [authEmail, setAuthEmail] = useState("coach@crossfitbox.com");
  const [authPassword, setAuthPassword] = useState("coachbuddy123");
  const [authDisplayName, setAuthDisplayName] = useState(
    "Coach Alex Mercer (CF-L3)"
  );
  const [authBoxName, setAuthBoxName] = useState(
    "CrossFit Iron Horizon"
  );
  const [authStatusMsg, setAuthStatusMsg] = useState<string | null>(null);

  // Tab 1: WOD Planner Form State
  const [wodTitle, setWodTitle] = useState(WOD_PRESETS[0].title);
  const [wodRawText, setWodRawText] = useState(WOD_PRESETS[0].rawText);
  const [coachConstraints, setCoachConstraints] = useState(
    WOD_PRESETS[0].coachConstraints
  );
  const [classDuration, setClassDuration] = useState<number>(60);
  const [imageBase64, setImageBase64] = useState<string>("");
  const [isGenerating, setIsGenerating] = useState(false);
  const [agentStepIndex, setAgentStepIndex] = useState<number>(-1);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  // Tab 2: Active Session & Coach Floor Mode State
  const [floorMode, setFloorMode] = useState(false);
  const [currentBlockIdx, setCurrentBlockIdx] = useState(0);
  const [blockSecondsLeft, setBlockSecondsLeft] = useState(240);
  const [timerRunning, setTimerRunning] = useState(false);
  const [wakeLockActive, setWakeLockActive] = useState(false);
  const wakeLockRef = useRef<{ release: () => Promise<void> } | null>(null);
  const [exerciseSubTab, setExerciseSubTab] = useState<
    Record<number, "both" | "cues" | "scaling">
  >({});
  const [scalingFilter, setScalingFilter] = useState<
    "all" | "rxPlus" | "scaled" | "seniorsMasters" | "injured"
  >("all");

  // Share Modal State
  const [showShareModal, setShowShareModal] = useState(false);
  const [includeCoachNotes, setIncludeCoachNotes] = useState(true);
  const [shareLinkData, setShareLinkData] = useState<SharedLink | null>(null);
  const [isSharing, setIsSharing] = useState(false);
  const [copiedShare, setCopiedShare] = useState(false);

  // Tab 3: History Search
  const [historyQuery, setHistoryQuery] = useState("");

  // Tab 4: BYOK Vault & Per-Agent Router State
  const [newKeyProvider, setNewKeyProvider] = useState("gemini");
  const [newKeyLabel, setNewKeyLabel] = useState("Production Box Key");
  const [newKeyValue, setNewKeyValue] = useState("");
  const [newKeyDefault, setNewKeyDefault] = useState(true);
  const [usePerAgentOverrides, setUsePerAgentOverrides] = useState(true);
  const [settingsSavedBanner, setSettingsSavedBanner] = useState<string | null>(
    null
  );
  const [orchestrationError, setOrchestrationError] = useState<string | null>(
    null
  );

  // Initial Bootstrap
  useEffect(() => {
    async function bootstrap() {
      setCoach(getStoredCoach());
      const h = await checkGatewayHealth();
      setGatewayOnline(h.online);
      if (h.online) {
        const tok = getStoredToken();
        if (!tok || tok.startsWith("local-jwt-")) {
          const authRes = await loginCoach({
            email: "coach@crossfitbox.com",
            password: "coachbuddy123",
          });
          setCoach(authRes.coach);
        }
      }
      const [loadedKeys, loadedPrefs, loadedSessions] = await Promise.all([
        listAPIKeys(),
        getAgentPreferences(),
        listWodSessions(),
      ]);
      setKeys(loadedKeys);
      setAgentPrefs(loadedPrefs);
      setSessions(loadedSessions);
      if (loadedSessions.length > 0) {
        setActiveSession(loadedSessions[0]);
      }
    }
    bootstrap();
  }, []);

  // Sync Floor Timer when Active Session or Block Index changes
  useEffect(() => {
    if (!activeSession?.timeline?.blocks?.length) return;
    const safeIdx = Math.min(
      currentBlockIdx,
      activeSession.timeline.blocks.length - 1
    );
    const blk = activeSession.timeline.blocks[safeIdx];
    if (blk) {
      const durSec = Math.max(1, (blk.endMin - blk.startMin) * 60);
      setBlockSecondsLeft(durSec);
    }
  }, [activeSession, currentBlockIdx]);

  // Countdown Interval for Coach Floor Mode
  useEffect(() => {
    if (!timerRunning) return;
    const interval = setInterval(() => {
      setBlockSecondsLeft((prev) => {
        if (prev <= 1) {
          // Auto-advance to next block if available
          if (
            activeSession?.timeline?.blocks &&
            currentBlockIdx < activeSession.timeline.blocks.length - 1
          ) {
            setCurrentBlockIdx((idx) => idx + 1);
          } else {
            setTimerRunning(false);
          }
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [timerRunning, activeSession, currentBlockIdx]);

  // Browser Screen WakeLock API for Coach Floor Mode
  useEffect(() => {
    async function requestWakeLock() {
      if (floorMode && typeof navigator !== "undefined" && "wakeLock" in navigator) {
        try {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const sentinel = await (navigator as any).wakeLock.request("screen");
          wakeLockRef.current = sentinel;
          setWakeLockActive(true);
        } catch {
          setWakeLockActive(false);
        }
      } else {
        if (wakeLockRef.current) {
          wakeLockRef.current.release().catch(() => {});
          wakeLockRef.current = null;
        }
        setWakeLockActive(false);
      }
    }
    requestWakeLock();
    return () => {
      if (wakeLockRef.current) {
        wakeLockRef.current.release().catch(() => {});
        wakeLockRef.current = null;
      }
    };
  }, [floorMode]);

  // Handlers
  const handleSelectPreset = (preset: (typeof WOD_PRESETS)[number]) => {
    setWodTitle(preset.title);
    setWodRawText(preset.rawText);
    setCoachConstraints(preset.coachConstraints);
    setClassDuration(preset.duration);
  };

  const handleToggleWarmupChip = (chip: string) => {
    if (coachConstraints.toLowerCase().includes(chip.toLowerCase())) {
      return;
    }
    setCoachConstraints((prev) =>
      prev.trim() ? `${prev.trim()} • ${chip}` : chip
    );
  };

  const handleFileUpload = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        setImageBase64(reader.result);
        // If the text box still holds the untouched default preset, clear it so Vision OCR focuses on the uploaded screenshot
        if (wodRawText === WOD_PRESETS[0].rawText) {
          setWodRawText("");
          const cleanName = file.name.replace(/\.[^/.]+$/, "");
          setWodTitle(`Whiteboard Vision: ${cleanName}`);
        }
      }
    };
    reader.readAsDataURL(file);
  };

  const handleGenerateSyntheticWhiteboard = () => {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="340" viewBox="0 0 640 340">
      <rect width="640" height="340" rx="16" fill="#0F172A" stroke="#CCFF00" stroke-width="4"/>
      <text x="28" y="46" fill="#CCFF00" font-family="monospace" font-weight="bold" font-size="20">IRONVOLT WHITEBOARD - WOD SNAPSHOT</text>
      <line x1="28" y1="62" x2="612" y2="62" stroke="#334155" stroke-width="2"/>
      <text x="28" y="100" fill="#F8FAFC" font-family="monospace" font-size="16">A) Every 2:30 x 6: 3 Heavy Front Squats (85% 1RM)</text>
      <text x="28" y="140" fill="#F97316" font-family="monospace" font-weight="bold" font-size="17">B) "FRAN RELOADED" - 21-15-9 FOR TIME:</text>
      <text x="44" y="172" fill="#E2E8F0" font-family="monospace" font-size="15">* Thrusters (95/65 lb | Rx+ 115/80 lb)</text>
      <text x="44" y="200" fill="#E2E8F0" font-family="monospace" font-size="15">* Pull-Ups (Rx+ Chest-to-Bar)</text>
      <text x="44" y="228" fill="#E2E8F0" font-family="monospace" font-size="15">* Buy-Out: 400m Run / 500m Row</text>
      <rect x="28" y="264" width="584" height="48" rx="8" fill="#1E293B"/>
      <text x="42" y="293" fill="#10B981" font-family="monospace" font-size="14">COACH NOTE: Broomstick reaction warm-up + 4-tier scaling</text>
    </svg>`;
    const utf8SafeBase64 = btoa(
      encodeURIComponent(svg).replace(/%([0-9A-F]{2})/g, (_, p1) =>
        String.fromCharCode(parseInt(p1, 16))
      )
    );
    const encoded = `data:image/svg+xml;base64,${utf8SafeBase64}`;
    setImageBase64(encoded);
  };

  const handleRunOrchestration = async () => {
    if (!wodRawText.trim() && !imageBase64) return;
    setIsGenerating(true);
    setOrchestrationError(null);
    setAgentStepIndex(0);

    const stepTimer1 = setTimeout(() => setAgentStepIndex((prev) => Math.max(prev, 1)), 450);
    const stepTimer2 = setTimeout(() => setAgentStepIndex((prev) => Math.max(prev, 2)), 950);
    const stepTimer3 = setTimeout(() => setAgentStepIndex((prev) => Math.max(prev, 3)), 1450);

    try {
      const [{ session, mode }] = await Promise.all([
        generateWodSession({
          title: wodTitle,
          rawText: wodRawText,
          imageBase64: imageBase64 || undefined,
          coachConstraints,
          targetDurationMinutes: classDuration,
        }),
        new Promise((r) => setTimeout(r, 1900)),
      ]);
      // Connect SSE stream for live/replayed step events
      const unsubscribe = subscribeToSessionEvents(session.sessionId, (evt) => {
        if (evt.step === "parser") setAgentStepIndex(0);
        else if (evt.step === "warmup") setAgentStepIndex(1);
        else if (evt.step === "scaling") setAgentStepIndex(2);
        else if (evt.step === "timeline") setAgentStepIndex(3);
        else if (evt.step === "completed") setAgentStepIndex(4);
      });
      setTimeout(() => unsubscribe(), 3000);
      setGatewayOnline(mode === "gateway");
      setActiveSession(session);
      setCurrentBlockIdx(0);
      setSessions((prev) => [
        session,
        ...prev.filter((s) => s.sessionId !== session.sessionId),
      ]);
      setAgentStepIndex(4);
      setTimeout(() => {
        setIsGenerating(false);
        setActiveTab("session");
      }, 350);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setOrchestrationError(msg);
      setIsGenerating(false);
      setAgentStepIndex(-1);
    } finally {
      clearTimeout(stepTimer1);
      clearTimeout(stepTimer2);
      clearTimeout(stepTimer3);
    }
  };

  const handleOpenShare = async (notesToggle: boolean) => {
    if (!activeSession) return;
    setIncludeCoachNotes(notesToggle);
    setShowShareModal(true);
    setIsSharing(true);
    setCopiedShare(false);
    try {
      const link = await createSessionShareLink(
        activeSession.sessionId,
        notesToggle,
        activeSession
      );
      setShareLinkData(link);
    } finally {
      setIsSharing(false);
    }
  };

  const handleCopyShareUrl = () => {
    if (!shareLinkData) return;
    navigator.clipboard?.writeText(shareLinkData.shareUrl).catch(() => {});
    setCopiedShare(true);
    setTimeout(() => setCopiedShare(false), 2500);
  };

  const handleAuthSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setAuthStatusMsg("Authenticating with CoachBuddy Vault...");
    if (authMode === "register") {
      const res = await registerCoach({
        email: authEmail,
        password: authPassword,
        displayName: authDisplayName,
        boxName: authBoxName,
        defaultClassDurationMin: classDuration,
      });
      setCoach(res.coach);
      setGatewayOnline(res.mode === "gateway");
      setAuthStatusMsg(
        `Signed in as ${res.coach.displayName} (${res.mode.toUpperCase()})`
      );
    } else {
      const res = await loginCoach({
        email: authEmail,
        password: authPassword,
      });
      setCoach(res.coach);
      setGatewayOnline(res.mode === "gateway");
      setAuthStatusMsg(
        `Signed in as ${res.coach.displayName} (${res.mode.toUpperCase()})`
      );
    }
    setTimeout(() => {
      setShowAuthModal(false);
      setAuthStatusMsg(null);
    }, 700);
  };

  const handleQuickCoachLogin = async () => {
    const res = await loginCoach({
      email: "coach@crossfitbox.com",
      password: "coachbuddy123",
    });
    setCoach(res.coach);
    setGatewayOnline(res.mode === "gateway");
    setShowAuthModal(false);
  };

  const handleAddKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyValue.trim()) return;
    const saved = await saveAPIKey({
      provider: newKeyProvider,
      label: newKeyLabel || `${newKeyProvider.toUpperCase()} Key`,
      apiKey: newKeyValue,
      isDefault: newKeyDefault,
    });
    setKeys((prev) => [saved, ...prev.filter((k) => k.id !== saved.id)]);
    setNewKeyValue("");
    setSettingsSavedBanner(
      `Encrypted & stored ${saved.provider.toUpperCase()} key (${saved.keyHint}) with AES-256-GCM`
    );
    setTimeout(() => setSettingsSavedBanner(null), 4000);
  };

  const handleDeleteKey = async (id: string) => {
    await deleteAPIKey(id);
    setKeys((prev) => prev.filter((k) => k.id !== id));
  };

  const updateAgentPref = (
    role: AgentRole,
    provider: string,
    modelIdentifier: string
  ) => {
    setAgentPrefs((prev) => {
      const exists = prev.some((p) => p.agentRole === role);
      if (exists) {
        return prev.map((p) =>
          p.agentRole === role ? { ...p, provider, modelIdentifier } : p
        );
      }
      return [...prev, { agentRole: role, provider, modelIdentifier }];
    });
  };

  const handleSaveAgentRouting = async () => {
    const updated = await saveAgentPreferences(agentPrefs);
    setAgentPrefs(updated);
    setSettingsSavedBanner(
      "Saved Per-Agent ADK + LiteLLM Routing Configuration!"
    );
    setTimeout(() => setSettingsSavedBanner(null), 3500);
  };

  const getPrefForRole = (role: AgentRole): AgentModelPref => {
    return (
      agentPrefs.find((p) => p.agentRole === role) ||
      agentPrefs.find((p) => p.agentRole === "global") || {
        agentRole: role,
        provider: "gemini",
        modelIdentifier: "gemini/gemini-3.8-flash",
      }
    );
  };

  const filteredSessions = sessions.filter((s) => {
    if (!historyQuery.trim()) return true;
    const q = historyQuery.toLowerCase();
    return (
      s.title.toLowerCase().includes(q) ||
      (s.parsedWod?.stimulus || "").toLowerCase().includes(q) ||
      (s.input?.rawText || "").toLowerCase().includes(q)
    );
  });

  return (
    <div className="min-h-screen bg-[#0B0F17] text-slate-100 flex flex-col">
      {/* Top Navigation & Auth Bar */}
      <header className="sticky top-0 z-40 border-b border-slate-800/90 bg-[#0B0F17]/95 backdrop-blur-md">
        <div className="max-w-7xl mx-auto px-4 py-3 flex flex-wrap items-center justify-between gap-3">
          {/* Brand Logo */}
          <div className="flex items-center gap-3">
            <div className="w-11 h-11 rounded-2xl bg-[#CCFF00] text-[#0B0F17] flex items-center justify-center shadow-lg shadow-[#CCFF00]/20">
              <Dumbbell className="w-6 h-6 stroke-[2.5]" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-black tracking-tight text-xl text-white">
                  COACH<span className="text-[#CCFF00]">BUDDY</span>
                </span>
                <span className="hidden sm:inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-mono font-bold bg-[#CCFF00]/15 text-[#CCFF00] border border-[#CCFF00]/30">
                  ADK + MCP
                </span>
              </div>
              <p className="text-xs text-slate-400">
                Tactical Box WOD Session Planner & Floor Companion
              </p>
            </div>
          </div>

          {/* Primary Navigation Tabs */}
          <nav className="flex items-center gap-1.5 bg-slate-900/90 p-1.5 rounded-2xl border border-slate-800">
            <button
              onClick={() => setActiveTab("planner")}
              className={`flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-bold transition ${
                activeTab === "planner"
                  ? "bg-[#CCFF00] text-[#0B0F17] shadow-md shadow-[#CCFF00]/20"
                  : "text-slate-300 hover:text-white hover:bg-slate-800/70"
              }`}
            >
              <Sparkles className="w-4 h-4" />
              <span>1. WOD Planner</span>
            </button>

            <button
              onClick={() => setActiveTab("session")}
              className={`flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-bold transition ${
                activeTab === "session"
                  ? "bg-[#CCFF00] text-[#0B0F17] shadow-md shadow-[#CCFF00]/20"
                  : "text-slate-300 hover:text-white hover:bg-slate-800/70"
              }`}
            >
              <Flame className="w-4 h-4" />
              <span>2. Active Plan & Floor Mode</span>
            </button>

            <button
              onClick={() => setActiveTab("history")}
              className={`flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-bold transition ${
                activeTab === "history"
                  ? "bg-[#CCFF00] text-[#0B0F17] shadow-md shadow-[#CCFF00]/20"
                  : "text-slate-300 hover:text-white hover:bg-slate-800/70"
              }`}
            >
              <History className="w-4 h-4" />
              <span>3. History ({sessions.length})</span>
            </button>

            <button
              onClick={() => setActiveTab("settings")}
              className={`flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-bold transition ${
                activeTab === "settings"
                  ? "bg-[#CCFF00] text-[#0B0F17] shadow-md shadow-[#CCFF00]/20"
                  : "text-slate-300 hover:text-white hover:bg-slate-800/70"
              }`}
            >
              <Settings className="w-4 h-4" />
              <span>4. BYOK & Agents</span>
            </button>
          </nav>

          {/* Right Status Badges & Coach Profile */}
          <div className="flex items-center gap-2.5">
            {/* BYOK Status Badge */}
            <button
              onClick={() => setActiveTab("settings")}
              title="Configure AES-256-GCM BYOK Keys & Per-Agent Routing"
              className="hidden lg:inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs font-mono hover:bg-emerald-500/20 transition"
            >
              <ShieldCheck className="w-3.5 h-3.5 text-emerald-400" />
              <span>BYOK Vault ({keys.length} Keys)</span>
            </button>

            {/* Gateway Connectivity Pill */}
            <div
              className={`hidden md:inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border text-[11px] font-mono ${
                gatewayOnline
                  ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-300"
                  : "bg-amber-500/10 border-amber-500/30 text-amber-300"
              }`}
            >
              <span
                className={`w-2 h-2 rounded-full ${
                  gatewayOnline ? "bg-emerald-400 animate-pulse" : "bg-amber-400"
                }`}
              />
              <span>{gatewayOnline ? "Gateway :8080" : "Resilient Mode"}</span>
            </div>

            {/* Coach Profile & Auth Trigger */}
            <button
              onClick={() => setShowAuthModal(true)}
              className="flex items-center gap-2.5 px-3 py-1.5 rounded-xl bg-slate-900 hover:bg-slate-800 border border-slate-700/80 transition text-left"
            >
              <div className="w-7 h-7 rounded-lg bg-[#F97316]/20 border border-[#F97316]/40 flex items-center justify-center text-[#F97316] font-bold text-xs">
                {coach.displayName.slice(0, 2).toUpperCase()}
              </div>
              <div className="hidden sm:block leading-tight">
                <div className="text-xs font-bold text-white truncate max-w-[140px]">
                  {coach.displayName}
                </div>
                <div className="text-[10px] font-mono text-slate-400 truncate max-w-[140px]">
                  {coach.boxName}
                </div>
              </div>
              <LogIn className="w-3.5 h-3.5 text-[#CCFF00] ml-0.5" />
            </button>
          </div>
        </div>
      </header>

      {/* Main Content Container */}
      <main className="flex-1 max-w-7xl w-full mx-auto px-4 py-6">
        {/* =========================================================
            TAB 1: WOD SESSION PLANNER (GENERATOR)
        ========================================================= */}
        {activeTab === "planner" && (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
            {/* Left 7 Columns: WOD Input, Screenshot, Warm-up Style & Duration */}
            <div className="lg:col-span-7 space-y-6">
              {/* Preset Templates Bar */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5">
                <div className="flex items-center justify-between mb-3">
                  <span className="text-xs font-mono uppercase tracking-wider text-[#CCFF00] font-bold flex items-center gap-1.5">
                    <Zap className="w-4 h-4" />
                    One-Click Box WOD Presets
                  </span>
                  <span className="text-xs text-slate-400">
                    Load realistic CrossFit programming in 1 click
                  </span>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-2.5">
                  {WOD_PRESETS.map((preset) => {
                    const isSelected = wodTitle === preset.title;
                    return (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => handleSelectPreset(preset)}
                        className={`text-left p-3 rounded-xl border transition flex flex-col justify-between ${
                          isSelected
                            ? "border-[#CCFF00] bg-[#CCFF00]/10 shadow-md shadow-[#CCFF00]/10"
                            : "border-slate-800 bg-[#0B0F17]/80 hover:border-slate-700"
                        }`}
                      >
                        <div className="text-[10px] font-mono font-bold uppercase text-[#F97316] mb-1">
                          {preset.tag}
                        </div>
                        <div className="text-xs font-bold text-white leading-snug">
                          {preset.label}
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* WOD Text & Whiteboard Screenshot Input Card */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h2 className="text-base font-bold text-white flex items-center gap-2">
                    <Dumbbell className="w-5 h-5 text-[#CCFF00]" />
                    Whiteboard WOD Input (Text & Multimodal Vision)
                  </h2>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={() => fileInputRef.current?.click()}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-[#CCFF00]/15 hover:bg-[#CCFF00]/25 text-xs font-mono font-bold text-[#CCFF00] border border-[#CCFF00]/40 transition"
                    >
                      <Upload className="w-3.5 h-3.5" />
                      Upload Screenshot File
                    </button>
                    <button
                      type="button"
                      onClick={handleGenerateSyntheticWhiteboard}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-mono text-slate-300 border border-slate-700 transition"
                    >
                      <Camera className="w-3.5 h-3.5" />
                      Demo SVG Whiteboard
                    </button>
                  </div>
                </div>

                <div>
                  <label className="block text-xs font-mono uppercase text-slate-400 mb-1.5">
                    Session Title
                  </label>
                  <input
                    type="text"
                    value={wodTitle}
                    onChange={(e) => setWodTitle(e.target.value)}
                    placeholder="e.g., Heavy Front Squat + Fran-style Thruster/Pull-up Metcon"
                    className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 focus:border-[#CCFF00] focus:outline-none px-3.5 py-2.5 text-sm text-white font-semibold"
                  />
                </div>

                <div>
                  <label className="block text-xs font-mono uppercase text-slate-400 mb-1.5">
                    WOD Programming (Strength, Skill, Metcon, Rx Weights)
                  </label>
                  <textarea
                    rows={7}
                    value={wodRawText}
                    onChange={(e) => setWodRawText(e.target.value)}
                    placeholder="Paste today's WOD from SugarWOD, Wodify, PushPress, or type movements..."
                    className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 focus:border-[#CCFF00] focus:outline-none p-3.5 text-xs md:text-sm font-mono text-slate-100 leading-relaxed"
                  />
                </div>

                {/* Drag & Drop / Paste WOD Screenshot */}
                <div
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault();
                    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
                      handleFileUpload(e.dataTransfer.files[0]);
                    }
                  }}
                  className="rounded-xl border border-dashed border-slate-700 bg-[#0B0F17]/70 p-4 text-center hover:border-[#CCFF00]/60 transition"
                >
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={(e) => {
                      if (e.target.files && e.target.files[0]) {
                        handleFileUpload(e.target.files[0]);
                      }
                    }}
                  />
                  {imageBase64 ? (
                    <div className="space-y-3">
                      <div className="flex items-center justify-between text-xs font-mono text-emerald-300 bg-emerald-500/10 border border-emerald-500/30 px-3 py-1.5 rounded-lg">
                        <span>
                          ✓ Whiteboard Screenshot Attached (Multimodal Vision Ready)
                        </span>
                        <button
                          type="button"
                          onClick={() => setImageBase64("")}
                          className="text-rose-400 hover:underline font-bold"
                        >
                          Remove
                        </button>
                      </div>
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={imageBase64}
                        alt="WOD Whiteboard Preview"
                        className="max-h-44 mx-auto rounded-lg border border-slate-800 object-contain"
                      />
                    </div>
                  ) : (
                    <div
                      onClick={() => fileInputRef.current?.click()}
                      className="cursor-pointer py-2 flex flex-col items-center"
                    >
                      <Upload className="w-6 h-6 text-[#CCFF00] mb-1.5" />
                      <p className="text-xs font-semibold text-slate-200">
                        Drag & drop a WOD Whiteboard Screenshot here, or{" "}
                        <span className="text-[#CCFF00] underline">
                          browse image
                        </span>
                      </p>
                      <p className="text-[11px] text-slate-400 mt-0.5">
                        Supports PNG, JPG, WebP — parsed by Google ADK{" "}
                        <code className="text-slate-300">WodParserAgent</code>
                      </p>
                    </div>
                  )}
                </div>
              </div>

              {/* Coach Cues, Warm-up Style Prompt & Class Duration */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-5">
                <div>
                  <label className="block text-xs font-mono uppercase text-[#CCFF00] font-bold mb-2">
                    Coach Warm-up Style & Floor Constraints
                  </label>
                  <div className="flex flex-wrap gap-1.5 mb-3">
                    {WARMUP_STYLE_CHIPS.map((chip) => (
                      <button
                        key={chip}
                        type="button"
                        onClick={() => handleToggleWarmupChip(chip)}
                        className="px-3 py-1.5 rounded-lg bg-[#0B0F17] hover:bg-slate-800 border border-slate-700 text-xs font-medium text-slate-200 hover:border-[#CCFF00] transition"
                      >
                        + {chip}
                      </button>
                    ))}
                  </div>
                  <textarea
                    rows={2}
                    value={coachConstraints}
                    onChange={(e) => setCoachConstraints(e.target.value)}
                    placeholder="e.g. Mostly Broomstick / PVC warm-up with a reaction game; 2 athletes have wrist/knee pain..."
                    className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 focus:border-[#CCFF00] focus:outline-none p-3 text-xs md:text-sm text-slate-100"
                  />
                </div>

                {/* Class Duration Selector (Default 60m) */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="text-xs font-mono uppercase text-slate-300 font-bold flex items-center gap-1.5">
                      <Clock className="w-4 h-4 text-[#CCFF00]" />
                      Target Class Duration
                    </label>
                    <span className="text-sm font-mono font-extrabold text-[#CCFF00]">
                      {classDuration} MINUTES
                    </span>
                  </div>
                  <div className="grid grid-cols-4 gap-2 mb-2">
                    {[45, 60, 75, 90].map((min) => (
                      <button
                        key={min}
                        type="button"
                        onClick={() => setClassDuration(min)}
                        className={`py-2 rounded-xl font-mono text-xs font-bold border transition ${
                          classDuration === min
                            ? "bg-[#CCFF00] text-[#0B0F17] border-[#CCFF00]"
                            : "bg-[#0B0F17] text-slate-300 border-slate-800 hover:border-slate-700"
                        }`}
                      >
                        {min} MIN {min === 60 && "(DEFAULT)"}
                      </button>
                    ))}
                  </div>
                  <input
                    type="range"
                    min={45}
                    max={90}
                    step={15}
                    value={classDuration}
                    onChange={(e) => setClassDuration(Number(e.target.value))}
                    className="w-full accent-[#CCFF00] cursor-pointer"
                  />
                </div>

                {orchestrationError && (
                  <div className="rounded-xl border border-rose-500/50 bg-rose-500/10 p-4 text-xs text-rose-200 space-y-2">
                    <div className="font-bold text-rose-300 uppercase font-mono">
                      Live Orchestration Error
                    </div>
                    <p className="font-mono leading-relaxed break-words">
                      {orchestrationError}
                    </p>
                    <button
                      type="button"
                      onClick={() => setActiveTab("settings")}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-[#CCFF00] text-[#0B0F17] font-bold text-xs uppercase"
                    >
                      <KeyRound className="w-3.5 h-3.5" />
                      Configure API Key in 4. BYOK & Agents Vault
                    </button>
                  </div>
                )}

                {/* Primary Action Button */}
                <button
                  type="button"
                  disabled={isGenerating}
                  onClick={handleRunOrchestration}
                  className="w-full py-4 rounded-2xl bg-[#CCFF00] hover:bg-[#d8ff33] disabled:opacity-60 text-[#0B0F17] font-black text-base tracking-wide uppercase shadow-xl shadow-[#CCFF00]/20 flex items-center justify-center gap-2.5 transition"
                >
                  {isGenerating ? (
                    <>
                      <div className="w-5 h-5 rounded-full border-2 border-[#0B0F17] border-t-transparent animate-spin" />
                      <span>Orchestrating Multi-Agent Session Plan...</span>
                    </>
                  ) : (
                    <>
                      <Sparkles className="w-5 h-5" />
                      <span>
                        Generate Complete {classDuration}-Min Coaching Plan
                      </span>
                    </>
                  )}
                </button>
              </div>
            </div>

            {/* Right 5 Columns: Live Multi-Agent Execution Stepper & Architecture Preview */}
            <div className="lg:col-span-5 space-y-6">
              <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <span className="text-[11px] font-mono uppercase tracking-wider text-[#CCFF00] font-bold">
                      Google ADK + LiteLLM + Rust MCP
                    </span>
                    <h3 className="text-lg font-extrabold text-white">
                      Multi-Agent Execution Stepper
                    </h3>
                  </div>
                  <Cpu className="w-6 h-6 text-[#CCFF00]" />
                </div>

                <p className="text-xs text-slate-400 leading-relaxed">
                  CoachBuddy runs a hierarchical{" "}
                  <code className="text-slate-200">SequentialAgent</code>{" "}
                  wrapping a fan-out{" "}
                  <code className="text-slate-200">ParallelAgent</code> stage
                  coupled with deterministic Rust MCP domain tools.
                </p>

                {/* 4 Agent Step Cards */}
                <div className="space-y-3">
                  {[
                    {
                      step: 0,
                      title: "1. WOD Vision & Parser Agent",
                      agentName: "WodParserAgent",
                      role: "parser" as AgentRole,
                      mcp: "Multimodal OCR + Biomechanical Joint Graph",
                      desc: "Extracts strength/metcon blocks, rep schemes, time caps, and primary loaded joints.",
                    },
                    {
                      step: 1,
                      title: "2a. Warm-up & Game Planner Agent (MCP)",
                      agentName: "WarmupPlannerAgent",
                      role: "warmup" as AgentRole,
                      mcp: "Rust MCP: query_warmup_drills",
                      desc: "Designs a fun 4-min reaction game + 8-min PVC broomstick mobility sequence.",
                    },
                    {
                      step: 2,
                      title: "2b. 4-Tier Scaling & Cues Agent (MCP)",
                      agentName: "ScalingAndCuesAgent",
                      role: "scaling" as AgentRole,
                      mcp: "Rust MCP: get_movement_substitutions",
                      desc: "Generates External/Internal cues, Fault→Fix pairs, and Rx+ / Scaled / Masters / Injured tiers.",
                    },
                    {
                      step: 3,
                      title: "3. Timeline & Logistics Coordinator Agent (MCP)",
                      agentName: "TimelineLogisticsAgent",
                      role: "timeline" as AgentRole,
                      mcp: "Rust MCP: validate_class_timeline",
                      desc: "Validates exact minute-by-minute class blocks, equipment transitions, and time-cap math.",
                    },
                  ].map((item) => {
                    const pref = getPrefForRole(item.role);
                    const isRunning =
                      isGenerating && agentStepIndex === item.step;
                    const isDone =
                      (!isGenerating && !orchestrationError && activeSession) ||
                      agentStepIndex > item.step;

                    return (
                      <div
                        key={item.agentName}
                        className={`rounded-xl border p-3.5 transition ${
                          isRunning
                            ? "border-[#CCFF00] bg-[#CCFF00]/10 shadow-lg shadow-[#CCFF00]/10"
                            : isDone
                            ? "border-emerald-500/40 bg-[#0B0F17]/90"
                            : "border-slate-800 bg-[#0B0F17]/60"
                        }`}
                      >
                        <div className="flex items-start justify-between gap-2 mb-1.5">
                          <div className="font-bold text-xs md:text-sm text-white flex items-center gap-2">
                            {isRunning ? (
                              <span className="w-3.5 h-3.5 rounded-full border-2 border-[#CCFF00] border-t-transparent animate-spin inline-block" />
                            ) : isDone ? (
                              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                            ) : (
                              <Layers className="w-4 h-4 text-slate-500 shrink-0" />
                            )}
                            <span>{item.title}</span>
                          </div>
                          <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-slate-800 text-[#CCFF00] border border-slate-700 shrink-0">
                            {pref.modelIdentifier}
                          </span>
                        </div>
                        <p className="text-xs text-slate-400 mb-2">
                          {item.desc}
                        </p>
                        <div className="flex items-center justify-between text-[11px] font-mono">
                          <span className="text-emerald-300">{item.mcp}</span>
                          <span className="uppercase text-slate-400">
                            Provider: {pref.provider}
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>

                {/* Quick Jump to Active Session if already loaded */}
                {activeSession && (
                  <div className="pt-2">
                    <button
                      type="button"
                      onClick={() => setActiveTab("session")}
                      className="w-full py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs font-bold text-[#CCFF00] flex items-center justify-center gap-2 transition"
                    >
                      <span>View Current Active Session: {activeSession.title}</span>
                      <ExternalLink className="w-3.5 h-3.5" />
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}

        {/* =========================================================
            TAB 2: ACTIVE SESSION PLAN & "COACH FLOOR MODE"
        ========================================================= */}
        {activeTab === "session" && activeSession && (
          <ActiveSessionTab
            activeSession={activeSession}
            onUpdateSession={(updated) => {
              setActiveSession(updated);
              setSessions((prev) =>
                prev.map((s) =>
                  s.sessionId === updated.sessionId ? updated : s
                )
              );
            }}
            floorMode={floorMode}
            setFloorMode={setFloorMode}
            currentBlockIdx={currentBlockIdx}
            setCurrentBlockIdx={setCurrentBlockIdx}
            blockSecondsLeft={blockSecondsLeft}
            setBlockSecondsLeft={setBlockSecondsLeft}
            timerRunning={timerRunning}
            setTimerRunning={setTimerRunning}
            wakeLockActive={wakeLockActive}
            exerciseSubTab={exerciseSubTab}
            setExerciseSubTab={setExerciseSubTab}
            scalingFilter={scalingFilter}
            setScalingFilter={setScalingFilter}
            onOpenShare={handleOpenShare}
          />
        )}

        {/* =========================================================
            TAB 3: SESSION HISTORY LIBRARY
        ========================================================= */}
        {activeTab === "history" && (
          <div className="space-y-6">
            <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 flex flex-wrap items-center justify-between gap-4">
              <div>
                <h1 className="text-xl font-black text-white flex items-center gap-2">
                  <History className="w-5 h-5 text-[#CCFF00]" />
                  Saved Box WOD Session Library
                </h1>
                <p className="text-xs text-slate-400">
                  Persisted in MongoDB 7 (`sessions` collection) with instant
                  offline PWA cache fallback.
                </p>
              </div>

              <div className="relative w-full sm:w-80">
                <Search className="w-4 h-4 text-slate-400 absolute left-3.5 top-1/2 -translate-y-1/2" />
                <input
                  type="text"
                  value={historyQuery}
                  onChange={(e) => setHistoryQuery(e.target.value)}
                  placeholder="Search by movement, title, stimulus..."
                  className="w-full pl-9 pr-3.5 py-2 rounded-xl bg-[#0B0F17] border border-slate-800 focus:border-[#CCFF00] focus:outline-none text-xs text-white"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {filteredSessions.map((sess) => {
                const isCurrent =
                  activeSession?.sessionId === sess.sessionId;
                return (
                  <div
                    key={sess.sessionId}
                    className={`rounded-2xl border p-5 flex flex-col justify-between transition ${
                      isCurrent
                        ? "border-[#CCFF00] bg-slate-900/90"
                        : "border-slate-800 bg-slate-900/50 hover:border-slate-700"
                    }`}
                  >
                    <div>
                      <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
                        <div className="flex items-center gap-2">
                          <span className="px-2 py-0.5 rounded bg-[#CCFF00]/15 text-[#CCFF00] font-mono text-[11px] font-bold">
                            {sess.timeline?.totalMinutes || 60} MIN
                          </span>
                          <span className="px-2 py-0.5 rounded bg-emerald-500/15 text-emerald-300 font-mono text-[11px]">
                            {sess.timeline?.feasibilityStatus || "OPTIMAL"}
                          </span>
                        </div>
                        <span className="text-[11px] font-mono text-slate-400">
                          {new Date(sess.createdAt).toLocaleString()}
                        </span>
                      </div>

                      <h3 className="text-base font-extrabold text-white mb-1.5">
                        {sess.title}
                      </h3>

                      <p className="text-xs text-slate-400 line-clamp-2 mb-3">
                        {sess.parsedWod?.stimulus}
                      </p>

                      <div className="flex flex-wrap gap-1.5 mb-4">
                        {(sess.parsedWod?.movements || []).map((m, i) => (
                          <span
                            key={i}
                            className="px-2 py-0.5 rounded bg-[#0B0F17] border border-slate-800 text-[11px] font-mono text-slate-300"
                          >
                            {m}
                          </span>
                        ))}
                      </div>
                    </div>

                    <div className="flex items-center justify-between gap-2 pt-3 border-t border-slate-800/80">
                      <button
                        type="button"
                        onClick={() => {
                          setActiveSession(sess);
                          setCurrentBlockIdx(0);
                          setActiveTab("session");
                        }}
                        className="flex-1 py-2 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-bold text-xs uppercase transition"
                      >
                        Load into Floor Mode
                      </button>

                      <button
                        type="button"
                        onClick={async () => {
                          await deleteWodSession(sess.sessionId);
                          setSessions((prev) =>
                            prev.filter((s) => s.sessionId !== sess.sessionId)
                          );
                        }}
                        title="Delete Session"
                        className="p-2 rounded-xl bg-slate-800 hover:bg-rose-500/20 text-slate-400 hover:text-rose-300 border border-slate-700 transition"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* =========================================================
            TAB 4: BYOK VAULT & PER-AGENT LLM SETTINGS
        ========================================================= */}
        {activeTab === "settings" && (
          <div className="space-y-6">
            {settingsSavedBanner && (
              <div className="rounded-xl bg-emerald-500/15 border border-emerald-500/40 p-3.5 text-xs font-mono text-emerald-200 flex items-center justify-between">
                <span className="flex items-center gap-2">
                  <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                  {settingsSavedBanner}
                </span>
                <button
                  type="button"
                  onClick={() => setSettingsSavedBanner(null)}
                  className="text-slate-400 hover:text-white"
                >
                  ✕
                </button>
              </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
              {/* Left 5 Columns: AES-256-GCM Encrypted API Key Vault */}
              <div className="lg:col-span-5 space-y-5">
                <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <span className="text-[11px] font-mono uppercase text-emerald-400 font-bold">
                        Go Gateway crypto/cipher • AES-256-GCM
                      </span>
                      <h2 className="text-lg font-extrabold text-white flex items-center gap-2">
                        <KeyRound className="w-5 h-5 text-[#CCFF00]" />
                        BYOK Encrypted API Key Vault
                      </h2>
                    </div>
                    <ShieldCheck className="w-6 h-6 text-emerald-400" />
                  </div>

                  <p className="text-xs text-slate-400 leading-relaxed">
                    Add your own provider keys (`Google Gemini`, `OpenAI`,
                    `Anthropic Claude`, `Groq`). Keys are envelope-encrypted at
                    rest in PostgreSQL (`BYTEA` + 12-byte GCM nonce) and never
                    returned in plaintext.
                  </p>

                  {/* Add Key Form */}
                  <form onSubmit={handleAddKey} className="space-y-3">
                    <div className="grid grid-cols-2 gap-2.5">
                      <div>
                        <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                          LLM Provider
                        </label>
                        <select
                          value={newKeyProvider}
                          onChange={(e) => setNewKeyProvider(e.target.value)}
                          className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3 py-2 text-xs text-white focus:border-[#CCFF00] focus:outline-none"
                        >
                          <option value="gemini">Google Gemini</option>
                          <option value="openai">OpenAI</option>
                          <option value="anthropic">Anthropic Claude</option>
                          <option value="groq">Groq (LPU)</option>
                        </select>
                      </div>

                      <div>
                        <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                          Key Label
                        </label>
                        <input
                          type="text"
                          value={newKeyLabel}
                          onChange={(e) => setNewKeyLabel(e.target.value)}
                          placeholder="e.g. Box Primary Key"
                          className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3 py-2 text-xs text-white focus:border-[#CCFF00] focus:outline-none"
                        />
                      </div>
                    </div>

                    <div>
                      <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                        Secret API Key (Encrypted before storage)
                      </label>
                      <input
                        type="password"
                        value={newKeyValue}
                        onChange={(e) => setNewKeyValue(e.target.value)}
                        placeholder="AIzaSy... / sk-proj-... / sk-ant-..."
                        className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3 py-2 text-xs font-mono text-white focus:border-[#CCFF00] focus:outline-none"
                      />
                    </div>

                    <div className="flex items-center justify-between pt-1">
                      <label className="inline-flex items-center gap-2 text-xs text-slate-300 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={newKeyDefault}
                          onChange={(e) => setNewKeyDefault(e.target.checked)}
                          className="accent-[#CCFF00]"
                        />
                        <span>Set as default key for this provider</span>
                      </label>

                      <button
                        type="submit"
                        className="px-4 py-2 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-bold text-xs uppercase transition"
                      >
                        Encrypt & Save Key
                      </button>
                    </div>
                  </form>

                  {/* Stored Keys List */}
                  <div className="pt-3 border-t border-slate-800 space-y-2">
                    <div className="text-xs font-mono uppercase text-slate-400">
                      Stored Vault Keys ({keys.length})
                    </div>
                    {keys.map((k) => (
                      <div
                        key={k.id}
                        className="rounded-xl bg-[#0B0F17] border border-slate-800 p-3 flex items-center justify-between gap-2"
                      >
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-xs font-bold text-white">
                              {k.label}
                            </span>
                            <span className="px-2 py-0.5 rounded bg-slate-800 text-[#CCFF00] font-mono text-[10px] uppercase">
                              {k.provider}
                            </span>
                          </div>
                          <div className="text-[11px] font-mono text-slate-400 mt-0.5">
                            Hint: <span className="text-emerald-300">{k.keyHint}</span>{" "}
                            • AES-256-GCM Sealed
                          </div>
                        </div>

                        <button
                          type="button"
                          onClick={() => handleDeleteKey(k.id)}
                          className="p-1.5 rounded-lg text-slate-400 hover:text-rose-400 hover:bg-slate-800 transition"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>

              {/* Right 7 Columns: Per-Agent Model Router */}
              <div className="lg:col-span-7 space-y-5">
                <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 space-y-5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <span className="text-[11px] font-mono uppercase text-[#CCFF00] font-bold">
                        Google ADK + LiteLLM Dynamic Router
                      </span>
                      <h2 className="text-lg font-extrabold text-white flex items-center gap-2">
                        <Cpu className="w-5 h-5 text-[#CCFF00]" />
                        Per-Agent LLM Provider & Model Routing
                      </h2>
                    </div>

                    <label className="inline-flex items-center gap-2 text-xs font-mono text-slate-300 bg-[#0B0F17] px-3 py-1.5 rounded-xl border border-slate-800 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={usePerAgentOverrides}
                        onChange={(e) =>
                          setUsePerAgentOverrides(e.target.checked)
                        }
                        className="accent-[#CCFF00]"
                      />
                      <span>Enable Per-Agent Overrides</span>
                    </label>
                  </div>

                  <p className="text-xs text-slate-400 leading-relaxed">
                    Route all 4 agents through a single Global model or assign
                    specialized models per agent role (e.g.,{" "}
                    <code className="text-slate-200">Gemini 2.5 Flash</code> for
                    multimodal whiteboard OCR,{" "}
                    <code className="text-slate-200">GPT-4o</code> for 4-tier
                    injury scaling, and{" "}
                    <code className="text-slate-200">Claude 3.7 Sonnet</code>{" "}
                    for timeline validation).
                  </p>

                  <div className="space-y-3">
                    {[
                      {
                        role: "global" as AgentRole,
                        title: "Global Default Orchestrator Model",
                        subtitle:
                          "Fallback model used by any agent without a custom override",
                      },
                      ...(usePerAgentOverrides
                        ? [
                            {
                              role: "parser" as AgentRole,
                              title: "1. WodParserAgent (Vision & Structural OCR)",
                              subtitle:
                                "Parses whiteboard screenshots & extracts primary loaded joints",
                            },
                            {
                              role: "warmup" as AgentRole,
                              title:
                                "2a. WarmupPlannerAgent (MCP: query_warmup_drills)",
                              subtitle:
                                "Designs reaction games & PVC broomstick mobility progressions",
                            },
                            {
                              role: "scaling" as AgentRole,
                              title:
                                "2b. ScalingAndCuesAgent (MCP: get_movement_substitutions)",
                              subtitle:
                                "Builds External/Internal cues & 4-tier Rx+/Scaled/Masters/Injured matrix",
                            },
                            {
                              role: "timeline" as AgentRole,
                              title:
                                "3. TimelineLogisticsAgent (MCP: validate_class_timeline)",
                              subtitle:
                                "Computes minute-by-minute class blocks & transition logistics",
                            },
                          ]
                        : []),
                    ].map((agentRow) => {
                      const pref = getPrefForRole(agentRow.role);
                      const providerModels =
                        MODEL_CATALOG[pref.provider] || MODEL_CATALOG.gemini;

                      return (
                        <div
                          key={agentRow.role}
                          className="rounded-xl border border-slate-800 bg-[#0B0F17] p-4 flex flex-col md:flex-row md:items-center justify-between gap-3"
                        >
                          <div>
                            <div className="font-bold text-sm text-white">
                              {agentRow.title}
                            </div>
                            <div className="text-xs text-slate-400">
                              {agentRow.subtitle}
                            </div>
                          </div>

                          <div className="flex flex-wrap items-center gap-2">
                            <select
                              value={pref.provider}
                              onChange={(e) => {
                                const newProv = e.target.value;
                                const firstMod =
                                  MODEL_CATALOG[newProv]?.[0]?.id ||
                                  "gemini/gemini-3.8-flash";
                                updateAgentPref(
                                  agentRow.role,
                                  newProv,
                                  firstMod
                                );
                              }}
                              className="rounded-xl bg-slate-900 border border-slate-700 px-3 py-2 text-xs font-mono text-[#CCFF00] focus:border-[#CCFF00] focus:outline-none uppercase"
                            >
                              <option value="gemini">Gemini</option>
                              <option value="openai">OpenAI</option>
                              <option value="anthropic">Anthropic</option>
                              <option value="groq">Groq</option>
                            </select>

                            <select
                              value={pref.modelIdentifier}
                              onChange={(e) =>
                                updateAgentPref(
                                  agentRow.role,
                                  pref.provider,
                                  e.target.value
                                )
                              }
                              className="rounded-xl bg-slate-900 border border-slate-700 px-3 py-2 text-xs font-mono text-white focus:border-[#CCFF00] focus:outline-none"
                            >
                              {providerModels.map((m) => (
                                <option key={m.id} value={m.id}>
                                  {m.label} ({m.badge})
                                </option>
                              ))}
                            </select>
                          </div>
                        </div>
                      );
                    })}
                  </div>

                  <button
                    type="button"
                    onClick={handleSaveAgentRouting}
                    className="w-full py-3.5 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-black text-xs uppercase tracking-wider shadow-lg shadow-[#CCFF00]/20 transition"
                  >
                    Save Per-Agent Model Routing Preferences
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* =========================================================
          MODAL 1: SHARE SESSION & RUST SVG QR CODE
      ========================================================= */}
      {showShareModal && activeSession && (
        <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4">
          <div className="w-full max-w-lg rounded-2xl border border-slate-800 bg-slate-900 p-6 space-y-5 shadow-2xl">
            <div className="flex items-center justify-between">
              <div>
                <span className="text-[11px] font-mono uppercase text-[#CCFF00] font-bold">
                  Rust MCP Zero-Alloc SVG QR Generator
                </span>
                <h3 className="text-lg font-black text-white">
                  Share Session Plan with Co-Coach or Athletes
                </h3>
              </div>
              <button
                type="button"
                onClick={() => setShowShareModal(false)}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-slate-800"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {/* Co-Coach vs Athlete View Toggle */}
            <div className="grid grid-cols-2 gap-2 bg-[#0B0F17] p-1.5 rounded-xl border border-slate-800">
              <button
                type="button"
                onClick={() => handleOpenShare(true)}
                className={`py-2 px-3 rounded-lg text-xs font-bold transition ${
                  includeCoachNotes
                    ? "bg-[#CCFF00] text-[#0B0F17]"
                    : "text-slate-400 hover:text-white"
                }`}
              >
                Co-Coach View (Full Cues & Notes)
              </button>
              <button
                type="button"
                onClick={() => handleOpenShare(false)}
                className={`py-2 px-3 rounded-lg text-xs font-bold transition ${
                  !includeCoachNotes
                    ? "bg-[#CCFF00] text-[#0B0F17]"
                    : "text-slate-400 hover:text-white"
                }`}
              >
                Athlete View (WOD & Scaling Only)
              </button>
            </div>

            {isSharing || !shareLinkData ? (
              <div className="py-12 flex flex-col items-center justify-center">
                <div className="w-10 h-10 rounded-full border-2 border-[#CCFF00] border-t-transparent animate-spin mb-3" />
                <span className="text-xs font-mono text-slate-400">
                  Generating Rust MCP SVG QR Code...
                </span>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="flex flex-col items-center justify-center bg-[#0B0F17] border border-slate-800 rounded-2xl p-5">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img
                    src={`data:image/svg+xml;utf8,${encodeURIComponent(
                      shareLinkData.qrSvg
                    )}`}
                    alt={`QR Code for ${shareLinkData.shortCode}`}
                    className="w-52 h-52 object-contain"
                  />
                  <span className="text-xs font-mono text-slate-400 mt-2">
                    Scan at the Whiteboard • Code:{" "}
                    <strong className="text-[#CCFF00]">
                      {shareLinkData.shortCode}
                    </strong>
                  </span>
                </div>

                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    readOnly
                    value={shareLinkData.shareUrl}
                    className="flex-1 rounded-xl bg-[#0B0F17] border border-slate-800 px-3.5 py-2.5 text-xs font-mono text-slate-200"
                  />
                  <button
                    type="button"
                    onClick={handleCopyShareUrl}
                    className="inline-flex items-center gap-1.5 px-4 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs font-bold text-white border border-slate-700 transition"
                  >
                    <Copy className="w-3.5 h-3.5 text-[#CCFF00]" />
                    <span>{copiedShare ? "Copied!" : "Copy URL"}</span>
                  </button>
                </div>

                <div className="flex items-center justify-between pt-2">
                  <Link
                    href={`/s/${shareLinkData.shortCode}`}
                    onClick={() => setShowShareModal(false)}
                    className="w-full py-3 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-black text-xs uppercase tracking-wider flex items-center justify-center gap-2 transition"
                  >
                    <span>Preview Public Share Page (/s/{shareLinkData.shortCode})</span>
                    <ExternalLink className="w-4 h-4" />
                  </Link>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* =========================================================
          MODAL 2: COACH AUTH & PROFILE MODAL
      ========================================================= */}
      {showAuthModal && (
        <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4">
          <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-6 space-y-4 shadow-2xl">
            <div className="flex items-center justify-between">
              <div>
                <span className="text-[11px] font-mono uppercase text-[#CCFF00] font-bold">
                  Go Gateway JWT Session
                </span>
                <h3 className="text-lg font-black text-white">
                  Coach Account & Box Profile
                </h3>
              </div>
              <button
                type="button"
                onClick={() => setShowAuthModal(false)}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-slate-800"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="grid grid-cols-2 gap-2 bg-[#0B0F17] p-1 rounded-xl border border-slate-800">
              <button
                type="button"
                onClick={() => setAuthMode("login")}
                className={`py-2 rounded-lg text-xs font-bold transition ${
                  authMode === "login"
                    ? "bg-[#CCFF00] text-[#0B0F17]"
                    : "text-slate-400 hover:text-white"
                }`}
              >
                Coach Login
              </button>
              <button
                type="button"
                onClick={() => setAuthMode("register")}
                className={`py-2 rounded-lg text-xs font-bold transition ${
                  authMode === "register"
                    ? "bg-[#CCFF00] text-[#0B0F17]"
                    : "text-slate-400 hover:text-white"
                }`}
              >
                Register New Coach
              </button>
            </div>

            <form onSubmit={handleAuthSubmit} className="space-y-3">
              {authMode === "register" && (
                <>
                  <div>
                    <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                      Coach Name & Credential
                    </label>
                    <input
                      type="text"
                      value={authDisplayName}
                      onChange={(e) => setAuthDisplayName(e.target.value)}
                      className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3.5 py-2 text-xs text-white"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                      CrossFit Box / Gym Name
                    </label>
                    <input
                      type="text"
                      value={authBoxName}
                      onChange={(e) => setAuthBoxName(e.target.value)}
                      className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3.5 py-2 text-xs text-white"
                    />
                  </div>
                </>
              )}

              <div>
                <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                  Coach Email
                </label>
                <input
                  type="email"
                  value={authEmail}
                  onChange={(e) => setAuthEmail(e.target.value)}
                  className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3.5 py-2 text-xs text-white"
                />
              </div>

              <div>
                <label className="block text-[11px] font-mono uppercase text-slate-400 mb-1">
                  Password
                </label>
                <input
                  type="password"
                  value={authPassword}
                  onChange={(e) => setAuthPassword(e.target.value)}
                  className="w-full rounded-xl bg-[#0B0F17] border border-slate-800 px-3.5 py-2 text-xs text-white"
                />
              </div>

              {authStatusMsg && (
                <div className="text-xs font-mono text-[#CCFF00] bg-[#CCFF00]/10 border border-[#CCFF00]/30 rounded-lg p-2.5">
                  {authStatusMsg}
                </div>
              )}

              <button
                type="submit"
                className="w-full py-3 rounded-xl bg-[#CCFF00] hover:bg-[#d8ff33] text-[#0B0F17] font-black text-xs uppercase tracking-wider transition"
              >
                {authMode === "login"
                  ? "Sign In to Coach Session"
                  : "Create Coach Account"}
              </button>
            </form>

            <div className="pt-2 border-t border-slate-800">
              <button
                type="button"
                onClick={handleQuickCoachLogin}
                className="w-full py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs font-bold text-emerald-300 border border-slate-700 flex items-center justify-center gap-2 transition"
              >
                <UserCheck className="w-4 h-4" />
                <span>Instant Quick Coach Login (Demo CF-L3 Profile)</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
