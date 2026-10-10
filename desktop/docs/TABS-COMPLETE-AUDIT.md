# Complete tabs audit

Status: **baseline audit of `199bf66c6`. Not a final acceptance.** The root integrator hands over the merged commit
(menus, history and web lanes integrated, fixes below applied); the same specs are then re-run and every row below is
re-scored. Until then nothing that exists only in an unintegrated candidate is counted as passing.

- **Design source:** the current design ZIP, read-only: `codeaf Shell` (blocks 2b, 2d, 2g, 2h, 3a–3l, 4a–4e),
  `codeaf Components` ("Tab · group · split tab", "Shell · menus", "Pane header · compact composer", "Edge states"),
  `codeaf Interactions` (Shell, Places/History, Flows, Shortcuts). Design ids below (`S-2h-7`, `S-3g-4`, `S-IX-4`…)
  are the per-item ids of the shell design inventory; `IX` is the Interactions page.
- **Code baseline:** `199bf66c6` ("Keep Home suggestion icon inside valid flow markup").
- **Candidates inspected read-only (not merged, not scored as passing):** `07f0b63bc` (tab and group menus, close vs
  close-and-stop, closing toast, Inbox), `2d26ba62a` (History tab, archive toast), `0ff19c89e` (native web tabs), and **(superseded attention scope: I2.1/I2.6)**
  the live Places shell lane (place Home tab, per-place workspace keys).
- **Evidence:** Playwright in Chromium **and** WebKit, plus the pure node tests. New audit journeys live in
  `desktop/tests/tabs-audit/` and run with `npx playwright test --config playwright.tabs-audit.config.ts` (port 1759,
  polling watcher, one worker). They assert the design, so on the baseline the failing ones are the reproductions of
  their findings; they are kept out of `tests/ui` so they do not redden the default gate before the fixes land.
- **Input honesty:** every journey uses real pointer and keyboard input. Drags are real mouse drags (Playwright's native
  HTML drag-and-drop in both engines); **no synthetic DragEvent is used anywhere in the audit.** Axe runs unmodified
  through the shared `expectAccessible` contract (only the existing, documented ink-3 contrast list applies).
- **Result words:** PASS (a test ran green in both engines), FAIL (the feature exists and does the wrong thing, test
  red), MISSING (no code at the baseline; where possible a test shows the absence), NOT VERIFIED (no evidence that
  could honestly be produced here: native-only, design-ambiguous, or candidate-only).

## Integrated acceptance update — 2026-10-09

The inventory below preserves the original `199bf66c6` baseline. Its FAIL/MISSING
labels describe that baseline, not the current integrated app. Do not use those
historical labels as current acceptance results.

- The integrated 244-case core gate passed 240; four stale persistence/menu
  assertions were corrected. The affected immutable `c38af1487` gate passed
  **98/98** in Chromium and WebKit, including those four cases.
- The final extra tab audit on `17f666a08` ran **104 cases: 103 passed**. One WebKit
  accessibility case failed while creating its browser context, before an app
  assertion; its unchanged isolated retry passed. Both logs remain preserved.
- Integrated web/pinch journeys passed **20/20**; favicon/fallback/file-glyph
  fixtures passed **4/4** across browser/theme combinations. Physical trackpad
  acceptance remains unverified.
- Native Linux controls independently verified canonical multi-window movement,
  terminal close/Undo and cleanup, split closing, real drag grouping, folder picker,
  first-turn Place working directory, multi-question Send and genuine web favicons.
  These proofs are separate from filled browser fixtures.
- Final acceptance remains open: native task quit/reattach worked, but completion
  replayed the original request and commissioned duplicate tasks without another
  Send. A bounded backend fix and live retest are required. The final visual audit
  also requires its measured preview/menu corrections and affected rechecks.

Evidence stays outside the source repository. Exact case-to-suite references,
original baseline results and remaining native limits are retained in the external
143-case audit crosswalk. No screenshot count alone constitutes acceptance.

## Totals at `199bf66c6`

| Suite | Chromium | WebKit |
| --- | --- | --- |
| Existing tab suites (`tabs`, `shell-tabs`, `tabs-sessions`, `shell-split`, `overview`, `newtab`, `tab-preview`, `shell-rail-keys`, `theme-motion`, `files-tabs`, `terminal-tab`, `conversation-tasks`) | all green | all green (two crashes on the loaded box re-ran green) |
| `responsive.spec.ts` | 5 red at 320/480px | 5 red at 320/480px |
| Pure node tests (`tabs` model, reducers, overview model, new-tab rows, previews, `keyboard`, `openKind`) | 72/72 | n/a |
| **New audit journeys** (`tests/tabs-audit/`, 50 journeys: 45 cases, the access case at 6 sizes × themes) | 25 pass, 25 fail | 25 pass, 25 fail (same cases) |

The `responsive.spec.ts` reds are not tab-owned: at 320 and 480px the Design system page's conversation specimens
(the API-key question card) overflow `.page-content`. Recorded here so nobody chases them in the strip.

## The five findings that matter most (priority order)

1. **Two windows overwrite each other's tabs (TA-WIN-01, FAIL).** Every window saves its whole workspace to one key,
   `codeaf.desktop.workspace.v1` (`features/tabs/model.ts:15`, written on every change at `Workspace.tsx:58`), and nothing
   listens for another window's save. A second window — Places "Open in new window", a native place window, or the
   menus lane's tab handoff — starts with the first window's tabs, and whichever window saves last wins. *Human impact:
   a person who opens a second window loses the tabs (and their drafts) they made in the first one on the next reload;
   a tab "moved to a new window" also stays visible in the old one.*
   **Fix:** key the save by the window's place (`workspaceKey(place)`: `now` keeps the bare key for migration, any other
   place `codeaf.desktop.workspace.v1:<place>`), and adopt another window's save of the same key from the `storage`
   event while keeping this window's own focus and recency. The Places shell lane is building exactly this (`workspaceKey`,
   `adopt`); land it **before** the menus window handoff so a moved tab is released from the shared set once.
   Acceptance: TA-WIN-01 green; a moved tab appears in exactly one window after both reload.

