import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/components/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        obsidian: {
          950: "#0B0F17",
          900: "#111827",
          800: "#1E293B",
          700: "#334155",
        },
        volt: {
          DEFAULT: "#CCFF00",
          400: "#D9FF4D",
          500: "#CCFF00",
          600: "#A3CC00",
        },
        blaze: {
          DEFAULT: "#F97316",
          500: "#F97316",
          600: "#EA580C",
        },
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "-apple-system", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "SFMono-Regular", "monospace"],
      },
    },
  },
  plugins: [],
};
export default config;
