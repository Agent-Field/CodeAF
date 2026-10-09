# Places components: ambiguous decisions

Decisions made while building `src/features/places/components/` where the design (Places, Components, Interactions) is silent or disagrees with itself. Each is a conservative assumption, kept here instead of the shared ledger so lanes do not collide. Change the token or the rule, not the test, when the designer answers.

| # | Question | Assumption taken |
| --- | --- | --- |
| Q-T1 | Tile and row hover/press fills are not drawn. | Hover fills `--field`, press `--field-2` (the shared Row recipe); the selected chat row is `--field`, and hovering it is `--field-2`. A tile in drop-target state keeps its soft accent fill on hover. |
| Q-T2 | Swatch colours are literals in the mock, identical for light and dark. | One value per tint, defined for both themes (`places-swatch-*`). Graphite is `oklch(.55 .012 250)` from Components. |
| Q-T3 | Home text in the mock ends in an ellipsis; the shell standard and the brief say long text fades under a mask. | Mask (`places-text-fade` = 20px, the tab title fade). No ellipsis anywhere. |
| Q-T4 | The design draws no model on a chat row. | `ChatRow` carries `model` as `data-model` only and draws nothing. |
| Q-T5 | Heading line height is not stated. | 1.2 (`type-home-title-leading`). |
| Q-T6 | The menu button icon is 15px; the icon scale has 14 and 16. | 14 (`sm`). No new size. |
| Q-T7 | Home tile grid is exactly four columns in a 680px column. | `repeat(auto-fill, minmax(140px, 1fr))`, which gives 4 at 632px of content and wraps to fewer below. Components draws `150px`; at 150 the Home column would hold three. |
| Q-T8 | Dragging a row or tile. | Source drops to `--opacity-subtle` (0.7). Drop target is the drawn "Add here" state. MIME types and the nest/add/move semantics (Interactions vs 8g disagree on ⌥) belong to the dnd lane; the components only expose native drag callbacks. |
| Q-T9 | Tint inherited vs overridden has no distinct look. | Same look. `tintSource` is carried as `data-tint-source`. |
| Q-T10 | Chat glyph icon. | The registry name `tab` (Lucide message-square) already is the glyph; no `Icon.tsx` change. |
| Q-T11 | Inline create: where the tint picker sits. | Five swatches (no Graphite) above the field, as drawn in Components. Radiogroup with arrow keys. Enter on an empty name does nothing; the unique-name check is the caller's (`invalid`). |
| Q-T12 | Inline rename (Q-14). | `PlaceHeading renaming` swaps the title for a field. While renaming the menu button is not drawn. The field reclaims focus from the menu's restore for one second. A shared `onCloseAutoFocus` on `DropdownMenu` would remove that workaround. |
| Q-T13 | Space on a tile. | Quick Look when `onQuickLook` is given; otherwise an ordinary press. ⌘/Ctrl-Enter and ⌘/Ctrl-click and middle click open in a new window. |
| Q-T14 | Tab order in the tile grid. | Every tile is a tab stop; arrows, Home and End are an accelerator, not roving tabindex. |
| Q-T15 | The 12px swatch pickers are below the 24px target size. | Kept as drawn; WCAG 2.2 target size is outside the suite's tags. Revisit with the designer for coarse pointers. |

## Shared edits outside the new folder

Additive only: `tokens.json` (`places-*`, `type-home-title-*`, `type-section-label-*`, theme `places-swatch-*`), regenerated `tokens.css`, `HomeTitle` and `SectionLabel` in `Typography.tsx` and `index.ts`, two type rules in `ui.css`, and the `test:places-components` script.
