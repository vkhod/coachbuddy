"use client";

// One scrolling page in class order (plan 2.1): context, timeline, warm-up,
// each part, athletes today. Used by the coach's plan page and, with
// audience="athlete" or "coach-share", by the public share page, which
// receives only what the server's share view allowed.

import type { Injury, PartScaling, Plan, TierKey, WodPart } from "@/lib/contract/plan";
import {
  TIER_LABEL,
  TIER_ORDER,
  blockAnchor,
  failReason,
  injuryForPart,
  injuryNote,
  isBusy,
  isFailed,
  partAnchor,
  todayNote,
  two,
  type SectionName,
} from "@/lib/plan";

type Audience = "coach" | "coach-share" | "athlete";

const STATUS_WORD: Record<string, string> = {
  OPTIMAL: "fits exactly",
  TIGHT: "tight fit",
  INVALID: "does not add up",
};

function Kicker({ children }: { children: React.ReactNode }) {
  return <span className="head block text-[15px] font-semibold tracking-wide text-sub">{children}</span>;
}

/** Short titles in condensed caps; long prescriptions in sentence case, which reads better. */
function titleClass(title: string): string {
  return title.length > 40 ? "m-0 text-lg font-semibold leading-snug" : "head m-0 text-2xl leading-tight";
}

/** "Build A" already names part A; "Build" does not. */
function partKicker(part: WodPart): string {
  const named = part.name.trim().toLowerCase().endsWith(` ${part.id.toLowerCase()}`) || part.name === part.id;
  return `${named ? part.name : `${part.name} · ${part.id}`} · ${part.minutes} min`;
}

function SectionHead({ at, kicker, title, id }: { at?: number; kicker: string; title: string; id: string }) {
  return (
    <div className="rule-B grid grid-cols-[48px_minmax(0,1fr)] items-end gap-1 pb-2">
      <span className="head text-[26px] leading-none">{at !== undefined ? `${two(at)}′` : ""}</span>
      <div>
        <Kicker>{kicker}</Kicker>
        <h2 id={id} className={titleClass(title)}>{title}</h2>
      </div>
    </div>
  );
}

/** A section that could not be built: the reason, and that nothing replaced it. */
export function SectionFailed({ title, reason }: { title: string; reason: string }) {
  return (
    <div role="alert" className="mt-3 border-l-2 border-ink py-1 pl-3">
      <p className="head m-0 text-xl">Could not build the {title}</p>
      <p className="mb-0 mt-1 text-base">{reason}</p>
      <p className="mb-0 mt-1 text-[15px] text-sub">Nothing was made up in its place.</p>
    </div>
  );
}

function SectionWaiting({ title, state }: { title: string; state?: string }) {
  return (
    <p className="head mt-3 text-lg text-sub" aria-live="polite">
      {state === "running" ? `Writing the ${title}…` : `Waiting for the ${title}.`}
    </p>
  );
}

// --- Progress while generating ---

const PROGRESS: { section: SectionName; label: string }[] = [
  { section: "wod", label: "Reading the WOD" },
  { section: "warmup", label: "Warm-up" },
  { section: "parts", label: "Scaling" },
  { section: "injuries", label: "Injuries" },
  { section: "timeline", label: "Timeline" },
];

const STATE_WORD: Record<string, string> = {
  pending: "Waiting",
  running: "Writing",
  done: "Done",
  failed: "Failed",
  skipped: "Not needed",
};

export function Progress({ plan }: { plan: Plan }) {
  return (
    <section aria-label="Building the plan" className="pt-4">
      <h2 className="head m-0 text-[32px] leading-none">Building your plan</h2>
      <ol className="m-0 mt-3 list-none border-t-2 border-ink p-0" aria-live="polite">
        {PROGRESS.map(({ section, label }) => {
          const st = plan.sectionStatus[section]?.state ?? "pending";
          const detail = section === "wod" && plan.wod ? plan.wod.parts.map((p) => `${p.id} · ${p.name}`).join(", ") : null;
          return (
            <li key={section} className="rule-b flex min-h-[52px] items-center justify-between gap-3 py-2">
              <div>
                <span className="head block text-xl">{label}</span>
                {detail && <span className="text-[15px] text-sub">{detail}</span>}
              </div>
              <span className={`head shrink-0 text-base ${st === "done" || st === "skipped" ? "" : "text-sub"}`}>{STATE_WORD[st]}</span>
            </li>
          );
        })}
      </ol>
      <p className="mb-0 mt-3 text-[15px] text-sub">You can leave the app. The plan is saved and appears in History when it is ready.</p>
    </section>
  );
}

