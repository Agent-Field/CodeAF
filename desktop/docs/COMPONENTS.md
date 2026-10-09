# Conversation components and token roles (design v3)

The designer's foundations live in `src/design/tokens.json`; `npm run design:generate`
writes them to `src/styles/tokens.css`. Values are the designer's exact oklch,
shadow, type, space, radius and motion values. Use only the names below in
conversation CSS. Never write a literal value.

## Tint

`--h` and `--a` carry the space tint (hue and accent chroma). The default is
`tide`. Set `data-tint="sand|sage|tide|iris|rose|graphite"` on any container;
every tinted role recomputes inside it. Do not read `--h`/`--a` directly.

## Colour roles (light and dark)

| Role | Use |
| --- | --- |
| `canvas` | Page ground of the conversation |
| `frame` | Space tint for the window frame (later, chrome) |
| `surface` | Cards, tray, composer, sheets |
| `field` | Chips, inputs, quiet buttons; hover fill |
| `field-2` | Hover on a field fill, selected surface, pressed fill |
| `bubble` | The person's messages and notes |
| `line` | Hairlines |
| `guide` | Work guide rule (line at 60% / 70%) |
| `ink` | Conversation rhythm text |
| `ink-2` | Work rhythm, secondary text |
| `ink-3` | Non-text only: icons, marks, separators, guides, strip fills, disabled. Text of any size, placeholders included, uses `ink-2` (ink-3 is 3.1 to 4.5:1 on canvas, surface, field and bubble, under WCAG AA 4.5:1) |
| `accent` | Primary action, running mark, links |
| `accent-ink` | Text on accent |
| `accent-soft` | Selection, focus halo, suggested tag |
| `accent-rule` | Update rule |
| `amber`, `amber-soft` | Waiting on you, your call |
| `danger`, `danger-soft` | Failed, irreversible, deletions |
| `success` | Diff additions only; Done stays muted |
| `term` | Terminal output, code blocks |
| `sh-1`, `sh-2`, `sh-3` | Depth: rest, dock/tooltip, sheet |

Status marks: `mark-running`, `mark-queued`, `mark-done`, `mark-waiting`,
`mark-failed`, `mark-stopped`, `mark-paused`, `mark-incomplete`; geometry
`mark-box` (14), `mark-dot` (6), `mark-ring` (8), `mark-ring-width` (1.5).
Step category icons use `step-category` (ink-2), sizes `icon-xs` 13 /
`icon-sm` 14 in work rows and `icon-md` 16 in chrome.

## Type, space, radius, motion

- Fonts: `sans`, `mono`.
- Type roles, each with `-size`, `-leading`, `-weight` (and `-tracking` where the
  designer set one): `type-title` 20/1.3/600/-.015em, `type-h1` 17/1.35/600/-.01em,
  `type-h2` 15/1.4/600, `type-h3` 13/1.5/600, `type-prose` 13/1.6/400,
  `type-label` 12/1.45/400 (`type-label-strong` 500), `type-caption` 11/1.4/500,
  `type-mono` 12/1.5/400, `type-mono-small-size` 11 (use `font-variant-numeric: tabular-nums`).
- Space: `space-1` 4, `space-2` 8, `space-3` 12, `space-4` 16, `space-5` 20,
  `space-6` 24, `space-8` 32, `space-12` 48. Rhythm: `space-turn` 32,
  `space-answer` 14, `space-work-row` 4, `space-work-block` 10, `work-indent` 20,
  `chat-column-max-width` 680, `row-h` 28, `hit` 32, `panel` 300.
- Radius: `radius-chip` 6, `radius-control` 8, `radius-card` 12,
  `radius-bubble` 16, `radius-dock` 20 (`radius-composer` points at it).
- Motion: `ease`, `spring`; durations `dur-press` 80, `dur-fast` 120 (hover,
  hover actions, Copy), `dur-base` 200 (folds, chevrons, tab switch),
  `dur-slow` 320 (sheets, task panel, lightbox), `dur-spring` 380 with `spring`
  (tray rising, new bubble). Reduced motion zeroes every `dur-*`.
- Control geometry: `control-height` 28, `control-pad` 12, `control-pad-primary` 14,
  `control-pad-ghost` 10, `field-height` 30, `field-pad` 10, `segment-*`,
  `tag-*`, `row-height-v3` 30, `row-pad` 8, `row-action-size` 24,
  `row-action-icon` 12, `message-action-size` 26, `message-action-icon` 13,
  `focus-ring-width` 2, `focus-halo-width` 6, `opacity-control-disabled` 0.4,
  `hairline` .5px.

## Legacy names

- Chrome (sidebar, tab strip, overview, menus, palette) keeps its previous look
  through `chrome-canvas`, `chrome-surface`, `chrome-field`, `chrome-accent`,
  `chrome-accent-soft`, `chrome-danger` and `chrome-ease`. Do not use these in
  the conversation.
- Mapped onto new roles (migrate to the new name when you touch the file):
  `work-guide` → `guide`, `convo-update-rule` → `accent-rule`,
  `attention` → `amber`, `composer-surface` → `surface`.
