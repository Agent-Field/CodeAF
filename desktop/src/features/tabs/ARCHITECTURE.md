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
- **`state.tabs` is the strip's order.** After every slice `workspaceReducer` applies `arrange` (helpers): pinned tabs first and in no group, each group's members one run, groups listed in run order. `visibleTabs` is that order less a collapsed group's hidden members: arrow keys and ⌘1–9 walk it. `stripItems` still cuts the full list, so a collapsed group keeps its pill when the selected tab is outside it; `MemberSlot` hides the members. Nothing re-sorts at render. Slices place tabs with `fitIndex`/`runOf` (never inside another group's run, never across the pinned boundary). Decisions TI1 to TI12 in `docs/DESIGN-QUESTIONS.md`.
- A closed tab is a `ClosedTab`: the tab plus `stood` (`before`, `after`, `group`; not `place`, which a Home uses for its place id) recorded by `close` and saved; `reopen`, `reopen-id` and `newtab-reopen` all go through `restore` in `reducers/tabs.ts`, which puts it back where it stood and remakes its group. `picked` (⌘-click picks for ⌘G) is window-local and never read back.
- **⌘Z** (`undo/`): `useStructuralUndo` wraps the workspace's dispatch (two lines in `Workspace.tsx`). An undoable action is predicted with the pure reducer, pinned to the ids it minted (`mint`, honoured by `workspaceReducer` through `mintWith`) and recorded in the window's 20-step stack (`structuralUndo.ts`, pure). The inverse of a close is Reopen; of anything else it is `undo-structure` (`reducers/undo.ts`): structure only, refused when the touched tabs changed since. A toast's own Undo and editors' native Undo win. Decisions TI15 to TI17.
- `workspaceReducer` composes slices: `reducers/tabs.ts` (new, open, open-task, select, pick, close, reopen, reopen-id, pin, rename, title, view, draft, reorder), `reducers/groups.ts` (group, group-picked, move-group, reorder-group, rename-group, collapse-group, ungroup), `reducers/split.ts` (split-merge, split-close-pane, split-focus, split-layout, split-unmerge, split-group). `view`, `draft`, `title`, `rename` and `select` accept a tab id OR a pane id.
- To add actions: new file `reducers/<lane>.ts` exporting `XAction` and `reduceX(state, action)` that returns `undefined` for foreign actions; add the type to `WorkspaceAction` and the function to `slices` in `model.ts` (two one-line edits). Add cases to your own tests in `<lane>.test.ts`, not `model.test.ts`.

## Inbox retirement (Iteration 2)

Inbox is no longer a kind or a pane. `kinds/retired.ts` drops its saved slots before
kind fallback in local and shared workspace readers. Other tabs, closed chats,
drafts and surviving split panes remain. An Inbox-only save opens one quiet New tab.
Old `ensure-inbox` actions replay as no-ops; shell `open-inbox` requests and
`codeaf://inbox` addresses start the existing Next up walker. Transfer offers ignore
retired slots. The historical Inbox references in lane ownership below are superseded
by this rule; `closing/background.ts` remains the data source for closing work.

## Kinds registry (`kinds/`)

`kinds/types.ts` lists the kinds (conversation, task, file, diff, web, terminal, settings, history, newtab, home). `kinds/registry.ts` maps each kind to a `KindDef` (`label`, `icon`, `backed`, `pane`, `preview`). No code switches on kind: ask `kindDef(kind)`.

- `pane` renders the body inside a card (`PaneRenderProps`: `pane`, `label`, `focused`, `split`, `actions`). `split` and `focused` are what the conversation lane needs for the compact 36px composer.
- `preview` is the hover-card/overview body slot (`PreviewRenderProps`), `null` until the preview lane fills it.
- Live kinds include conversation, task, newtab, history, file, diff, terminal, settings and web. History uses the engine `/history` routes. Web uses a native child view in the desktop app and an Open in browser line elsewhere; its address is `Pane.target.url`. Each kind owns its renderer and capability checks.
History search in the new-tab field offers two matches and See all; Ctrl/Cmd Enter opens the matching History query.

## Ownership by lane (a lane edits ONLY its files; shared files are listed last)