// --- Timeline ---

function TimelineSection({ plan, linked, warmupShown }: { plan: Partial<Plan>; linked: boolean; warmupShown: boolean }) {
  const st = plan.sectionStatus?.timeline;
  const tl = plan.timeline;
  return (
    <section id="time" aria-labelledby="time-h" className="pt-[18px]">
      <div className="rule-B flex items-baseline justify-between pb-1.5">
        <h2 id="time-h" className="head m-0 text-2xl">Timeline</h2>
        {tl && (
          <span className="head text-[17px] font-semibold normal-case">
            {tl.totalMinutes} min · {tl.provisional ? "provisional" : STATUS_WORD[tl.status] ?? tl.status}
          </span>
        )}
      </div>
      {st?.state === "failed" && <SectionFailed title="timeline" reason={failReason(plan as Plan, "timeline")} />}
      {!tl && isBusy(st?.state) && <SectionWaiting title="timeline" state={st?.state} />}
      {tl?.blocks.map((b, i) => {
        const target = linked ? blockAnchor(b.section, plan) : null;
        // The athlete view has no warm-up section to jump to.
        const anchor = target === "warmup" && !warmupShown ? null : target;
        const row = (
          <>
            <span className="head text-[19px]">
              {two(b.startMin)}–{two(b.endMin)}
            </span>
            <span className="text-base">{b.label}</span>
            <span className="text-right text-[15px] text-sub">{b.endMin - b.startMin}</span>
          </>
        );
        const cls = "rule-b grid min-h-11 grid-cols-[66px_minmax(0,1fr)_36px] items-center no-underline";
        return anchor ? (
          <a key={i} href={`#${anchor}`} className={cls}>
            {row}
          </a>
        ) : (
          <div key={i} className={cls}>
            {row}
          </div>
        );
      })}
      {tl?.provisional && <p className="mb-0 mt-2 text-[15px] text-sub">Times are provisional until the warm-up is built.</p>}
      {tl?.warnings?.map((w, i) => (
        <p key={i} className="mb-0 mt-2 text-[15px] text-sub">
          {w}
        </p>
      ))}
    </section>
  );
}

// --- Rows for athletes who need attention today (the only accent) ---

function AccentLabel({ label, note }: { label: string; note: string | null }) {
  return (
    <dt className="head text-lg leading-[1.05] text-accent">
      {label}
      {note && <span className="block text-sm font-semibold">{note}</span>}
    </dt>
  );
}

function TierRow({ tier, line, note }: { tier: TierKey; line: string; note: string | null }) {
  return (
    <div className="rule-b grid grid-cols-[84px_minmax(0,1fr)] py-2.5">
      {note ? <AccentLabel label={TIER_LABEL[tier]} note={note} /> : <dt className="head text-lg">{TIER_LABEL[tier]}</dt>}
      <dd className="m-0 text-base leading-snug">{line}</dd>
    </div>
  );
}

function InjuryRow({ injury, text }: { injury: Injury; text: string }) {
  return (
    <div className="rule-b grid grid-cols-[84px_minmax(0,1fr)] py-2.5">
      <AccentLabel label={injury.label} note={injuryNote(injury)} />
      <dd className="m-0 text-base leading-snug">{text}</dd>
    </div>
  );
}

// --- Warm-up ---

