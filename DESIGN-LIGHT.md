---
name: Taskwell light
colors:
  canvas: "#FFFFFF"
  surface: "#FAFAFC"
  well: "#F5F5F7"
  ink: "#1D1D1F"
  ink-muted: "#6E6E73"
  hairline: "#E0E0E0"
  primary: "#0066CC"
  on-primary: "#FFFFFF"
  chip: "#D2D2D7"
---

# Taskwell light mode

The light theme of the Taskwell app. `DESIGN.md` stays the source of truth for everything except color: typography,
the 8px spacing scale, radii, border widths, z-index, breakpoints, touch targets and component shapes are the same in
both themes. This file only says which color each role takes in light mode. The palette is Apple-inspired: a white
canvas, near-black ink, hairline dividers and a single Action Blue for everything interactive.

Implemented in `frontend/shared/src/styles/tokens.css` (`:root[data-theme='light']` and the `prefers-color-scheme`
fallback). Users choose System, Light or Dark in Settings → Profile → Appearance, or flip it with the navbar's sun/moon
button. The landing page is always dark.

## 1. Atmosphere

- Calm and bright: white canvas, white cards, structure drawn with hairlines rather than shadows.
- One interactive color. Links, focus rings, the strong button and the selected nav item and tab are all Action Blue.
  Nothing else is blue.
- Accent pink (`#F91880`), success green (`#00BA7C`) and danger red (`#EB5757`, buttons `#D61470`) keep their roles from
  `DESIGN.md` in both themes: unread counts, errors, overdue dates, completed status, destructive actions.

## 2. Color palette and roles

| Role | Token | Color |
|---|---|---|
| Page background | `--bg` | canvas `#FFFFFF` |
| Nested surface (panels, popovers, dialogs, inputs' container) | `--surface` | `#FAFAFC` |
| Well (board columns, anything that holds cards) | `--well` | `#F5F5F7` |
| Card inside a well | `--card` | `#FFFFFF` |
| Primary text | `--text` | ink `#1D1D1F` |
| Secondary text, captions, placeholders | `--text-2` | ink-muted `#6E6E73` |
| Input text | `--input-text` | ink `#1D1D1F` |
| Borders, dividers, control outlines | `--border`, `--ring` | hairline `#E0E0E0` |
| Links, focus rings, mentions | `--link`, `--focus` | primary `#0066CC` |
| Strong button | `--button-strong` / `--button-strong-text` | `#0066CC` / `#FFFFFF` (5.6:1) |
| Selected nav item and tab | `--selected-bg` / `--selected-text` | `#0066CC` at 10% / `#0066CC` |
| Tooltip | `--raised` | `#FFFFFF` with a hairline border |
| Avatar fallback, blockquote rule | `--avatar-bg` | chip `#D2D2D7` |
| Hover, pressed and overlay tints | `rgba(var(--ink), a)` | ink `29, 29, 31` at the dark theme's opacities (0.02 to 0.16) |
| Modal backdrop | | `rgba(0, 0, 0, 0.6)`, as in dark |

Contrast: ink on canvas is 16.8:1, white on primary 5.6:1, primary on canvas 5.6:1. Ink-muted on canvas is 5.1:1, so it passes the 4.5:1 body-text minimum in `DESIGN.md`.

## 3. Components

Shapes, sizes and padding are `DESIGN.md` section 4. In light mode:

- **Buttons**
  - Primary (default): ink at 5% on the page, ink at 8% on hover, a hairline outline. Text ink.
  - Secondary (strong): Action Blue fill, white text, ink mixed 6% darker on hover.
  - Ghost: no fill, ink-muted text; ink text and ink at 5% on hover.
  - Danger: `#D61470` fill, white text (unchanged).
- **Inputs**: ink at 2% fill, ink at 8% border, no outer ring. Hover border ink at 10%, focus border ink at 15% with
  ink at 4% fill; keyboard focus adds a 2px Action Blue ring inside the border (`--focus`). Placeholder ink-muted at 60%.
- **Checked controls** (checkboxes, checklist items, the selected date, a selected note): Action Blue fill with a white
  check.
- **Cards and containers**: `--surface` or white with a hairline border, radius 8px. No shadows on chrome.
- **Board**: columns are wells (`#F5F5F7`, hairline border); cards inside are white.
- **Shadows**: none on chrome (`--drop` is empty). Popovers, menus and tooltips float on a soft ink shadow
  (`--float`, ink at 8%, 0 8px 24px).
- **Popovers and dialogs**: `--surface` with a hairline outline.
- **Navigation**: the sidebar and navbar sit on the canvas. The current item is Action Blue text on a 10% Action Blue
  tint; others are ink-muted, ink on hover.
- **Tabs (segmented)**: hairline track; the indicator is a 10% Action Blue tint and the pressed tab's text is Action
  Blue.
- **Status icons**: started is ink, completed is success green, others ink-muted. A custom icon saved with the old
  near-white color shows as ink.
- **Chat**: highlighted and pressed reactions use Action Blue at 12 to 24%.
- **Notes**: color tints mix 16% of the note color into `--surface`, so they work on both themes.

## 4. Do and don't

- Do use the tokens; never hard-code `#FFF`, white or black tints. Tints are `rgba(var(--ink), a)`.
- Do keep Action Blue for interaction only. Don't use it for decoration, headings or status.
- Don't add drop shadows to fix separation; use a hairline or a well.
- Don't introduce colors outside this table and the shared accents above.
