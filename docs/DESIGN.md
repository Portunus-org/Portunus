# Design

The interface is shadcn/ui's `dashboard-01` block, used as shipped:
components are not edited locally, so `shadcn add … --overwrite` keeps
producing the same result. When in doubt, compare with a fresh copy:

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
- On a phone the settings take the whole screen, as a phone app's do: the
  sections as a grouped list (with a short state under some), a section on
  a screen of its own with a way back, its settings in one rounded card and
  its button at full width below.
- On a phone (below md) a table is a list of cards: what a row is called
  and its actions on top, the other columns under it as named values, an
  empty value left out. Fields that stand side by side on a wide screen
  stand one under another. Forms stand in the upper part of the screen and
  the field being typed in is brought above the keyboard (KeyboardAware).
- An error that is about one field is shown at that field: the control is
  outlined in red (aria-invalid) and the words are right under it
  (`useFormErrors`, which places the server's refusals by the field they
  name). Only what belongs to no field is said at the foot of the form.
- Text lives in `web/locales/<code>.json` (English and Russian), one file per
  language with the same keys; no strings in components.

## Tone

Short, plain sentences, neutral wording, no slang or jokes. Say what happens
and what the person can do. The
wizard makes decisions and explains them; it does not ask what it can find
out itself.