function WarmupSection({ plan, at }: { plan: Partial<Plan>; at?: number }) {
  const st = plan.sectionStatus?.warmup;
  const w = plan.warmup;
  const title = w ? w.blocks.map((b) => b.name).join(", ") : "Warm-up";
  return (
    <section id="warmup" aria-labelledby="w-h" className="pt-7">
      <div className="rule-B grid grid-cols-[48px_minmax(0,1fr)] items-end gap-1 pb-2">
        <span className="head text-[26px] leading-none">{at !== undefined ? `${two(at)}′` : ""}</span>
        <div>
          {w && <Kicker>{`Warm-up · ${w.minutes} min`}</Kicker>}
          <h2 id="w-h" className={titleClass(title)}>{title}</h2>
        </div>
      </div>
      {st?.state === "failed" && <SectionFailed title="warm-up" reason={failReason(plan as Plan, "warmup")} />}
      {!w && isBusy(st?.state) && <SectionWaiting title="warm-up" state={st?.state} />}
      {w && (
        <>
          <h3 className="head mb-1 mt-4 text-lg">Targets</h3>
          <dl className="m-0">
            {w.targets.map((t, i) => (
              <div key={i} className="rule-b grid grid-cols-[110px_minmax(0,1fr)] gap-2 py-2">
                <dt className="head text-lg leading-tight">{t.area}</dt>
                <dd className="m-0 text-[15px] leading-snug text-sub">{t.why}</dd>
              </div>
            ))}
          </dl>
          <ol className="m-0 mt-3 list-none p-0">
            {w.blocks.map((b, i) => (
              <li key={i} className="rule-b grid grid-cols-[28px_minmax(0,1fr)_40px] gap-y-1 py-2.5">
                <span className="head text-xl">{i + 1}</span>
                <span className="head text-xl">{b.name}</span>
                <span className="head text-right text-xl">{b.minutes}′</span>
                <span />
                <span className="col-span-2 text-base leading-snug">{b.items.join(" · ")}</span>
              </li>
            ))}
          </ol>
          {(plan.injuries?.length ?? 0) > 0 && (
            <dl className="m-0">
              {plan.injuries!.map((inj, i) => (
                <InjuryRow key={i} injury={inj} text={inj.warmup} />
              ))}
            </dl>
          )}
        </>
      )}
    </section>
  );
}

// --- Parts ---

