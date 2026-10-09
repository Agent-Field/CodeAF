# Tabs and shell: module map and seams

Design source: `.git/design-v3/v2/` (Shell 2a-2h and 3a-3l, Components "Tab · group · split tab", "Pane header · compact composer", `shell-helpers.js`). The design wins over older docs in this repo. Every number is a token in `src/design/tokens.json`; CSS holds no literals.

## Data flow

```
Workspace.tsx  (owns useReducer(workspaceReducer), summaries, dialogs; builds TabsApi)
 ├─ TabStrip.tsx ── TabItem.tsx ── Tab.tsx | SplitTab.tsx      (the strip, 46px)
 │                  └─ hosts/{previewHost,menuHost,dragHost}   (one function each, see ownership)
 │   GroupCapsule.tsx                                            (capsule, collapsed pill)
 ├─ PaneGrid.tsx ── kinds/registry.ts ── kinds/<kind>.ts        (the card; 1 pane or a split grid)
 ├─ TabOverview.tsx ── OverviewCard.tsx | OverviewFilmstrip.tsx   (full-window layer: grid or filmstrip; overview lane)
 └─ useTabKeys.ts                                                (keys + native menu events)
```

`model.ts` is pure (no React, no CSS) and has a node test, `model.test.ts` (`node --test src/features/tabs/model.test.ts`).

## The model (`model.ts`, `types.ts`, `helpers.ts`, `reducers/`)

- `Tab = Pane & { pinned, groupId?, split? }`. A plain tab IS one pane (its own `kind`, `title`, `draft`, session and route fields). A split tab is ONE merged tab: `split = { layout: '1x2' | '2x1' | '2x2', focus, panes: Pane[] }` with 2 to 4 panes, and its own content fields are unused. `panesOf(tab)` gives the panes of either; `focusedPane(tab)` the focused one. Pane ids are the ids of the tabs that were merged, so drafts, summaries and sessions keep their keys.
- Persisted under `codeaf.desktop.workspace.v1` (additive fields). A v1 save has no `kind`: every tab loads as a conversation. An invalid `split` is dropped and the tab stays plain; any invalid tab resets the workspace (as before).
- **`state.tabs` is the strip's order.** After every slice `workspaceReducer` applies `arrange` (helpers): pinned tabs first and in no group, each group's members one run, groups listed in run order. `visibleTabs` is that order less a collapsed group's hidden members, and `stripItems` cuts it into loose tabs and group runs for `TabStrip`; nothing re-sorts at render. Slices place tabs with `fitIndex`/`runOf` (never inside another group's run, never across the pinned boundary). Decisions TI1 to TI12 in `docs/DESIGN-QUESTIONS.md`.
- **⌘Z** (`undo/`): `useStructuralUndo` wraps the workspace's dispatch (two lines in `Workspace.tsx`). An undoable action is predicted with the pure reducer, pinned to the ids it minted (`mint`, honoured by `workspaceReducer` through `mintWith`) and recorded in the window's 20-step stack (`structuralUndo.ts`, pure). The inverse of a close is Reopen; of anything else it is `undo-structure` (`reducers/undo.ts`): structure only, refused when the touched tabs changed since. A toast's own Undo and editors' native Undo win. Decisions TI15 to TI17.
- A closed tab is a `ClosedTab`: the tab plus `place` (`before`, `after`, `group`) recorded by `close` and saved; `reopen`, `reopen-id` and `newtab-reopen` all go through `restore` in `reducers/tabs.ts`, which puts it back where it stood and remakes its group. `picked` (⌘-click picks for ⌘G) is window-local and never read back.
- `workspaceReducer` composes slices: `reducers/tabs.ts` (new, open, open-task, select, pick, close, reopen, reopen-id, pin, rename, title, view, draft, reorder), `reducers/groups.ts` (group, group-picked, move-group, reorder-group, rename-group, collapse-group, ungroup), `reducers/split.ts` (split-merge, split-close-pane, split-focus, split-layout, split-unmerge, split-group). `view`, `draft`, `title`, `rename` and `select` accept a tab id OR a pane id.
- To add actions: new file `reducers/<lane>.ts` exporting `XAction` and `reduceX(state, action)` that returns `undefined` for foreign actions; add the type to `WorkspaceAction` and the function to `slices` in `model.ts` (two one-line edits). Add cases to your own tests in `<lane>.test.ts`, not `model.test.ts`.

