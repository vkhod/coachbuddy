"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { BrandMark } from "./Brand";

// --- Icons (square line caps, as in the mockups) ---

const iconProps = {
  width: 22,
  height: 22,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "square" as const,
  "aria-hidden": true,
};

export const HistoryIcon = () => (
  <svg {...iconProps}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 2" />
  </svg>
);

export const SettingsIcon = () => (
  <svg {...iconProps}>
    <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
    <circle cx="16" cy="7" r="2" />
    <circle cx="10" cy="17" r="2" />
  </svg>
);

export const BackIcon = () => (
  <svg {...iconProps}>
    <path d="M15 5l-7 7 7 7" />
  </svg>
);

export const ShareIcon = () => (
  <svg {...iconProps}>
    <path d="M12 4v11M7 9l5-5 5 5M5 14v6h14v-6" />
  </svg>
);

export const CameraIcon = () => (
  <svg {...iconProps} width={40} height={40}>
    <path d="M4 8h4l2-3h4l2 3h4v11H4z" />
    <circle cx="12" cy="13" r="3.5" />
  </svg>
);

// --- Headers ---

export function AppHeader({ history = true, settings = true }: { history?: boolean; settings?: boolean }) {
  return (
    <header className="rule-B flex h-14 items-center pl-4 pr-1">
      <Link href="/" className="flex flex-1 items-center self-stretch no-underline">
        <BrandMark />
      </Link>
      {history && (
        <Link href="/history" aria-label="History" className="tap flex items-center justify-center">
          <HistoryIcon />
        </Link>
      )}
      {settings && (
        <Link href="/settings" aria-label="Settings" className="tap flex items-center justify-center">
          <SettingsIcon />
        </Link>
      )}
    </header>
  );
}

export function BackHeader({ href, label, title }: { href: string; label: string; title: ReactNode }) {
  return (
    <header className="rule-B flex h-14 items-center gap-1 pl-1 pr-4">
      <Link href={href} aria-label={label} className="tap flex items-center justify-center">
        <BackIcon />
      </Link>
      <span className="head text-lg tracking-wide">{title}</span>
    </header>
  );
}

/** Full-height phone column. The app is portrait-first; wider screens get a centred column. */
export function Screen({ children }: { children: ReactNode }) {
  return <div className="mx-auto flex min-h-dvh max-w-[480px] flex-col">{children}</div>;
}

// --- Controls ---

export const CLASS_LENGTHS = [45, 60, 75, 90];

export function ClassLength({
  value,
  onChange,
  legend = "Class length, min",
}: {
  value: number;
  onChange: (n: number) => void;
  legend?: string;
}) {
  return (
    <fieldset className="m-0 border-0 p-0">
      <legend className="head pb-2 pt-[18px] text-lg">{legend}</legend>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(4.5rem,1fr))] gap-px border border-ink bg-ink">
        {CLASS_LENGTHS.map((n) => {
          const on = n === value;
          return (
            <label
              key={n}
              className={`head flex h-[46px] cursor-pointer items-center justify-center text-xl has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 ${
                on ? "bg-ink text-paper" : "bg-paper"
              }`}
            >
              <input
                type="radio"
                name={legend}
                value={n}
                checked={on}
                onChange={() => onChange(n)}
                className="absolute h-px w-px opacity-0"
              />
              {n}
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

/** A failed request: the server's reason, and a retry when one makes sense. */
export function ErrorNote({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="mt-4 border-l-2 border-ink py-1 pl-3">
      <p className="m-0 text-base">{message}</p>
      {onRetry && (
        <button type="button" onClick={onRetry} className="lnk tap mt-1 bg-transparent p-0 text-[15px] text-ink">
          Try again
        </button>
      )}
    </div>
  );
}

export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <p className="head px-4 py-8 text-lg text-sub" aria-live="polite">
      {label}…
    </p>
  );
}
