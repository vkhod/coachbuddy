# Box branding

The coach app (`web/pwa-nextjs`) can carry a box's name, colours, logo and app icon instead of Coach Buddy's. They are read from a JSON file when the server starts, so the same image serves every box; no rebuild is needed.

## Setup

1. Put `branding.json` and its images in one folder. This folder works (its other files are ignored by git), or any folder on the host set as `BRANDING_DIR` in `.env`.
2. In `.env`, set `BRANDING_FILE=/config/branding/branding.json` (the path inside the container; the folder is mounted read-only at `/config/branding`).
3. Restart `pwa-nextjs`. Its log shows `[pwa] branding: <name> (...)`.

Without `BRANDING_FILE` the app is Coach Buddy. With it, the file must be valid: if it is not, `pwa-nextjs` stops at start with the reason, for example

```
[pwa] branding file /config/branding/branding.json: "ink" on "paper" has contrast 2.85:1; at least 4.5:1 is needed to read it
```

Changes to the file or the images apply after a restart.

## Example

`attakashaka/` is a complete branded install (the box the app was built for): see its README.

## Fields

See `example.json`. Only `name` is required.

| Field | Meaning |
|---|---|
| `name` | Box name, 1 to 40 characters. Page title, and the header when there is no logo. |
| `shortName` | Home screen label, up to 12 characters. Default: the first 12 characters of `name`. |
| `logo` | Wide logo for the header and the share page, shown 40 px high (up to 220 px wide). A file name next to `branding.json` (no folders): `.svg`, `.png`, `.jpg`, `.jpeg` or `.webp`, up to 1 MB. A logo on a transparent or `paper`-coloured background looks right. |
| `icon` | Square app icon (browser tab, home screen). Same file rules. Default: the Coach Buddy icon. |
| `colours` | Any of `paper` (page background), `ink` (text, rules, buttons), `sub` (secondary text), `rule` (hairlines), `accent` (only the rows about athletes who need attention today: injuries and the `today` groups), `card` (input fields). Hex values like `#1a2b3c`. Missing ones keep the Coach Buddy colours. |

Contrast is checked at start: `ink` on `paper` and on `card`, `sub` on `paper` and `accent` on `paper` must each be at least 4.5:1 (WCAG AA for body text).
