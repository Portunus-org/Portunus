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
  time, for the interface. Titles (page headers, cards, drawers, dialogs,
  the wordmark, documentation headings) are set in Source Serif 4 through
  `--font-heading`: a serif for headings over a sans for the interface, in
  both languages. Numbers stay in the sans. Great Vibes only for the
  handwritten greeting on the welcome screen.
- Before building or changing an element, read its page in the shadcn docs
  (<https://ui.shadcn.com/docs/components.md>) and follow the documented
  example. Confirmations use Alert Dialog: the "Destructive" example for
  deletion, "Basic" for the rest.
- No em dashes and no middle dots as separators in interface text. Use a
  second muted line, a badge or plain punctuation. No placeholder dashes
  for missing values: a skeleton while loading, words otherwise.
- The theme and language switches live in the user menu only.
- Forms are built from shadcn's Field parts through `components/form-field.tsx`
  (`FormField`, `SwitchField`, `FormError`); choices in wizards are its
  "Choice Card". Action menus carry an icon per item, as in the docs.
- The frame never scrolls: the sidebar and the header stay, the page
  content scrolls under the header.
- Formatting is Prettier with `web/.prettierrc.json`; `components/ui` is
  excluded, registry components stay as installed.
- Text lives in `web/lib/messages.ts` (English and Russian); no strings in
  components.

## Tone

Short, plain sentences, neutral wording, no slang or jokes. Say what happens
and what the person can do. The
wizard makes decisions and explains them; it does not ask what it can find
out itself.
