import type { Config } from "tailwindcss";

// "Programme sheet" look (Phase 1 mockups). Colours are CSS variables set in
// globals.css, so branding from configuration (Phase 3 step 5) can replace them.
const config: Config = {
  content: ["./src/app/**/*.{ts,tsx}", "./src/components/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        paper: "var(--paper)",
        ink: "var(--ink)",
        sub: "var(--sub)",
        rule: "var(--rule)",
        accent: "var(--accent)",
        card: "var(--card)",
      },
      fontFamily: {
        sans: ["Barlow", "Helvetica Neue", "Arial", "sans-serif"],
        cond: ["Barlow Condensed", "Arial Narrow", "sans-serif"],
      },
      borderRadius: {
        sm: "2px",
      },
    },
  },
  plugins: [],
};
export default config;
