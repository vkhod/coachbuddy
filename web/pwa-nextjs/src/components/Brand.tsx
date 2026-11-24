"use client";

// The box's name and logo for client components, from the root layout
// (src/lib/branding.ts reads the configuration on the server).

import { createContext, useContext, type ReactNode } from "react";
import type { PublicBranding } from "@/lib/branding";

const BrandContext = createContext<PublicBranding>({ name: "Coach Buddy", shortName: "Coach Buddy", hasLogo: false, version: "0" });

export function BrandProvider({ value, children }: { value: PublicBranding; children: ReactNode }) {
  return <BrandContext.Provider value={value}>{children}</BrandContext.Provider>;
}

export const useBrand = () => useContext(BrandContext);

/** The logo when the box configured one, else the name in the heading face. */
export function BrandMark({ className = "" }: { className?: string }) {
  const brand = useBrand();
  if (brand.hasLogo) {
    return (
      // A plain <img>: the logo is served by /brand/logo from the mounted file,
      // so next/image's build-time optimisation does not apply.
      // eslint-disable-next-line @next/next/no-img-element
      <img src={`/brand/logo?v=${brand.version}`} alt={brand.name} className={`block h-10 w-auto max-w-[220px] object-contain object-left ${className}`} />
    );
  }
  return <span className={`head text-[22px] tracking-wide ${className}`}>{brand.name}</span>;
}
