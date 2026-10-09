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
| `ink-3` | Meta: durations, directories, eyebrows |
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