2. **On Linux a terminal never receives Ctrl+W, Ctrl+T, Ctrl+K (TA-KEY-07, FAIL).** The one shortcut registry listens on
   `window` keydown in the capture phase (`design/keyboard.ts:102`), before xterm; the terminal only exempts ⌃` and
   ⌃Tab (`features/terminal/TerminalScreen.tsx:71`). Ctrl is the primary modifier on Linux, so in a shell Ctrl+W closes
   or toggles tabs instead of deleting a word, Ctrl+T opens a tab, Ctrl+K opens the palette, Ctrl+S/Ctrl+B toggle the
   rail, Ctrl+Y opens History once it is backed. *Human impact: everyday shell editing is unusable in a terminal tab on
   Linux, and a reflexive Ctrl+W closes the tab.*
   **Fix:** in `dispatchShortcut`, when `!isMac` and `event.target` is inside `.xterm`, pass the chord through unless it
   is ⌃Tab/⌃⇧Tab, ⌃` or a Ctrl+Shift chord (the GNOME Terminal convention: Ctrl+Shift+T/W act on tabs), and have
   `attachCustomKeyEventHandler` return `false` only for those same chords. Name Ctrl+Shift+W in the Linux menu hint.

3. **Reopening loses or misplaces tabs (TA-UNDO-04 FAIL, TA-UNDO-01 FAIL).**
   - Reopening a closed **split** from the ⌘T field's "closed" row keeps only its focused pane and deletes the whole
     split from the closed list (`reducers/newtab.ts:20-23`): the other panes and their drafts become unreachable.
     **Fix:** when `closed.split` exists and the field is a plain tab, replace the field's tab with the closed split
     (keep the field's group); when the field is itself a pane of a split, offer the row only for plain closed tabs.
   - ⌘⇧T / "Reopen closed tab" appends the tab at the end of the strip and drops its group if closing it emptied the
     group (`reducers/tabs.ts:56`). Candidate `07f0b63bc` adds `reopen-id` (before/after, group) for the toast's Undo
     only. **Fix:** record `{ before, after, group }` on close and route `reopen` through the `reopen-id` placement.
   *Human impact: "reopen" is the safety net for a mis-close; today it can silently drop work or put it elsewhere.*

4. **The × of a split tab does not close it (TA-SPLIT-13, FAIL).** The last segment's title is pulled under the close
   slot by `margin-right: calc(var(--tab-title-fade) * -1)` (`features/tabs/split-tab.css:8`), so the visible × hit-tests
   to that segment: clicking it focuses the last pane instead of closing the split (both engines, 3-pane split; the
   2-pane journey timed out the same way). *Human impact: the one visible way to close a split does the wrong thing.*
   **Fix:** drop the negative margin on the last segment (`.workspace-split-segment:last-of-type .workspace-tab-title {
   margin-right: 0 }`) or give `.workspace-split-tab .workspace-tab-close-slot` `position: relative; z-index: 1`.

5. **The strip cannot hold the order the design draws (TA-GRP-07 FAIL, TA-GRP-09 FAIL).** `TabStrip.tsx:64-67` draws
   pinned tabs, then every ungrouped tab, then every group. So a group made from the first tab jumps to the end, a group
   can never sit before a loose tab (design 2b draws exactly that), a new tab lands in front of all groups instead of at
   the end, and dropping a tab after a group's last member always joins the group. *Human impact: tabs move by
   themselves when grouped; the order a person builds is not the order they see.* **Fix:** render (and compute
   `visibleTabs`) in `state.tabs` order, drawing a capsule at the first member of each group and keeping members
   contiguous (`group` moves members next to the first one; `new` appends).

Everything else is in the inventory below. The largest block of MISSING rows (3g menu items, ⌥ stop square, closing toast,
Inbox, History, web tab) is **work pending integration**, not missing work: it exists in `07f0b63bc`, `2d26ba62a` and **(superseded attention scope: I2.1/I2.6)**
`0ff19c89e` and is re-scored on the root's merged commit.

## Inventory

Columns: **Case** · **Requirement** · **Design** · **Baseline code (`199bf66c6`)** · **Candidate** · **Result** ·
**Evidence** · **Human impact** (only where it is not obvious). Evidence names a spec and test; `audit:` is
`tests/tabs-audit/`. Every result is the same in Chromium and WebKit unless stated.

### Strip and the tab primitive

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-STRIP-01 | Strip 46px; tab 30px, radius 8, 13px glyph, 12px title | S-T-1, S-2h-2 | `TabStrip.tsx`, `tab.css`, tokens | — | PASS | `shell-tabs` "the strip, a tab and the content card match the shell design" | |
| TA-STRIP-02 | Equal widths 190→112px, then scroll under a 40px mask with a "+N" menu | S-2h-3, S-2b-6, S-2b-10 | `TabStrip.tsx` measure + overflow menu | — | PASS | `shell-tabs` "tabs compress from 190px to 112px…"; `tabs` "many top tabs scroll under a mask…" | |
| TA-STRIP-03 | Active = canvas + sh-1; inactive no fill; hover `--tab-hover` | S-T-6, S-T-7, S-2h-4 | `tab.css` | — | PASS | `shell-tabs` strip test; `theme-motion` "shared hover, press, selection and disabled states" | |
| TA-STRIP-04 | Close in a fixed 20px slot, on hover and on the active tab; width never changes | S-3l-10 | `Tab.tsx`, `tab.css` | — | PASS | `tabs` "tab close sits in a fixed slot…" | |
| TA-STRIP-05 | Long titles fade over 20px under a mask, never an ellipsis | S-3l-9 | `tab.css` `.workspace-tab-title` | — | PASS | `shell-tabs` strip test asserts the title mask | |
| TA-STRIP-06 | Double-click a tab renames it | S-3l-12, S-3l-13 | `Tab.tsx:71` `onDoubleClick` | — | PASS | `shell-tabs` "double-clicking a tab renames it" | |
| TA-STRIP-07 | The full title in a 500ms tooltip | S-3l-11 | none: the tab button has `aria-label`, no `title`/tooltip (`Tab.tsx:71`) | none | MISSING | audit: TA-STRIP-07 red, both engines | A long title can only be read by opening the tab or the overview. Fix: wrap the select button in the shared `useTooltip`/`Tooltip` with the full title, 500ms, suppressed while the hover preview is open. |
| TA-STRIP-08 | Kind glyphs: conversation, task, file, diff, terminal, settings, new tab | S-3j-2…9 | `kinds/registry.ts` | — | PASS | `shell-tabs` "the Design system page shows every tab kind and state, light and dark" | |
| TA-STRIP-09 | File tab glyph by type: file-code-2, file-json, file-text, image | S-3j-17, S-3e-10 | `kinds/file.ts` always `fileCode` | none | MISSING | source; no live path picks a type icon | JSON, text and image tabs look like code files. |
| TA-STRIP-10 | Web: real favicon, else a monogram | S-3j-22, S-3d-8 | `Tab.tsx` `KindIcon` monogram; web unbacked | `0ff19c89e` (monogram from site) | MISSING (pending web) | candidate not scored | |
| TA-STRIP-11 | Running is silent; needs you replaces the glyph with an amber dot, named "Needs you"; it clears when the engine stops asking | S-2h-5, S-3j-23, S-IX-13 | `TabItem.tsx` `stateOfMark`, `Tab.tsx` `TabGlyph` | — | PASS | audit: TA-STRIP-11 (mock engine flips `needsPerson`) | |
| TA-STRIP-12 | An inactive tab that needs you lifts its title to full ink | S-T-8 | `tab.css` `[data-state="waiting"]` | — | PASS | audit: TA-STRIP-12 (computed colour = `--ink`, quiet tab = `--ink-2`) | |
| TA-STRIP-13 | Failed = red glyph | S-3j-2 | `tab.css` `.tab-dot[data-state="failed"]` | — | PASS (specimen); live path NOT VERIFIED | `shell-tabs` Design system specimen; no live journey drives a failed mark | |
| TA-STRIP-14 | Loading web page: nothing on the tab, a 2px line on the card | S-3d-7 | `LoadingLine.tsx` (specimen) | `0ff19c89e` | MISSING (pending web) | | |
| TA-STRIP-15 | "Compressed" icon-only tab | S-3j-14 | `compressed` prop, specimen only; live strip scrolls at 112px per 2h | — | NOT VERIFIED (design ambiguous: 2h says scroll at 112px, 3j draws an icon-only specimen) | | |
| TA-STRIP-16 | Hover an inactive tab 500ms → 300px text card; the active tab opens none | S-3k-7 | `hosts/previewHost.tsx`, `preview/usePreviewTrigger.ts` | — | PASS | `tab-preview` "a 500ms hover opens a 300px text card…" | |
| TA-STRIP-17 | Moving across neighbours swaps the card instantly | S-3k-7 | `preview/previewStore.ts` | — | PASS | `tab-preview` "moving across neighbouring tabs swaps the card at once…" | |
| TA-STRIP-18 | The card disappears on click and on drag | S-2g-7, S-2h "Preview" | `usePreviewTrigger` `onPointerDown`/`onDragStart` | — | PASS | `tab-preview` "a click, a press and Escape close the card…"; audit: TA-STRIP-18 (real drag) | |
| TA-STRIP-19 | Card body by kind: last reply, task question, terminal last lines, diff head, settings summary | S-3k-2…5, S-3h-18 | `preview/bodies.tsx`, `content.ts` | — | PASS (web screenshot: MISSING, pending web) | `tab-preview` "file, diff and terminal cards read the target…", "the card is a text surface…", task cases | |
| TA-STRIP-20 | Act from the preview: Allow all, Review; failures said in the card | S-3k-9 | `previewHost.tsx` `useActions` | — | PASS | `tab-preview` "Allow all in the card answers…", "a failed Allow all says so…" | |
| TA-STRIP-21 | Keyboard previews (AGENTS "delayed hover and keyboard previews") | AGENTS only | focus-visible opens the card, but roving focus never rests on an inactive tab | — | NOT VERIFIED (design silent; AGENTS requirement unreachable by construction) | source | Decide whether the ⌃Tab switcher is the keyboard preview. |

### Pinned, Home and Inbox **(superseded attention scope: I2.1/I2.6)**

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-PIN-01 | Pinned tabs are icon-only, before a hairline, with no close | S-2h "Pinned", S-3j-10 | `Tab.tsx` `pinned`, `TabStrip.tsx` divider | — | PASS | `tabs` "pinning, groups and keyboard context menus retain visible selection"; audit: TA-PIN-03 | |
| TA-PIN-02 | ⌘W never closes a pinned tab | S-T-2 | `useTabKeys.ts` `close` | — | PASS | `shell-rail-keys` "⌘W leaves a pinned tab open" | |
| TA-PIN-03 | Pinned order is predictable (strip order) and survives reload | AGENTS "predictable ordering" | `visibleTabs` | — | PASS | audit: TA-PIN-03 | |
| TA-PIN-04 | The place's Home is the pinned first slot, never closes, ⌘0 | S-T-2, S-T-3, S-K-11, S-IX-16 | none | Places shell lane (uncommitted) | MISSING (pending places) | | |
| TA-PIN-05 | **Superseded attention scope: I2.1/I2.6.** Pinned Inbox, the only pinned tab with a dot; background work listed at its top | S-3j-10, S-3j-21, S-3l-7 | `kinds/inbox.ts` placeholder, `backed: false` | `07f0b63bc` | MISSING (pending menus) | | |

### Groups

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-GRP-01 | Create a group from the menu; default names are unique ("New group", "New group 2") | S-3g-13, AGENTS | `helpers.ts:107` `nextGroupTitle` | — | PASS | `tabs` "groups have distinct names and support overview moves, rename and reload" | |
| TA-GRP-02 | Renaming a group to empty never produces a duplicate name | AGENTS "names are unique" | `reducers/groups.ts:27` falls back to a literal "New group" | — | FAIL | audit: TA-GRP-02 (two groups named "New group") | Two identical capsules and two identical "Add to group" entries. Fix: fall back to `nextGroupTitle(others)`. |
| TA-GRP-03 | Rename a group; the name survives reload | S-3g-14 | `rename-group`, rename dialog | — | PASS | `tabs` groups test | |
| TA-GRP-04 | Click the label to collapse to "Label N"; the active member stays visible; hidden members leave the keys and ⌘1–9 | S-2h-9, AGENTS | `GroupCapsule.tsx`, `visibleTabs` | — | PASS | audit: TA-GRP-04 | |
| TA-GRP-05 | A collapsed group with a member that needs you shows the amber dot on its pill | S-3j-12, S-2b-4 | `GroupCapsule` `needsYou` | — | PASS | audit: TA-GRP-05 | |
| TA-GRP-06 | Capsule: `--tab-hover` fill, 12px medium label, 26px members | S-2h "Groups", S-2b-3 | `group.css`, `tab.css` | — | PASS | audit: TA-GRP-06 | |
| TA-GRP-07 | A group keeps its place in the strip (2b draws a group before loose tabs) | S-2b-3, S-2b-1 | `TabStrip.tsx:64-67` draws all loose tabs before all groups | none | FAIL | audit: TA-GRP-07 | See finding 5. |
| TA-GRP-08 | Dragging a tab onto the middle of another tab shows "Group" and groups both | S-2g-2, S-2h-7 | `hosts/dragHost.ts` `dropZoneOf` | — | PASS | `shell-split` "dragging onto another tab shows the Group target and groups both" | |
| TA-GRP-09 | A new tab opens at the end of the strip, after every group | design silent; 2b draws "+" after all tabs | `reducers/tabs.ts:29` appends, `TabStrip` draws it before groups | none | FAIL | audit: TA-GRP-09 | Same cause as TA-GRP-07. |
| TA-GRP-10 | ⌘-select several tabs, ⌘G groups them | S-2h-7, S-2h-8, IX Shortcuts ⌘G | `group` action accepts `ids`, but nothing selects and no ⌘G | none | MISSING | audit: TA-GRP-10 | Fix: ⌘/Ctrl-click toggles a selection set (`aria-selected` stays on the active tab; use a `data-picked` soft fill), `ShortcutId 'group'` for ⌘/Ctrl+G. Resolve IX's "⌘-click = background tab" conflict for strip tabs (design question). |
| TA-GRP-11 | Dropping on a collapsed group label moves the tab in and opens the group | AGENTS | `groupDropProps`, `move-group` uncollapses | — | PASS | `tabs` "dragging onto a collapsed group label groups the tab and preserves its draft" | |
| TA-GRP-12 | Dragging a group label moves the whole group | IX "Group label · drag" | label is not draggable | none | MISSING | audit: TA-GRP-12 | |
| TA-GRP-13 | Dragging a member onto the outer quarter of another member reorders inside the group | S-2g-5 | `dragHost` `before`/`after`, `reorder` | — | PASS | audit: TA-GRP-13 (real drag) | |
| TA-GRP-14 | Dragging a tab onto a member of another group moves it across; an emptied group disappears | S-2g-5 | `move-group`, `normalize` | — | PASS | audit: TA-GRP-14 (real drag) | |
| TA-GRP-15 | A small pointer wobble is a click, not a reorder or a group (drag threshold) | S-2g (implied) | native HTML drag threshold | — | PASS | audit: TA-GRP-15 | |
| TA-GRP-16 | Ungroup keeps every member open, in order, with no empty capsule | S-3g-14 | `ungroup` | — | PASS | audit: TA-GRP-16 | |
| TA-GRP-17 | Tasks opened from a conversation join its group | S-2h-10 | `Workspace.tsx:36-38` `openTaskTab` sets no `groupId` | none (planner `t-d5-sh-task-joins-group`) | MISSING | audit: TA-GRP-17 | Background task tabs scatter outside the group they belong to. Fix: `groupId: tabHolding(state, source.id)?.groupId` in `openTaskTab`. |
| TA-GRP-18 | Suggestion pill: ≥3 tabs on one repo/topic, once per session per set | S-2b-7, S-2b-8 | none | none | MISSING | source | |
| TA-GRP-19 | Group label menu: Rename · Open as split · Collapse · Ungroup · Close N tabs | S-3g-14, S-3g-17 | `menuHost.tsx` `groupMenuItems`: no Close N tabs; adds "New tab in group" | `07f0b63bc` | FAIL (pending menus) | audit: TA-GRP-19 | |

### Selection, keys and the operating system

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-KEY-01 | Click focuses a tab | IX "Tab" | `TabItem.tsx` | — | PASS | every tab suite | |
| TA-KEY-02 | Roving focus: ←/→/Home/End select along the visible order | AGENTS | `TabItem.tsx` `navigate` | — | PASS | audit: TA-GRP-04; `tabs` keyboard menus test | |
| TA-KEY-03 | Held ⌃Tab walks most-recently-used order; Esc cancels; release commits; correct after a close | S-2h-6, AGENTS | `useTabKeys.ts` switcher | — | PASS | `tabs` "held Control Tab…"; audit: TA-KEY-03 | |
| TA-KEY-04 | ⌘1–9 jump; 9 is the last tab | S-IX-16 | `useTabKeys.ts` `jump` | — | PASS | `shell-rail-keys` "⌘T, ⌘W, ⌘⇧T, ⌘1–9 and ⌃Tab" | |
| TA-KEY-05 | ⌘T, ⌘W, ⌘⇧T, ⌘⇧[ ] and Ctrl+PageUp/PageDown; platform hints | S-2h-6, AGENTS | `design/keyboard.ts` | — | PASS | `tabs` "platform tab shortcuts…"; menu hints read "Ctrl W" on Linux (audit: TA-MENU-01 output) | |
| TA-KEY-06 | Text-editing chords in the composer are never taken by the shell | AGENTS | registry only claims listed chords | — | PASS | audit: TA-KEY-06 | |
| TA-KEY-07 | A Linux terminal keeps its own control keys | AGENTS "Command+Tab must remain available" (OS non-interception) | `keyboard.ts:102` capture listener | none | FAIL | audit: TA-KEY-07 (no `\x17\x14\x0b` reached the PTY) | See finding 2. |
| TA-KEY-08 | ⌘Tab is left to macOS | AGENTS | `shortcutOf` ignores ⌘Tab | — | PASS (unit); native NOT VERIFIED | `keyboard.test.ts` "⌃Tab switches recent tabs on both platforms and ⌘Tab is left to macOS" | |
| TA-KEY-09 | Native menu accelerators drive the same tab actions | AGENTS | `src-tauri/src/menu.rs`, `useDesktopTabActions` | — | PASS (event path); native NOT VERIFIED | `tabs` "native tab actions attach to workspace without invoking the engine" | |
| TA-KEY-10 | ⌘⇧\ (Ctrl Shift A) toggles the overview; ⌘↑ does not | S-IX-19 | `keyboard.ts` | — | PASS | `shell-rail-keys` "⌘⇧\ toggles the overview…" | |
| TA-KEY-12 | ⌘O opens "Open file…" (the field's hint advertises it) | S-3f-7 | `NewTabPane.tsx:17` prints "Ctrl O"/"⌘O"; no binding | none | FAIL | audit: TA-KEY-12 | The hint promises a key that does nothing. Bind it in the field's `onKeyDown` (and as `ShortcutId 'open-file'` on the workspace layer) or drop the hint. |
| TA-KEY-13 | ⌃` opens a terminal, also from inside one | S-3f-6 | `keyboard.ts`, `TerminalScreen.tsx:71` | — | PASS | `terminal-tab` "the new terminal key opens a shell…" | |
| TA-KEY-14 | ⌘⌥←/→ move focus between panes | S-2d-8 | `usePaneKeys.ts` | — | PASS | `shell-split` "control+alt arrows move focus between panes…" | |
| TA-KEY-15 | ⌘, opens Settings once | S-IX-12 | `App.tsx` app layer, `openKind.ts` | — | PASS | `shell-rail-keys` "⌘, opens the Settings tab once, from any page" | |

### Context menus

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-MENU-01 | Tab menu: Open in split ▸ · Add to group ▸ · Pin tab · Duplicate · Copy link · Move to new window · Close tab · Close and stop · Close other tabs · Close tabs to the right | S-3g-4…12, S-3g-15 | `menuHost.tsx` `tabMenuItems`: Rename, Pin, Move to group, Separate split, Close, Reopen, Move tab | `07f0b63bc` (all but Copy link) | FAIL (pending menus) | audit: TA-MENU-01 ("menu has: Rename tab \| Pin tab \| Move to group \| Close tab \| Reopen closed tab \| Move tab") | |
| TA-MENU-02 | Right-click and Shift+F10 open the menus; nested dismissal restores focus | AGENTS | shared `ContextMenu` | — | PASS | `tabs` "pinning, groups and keyboard context menus…", "overview … restores focus after nested organization menus" | |
| TA-MENU-03 | Overflow "+N" lists every tab | S-2b-6 | `overflowItems` | — | PASS | `tabs` "many top tabs…" | |
| TA-MENU-04 | Duplicate: a copy with the same draft right after the tab | S-3g-7 | none | `07f0b63bc` `duplicate` | MISSING (pending menus) | audit: TA-MENU-04 | |
| TA-MENU-05 | Close other tabs / Close tabs to the right keep pinned tabs, each reopenable | S-3g-11, S-3g-12 | none | `07f0b63bc` `close-others`, `close-right` | MISSING (pending menus) | audit: TA-MENU-05 | |
| TA-MENU-06 | Copy link ⌘⇧C | S-3g-8 | none; no link scheme | absent by law in the menus integration | MISSING (decision: needs a `codeaf://` scheme) | | |
| TA-MENU-07 | Open in split ▸ lists the tabs it can merge with | S-3g-4 | none (drag only) | `07f0b63bc` | MISSING (pending menus) | audit: TA-MENU-01 | |
| TA-MENU-08 | Menu rows 28px, radius 6, hover `--field-2` | Components "Shell · menus" | token `menu-row-height` 32px (`tokens.json:108`) | `07f0b63bc` adds `menu-item-height` 28px | FAIL (pending menus) | audit: TA-MENU-08 (row measured 31.7px) | |
| TA-MENU-09 | ⌥⌘W conflict: 3g gives it to Close other tabs, 3l/IX to Close and stop | S-3g-11 vs S-3l-14 | — | menus lane chose Close and stop | NOT VERIFIED (design conflict, recorded) | | |

### Closing, reopening and undo

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-CLOSE-01 | Closing a running tab detaches; the engine is never told to stop | S-3l-2, AGENTS | `closeTab` → `close` only | — | PASS | audit: TA-CLOSE-01 (no stop/cancel/close POST) | |
| TA-CLOSE-02 | Holding ⌥ over a running tab turns × into a stop square ("Close and stop") | S-3l-3 | `Tab.tsx` `closeMode` prop exists; `TabItem` never passes it | `07f0b63bc` | MISSING (pending menus) | audit: TA-CLOSE-02 | |
| TA-CLOSE-03 | A 6s toast "X closed and still running · Stop it · Undo"; Undo restores in place | S-3l-4 | none | `07f0b63bc` + shared toast in the menus integration | MISSING (pending menus) | audit: TA-CLOSE-03 | |
| TA-CLOSE-04 | On an idle tab ⌥ does nothing | S-3l-6 | trivially true (no ⌥ behaviour at all) | `07f0b63bc` | NOT VERIFIED (vacuous until TA-CLOSE-02 exists) | | |
| TA-CLOSE-05 | Closing the active tab selects its neighbour; the last close leaves one fresh tab; closed list keeps every close | browser convention | `reducers/tabs.ts` `close` | — | PASS | audit: TA-CLOSE-05 | |
| TA-CLOSE-06 | ⌥⌘W closes and stops | S-3l-14 | none | `07f0b63bc` | MISSING (pending menus) | | |
| TA-CLOSE-07 | **Superseded attention scope: I2.1/I2.6.** Closed-but-running work is listed in the Inbox; clicking reopens it where it was | S-3l-5, S-3l-7 | none | `07f0b63bc` | MISSING (pending menus) | | |
| TA-CLOSE-08 | A stop that fails keeps the tab and says so in words, no modal | IX Flows "Errors" | none | `07f0b63bc` `stopWork` | NOT VERIFIED (candidate only) | | |
| TA-CLOSE-09 | Close N tabs from the group menu | S-3g-14 | none | `07f0b63bc` `close-group` | MISSING (pending menus) | audit: TA-GRP-19 | |
| TA-CLOSE-10 | The closed list keeps at most 20 | IX "Undo … up to 20" | `closed.slice(-19)` | — | PASS | `model.test.ts` | |
| TA-UNDO-01 | ⌘⇧T reopens a tab where it was, in its group, with its draft | S-IX-14, S-3l-4 "restores in place" | `reducers/tabs.ts:56` appends | `07f0b63bc` `reopen-id` for the toast only | FAIL | audit: TA-UNDO-01 (draft kept; position and group lost) | See finding 3. |
| TA-UNDO-02 | ⌘Z outside a text field undoes the last close | S-IX-22 | none | none | MISSING | audit: TA-UNDO-02 | |
| TA-UNDO-03 | Reopen keeps drafts, session and route | AGENTS | `closed` keeps the whole tab | — | PASS | `tabs` "top tabs preserve isolated drafts across closing, reopening and reload" | |
| TA-UNDO-04 | Reopening a closed split from the ⌘T field brings every pane back | S-3f-11 | `reducers/newtab.ts:20-23` keeps one pane | none | FAIL | audit: TA-UNDO-04 | See finding 3. |

### Find, search and history

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-FIND-01 | ⌘T matches open tabs (with their ⌘ digit) and recently closed tabs | S-3f-10, S-3f-11 | `kinds/newtab/rows.ts` | — | PASS | `newtab` "typing puts the conversation row first, then Start, then matching open and closed tabs", "a closed tab row brings the tab back…" | |
| TA-FIND-02 | ⌘T "From history" rows and "See all N in History ⌘↵" | S-4c-4…7, S-3f-13 | none | none found | MISSING | source | |
| TA-FIND-03 | ⌘Y opens/focuses History; several may be open; can split | S-4a-23, S-4e | `kinds/history.ts` placeholder; ⌘Y left to the browser | `2d26ba62a` | MISSING (pending history) | `shell-rail-keys` "⌘Y opens History only when the engine backs it…" (absent-not-broken: PASS) | |
| TA-FIND-04 | Overview type-to-filter "Search N tabs" | S-3h-14 | `overview-model.ts` | — | PASS | `overview` "typing filters the cards and Enter opens the first match" | |
| TA-FIND-05 | Idle tabs archive at 12h; one toast on next launch (Review, Restore all) | S-4c-8, S-4c-9 | none | `2d26ba62a` | MISSING (pending history) | | |

### Kinds

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-KIND-01 | Conversation tab: drafts, session attach, engine title, manual name wins | S-K-1, AGENTS | `ConversationPane`, `title` ranking | — | PASS | `tabs-sessions` (3), `model.test.ts` title ranking | |
| TA-KIND-02 | Task tab: opens in place from a notice, Back/Ctrl+[, modifier-click = background tab | S-IX-23, AGENTS task workspace | `kinds/task.ts`, `view-state.ts` route | — | PASS | `conversation-tasks` (all green both engines) | |
| TA-KIND-03 | File/diff tab: Changes/File toggle persists, hunks, folds, Open in editor locally, one tab per file target | S-3e | `features/files/*`, `fileTarget.ts` | — | PASS | `files-tabs` (10) | |
| TA-KIND-04 | Terminal/job tab: header, live state, Ask about this output, Run again, limits | S-3c | `features/terminal/*` | — | PASS | `terminal-tab` (17) | |
| TA-KIND-05 | Web tab: address field, page on its own sheet, chat-plus, external, load line | S-3d | `kinds/web.ts` placeholder | `0ff19c89e` | MISSING (pending web) | | |
| TA-KIND-06 | Settings is one tab | S-IX-12 | `openKind.ts` | — | PASS | `shell-rail-keys` "Settings is a tab…" | |
| TA-KIND-07 | Unbacked kinds are absent, never opened broken | repo law | `kinds/*` `backed:false` | — | PASS | `shell-rail-keys` ⌘Y case | |

### New tab field

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-NEW-01 | ⌘T / + opens one centred field, no engine call | S-3f-1, S-3f-14 | `kinds/newtab/NewTabPane.tsx` | — | PASS | `newtab` "the New tab button opens an empty card…" | |
| TA-NEW-02 | Field, rows and caption geometry | S-3f | `newtab.css` | — | PASS | `newtab` "the field, its rows and the caption have the design geometry" | |
| TA-NEW-03 | Enter on the first row starts a conversation; offline keeps the words | S-3f-15 | `ask()` | — | PASS | `newtab` (2 cases) | |
| TA-NEW-04 | Matching files from the engine open a file tab | S-3f-9 | `useFileMatches.ts` | — | PASS | `newtab` "matching files come from the engine and open a file tab" | |
| TA-NEW-05 | New terminal row | S-3f-6 | `rows.ts` | — | PASS | `terminal-tab` "the new-tab terminal action starts an interactive shell…" | |
| TA-NEW-06 | A pasted URL opens a web tab | S-3d-9, S-3k-13 | a URL is a question (`NewTabPane.tsx` comment) | web integration adds a first `web` row | MISSING (pending web) | audit: TA-NEW-06 | |
| TA-NEW-07 | "…or a command" | S-3f-1, S-3f-12 | no command rows | none | NOT VERIFIED (design does not define a command) | | |
| TA-NEW-08 | Open-tab rows show a jump key | S-3f-10 (draws ⌥⌘3) | shows ⌘/Ctrl digit (matches IX ⌘1–9) | — | PASS with a recorded design conflict | `newtab` rows case | |

### Split

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-SPLIT-01 | One merged tab, a segment per pane, focused segment filled, 40px pane title, 1.5px ring | S-2h-11, S-3b-2…4 | `SplitTab.tsx`, `PaneGrid.tsx` | — | PASS | `shell-tabs` "a split is one merged tab…" | |
| TA-SPLIT-02 | Drag to the left/right/bottom edge: zone + "Split …" pill, drop splits | S-2g-3, S-2g-5 | `SplitZones.tsx` | — | PASS | `shell-split` (2 cases, real drags) | |
| TA-SPLIT-03 | Up to 2×2; a fifth is refused | S-2d-12 | `merge` capacity | — | PASS | `shell-split`, `split.test.ts` | |
| TA-SPLIT-04 | Open a group as a split | S-3h-16 | `split-group` | — | PASS | `overview` "close shows on hover…; Open as split merges the group" | |
| TA-SPLIT-05 | Pane menu: Close pane · Swap · Maximize (restore without unmounting) | S-2d-9 | `PaneGrid.tsx` `paneMenu` | — | PASS | `shell-split` "the pane menu swaps, maximizes…" | |
| TA-SPLIT-06 | Resize by handle; double-click equalizes; ratio persists | S-2d-10 | `SplitHandles.tsx` | — | PASS | `shell-split` (2 cases) | |
| TA-SPLIT-07 | Compact 36px composer in unfocused panes; tray above it | S-3b-5, S-3b-6, S-3b-8 | `CompactComposer` | — | PASS | `shell-split` (3 cases) | |
| TA-SPLIT-08 | Pane controls on hover, focus and touch | S-2d-11 | `panes.css` | — | PASS | `shell-split` "pane controls show on hover…" | |
| TA-SPLIT-09 | Separate split returns each pane as a tab, in place, with its draft | not in 3g (extra, kept) | `split-unmerge` | — | PASS | audit: TA-SPLIT-09 | |
| TA-SPLIT-10 | Closing a split and ⌘⇧T restores all its panes | S-IX-14 | `closed` keeps the split | — | PASS | audit: TA-SPLIT-10 | |
| TA-SPLIT-11 | 2×2 at the 800px native minimum: no overflow, no sliver panes | AGENTS responsive | `panes.css` | — | PASS | audit: TA-SPLIT-11 | |
| TA-SPLIT-12 | 2×2 at 600px: no overflow, every pane's controls on screen | AGENTS responsive; design silent (planner `t-d5-sh-split-narrow`) | `panes.css` | — | PASS (focused pane measured 288px; narrow layout still a design decision) | audit: TA-SPLIT-12 | |
| TA-SPLIT-13 | The split's × closes the split | S-3b-2 | `split-tab.css:8` negative margin | none | FAIL | audit: TA-SPLIT-13 (hit test lands on "Bottom pane") | See finding 4. |
| TA-SPLIT-14 | Tasks panel opens inside its pane | S-3b-9 | conversation-owned | — | NOT VERIFIED (conversation audit) | | |

### Overview

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-OV-01 | Bar (search, Grid/Filmstrip, Done), sections by group with Open as split | S-3h-2, S-3h-3 | `TabOverview.tsx` | — | PASS | `overview` "the grid icon opens the overview with the design bar and sections" | |
| TA-OV-02 | Readable cards: kind, state, title, one piece of content, footer | S-3h-4…12, S-3h-18 | `OverviewCard.tsx` | — | PASS | `overview`, `tabs` "overview cards show persisted work…" | |
| TA-OV-03 | ← → ↑ ↓ ↵ Esc and ⌘W in grid and filmstrip | S-3h-17, S-3i-7 | `TabOverview.tsx` `onKeyDown` | — | PASS | `overview` (3 cases) | |
| TA-OV-04 | Filmstrip: live half-scale panes, same order, read-only | S-3i | `OverviewFilmstrip.tsx` (`inert`) | — | PASS | `overview` "the filmstrip lists the same order with real half-scale panes…" | |
| TA-OV-05 | ⌘-click / middle-click on a card opens it in the background | S-3h-15 | `OverviewCard.tsx:58` plain `onClick` → open and close | none | MISSING | audit: TA-OV-05 (overview closes, tab switches) | |
| TA-OV-06 | Right-click on a card gives the tab menu | S-3h-15 | `TabOverview.tsx:95` own short menu (Pin, Move to group, Close) | `07f0b63bc` menu could be reused | FAIL | audit: TA-OV-06 | Fix: build the card menu from `tabMenuItems(api, tab)`. |
| TA-OV-07 | Drag a card onto a section to regroup | S-3h-15 | section drop target | — | PASS | `overview` "dragging a card onto another section regroups the tab" | |
| TA-OV-08 | Close on hover | S-3h-15 | `OverviewCard` | — | PASS | `overview` "close shows on hover and closes the tab…" | |
| TA-OV-09 | The active card has the 2px accent ring | S-3h, AGENTS (superseded) | `overview.css` | — | PASS | `tabs` "…mark the active tab with a ring" | |
| TA-OV-10 | Pinch out opens the overview | S-3h-13 | none | none | MISSING (decision U9: no reliable gesture in the webview) | | |
| TA-OV-11 | Web cards show a screenshot | S-3h-10 | none | `0ff19c89e` `shots.ts` | MISSING (pending web) | | |
| TA-OV-12 | Fits 320px; themed and accessible light/dark | AGENTS | `overview.css` | — | PASS | `overview` "fits 320px…", specimen light/dark | |

### Persistence and windows

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-PERS-01 | Drafts are per tab and survive switching and reload | AGENTS | `draft` in the model | — | PASS | `tabs` drafts case | |
| TA-PERS-02 | Saved tabs reattach exactly once | AGENTS | `useBackgroundSessions` | — | PASS | `tabs-sessions` "reload attaches each saved tab exactly once" | |
| TA-PERS-03 | Scroll position restores when you come back to a tab (and on relaunch) | S-IX-5 | none: the active pane unmounts; `TabView` has no scroll field | planner `t-d5-int-cv-view-state` | MISSING | audit: TA-PERS-03 (scrollTop 382 → 788) | Coming back to a long conversation throws away the reading position. |
| TA-PERS-04 | Malformed saved state recovers | AGENTS | `readWorkspace` | — | PASS | `tabs` "malformed saved state recovers…", `model.test.ts` | |
| TA-PERS-05 | Groups, splits and ratios restore on relaunch | S-IX-5 | `readWorkspace` | — | PASS | `tabs` groups reload; `shell-split` ratio persists | |
| TA-WIN-01 | Two windows never drop each other's tabs | S-IX-4 | one global key, no `storage` listener | Places shell lane (uncommitted) | FAIL | audit: TA-WIN-01 | See finding 1. |
| TA-WIN-02 | Move to new window / drag out of the strip; no tab lost or duplicated | S-3g-9, S-2g-6, S-IX-4 | typed native window commands exist; no UI | menus integration (two-phase handoff, in progress) | NOT VERIFIED (native; depends on TA-WIN-01) | | |
| TA-WIN-03 | ⌘N opens a new window on Now | S-IX-4 | none | none found | MISSING | | |
| TA-WIN-04 | The same place in two windows mirrors live | S-IX-4 | none | Places shell lane `adopt` | MISSING (pending places) | | |
| TA-WIN-05 | Native web views hide beneath DOM overlays (menus, previews, toast, overview); read-only snapshot shown instead | web arch | none | `0ff19c89e` `covers.ts` | NOT VERIFIED (candidate only; needs native capture) | | |

### Appearance and access

| Case | Requirement | Design | Baseline code | Candidate | Result | Evidence | Human impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| TA-A11Y-01 | A grouped, pinned, collapsed strip at 320/600/800px, light and dark, reduced motion: no page overflow, + and overview on screen, nothing animating, axe clean | AGENTS responsive + a11y, S-IX-13 | strip CSS | — | PASS (12 runs) | audit: TA-A11Y-{light,dark}-{320,600,800} | |
| TA-A11Y-02 | Status is announced in words ("Needs you") | S-IX-13 | `aria-description` | — | PASS | audit: TA-STRIP-11 | |
| TA-A11Y-03 | Light, dark, system and reduced motion through the shared provider | AGENTS | `ThemeProvider.tsx` | — | PASS | `theme-motion` (8 per engine) | |
| TA-A11Y-04 | Every tab kind and state accessible in the specimen, light and dark | AGENTS | `TabsSpecimen.tsx` | — | PASS | `shell-tabs` specimen case | |
| TA-A11Y-05 | Responsive pages at 320/480px | AGENTS | Design system page specimens | — | FAIL (not tab-owned) | `responsive` 5 red per engine (`.page-content` overflow on the Design system page) | |

## Fix order for the root

1. TA-WIN-01 (per-place key + adopt), before the menus window handoff is enabled.
2. TA-KEY-07 (terminal passthrough), TA-SPLIT-13 (split ×), TA-UNDO-04 and TA-UNDO-01 (reopen) — small, independent.
3. Integrate menus (`07f0b63bc` + integration), then re-run TA-MENU-*, TA-CLOSE-02/03/06/07/09, TA-GRP-19, TA-OV-06.
4. Strip order (TA-GRP-07/09), then ⌘-select/⌘G and group drag (TA-GRP-10/12) on top of it.
5. TA-GRP-17, TA-GRP-02, TA-KEY-12, TA-STRIP-07, TA-OV-05, TA-PERS-03, TA-UNDO-02.
6. History and web integration, then re-run TA-FIND-03/05, TA-NEW-06, TA-KIND-05, TA-STRIP-10/14, TA-OV-11.

## Re-running

```sh
cd desktop
npx playwright test --config playwright.tabs-audit.config.ts                 # both engines, one worker, port 1759
npx playwright test tabs tabs-sessions shell-tabs shell-split overview newtab tab-preview shell-rail-keys theme-motion files-tabs terminal-tab conversation-tasks
node --test src/features/tabs/model.test.ts src/features/tabs/overview-model.test.ts src/features/tabs/reducers/*.test.ts src/features/tabs/kinds/newtab/rows.test.ts src/features/tabs/preview/*.test.ts src/design/keyboard.test.ts src/features/shell/openKind.test.ts
```
