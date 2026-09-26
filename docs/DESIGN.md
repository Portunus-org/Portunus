# Design

The interface is shadcn/ui's `dashboard-01` block, used as shipped
(DECISIONS D-22). When in doubt, compare with a fresh copy:

```sh
npx shadcn@latest add dashboard-01
```

## Rules

- Components in `web/components/ui/` are the registry versions, unmodified.
  Update them with `npx shadcn@latest add <name> --overwrite`.
- Page composition follows the block: inset sidebar, header with the page
  title, section cards, data tables with view tabs, drawers for details.
- Theme tokens are the `radix-nova` neutral set in `web/app/globals.css`.
- Status is shown with outline badges and an icon, as in the block
  (green check for healthy, spinner for pending).
- Fonts: Geist and Geist Mono (Latin and Cyrillic), self-hosted at build
  time. Great Vibes only for the handwritten greeting on the welcome screen.
- Text lives in `web/lib/messages.ts` (English and Russian); no strings in
  components.

## Tone

Short, plain sentences. Say what happens and what the person can do. The
wizard makes decisions and explains them; it does not ask what it can find
out itself.