- Still legacy and shared with chrome: `text`, `muted`, `border`,
  `overlay-surface`, `control-*`, `focus-ring`, `warning`. In conversation CSS
  replace them with `ink`, `ink-2`/`ink-3`, `line`, `surface`, `field`/`field-2`,
  the shared focus ring, and `amber`.

## Shared controls (src/components/ui/, owned by the foundations lane)

Import from `src/components/ui`. Features compose these; they never restyle them.

| Design component | Primitive | File | Token roles |
| --- | --- | --- | --- |
| Button · Primary ("Allow once") | `<Button variant="primary">` | Button.tsx | accent, accent-ink, brightness-hover/press |
| Button · Raised ("Always…") | `<Button variant="raised">` | Button.tsx | surface, sh-1; hover field |
| Button · Quiet ("Deny", the safe choice) | `<Button variant="quiet">` | Button.tsx | field; hover/press field-2 |
| Button · Ghost ("Later"; default, bare rows) | `<Button>` / `variant="ghost"` | Button.tsx | ink-2; hover field + ink; press field-2 |
| Button · Danger ("Stop task") | `<Button variant="danger">` | Button.tsx | danger-soft, danger |
| Icon button (toolbar) | `<IconButton label icon iconSize="sm">` | Button.tsx | ink-2; hover field + ink; press field-2 |
| Row action (24px, in a row) | `<IconButton size="row" iconSize="xs">` inside `<RowActions>` | Button.tsx, RowActions.tsx | hover field-2 |
| Message action (26px: Copy, Edit) | `<IconButton size="message" iconSize="xs">` inside `<RowActions>` on a `data-actions-host` element | Button.tsx, RowActions.tsx | ink-3; hover field + ink |
| Tooltip | built into IconButton (text = `title` or `label`); `useTooltip` for other icon-only triggers; `<TruncatedText>` for cut text | Tooltip.tsx | surface, ink, sh-2, type-caption-size |
| Segmented | `<Segmented label options value onChange>` | Segmented.tsx | field track, surface + sh-1 chosen |
| Field | `<TextInput appearance="field">` | TextInput.tsx | field; focus surface + accent ring + accent-soft halo |
| Tags (Suggested, Reversible, Irreversible, key) | `<Tag tone="accent" / "neutral" / "danger" / "key">` | Chip.tsx | accent-soft, field, danger-soft, surface + line |
| Chip base (file chip, link chip) | `<Chip>` static, `<ChipButton>` interactive, `muted` for missing/outside | Chip.tsx | field, hover field-2, ink, ink-2 (muted: ink-2, text only) |
| Status mark (Running … Incomplete) | `<StatusMark status label>` | StatusMark.tsx | mark-* roles |
| Row · tree, table (rest none, hover field, selected field) | `<Row selected>` with `<RowActions>` | RowActions.tsx | field, radius-control |
| Copy feedback | `<CopyButton>` | CopyButton.tsx | (IconButton) |
| Menus, Select, delayed tab preview | `DropdownMenu`, `ContextMenu`, `Select`, `HoverPreview` | Menu.tsx, Select.tsx, HoverPreview.tsx | legacy chrome roles (not restyled in v3 yet) |

States are built in: hover changes only the fill, press darkens one step for
`dur-press`, focus-visible is the 2px accent ring with the 4px halo (keyboard
only), disabled is `opacity-control-disabled` on the whole control. Do not add
per-feature hover, focus or disabled CSS for these controls.

## What each downstream lane reuses

- **turns**: `Row`/`RowActions` with `IconButton size="message"` for message
  actions (host element gets `data-actions-host`), `Chip` for attachments,
  `bubble`, `ink`, `space-turn`, `space-answer`, `radius-bubble`, `type-prose-*`.
  Folded turn rows use `Button` ghost restyled only by layout.
- **work**: `StatusMark` for step marks (replace `features/conversation/StateMark.tsx`
  icon marks), `step-category` colour with `icon-xs`/`icon-sm`, `guide`,
  `work-indent`, `space-work-row`, `space-work-block`, `term` for output,
  `type-mono-*`, `TruncatedText` for cut step titles.
- **tray**: `Button` primary/raised/quiet/ghost/danger (the tray's
  `optionVariant` already returns primary/raised/quiet), `Tag`, `Segmented`,
  `TextInput appearance="field"`, `surface` + `sh-2`, `amber`/`amber-soft`.
- **composer**: `IconButton` (attach, send/stop), `Button`, `Chip`, `surface`,
  `sh-2`, `radius-dock`, the field focus ring.
- **tasks**: `Row`, `RowActions`, `StatusMark`, `panel`, `row-h`, `sh-3` for the
  narrow sheet.
- **assets**: build FileChip and LinkChip on `Chip`/`ChipButton` (they still carry
  their own CSS today), `field-2` for hover, `success`/`danger` for +/− stats,
  `sh-3` and `dur-slow` for sheets and the lightbox.
