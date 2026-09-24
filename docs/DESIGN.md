# Design system

Minimalist, monochrome, editorial. The interface should read like a
well-typeset technical document: every element carries information. If in
doubt, remove it.

Implementation: shadcn/ui (`radix-nova`) + Tailwind v4 only. Tokens are the
shadcn CSS variables in `web/app/globals.css`.

## Type

| Face | Utility | Use for |
|---|---|---|
| Fraunces (+ Source Serif 4 for Cyrillic) | `font-display` | wordmark, page `h1`, rare editorial lines (empty states, onboarding) |
| Geist | default (`font-sans`) | everything a user scans or clicks: nav, labels, buttons, tables |
| Geist Mono | `font-mono` | technical values: IPs, ports, keys, node IDs, versions, config |

Never mix serif and sans inside one text block. `font-display` sets weight
550, `-0.02em` tracking and `opsz 36`. shadcn's `font-heading` (card and sheet
titles) maps to sans on purpose: those titles are UI chrome.

## Colour

Near-monochrome. Light: white surfaces, `#FAFAFA` sidebar, `#18181B` text,
`#E5E5E5` hairlines. Dark: `#111113` surfaces, `#0A0A0A` sidebar, `#F5F5F5` text,
`#27272A` hairlines.

**One accent**, `--signal` (`bg-signal`, `text-signal`, `border-signal`): the
active-nav marker, status dots that need attention, critical alerts and
errors. `--destructive` is an alias for it. Healthy status dots are
`bg-foreground`, not green. No gradients, glass, or coloured shadows.

## Shape and depth

- Radius `--radius: 0.5rem` (8 px; `rounded-md` ≈ 6 px). Fully rounded only for
  badges and status dots.
- Elevation is a 1 px border or a slight tint, never a shadow. `shadow-sm` is
  allowed only on floating layers (dropdowns, popovers, sheets). The shadcn
  components were edited accordingly.
- Functional screens may be dense: tables use default shadcn cell padding.
  Don't pad data views like marketing pages.

## Icons and logo

- Lucide, 1.5 px stroke (set globally for `svg.lucide`), single colour
  inheriting `currentColor`, no icon backgrounds.
- The mark (`components/brand/logo.tsx`, `app/icon.svg`) is three nested
  arches receding toward the centre, drawn on Lucide's 24 px grid at the same
  1.5 px stroke with round caps. Always outlines, never filled.

## Tone of copy

Precise and quiet. State what is, not how the user should feel. Short
sentences, no exclamation marks, no emoji.