| Lane | Owns |
| --- | --- |
| hover preview | `hosts/previewHost.tsx`, each kind's `preview`, new `preview/` folder |
| menus and closing | `hosts/menuHost.tsx`, `actions.ts` (typed `TabActions`: link, move to window), `useWindowHandoff.ts` (claim and release, both halves of a move), `reducers/closing.ts` + `reducers/handoff.ts`, `closing/` (Alt stop, `closeMany` bulk closes behind one `restore-closed` Undo, `inboxFocus.ts` oldest-question focus, `background.ts` Inbox model, `useBackground.ts` world feed + signals, `failedSeen.ts` + `seenMarks.ts`: failure Seen is the engine's durable mark shared by every window, `docs/ENGINE.md` "Failed-task Seen"), `kinds/inbox.ts` + `kinds/inbox/`, `Tab.tsx` `closeMode` wiring in `TabItem.tsx`. The toast is shared: post with `toasts.show` from `design/toasts.ts`, drawn once by `ToastRegion` (undo is a slot of the toast, never a feature-drawn button). Seam for the rail: dispatch `{ type: 'open-inbox' }`. Seam for the shell: `<Workspace onOpenChat>` lets the Inbox open work that has no tab here. Closing never stops engine work; "Close and stop" calls the session stop endpoint through `closing/stopWork.ts` and a failure stays on screen (toast + Inbox). "Copy link" is ABSENT (no deep-link scheme exists); "Move to new window" is desktop-only and two-phase (the source tab leaves only when the new window claims it; an unclaimed handoff is reported once after `HANDOFF_CLAIM_WAIT_MS` and the tab stays). Shell seam: the `codeaf:shell-open` event with detail `inbox` (or `{type:'open-inbox'}`) opens the Inbox on its oldest question. A failed task lights the Inbox dot red (`data-kind="failed"`) and summons the Inbox; a question outranks it. |
| split panes | `reducers/split.ts`, `PaneGrid.tsx`, `panes.css`, `split-tab.css`, `hosts/dragHost.ts` (edge drops), `PaneHeader` controls |
| overview and filmstrip | `TabOverview.tsx` (the layer, bar, keys, cursor), `OverviewCard.tsx`, `OverviewFilmstrip.tsx`, `overview-model.ts` (pure order and filter, node test), `overview.css`, `specimens/OverviewSpecimen.tsx`. No `.overview-*` rules live in `styles/ui.css` any more. |
| new tab field | `kinds/newtab.ts` (+ its folder), switch `new` to kind `newtab` in `reducers/tabs.ts` |
| history tab | `kinds/history.ts`, new `features/history/` |
| rail and keys | `features/shell/Rail.tsx`, `rail.css`, `useTabKeys.ts`, `design/keyboard.ts` |
| conversation (A3) | `kinds/ConversationPane.tsx` callers, conversation header "Tasks 5/14" button (not built here: it would collide with `features/conversation/` files) |
| kind lanes (file, diff, web, terminal, settings, inbox) | their own `kinds/<kind>.ts`, flip `backed` when an engine/bridge source exists |

Shared, change with care and keep edits small: `tokens.json` (add keys, never rename), `Tab.tsx`/`tab.css` (the primitive: states are props), `model.ts` slice list, `TabItem.tsx` (three host calls), `Workspace.tsx`.

## Tab polish seams

- **Title tooltip** (`Tab.tsx` + `useTitleOverflow.ts`): the shared 500ms tooltip with the full title when the name is cut and no hover preview opens (the active tab, a compressed tab). An inactive tab's hover preview already holds its title, and no tooltip opens while any preview card is open. A split's segments still use `hosts/titleTooltipHost.tsx` through `SplitTab`'s `wrapSegment`.
- **⌘/Ctrl O** is ShortcutId `open-file` in `design/keyboard.ts`. The focused new-tab field runs the Open file… row's own pick. From any other tab, `useNewTabKeys` opens a New tab or focuses one already open, and the caption becomes "Type part of a file name." A dialog that is open keeps the chord.
- **Overview card press** (Interactions "Overview card · ⌘-click / middle: Background tab"): every card is a tab that is ALREADY open, so a ⌘/Ctrl-click or middle-click opens nothing: the active tab and the overlay stay as they are and only the cursor moves to the card (`backgroundPress` in `OverviewCard.tsx`, predicate `isBackgroundPress` in `overview-model.ts`). A plain click opens and closes the overview.
- **Overview card menu** is `tabMenuFor(api, tab)` from `hosts/menuHost.tsx`, the strip's own builder, passed down as `TabOverview`'s `menuFor`. There is no second menu array; the Inbox has no menu in either place. `ToastRegion` draws inside an open modal dialog so a toast posted from the overview is seen.
## Group suggestion

`offerRules.ts` (pure, node-tested: `findOffers`, canonical chat ids, the 30-day memory), `canonicalOffers.ts` (`canonicalGroupOffers`), `useGroupOffer.ts` (settled once-per-set ask, shown-this-launch, decide), `GroupOffer.tsx` (`GroupOffer` takes the `TabsApi`; `GroupOfferPill` is the drawing) and `group-offer.css`. It dispatches the reducer's own `group` action and owns no tab state. Decisions GO1 to GO8 in `docs/DESIGN-QUESTIONS.md`; wiring in the report `tab-group-offer-api.md` and `tab-group-offer-completion-api.md`.

## Primitives and specimens

`Tab` (30px, radius 8, 13px glyph, 12px title faded over its last 20px with a mask, close in a fixed 20px slot), `SplitTab`, `GroupCapsule` + `MemberSlot`, `LoadingLine` (2px, card top), `TabStrip` (46px). Every state has a row in `specimens/TabsSpecimen.tsx` on the Design system page; extend it with any new state. Specimens pass `specimen` so tabs render outside a tablist without a `tab` role.

## Frame (`App.tsx`, `App.css`)

`.app-shell` is the frame (`--frame` ground), `Rail` is 252px (`--sidebar-width`: the design's 232px row column plus 10px padding each side), `.content-pane` is the column with the strip and the card(s), padded 8px right and bottom. A workspace tab draws `.workspace-pane` cards (radius 10, canvas, sh-1); other pages sit in `.content-card`. A split adds a 40px `.pane-header` per pane and a 1.5px accent-soft ring on the focused pane.

## Window providers (`App.tsx`)

The shell div carries `data-providers="places next-up focus-history"`, outer to inner. `ThemeProvider` stays outside App, in `main.tsx`.

1. **PlacesShellProvider** — which place this window shows, and Go to. Focus history's restore calls that, so the shell is outside the history provider.
2. **NextUpProvider** — one `createNextUp()` for this window. Skip and a pending Accept stay on it. The strip (`TabStrip`'s frame slot) draws the frame pill and the banner from `useNextUp`, which reads this provider. App does not draw a second pill or banner: Focus mode hides the strip, and the pill hides with it.
3. **FocusHistoryProvider** — one wire per window label (`focusHistoryStorageKey`). Back and forward ask the shell to change place, then select the tab and put its drill back (a task id, or the tab's own page when the path is empty). The workspace uses this wire and does not keep a second stack.

An attention item from an engine that omits the newer fields (`blocking`, `stakes`, `suggestion`, `holdingUp`, place ids) still counts on the pill. A missing `blocking` is read as blocking the turn. The rail has no Inbox row; that count is the pill.

## Rules that bind every lane

Hover is a fill change only (120ms); press 80ms; focus ring for keyboard only; colour only on 6px glyphs (amber needs you, red failed); running is silent on a tab; fades are masks, never ellipses; menus are the shared `ContextMenu`/`DropdownMenu`; icons come from `components/ui/Icon.tsx` (lucide names missing from the animated set are noted in the lane report).

## Overview and filmstrip (design 2h, 3h, 3i)

`TabOverview` is a full-window `<dialog>` on the `--frame` ground: a 56px bar (search, Grid/Filmstrip, Done), then the grid or the filmstrip. Both read one list from `overview-model.ts` (Pinned, each group in strip order, Other tabs; the filter drops empty sections) and one cursor, so ← → ↑ ↓ ↵ and ⌘W mean the same in both. A card is the kind's `preview` slot when filled, else the draft or latest answer; the filmstrip mounts the kind's real `pane` renderer (inert, no-op actions) at 1040x660 under a 0.5 transform, for the centre card and three on each side. ⌘⇧\ (Ctrl Shift A) and the strip's grid icon open the layer; ⌘↑ is left to stepping turns. Ledger rows OV1 to OV16 in `docs/DESIGN-QUESTIONS.md` list every place the design was silent.

## Scroll restoration (`scroll/`, wired in `PaneGrid.tsx`)

Only the active tab is mounted, so a pane remounts empty on every return. `PaneBody` (in `PaneGrid.tsx`) calls `useScrollRestore(paneId, bodyRef, visible, layout)`: one capturing scroll listener records every nested scroller of the pane body (vertical and horizontal offsets, and whether it was at its end) into `ScrollMemory`, and a restore loop puts each back once its content is big enough. A wheel, touch, pointer or key from the reader ends the restore; a pane with no saved position is never touched, so a first-opened conversation still sticks to its end.

- **Names.** A scroller is named by `data-scroll-key` (the container's meaning: `conversation`, `task-page`, `file-body`, `history-list`, `history-results`, `history-read`, `settings`, `newtab`, ...). Add it to any new scroller. An unnamed nested output is named by tag, first class and what it says, never by its order in the pane.
- **Not DOM.** xterm owns its viewport, so the terminal registers a `Scroller` (`useScrollAdapter('terminal', ...)`) that reads and writes the buffer line through xterm's public API. It waits for the replay to grow the buffer to the saved line and never writes to the program.
- **Per window.** The memory lives in `localStorage` under `codeaf.desktop.tabScroll.v2.<namespace>`: the native window label, or for a browser a token in `sessionStorage`. Windows share tabs and drafts, never scroll (PLACES-ARCHITECTURE 3.4).
- **Closed tabs.** Panes that leave the strip are retired into a bounded ring (80 panes) so Reopen finds them. `PaneGrid` also takes `retainedPaneIds` (the panes of `state.closed`); when the workspace passes it, exactly those and the open tabs are kept.