## Kinds registry (`kinds/`)

`kinds/types.ts` lists the kinds (conversation, task, file, diff, web, terminal, settings, history, newtab, inbox). `kinds/registry.ts` maps each kind to a `KindDef` (`label`, `icon`, `backed`, `pane`, `preview`). No code switches on kind: ask `kindDef(kind)`.

- `pane` renders the body inside a card (`PaneRenderProps`: `pane`, `label`, `focused`, `split`, `actions`). `split` and `focused` are what the conversation lane needs for the compact 36px composer.
- `preview` is the hover-card/overview body slot (`PreviewRenderProps`), `null` until the preview lane fills it.
- Live now: conversation, task (both `ConversationPane`), newtab (`NewTabPane`, the command field: `new` opens it, see `kinds/newtab/` and `reducers/newtab.ts`). The rest are `placeholderPane(...)` and `backed: false`; they are specimens only and nothing in the live app opens them (engine-truth law).

## Ownership by lane (a lane edits ONLY its files; shared files are listed last)

| Lane | Owns |
| --- | --- |
| hover preview | `hosts/previewHost.tsx`, each kind's `preview`, new `preview/` folder |
| menus and closing | `hosts/menuHost.tsx`, `actions.ts` (typed `TabActions`: link, move to window), `useWindowHandoff.ts` (claim and release, both halves of a move), `reducers/closing.ts` + `reducers/handoff.ts`, `closing/` (Alt stop, `background.ts` Inbox model, `useBackground.ts` world feed + signals, `failedSeen.ts`), `kinds/inbox.ts` + `kinds/inbox/`, `Tab.tsx` `closeMode` wiring in `TabItem.tsx`. The toast is shared: post with `toasts.show` from `design/toasts.ts`, drawn once by `ToastRegion` (undo is a slot of the toast, never a feature-drawn button). Seam for the rail: dispatch `{ type: 'open-inbox' }`. Seam for the shell: `<Workspace onOpenChat>` lets the Inbox open work that has no tab here. Closing never stops engine work; "Close and stop" calls the session stop endpoint through `closing/stopWork.ts` and a failure stays on screen (toast + Inbox). "Copy link" is ABSENT (no deep-link scheme exists); "Move to new window" is desktop-only and two-phase (the source tab leaves only when the new window claims it). |
| split panes | `reducers/split.ts`, `PaneGrid.tsx`, `panes.css`, `split-tab.css`, `hosts/dragHost.ts` (edge drops), `PaneHeader` controls |
| overview and filmstrip | `TabOverview.tsx` (the layer, bar, keys, cursor), `OverviewCard.tsx`, `OverviewFilmstrip.tsx`, `overview-model.ts` (pure order and filter, node test), `overview.css`, `specimens/OverviewSpecimen.tsx`. No `.overview-*` rules live in `styles/ui.css` any more. |
| new tab field | `kinds/newtab.ts` (+ its folder), switch `new` to kind `newtab` in `reducers/tabs.ts` |
| history tab | `kinds/history.ts`, new `features/history/` |
| rail and keys | `features/shell/Rail.tsx`, `rail.css`, `useTabKeys.ts`, `design/keyboard.ts` |
| conversation (A3) | `kinds/ConversationPane.tsx` callers, conversation header "Tasks 5/14" button (not built here: it would collide with `features/conversation/` files) |
| kind lanes (file, diff, web, terminal, settings, inbox) | their own `kinds/<kind>.ts`, flip `backed` when an engine/bridge source exists |

