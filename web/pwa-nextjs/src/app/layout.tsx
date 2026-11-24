import type { Metadata, Viewport } from "next";
import { connection } from "next/server";
import "@fontsource/barlow/400.css";
import "@fontsource/barlow/500.css";
import "@fontsource/barlow/600.css";
import "@fontsource/barlow-condensed/500.css";
import "@fontsource/barlow-condensed/600.css";
import "@fontsource/barlow-condensed/700.css";
import "./globals.css";
import { BrandProvider } from "@/components/Brand";
import { ServiceWorker } from "@/components/ServiceWorker";
import { cssVariables, getBranding, publicBranding } from "@/lib/branding";

// Branding is read from the mounted file when the server runs, not at build time,
// so one image serves every box.

export async function generateMetadata(): Promise<Metadata> {
  await connection();
  const b = getBranding();
  const v = publicBranding(b).version;
  return {
    title: b.name,
    description: "WOD plans for CrossFit coaches: warm-up, scaling tiers, injuries and a class timeline from a screenshot.",
    manifest: "/manifest.webmanifest",
    icons: {
      icon: [
        { url: `/brand/icon?v=${v}` },
        { url: `/brand/icon-192.png?v=${v}`, sizes: "192x192", type: "image/png" },
      ],
      apple: { url: `/brand/apple-icon.png?v=${v}`, sizes: "180x180", type: "image/png" },
    },
    appleWebApp: { capable: true, statusBarStyle: "default", title: b.shortName },
  };
}

export async function generateViewport(): Promise<Viewport> {
  await connection();
  return { themeColor: getBranding().colours.paper, width: "device-width", initialScale: 1 };
}

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  await connection();
  const b = getBranding();
  return (
    <html lang="en">
      <head>
        {/* Hex values only (checked in lib/branding.ts), so nothing else can reach this style. */}
        <style id="brand-colours">{cssVariables(b)}</style>
      </head>
      <body className="min-h-dvh bg-paper font-sans text-ink">
        <BrandProvider value={publicBranding(b)}>
          {children}
          <ServiceWorker />
        </BrandProvider>
      </body>
    </html>
  );
}
