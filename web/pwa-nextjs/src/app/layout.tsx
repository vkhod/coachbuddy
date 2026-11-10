import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "CoachBuddy — Tactical Box WOD Planner & Floor Companion",
  description:
    "Multi-Agent WOD Session Planner, Broomstick Warm-Up Game Generator, 4-Tier Scaling Matrix, BYOK Vault, and Live Gym Floor Timer for CrossFit Coaches.",
  manifest: "/manifest.json",
  icons: {
    icon: "/icon.svg",
    apple: "/icon.svg",
  },
  appleWebApp: {
    capable: true,
    statusBarStyle: "black-translucent",
    title: "CoachBuddy",
  },
};

export const viewport: Viewport = {
  themeColor: "#0B0F17",
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-[#0B0F17] text-slate-100 selection:bg-[#CCFF00] selection:text-[#0B0F17]">
        {children}
      </body>
    </html>
  );
}