Shared, change with care and keep edits small: `tokens.json` (add keys, never rename), `Tab.tsx`/`tab.css` (the primitive: states are props), `model.ts` slice list, `TabItem.tsx` (three host calls), `Workspace.tsx`.

## Tab polish seams

- **Title tooltip** (`hosts/titleTooltipHost.tsx`, the strip's third `wrapSelect` host beside preview and menu): the shared 500ms `useTooltip` with the full title for the active tab, a pinned tab (icon only) and a cut title. An inactive tab's hover preview already holds its title, and no tooltip opens while any preview card is open, so the two never overlap. A split's segments use `SplitTab`'s `wrapSegment`.
- **⌘/Ctrl O** is ShortcutId `open-file` in `design/keyboard.ts`. Only the focused new-tab field registers for it (surface layer) and it runs the Open file… row's own pick; no other surface claims it.
- **Overview card press** (Interactions "Overview card · ⌘-click / middle: Background tab"): every card is a tab that is ALREADY open, so a ⌘/Ctrl-click or middle-click opens nothing: the active tab and the overlay stay as they are and only the cursor moves to the card (`backgroundPress` in `OverviewCard.tsx`, predicate `isBackgroundPress` in `overview-model.ts`). A plain click opens and closes the overview.
- **Overview card menu** is `tabMenuFor(api, tab)` from `hosts/menuHost.tsx`, the strip's own builder, passed down as `TabOverview`'s `menuFor`. There is no second menu array; the Inbox has no menu in either place. `ToastRegion` draws inside an open modal dialog so a toast posted from the overview is seen.

## Primitives and specimens

`Tab` (30px, radius 8, 13px glyph, 12px title faded over its last 20px with a mask, close in a fixed 20px slot), `SplitTab`, `GroupCapsule` + `MemberSlot`, `LoadingLine` (2px, card top), `TabStrip` (46px). Every state has a row in `specimens/TabsSpecimen.tsx` on the Design system page; extend it with any new state. Specimens pass `specimen` so tabs render outside a tablist without a `tab` role.

## Frame (`App.tsx`, `App.css`)

`.app-shell` is the frame (`--frame` ground), `Rail` is 232px (`--sidebar-width`), `.content-pane` is the column with the strip and the card(s), padded 8px right and bottom. A workspace tab draws `.workspace-pane` cards (radius 10, canvas, sh-1); other pages sit in `.content-card`. A split adds a 40px `.pane-header` per pane and a 1.5px accent-soft ring on the focused pane.

## Rules that bind every lane

Hover is a fill change only (120ms); press 80ms; focus ring for keyboard only; colour only on 6px glyphs (amber needs you, red failed); running is silent on a tab; fades are masks, never ellipses; menus are the shared `ContextMenu`/`DropdownMenu`; icons come from `components/ui/Icon.tsx` (lucide names missing from the animated set are noted in the lane report).

## Overview and filmstrip (design 2h, 3h, 3i)

`TabOverview` is a full-window `<dialog>` on the `--frame` ground: a 56px bar (search, Grid/Filmstrip, Done), then the grid or the filmstrip. Both read one list from `overview-model.ts` (Pinned, each group in strip order, Other tabs; the filter drops empty sections) and one cursor, so ← → ↑ ↓ ↵ and ⌘W mean the same in both. A card is the kind's `preview` slot when filled, else the draft or latest answer; the filmstrip mounts the kind's real `pane` renderer (inert, no-op actions) at 1040x660 under a 0.5 transform, for the centre card and three on each side. ⌘⇧\ (Ctrl Shift A) and the strip's grid icon open the layer; ⌘↑ is left to stepping turns. Ledger rows OV1 to OV16 in `docs/DESIGN-QUESTIONS.md` list every place the design was silent.
