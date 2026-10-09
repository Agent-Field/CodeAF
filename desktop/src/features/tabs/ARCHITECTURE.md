# Tabs and shell: module map and seams

Design source: `.git/design-v3/v2/` (Shell 2a-2h and 3a-3l, Components "Tab · group · split tab", "Pane header · compact composer", `shell-helpers.js`). The design wins over older docs in this repo. Every number is a token in `src/design/tokens.json`; CSS holds no literals.

## Data flow

```
Workspace.tsx  (owns useReducer(workspaceReducer), summaries, dialogs; builds TabsApi)
 ├─ TabStrip.tsx ── TabItem.tsx ── Tab.tsx | SplitTab.tsx      (the strip, 46px)
 │                  └─ hosts/{previewHost,menuHost,dragHost}   (one function each, see ownership)
 │   GroupCapsule.tsx                                            (capsule, collapsed pill)
 ├─ PaneGrid.tsx ── kinds/registry.ts ── kinds/<kind>.ts        (the card; 1 pane or a split grid)
 ├─ TabOverview.tsx                                              (dialog; overview lane)
 └─ useTabKeys.ts                                                (keys + native menu events)
```

`model.ts` is pure (no React, no CSS) and has a node test, `model.test.ts` (`node --test src/features/tabs/model.test.ts`).

## The model (`model.ts`, `types.ts`, `helpers.ts`, `reducers/`)

- `Tab = Pane & { pinned, groupId?, split? }`. A plain tab IS one pane (its own `kind`, `title`, `draft`, session and route fields). A split tab is ONE merged tab: `split = { layout: '1x2' | '2x1' | '2x2', focus, panes: Pane[] }` with 2 to 4 panes, and its own content fields are unused. `panesOf(tab)` gives the panes of either; `focusedPane(tab)` the focused one. Pane ids are the ids of the tabs that were merged, so drafts, summaries and sessions keep their keys.
- Persisted under `codeaf.desktop.workspace.v1` (additive fields). A v1 save has no `kind`: every tab loads as a conversation. An invalid `split` is dropped and the tab stays plain; any invalid tab resets the workspace (as before).
- `workspaceReducer` composes slices: `reducers/tabs.ts` (new, open, select, close, reopen, pin, rename, title, view, draft, reorder), `reducers/groups.ts` (group, move-group, rename-group, collapse-group, ungroup), `reducers/split.ts` (split-merge, split-close-pane, split-focus, split-layout, split-unmerge, split-group). `view`, `draft`, `title`, `rename` and `select` accept a tab id OR a pane id.
- To add actions: new file `reducers/<lane>.ts` exporting `XAction` and `reduceX(state, action)` that returns `undefined` for foreign actions; add the type to `WorkspaceAction` and the function to `slices` in `model.ts` (two one-line edits). Add cases to your own tests in `<lane>.test.ts`, not `model.test.ts`.

## Kinds registry (`kinds/`)

`kinds/types.ts` lists the kinds (conversation, task, file, diff, web, terminal, settings, history, newtab, inbox). `kinds/registry.ts` maps each kind to a `KindDef` (`label`, `icon`, `backed`, `pane`, `preview`). No code switches on kind: ask `kindDef(kind)`.

- `pane` renders the body inside a card (`PaneRenderProps`: `pane`, `label`, `focused`, `split`, `actions`). `split` and `focused` are what the conversation lane needs for the compact 36px composer.
- `preview` is the hover-card/overview body slot (`PreviewRenderProps`), `null` until the preview lane fills it.
- Live now: conversation, task (both `ConversationPane`), newtab (stand-in, `new` still opens a conversation), history (`features/history/HistoryPane`, backed by the engine's `/history` routes). The rest are `placeholderPane(...)` and `backed: false`; they are specimens only and nothing in the live app opens them (engine-truth law).

## Ownership by lane (a lane edits ONLY its files; shared files are listed last)

| Lane | Owns |
| --- | --- |
| hover preview | `hosts/previewHost.tsx`, each kind's `preview`, new `preview/` folder |
| menus and closing | `hosts/menuHost.tsx`, `closing/` (toast, Alt stop), `reducers/closing.ts`, `Tab.tsx` `closeMode` wiring in `TabItem.tsx` |
| split panes | `reducers/split.ts`, `PaneGrid.tsx`, `panes.css`, `split-tab.css`, `hosts/dragHost.ts` (edge drops), `PaneHeader` controls |
| overview and filmstrip | `TabOverview.tsx`, `overview.css`, `styles/ui.css` `.overview-*` |
| new tab field | `kinds/newtab.ts` (+ its folder), switch `new` to kind `newtab` in `reducers/tabs.ts` |
| history tab | `kinds/history.ts`, new `features/history/` |
| rail and keys | `features/shell/Rail.tsx`, `rail.css`, `useTabKeys.ts`, `design/keyboard.ts` |
| conversation (A3) | `kinds/ConversationPane.tsx` callers, conversation header "Tasks 5/14" button (not built here: it would collide with `features/conversation/` files) |
| kind lanes (file, diff, web, terminal, settings, inbox) | their own `kinds/<kind>.ts`, flip `backed` when an engine/bridge source exists |

Shared, change with care and keep edits small: `tokens.json` (add keys, never rename), `Tab.tsx`/`tab.css` (the primitive: states are props), `model.ts` slice list, `TabItem.tsx` (three host calls), `Workspace.tsx`.

## Primitives and specimens

`Tab` (30px, radius 8, 13px glyph, 12px title faded over its last 20px with a mask, close in a fixed 20px slot), `SplitTab`, `GroupCapsule` + `MemberSlot`, `LoadingLine` (2px, card top), `TabStrip` (46px). Every state has a row in `specimens/TabsSpecimen.tsx` on the Design system page; extend it with any new state. Specimens pass `specimen` so tabs render outside a tablist without a `tab` role.

## Frame (`App.tsx`, `App.css`)

`.app-shell` is the frame (`--frame` ground), `Rail` is 232px (`--sidebar-width`), `.content-pane` is the column with the strip and the card(s), padded 8px right and bottom. A workspace tab draws `.workspace-pane` cards (radius 10, canvas, sh-1); other pages sit in `.content-card`. A split adds a 40px `.pane-header` per pane and a 1.5px accent-soft ring on the focused pane.

## Rules that bind every lane

Hover is a fill change only (120ms); press 80ms; focus ring for keyboard only; colour only on 6px glyphs (amber needs you, red failed); running is silent on a tab; fades are masks, never ellipses; menus are the shared `ContextMenu`/`DropdownMenu`; icons come from `components/ui/Icon.tsx` (lucide names missing from the animated set are noted in the lane report).