function GridTable({ part, scaling, plan }: { part: WodPart; scaling: PartScaling; plan: Partial<Plan> }) {
  const cols = scaling.columns ?? [];
  return (
    <div className="-mx-4 mt-3 overflow-x-auto px-4">
      <table className="w-full border-collapse text-left text-[15px]">
        <caption className="sr-only">Each tier per movement for {part.name}</caption>
        <thead>
          <tr className="rule-B">
            <th scope="col" className="head w-[76px] py-1.5 pr-2 text-sm text-sub">Tier</th>
            {cols.map((c, i) => (
              <th key={i} scope="col" className="head px-1.5 py-1.5 align-bottom text-base leading-tight">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {TIER_ORDER.map((t) => {
            const tier = scaling.tiers[t];
            if (!tier) return null;
            const note = todayNote(plan.today, t);
            return (
              <tr key={t} className="rule-b align-top">
                <th scope="row" className={`head py-2 pr-2 text-lg leading-[1.05] ${note ? "text-accent" : ""}`}>
                  {TIER_LABEL[t]}
                  {note && <span className="block text-sm font-semibold">{note}</span>}
                </th>
                {tier.cells
                  ? tier.cells.map((c, i) => (
                      <td key={i} className="px-1.5 py-2 leading-snug">
                        {c}
                      </td>
                    ))
                  : <td colSpan={cols.length} className="px-1.5 py-2 leading-snug">{tier.line}</td>}
              </tr>
            );
          })}
          {plan.injuries?.map((inj, i) => {
            const text = injuryForPart(inj, part.id);
            if (!text) return null;
            const note = injuryNote(inj);
            return (
              <tr key={`inj-${i}`} className="rule-b align-top">
                <th scope="row" className="head py-2 pr-2 text-lg leading-[1.05] text-accent">
                  {inj.label}
                  {note && <span className="block text-sm font-semibold">{note}</span>}
                </th>
                <td colSpan={cols.length} className="px-1.5 py-2 leading-snug">
                  {text}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function PartSection({ part, plan, at, audience }: { part: WodPart; plan: Partial<Plan>; at?: number; audience: Audience }) {
  const scaling = plan.parts?.find((p) => p.partId === part.id);
  const st = plan.sectionStatus?.parts;
  const showToday = audience !== "athlete";
  const forRows: Partial<Plan> = showToday ? plan : { ...plan, today: undefined, injuries: undefined };
  return (
    <section id={partAnchor(part.id)} aria-labelledby={`h-${part.id}`} className="pt-7">
      <SectionHead id={`h-${part.id}`} at={at} kicker={partKicker(part)} title={part.prescription} />
      {st?.state === "failed" && <SectionFailed title="scaling" reason={failReason(plan as Plan, "parts")} />}
      {!scaling && isBusy(st?.state) && <SectionWaiting title="scaling" state={st?.state} />}
      {scaling && (
        <>
          {scaling.layout === "grid" && scaling.columns ? (
            <GridTable part={part} scaling={scaling} plan={forRows} />
          ) : (
            <dl className="m-0 mt-1">
              {TIER_ORDER.map((t) => {
                const tier = scaling.tiers[t];
                return tier ? <TierRow key={t} tier={t} line={tier.line} note={showToday ? todayNote(plan.today, t) : null} /> : null;
              })}
              {showToday &&
                plan.injuries?.map((inj, i) => {
                  const text = injuryForPart(inj, part.id);
                  return text ? <InjuryRow key={i} injury={inj} text={text} /> : null;
                })}
            </dl>
          )}
          {audience !== "athlete" && scaling.cues.length > 0 && (
            <ol className="m-0 mt-3 list-none p-0">
              {scaling.cues.map((c, i) => (
                <li key={i} className="grid grid-cols-[28px_minmax(0,1fr)] py-1 text-base leading-snug">
                  <span className="head text-lg">{i + 1}</span>
                  {c}
                </li>
              ))}
            </ol>
          )}
          {audience !== "athlete" && scaling.more && (scaling.more.substitutions?.length || scaling.more.faults?.length) ? (
            <details className="mt-2">
              <summary className="lnk tap flex cursor-pointer items-center text-[15px]">More: substitutions, common faults</summary>
              {scaling.more.substitutions?.length ? (
                <>
                  <h3 className="head mb-1 mt-2 text-lg">Substitutions</h3>
                  <ul className="m-0 pl-5 text-base leading-snug">
                    {scaling.more.substitutions.map((s, i) => (
                      <li key={i}>{s}</li>
                    ))}
                  </ul>
                </>
              ) : null}
              {scaling.more.faults?.length ? (
                <>
                  <h3 className="head mb-1 mt-3 text-lg">Common faults</h3>
                  <dl className="m-0">
                    {scaling.more.faults.map((f, i) => (
                      <div key={i} className="rule-b py-2">
                        <dt className="font-semibold">{f.fault}</dt>
                        <dd className="m-0 text-[15px] text-sub">{f.fix}</dd>
                      </div>
                    ))}
                  </dl>
                </>
              ) : null}
            </details>
          ) : null}
        </>
      )}
    </section>
  );
}

// --- Athletes today ---

function AthletesToday({ plan }: { plan: Partial<Plan> }) {
  const today = plan.today ?? [];
  const injuries = plan.injuries ?? [];
  const st = plan.sectionStatus?.injuries;
  if (!today.length && !injuries.length && st?.state !== "failed" && st?.state !== "running") return null;
  return (
    <section id="today" aria-labelledby="t-h" className="pt-7">
      <h2 id="t-h" className="head rule-B m-0 pb-1.5 text-2xl">Athletes today</h2>
      {today.map((g, i) => (
        <div key={i} className="rule-b py-2.5">
          <span className="head block text-xl text-accent">{g.count ? `${g.label} ×${g.count}` : g.label}</span>
          <span className="text-[15px] text-sub">{TIER_LABEL[g.tier]} rows are marked in every part. From your comment: “{g.fromComment}”</span>
        </div>
      ))}
      {st?.state === "failed" && <SectionFailed title="injury notes" reason={failReason(plan as Plan, "injuries")} />}
      {st?.state === "running" && <SectionWaiting title="injury notes" state="running" />}
      {injuries.map((inj, i) => (
        <div key={i} className="rule-b py-2.5">
          <span className="head block text-xl text-accent">{inj.returnNote ? `${inj.label}, ${inj.returnNote}` : inj.label}</span>
          <span className="text-[15px] text-sub">{inj.caution}</span>
        </div>
      ))}
      <p className="mb-0 mt-3 text-sm text-sub">Injuries and your comment never appear on athlete share links.</p>
    </section>
  );
}

// --- The page body ---

export function PlanSections({ plan, audience = "coach" }: { plan: Partial<Plan>; audience?: Audience }) {
  const startOf = (section: string) => plan.timeline?.blocks.find((b) => b.section === section)?.startMin;
  const parts = plan.wod?.parts ?? [];
  const wodFailed = plan.sectionStatus && isFailed(plan as Plan, "wod");
  return (
    <>
      {wodFailed && <SectionFailed title="WOD from the screenshot" reason={failReason(plan as Plan, "wod")} />}
      <TimelineSection plan={plan} linked warmupShown={audience !== "athlete"} />
      {audience !== "athlete" && (plan.warmup || plan.sectionStatus?.warmup) && <WarmupSection plan={plan} at={startOf("warmup")} />}
      {parts.map((p) => (
        <PartSection key={p.id} part={p} plan={plan} at={startOf(p.id)} audience={audience} />
      ))}
      {audience !== "athlete" && <AthletesToday plan={plan} />}
    </>
  );
}
