# Light mode plan

Scope: a light theme for the user and admin apps. The user picks System (default), Light or Dark in Settings → Profile;
the choice is per browser. Frontend only, no backend or schema change. The landing page stays dark.

Out of scope: a per-account theme saved on the server, a high-contrast theme, recoloring stored status or label colors.

## Tokens (`frontend/shared/src/styles/tokens.css`)

Dark stays as `DESIGN.md` defines it. Light overrides the same tokens, plus a few new ones the components used to
hard-code:

Light colors come from `DESIGN-LIGHT.md` (an Apple-inspired palette); the names in brackets are its color names.

| Token | Dark | Light |
|---|---|---|
| `--bg` | `#08090a` | `#ffffff` (canvas) |
| `--surface` | `#0f1011` | `#fafafc` (surface-pearl) |
| `--text` | `#f7f8f8` | `#1d1d1f` (ink) |
| `--text-2` | `#8a8f98` | `#6e6e73` (ink-muted) |
| `--border` | `rgba(255,255,255,.05)` | `#e0e0e0` (hairline) |
| `--link` | `#5e6ad2` | `#0066cc` (primary) |
| `--ink` (new) | `255, 255, 255` | `29, 29, 31` (ink) |
| `--input-text` (new) | `#d0d6e0` | `#1d1d1f` |
| `--input-ring` (new) | `rgba(0,0,0,.2)` | `transparent` |
| `--button-strong` / `-text` (new) | `#e5e5e6` / `#08090a` | `#0066cc` / `#ffffff` |
| `--selected-bg` / `-text` (new) | `rgba(255,255,255,.05)` / `--text` | primary at 10% / `#0066cc` |
| `--raised` (new, tooltips) | `#1c1d1f` | `#ffffff` |
| `--avatar-bg` (new) | `#3e3e44` | `#d2d2d7` (surface-chip-translucent) |
| `--ring` (new) | `rgba(0,0,0,.6)` | `#e0e0e0` (hairline) |
| `--well` / `--card` (new, board) | `--surface` / `rgba(255,255,255,.02)` | `#f5f5f7` (canvas-parchment) / `#ffffff` |

`--accent`, `--success` and `--danger` are the same in both (the light palette has no attention or status colors).

- Every `rgba(255, 255, 255, a)` tint (hover, overlay, inset border) becomes `rgba(var(--ink), a)`, so it turns into
  the same tint of black in light mode. Shadows and backdrops (`rgba(0, 0, 0, a)`) stay as they are.
- Selection: `:root` dark by default; `[data-theme="light"]` or, with no choice, `prefers-color-scheme: light` switch
  the overrides on. `color-scheme` follows, so native controls and scrollbars match.
- A custom status icon stored with the old started default `#f7f8f8` renders with `--text`, so it isn't white on white.

## Frontend

- **Theme choice:** `localStorage` key `taskwell:theme` (`light`, `dark`, absent for System). An inline script in each
  app's `index.html` sets `data-theme` on `<html>` before first paint, so there's no flash.
- **Setting:** Settings → Profile gets an "Appearance" section with System / Light / Dark tabs.
- **Landing page:** `landing.html` pins `data-theme="dark"`.
- `DESIGN.md` gets a "Light mode" section with the table above.

## Verification

- `npm run build`, `npm run check`.
- Click through in both themes: navbar and sidebar, issues list and board, issue page, notes, chat, settings, dialogs,
  popovers, tooltips, the editor, the admin app.
