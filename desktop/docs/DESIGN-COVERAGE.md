# Design coverage: latest design vs the desktop app

Status: planning record, 2026-10-09. Code base: `39d440c4f` (d3-int), with every in-flight lane worktree
read but not changed. Design source: the six `.git/design-current/*.dc.html` pages (Foundations,
Components, Conversation, Shell, Places, Interactions) plus `shell-helpers.js` and `places-helpers.js`.
Companion: [`PLACES-ARCHITECTURE.md`](PLACES-ARCHITECTURE.md), the data paths, bridges and security
boundary these tasks build. Work orders live in PlanDB project `p-6xuu` under `t-d5-plan`. Every task
id in this file is a PlanDB id.

## Current audit status

The counts and per-row statuses below are the planning baseline at `39d440c4f`.
They are preserved for traceability and must not be presented as the current app's
missing-feature totals. Implementation and acceptance have since proceeded through
integrated browser, backend and native gates; see [Complete tabs audit](TABS-COMPLETE-AUDIT.md)
for the recorded revisions and the separate native/browser evidence scopes.

Final acceptance is still open. Newly reproduced task-completion recommissioning
has a bounded enforced answer-only fix and native retest; actual rendered preview,
menu and text-line geometry are being checked against the reference screenshots.
The Overview model footer is a concrete remaining wiring gap, while inactive
keyboard-preview navigation is a recorded design question. Historical `partial`
or `missing` rows are neither automatically accepted nor assumed still absent.

Notification click (CV-289, BE-INB-06, the click half of PL-254, and assumption 9
below) no longer rests on that assumption. The notification plugin still drops click
actions on desktop, so `src-tauri/src/activation.rs` posts through the platform's
own service and keeps, per notification, the conversation and question the renderer
named when it was posted. Only the platform's activation of that notification opens
it: a freedesktop `ActionInvoked` default action from the server that posted it, the
macOS notification-center delegate's content click, or a Windows toast's `Activated`
event. The addressed window claims the target, the conversation opens through the
link path and its tray brings that question forward; a grouped notification opens
its first question still waiting, and one whose questions were all answered opens
the conversation only. Proof: a Linux click over a private session bus and display
(`tests/notification_click_smoke.rs`), browser routing in
`tests/ui/notification-clicks.spec.ts`, macOS compilation and unit tests, and a
Windows type-check of the toast module only. A real notification server (dunst
1.9.2, X11, its own session bus) drew the grouped notification and a pointer click
on it opened the question still waiting (`scripts/notification-server-smoke.sh`);
GNOME Shell, KDE Plasma, Wayland, a signed macOS bundle and Windows are still
unproven. A notification is not withdrawn when its question is answered elsewhere.
A macOS development build posts as Terminal, so the system brings Terminal forward
there.

Several windows post the attention list, each from its own stream of the engine's
world feed. Each list carries the feed sequence it was read at, and Rust lets only
the newest reading any open window has reported say what is pending, so a window a
reading behind can neither announce an answered question again nor point a grouped
click at it; a window whose own sequence goes backwards marks an engine restart.
Failures in that list come from the engine's `unseenFailed` and the newest
failure's landing instant, not from the Inbox's optimistic seen marks, its five-row
limit or a window's count record, and a failure is announced once. Two limits stay:
an engine that predates the seen mark leaves failures to each window's own record,
so two windows can disagree about one (it is still announced once); and the badge
and pending set follow whichever window read the feed last, with no engine-wide
answer to "what is pending" beyond that sequence.

## 1. Bottom line

- **1131 design items are scored.** 143 are complete, 290 partial, 486 missing and 196 in flight in
  an uncommitted lane; 16 are removed by a recorded decision. A row is **complete** only when the code
  was opened **and** a named test asserts it. Unverified lane work is `in-flight`, never complete.
- **368 new PlanDB tasks close every partial or missing row** that no lane owns: 358 work tasks plus
  10 group tasks under `t-d5-plan`. They are ordered contract → primitive → wiring → test and joined
  by 1,012 dependency edges (949 from the plan and 63 waits on coordinator lanes). Every file two tasks share is serialized, and every task touching a file
  an in-flight lane edits waits for that lane.
- **The largest gaps:**
  - **Places** (162 of 179 rows missing). Nothing exists in the renderer or the bridge.
  - **The engine-wide data path:** world stream, attention, workspace store and incremental
    snapshots.
  - **Native multiwindow and the native Web tab.**
  - **Token and primitive drift** from the latest Foundations (138 partial rows).
- **Three coordinator lanes started during planning:** `t-d5-places-store`, `t-d5-places-primitives`
  and `t-d5-native-web`. The 35 tasks they overlap were rewritten as verify-and-close-the-gap tasks
  that wait for those lanes (§5.3). Nothing was cancelled or reclaimed, and no `t-s1-*` task was
  touched.

## 2. Reference inventory (what was enumerated)

| Page | Sections enumerated | Items |
|---|---|---|
| Foundations | 1a colour ramps · 1b place tints · 1c type · 1d space · 1e radius · 1f depth · 1g status marks · 1h step categories and glyph sizes · 1i motion · 1j materials | about 120 token lines |
| Components | controls · file/link chips · user message · steer · update/final answer · turn footer and system notes · work block and step · tool call row · thinking · task notice · task tree/view · decision tray · shell tabs · split · preview · menus · new tab · history row · places rail/Home tab/switcher/tint/tile/suggestion/Using · conversation navigation · inputs (paste card, model picker, search, filters) · overlays (toast, tooltip, Quick Look) · places page · edge states · composer | 221 component-state lines |
| Conversation | 1a live at the bottom · 1b scrolled up · 1c task opened · 1d tasks expanded · 1e states · 1f behaviour spec, plus the page's prototype logic | part of 467 |
| Interactions | Shell table · Conversation table · Composer/tray/queue table · Places/History table · flows the screens don't draw · the full shortcut table · cross-references | part of 467 |
| Shell | 2b many tabs · 2d split 4 · 2g drag · 2h behaviour spec · 3a conversation and tasks · 3b split chats · 3c terminal/job · 3d web · 3e file/diff · 3f new tab · 3g context menus · 3h overview · 3i filmstrip · 3j kinds × states · 3k hover previews · 3l close and long titles · 4a–4e history | 346 |
| Places | 6a working in a place · 6c All places (⌘P) · 6d journeys · 6e model and rules · 6f Using popover · 8a–8g the place page · 9a–9e going in, starting, collapsed, tint · 10a the rail | 337 |

The full itemised inventories (IDs `S-*`, `P-*`, `C-*`, `I-*`, cited in the Source column below) and
the three auditor reports are kept outside the repository with the run records
(`~/.codex/codeaf-design-run/d5-inventory/`). The matrices below are self-sufficient: each row names
the item, its page section and the code path.

## 3. Coverage at a glance

#### Rows by status

| Area | complete | partial | missing | in-flight | n/a-decided | rows |
|---|---|---|---|---|---|---|
| foundations | 9 | 138 | 63 | 17 | 10 | 237 |
| shell | 35 | 27 | 72 | 93 | 0 | 227 |
| tabs | 6 | 18 | 51 | 61 | 0 | 136 |
| conversation | 73 | 73 | 34 | 5 | 4 | 189 |
| places | 1 | 8 | 162 | 7 | 1 | 179 |
| backend | 19 | 26 | 104 | 13 | 1 | 163 |
| **total** | 143 | 290 | 486 | 196 | 16 | 1131 |

#### Interaction dimensions covered (rows mentioning each dimension)

| Area | hover | focus/keyboard | pressed/selected/disabled | context menu | drag/drop | narrow/mobile/touch | native | multiwindow |
|---|---|---|---|---|---|---|---|---|
| foundations | 19 | 37 | 15 | 29 | 6 | 11 | 6 | 3 |
| shell | 20 | 153 | 18 | 53 | 16 | 19 | 16 | 8 |
| tabs | 7 | 103 | 2 | 22 | 1 | 8 | 10 | 0 |
| conversation | 16 | 48 | 6 | 15 | 6 | 8 | 4 | 1 |
| places | 12 | 69 | 9 | 19 | 14 | 9 | 3 | 10 |
| backend | 2 | 30 | 0 | 8 | 4 | 0 | 22 | 8 |

The `tabs` area has no multiwindow rows of its own on purpose. A tab kind's content does not change
between windows, and moving a tab between windows is covered once by shell rows (§13 of the shell
matrix) and backend rows (§9 of the backend matrix). The web view of a web tab is created on the window
that calls it (`t-d5-nat-web-webview`).

## 4. Status legend

| Status | Meaning |
|---|---|
| complete | The code at `39d440c4f` does it **and** the named test asserts it |
| partial | Some of it exists, or it exists with no test, or the values differ from the design |
| missing | Nothing does it |
| in-flight:`<task>` | Only an uncommitted or unmerged lane does it. Unverified until that lane lands |
| n/a-decided | A recorded decision removes it (`docs/DESIGN-QUESTIONS.md` D1–D6, Q1–Q34) |

Rows whose Item starts with `ASSUME:` mark where the design is silent for a dimension (hover, focus,
drag, menu, keyboard, narrow width, native, multiwindow). The assumption is what the tasks build until
the designer answers.

## 5. Phase, ownership and dependency map

### 5.1 Phases

1. **Contracts:** engine and bridge (`t-d5-be-*`) and native commands (`t-d5-nat-*`). These are new
   Go/Rust files, testable without the UI.
2. **Integrator waves** (`t-d5-int-*`, tag `integrator-only`). One owner at a time edits:
   - the bridge route table;
   - `cmd/codeaf/desktop_bridge.go`;
   - `lib.rs`, the capabilities and `Cargo.toml`;
   - `tokens.json`, `Icon.tsx`, `ui.css` and `components/ui/index.ts`;
   - the tabs model and registry, `keyboard.ts`, `mock-engine.ts`, `App.tsx` and `Workspace.tsx`.

   The token waves run A colour → B dimensions → C motion → D icons → E legacy palette → F exports.
3. **Primitives and typed client adapters** (`t-d5-prim-*`): `world-client`, `places-client`,
   `workspace-sync`, `snapshot-merge`, the native window/web wrappers, Toast, Quick Look, Kbd,
   SearchField, FilterTabs, material and motion.
4. **UI wiring:** `t-d5-pl-*`, `t-d5-sh-*`, `t-d5-tab-*` and `t-d5-cv-*`.
5. **Tests:** every feature ends in a node and/or Playwright task (Chromium and WebKit, Light and
   Dark, 320/600/1200px where width matters). Backend flow tests are `t-d5-qa-be-*`. `t-d5-gates`
   (existing) is the final integrated and live verification with
   `deepseek/deepseek-v4.1-flash` on every role.

### 5.2 Tasks by group and phase, and serialized files

The tables are generated from the imported graph. A file listed under "Serialized shared files" has a
dependency edge between consecutive owners, so no two lanes edit it at once.

#### Tasks by group and phase

| Group | contract | primitive | wiring | integrate | test | decision | composites | total |
|---|---|---|---|---|---|---|---|---|
| `t-d5-be` | 21 | 0 | 11 | 0 | 0 | 6 | 1 | 39 |
| `t-d5-nat` | 2 | 7 | 2 | 0 | 0 | 0 | 0 | 11 |
| `t-d5-tok` | 8 | 0 | 1 | 0 | 0 | 0 | 0 | 9 |
| `t-d5-prim` | 0 | 24 | 3 | 0 | 0 | 0 | 0 | 27 |
| `t-d5-pl` | 0 | 15 | 32 | 0 | 8 | 1 | 8 | 64 |
| `t-d5-sh` | 1 | 6 | 31 | 0 | 15 | 3 | 0 | 56 |
| `t-d5-tab` | 2 | 8 | 31 | 0 | 7 | 2 | 7 | 57 |
| `t-d5-cv` | 1 | 4 | 20 | 0 | 17 | 0 | 0 | 42 |
| `t-d5-qa` | 0 | 0 | 0 | 0 | 15 | 0 | 0 | 15 |
| `t-d5-int` | 6 | 1 | 7 | 23 | 1 | 0 | 0 | 38 |
| **total** | | | | | | | | **358** (+10 group tasks) |

#### Serialized shared files (one owner at a time, in this order)

| File | Owners in order |
|---|---|
| `desktop/src/design/tokens.json` | `t-d5-int-tok-a-colour` → `t-d5-int-tok-b-dimensions` → `t-d5-tok-sh-pill` → `t-d5-int-tok-c-motion` → `t-d5-tok-sh-touch` → `t-d5-int-tok-d-icons` → `t-d5-tok-sh-toast` → `t-d5-tok-cv-geometry` → `t-d5-tok-pl-rail` → `t-d5-int-tok-e-legacy` → `t-d5-tok-pl-page` → `t-d5-tok-pl-overlay` |
| `desktop/src/styles/tokens.css` | `t-d5-int-tok-a-colour` → `t-d5-int-tok-b-dimensions` → `t-d5-tok-sh-pill` → `t-d5-int-tok-c-motion` → `t-d5-tok-sh-touch` → `t-d5-int-tok-d-icons` → `t-d5-tok-sh-toast` → `t-d5-tok-cv-geometry` → `t-d5-tok-pl-rail` → `t-d5-int-tok-e-legacy` → `t-d5-tok-pl-page` → `t-d5-tok-pl-overlay` |
| `desktop/src/styles/ui.css` | `t-d5-int-tok-a-colour` → `t-d5-int-tok-b-dimensions` → `t-d5-int-tok-c-motion` → `t-d5-int-tok-d-icons` → `t-d5-int-tok-e-legacy` → `t-d5-int-pl-menu-swatches` → `t-d5-int-sh-app` |
| `internal/desktopbridge/bridge.go` | `t-d5-int-cv-bridge-route` → `t-d5-int-tab-bridge-routes` → `t-d5-int-be-route-table-1` → `t-d5-int-be-route-table-2` → `t-d5-int-be-snapshot-ring` |
| `desktop/src/features/conversation/ConversationView.tsx` | `t-d5-cv-header-wire` → `t-d5-cv-offline-notice` → `t-d5-cv-menu-message` → `t-d5-cv-queue-menu` → `t-d5-cv-notify-focus` |
| `desktop/src/features/tabs/TabStrip.tsx` | `t-d5-sh-drag-regions` → `t-d5-sh-strip-menu` → `t-d5-sh-strip-compressed` → `t-d5-sh-tab-close-motion` → `t-d5-int-pl-home-kind` |
| `desktop/src/features/chat/engine-client.ts` | `t-d5-prim-engine-fetch-export` → `t-d5-prim-snapshot-merge` → `t-d5-cv-incremental-wire` → `t-d5-cv-queue-send-now-client` |
| `desktop/src-tauri/src/lib.rs` | `t-d5-int-be-tauri-windows` → `t-d5-int-be-tauri-web` → `t-d5-int-be-tauri-plugins` → `t-d5-int-tab-native` |
| `desktop/tests/ui/support/mock-engine.ts` | `t-d5-int-cv-mock-engine` → `t-d5-int-sh-workspace` → `t-d5-int-pl-test-wiring` → `t-d5-int-be-mock-engine` |
| `desktop/src/features/shell/Rail.tsx` | `t-d5-sh-rail-inbox-wire` → `t-d5-sh-rail-now-wire` → `t-d5-sh-rail-legacy-retire` → `t-d5-int-pl-rail-slot` |
| `desktop/src/features/tabs/TabItem.tsx` | `t-d5-sh-strip-compressed` → `t-d5-sh-tab-multiselect` → `t-d5-sh-tab-keyboard-move` → `t-d5-sh-inbox-tab-dot` |
| `desktop/src/features/tabs/kinds/newtab/rows.ts` | `t-d5-sh-newtab-history` → `t-d5-sh-newtab-closed-age` → `t-d5-sh-newtab-start-rows` → `t-d5-sh-newtab-url` |
| `desktop/src/features/tabs/model.ts` | `t-d5-int-be-tabs-persistence` → `t-d5-int-sh-workspace` → `t-d5-int-pl-workspace-key` |
| `desktop/src/design/keyboard.ts` | `t-d5-int-cv-keys` → `t-d5-int-tab-keys` → `t-d5-int-sh-keymap` |
| `desktop/src/features/tabs/view-state.ts` | `t-d5-int-cv-view-state` → `t-d5-int-tab-kind-slots` → `t-d5-int-sh-workspace` |
| `desktop/src/features/tabs/kinds/slots.ts` | `t-d5-int-cv-pane-actions` → `t-d5-int-tab-kind-slots` → `t-d5-int-pl-home-kind` |
| `desktop/src/features/conversation/ConversationDock.tsx` | `t-d5-cv-pane-mini-tray` → `t-d5-cv-queue-chip` → `t-d5-cv-queue-menu` |
| `desktop/src/features/conversation/useConversation.ts` | `t-d5-cv-incremental-wire` → `t-d5-cv-queue-send-now-client` → `t-d5-cv-offline-queue` |
| `desktop/src/features/conversation/Composer.tsx` | `t-d5-cv-menu-composer` → `t-d5-cv-queue-chip` → `t-d5-cv-at-picker` |
| `desktop/src/App.tsx` | `t-d5-int-tok-f-exports` → `t-d5-int-sh-app` → `t-d5-int-pl-app-mount` |
| `desktop/src-tauri/src/menu.rs` | `t-d5-nat-window-menu` → `t-d5-sh-native-menu-items` |
| `internal/desktopbridge/routes.go` | `t-d5-int-be-route-table-1` → `t-d5-int-be-route-table-2` |
| `desktop/src-tauri/capabilities/default.json` | `t-d5-int-be-tauri-windows` → `t-d5-int-be-tauri-plugins` |
| `desktop/src-tauri/tauri.conf.json` | `t-d5-int-be-tauri-windows` → `t-d5-int-be-tauri-plugins` |
| `desktop/src-tauri/Cargo.toml` | `t-d5-int-be-tauri-web` → `t-d5-int-be-tauri-plugins` |
| `desktop/package.json` | `t-d5-int-be-package-json` → `t-d5-int-tok-f-exports` |
| `desktop/src/features/conversation/ConversationBar.tsx` | `t-d5-cv-header-bar` → `t-d5-int-pl-conv-slots` |
| `desktop/src/features/conversation/work/WorkStepView.tsx` | `t-d5-cv-step-shimmer` → `t-d5-cv-menu-work` |
| `desktop/scripts/design-output.mjs` | `t-d5-int-tok-a-colour` → `t-d5-int-tok-c-motion` |
| `desktop/src/components/ui/Icon.tsx` | `t-d5-int-tok-d-icons` → `t-d5-int-pl-icons` |
| `desktop/tests/ui/theme-motion.spec.ts` | `t-d5-sh-palette-retire` → `t-d5-int-tok-e-legacy` |
| `desktop/tests/ui/contracts.ts` | `t-d5-int-tok-f-exports` → `t-d5-int-pl-test-wiring` |
| `desktop/src/features/shell/rail.css` | `t-d5-sh-rail-legacy-retire` → `t-d5-int-pl-rail-slot` |
| `desktop/src/features/tabs/Workspace.tsx` | `t-d5-int-sh-workspace` → `t-d5-int-pl-workspace-key` |
| `desktop/src/features/tabs/strip.css` | `t-d5-sh-drag-regions` → `t-d5-sh-strip-compressed` |
| `desktop/src/features/shell/railActions.ts` | `t-d5-sh-rail-inbox-wire` → `t-d5-sh-rail-now-wire` |
| `desktop/src/features/tabs/tab.css` | `t-d5-sh-tab-chrome` → `t-d5-sh-tab-multiselect` |
| `desktop/src/features/tabs/hosts/dragHost.ts` | `t-d5-sh-group-drag` → `t-d5-sh-tab-tearoff` |
| `desktop/src/features/tabs/OverviewCard.tsx` | `t-d5-sh-overview-tab-menu` → `t-d5-sh-overview-card-body` |
| `desktop/src/features/tabs/kinds/terminal.ts` | `t-d5-tab-term-tab-menu` → `t-d5-tab-jobs-pane` |

### 5.3 In-flight lanes this plan waits for (never duplicated)

| Lane | Worktree | Owns | Tasks that wait for it |
|---|---|---|---|
| `t-s1-preview` | `.claude/worktrees/agent-a7359f658c917f69f` | tab hover preview cards | preview-dependent shell/places tasks |
| `t-s1-menus` | `agent-a320e518fe154e07c` | context menus, close vs close-and-stop, closing toast, local Inbox | 46 tasks (largest bottleneck) |
| `t-s1-split` (+ `t-s1-menu`) | `agent-ad64d2e18339b086b` | split 2x2, drag zones, pane focus, compact composer | split follow-ups |
| `t-s1-overview` (+ `t-ov-*`) | `agent-a4590d5501d02cd1f` | overview grid, filmstrip | overview menu/body follow-ups |
| `t-s1-newtab` | `agent-a2d4b3e6b5845bad3` | ⌘T field | new-tab history/URL/start rows |
| `t-s1-history` | `agent-ab15dab692b3b743f` | History tab, search, recap, auto-archive, `internal/desktopbridge/history.go` | 28 tasks, including every `bridge.go`/`place.go` edit |
| `t-s1-rail` | `agent-a99c430e0f6cc71ca` | rail collapse/peek/focus, keys, `SettingsPane` | 31 tasks |
| `t-s1-files` | `agent-a451c7c04660863b4` | file/diff panes | 30 tasks |
| `t-s1-terminal` | `agent-aa8990887568558b7` | terminal/job pane | terminal/job follow-ups |
| `t-s1-settings` | (ready, unclaimed) | settings tab model per role | settings follow-ups |
| `t-d5-places-store` | `~/codeaf-places-store` (`dfdd4b22c`) | `internal/placegraph` store, graph, membership, pins, undo receipts, delete/merge | the `t-d5-be-pg-*` store, membership, rail, resolve, sources and archive tasks (reconciled) |
| `t-d5-places-primitives` | `~/codeaf-places-primitives` (`03aca4119`) | PlaceSwatch, PlaceTile, ChatRow, AttentionRow, PlaceHeading, `places-*` tokens | swatch/tile/heading/chat-list/attention/token tasks (reconciled) |
| `t-d5-native-web` | `~/codeaf-native-web` | `web.rs` child webviews, security, viewport, lifecycle, navigation, WebPane, snapshots | `t-d5-nat-web-*`, `t-d5-tab-web-*`, `t-d5-int-be-tauri-web` (reconciled) |

A reconciled task keeps its id and edges. Its description now starts with `RECONCILED`, names what
the lane delivers, and reduces the task to verifying that work and closing the named remaining gap.

### 5.4 Duplicates folded across areas

The area writers proposed 379 tasks. These 21 were folded into the task that now owns their scope;
every dependency edge was remapped. Toast: `d5-sh-toast-primitive` and `d5-pl-toast` →
`t-d5-prim-toast`; `d5-sh-toast-migrate` → `t-d5-prim-toast-adopt-*`; `d5-int-sh-ui-exports` →
`t-d5-int-tok-f-exports`. World data: `d5-cv-bg-summary-world` → `t-d5-prim-world-background`;
`d5-cv-incremental-merge` → `t-d5-prim-snapshot-merge`. New tab: `d5-tab-hist-newtab`,
`d5-tab-web-newtab-url`, `d5-tab-term-newtab-row` → `t-d5-sh-newtab-*`. Windows:
`d5-prim-window-restore` → `t-d5-sh-window-restore`; `d5-qa-nat-multiwindow` → `t-d5-sh-window-test`;
`d5-cv-scroll-restore` → `t-d5-sh-scroll-restore` (window-local). Primitives: `d5-pl-swatch` →
`t-d5-prim-swatch`; `d5-sh-frame-material` → `t-d5-prim-material`. Tokens: `d5-tok-pl-swatch`,
`d5-tok-sh-tab-fill`, `d5-tok-sh-material` → `t-d5-int-tok-a-colour`; `d5-tok-cv-motion` →
`t-d5-int-tok-c-motion`. Others: `d5-nat-editors-list` → `t-d5-tab-files-open-with-native` (editors
are listed on the engine machine, Q1); `d5-int-pl-history-menu` → `t-d5-tab-hist-add-to-place`;
`d5-tab-web-link-chip` → `t-d5-cv-link-chip`. Single owners were also fixed where two areas built the
same thing: one undo stack (`t-d5-sh-undo-stack`, which places register into), one rail row
(`t-d5-sh-rail-rows`, which places extends), one window-place key (`t-d5-sh-window-boot`) and one
keymap (`t-d5-int-sh-keymap`).

## 6. Open questions that cut across areas

Each area matrix ends with its own open questions and the assumption its tasks build. Places, Web and
multiwindow questions are in `PLACES-ARCHITECTURE.md` §10. The cross-cutting ones:

| # | Question | Assumption shipped |
|---|---|---|
| X1 | Q30 is stale: the latest Interactions binds ⌥⌘1–3 to pinned models and ⌘1–9 to tabs | Follow the latest Interactions. `t-s1-rail` implements it, and `t-m-docs` (menus lane) records Q30 as decided |
| X2 | Engine offline: Interactions queues sends locally and sends them on reconnect; AGENTS says to keep the draft with Retry and never auto-send | Design wins for the line and the 30s Retry (`t-d5-cv-offline-notice`). Local queueing (`t-d5-cv-offline-queue`) sends only after the engine acknowledges a reattach, never silently twice |
| X3 | ⌘N: a new window on Now, or a new place? | Scoped by focus: inside the ⌘P palette it means new place, elsewhere a new window |
| X4 | Rail toggle: ⌘S (Places 9c) or ⌘B (app and DESIGN.md)? | ⌘S per the latest design, with ⌘B kept as an alias (`t-s1-rail`) |
| X5 | Overview selection: a 2px ring (design 3h) or a quiet "Current" label (AGENTS)? | The design wins. The AGENTS edit lands with `t-s1-overview` |
| X6 | Hover previews hold buttons (design); AGENTS says they are noninteractive | The design wins. The AGENTS edit lands with `t-s1-preview` |
| X7 | `--frame` material values differ between Foundations and Shell/Places | The Foundations values (`t-d5-int-tok-a-colour`) |
| X8 | Node `*.test.ts` files run under no npm script or CI | `t-d5-int-be-package-json` adds the node test glob to `npm run check`, so node-only rows can become complete |

## 7. Matrices by area

Columns: Cov ID · design item (state or input) · source (page § and inventory id) · current code path
(file at `39d440c4f`, or `lane:path`) · status · PlanDB task(s). Each area opens with its writer's
verification notes as of 13:45 on 2026-10-09. The auditor reports that landed later were reconciled in
§5.3 and §6, not rewritten into these notes.

### 7.1 Foundations and shared primitives

Verified at HEAD 39d440c4f in /home/santosh/codeaf-design-plan/desktop. In-flight lane state read from worktrees under /home/santosh/codeaf-workspace/.claude/worktrees (s1-menus agent-a320e518, s1-preview agent-a7359f65, s1-history agent-ab15dab6). Icon availability checked against @animateicons/react 0.12.0 (lucide set, 669 glyphs). Paths are relative to desktop/src unless prefixed.

Notes on status: token rows whose value already equals the design are `partial` because no test asserts the value yet (closed by t-d5-qa-prim-foundations-spec). `complete` rows name the asserting test.

| Cov ID | Design item (state/input) | Source (page §id + inventory ID) | Current code path(s) (HEAD file or lane:path) | Status | Task |
|---|---|---|---|---|---|
| FD-COL-1 | --canvas light + dark value (value matches design) | Foundations §03 Colour; F-COL-1 | desktop/src/design/tokens.json themes.light/dark.canvas; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-2 | --frame light + dark value (value matches design) | Foundations §03 Colour; F-COL-2 | desktop/src/design/tokens.json themes.light/dark.frame; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-3 | --surface light + dark value (value matches design) | Foundations §03 Colour; F-COL-3 | desktop/src/design/tokens.json themes.light/dark.surface; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-4 | --field light + dark value (value matches design) | Foundations §03 Colour; F-COL-4 | desktop/src/design/tokens.json themes.light/dark.field; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-5 | --field-2 light + dark value (value matches design) | Foundations §03 Colour; F-COL-5 | desktop/src/design/tokens.json themes.light/dark.field-2; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-6 | --bubble light + dark value (value matches design) | Foundations §03 Colour; F-COL-6 | desktop/src/design/tokens.json themes.light/dark.bubble; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-7 | --term light + dark value (value matches design) | Foundations §03 Colour; F-COL-7 | desktop/src/design/tokens.json themes.light/dark.term; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-8 | --line light + dark value (value matches design) | Foundations §03 Colour; F-COL-8 | desktop/src/design/tokens.json themes.light/dark.line; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-9 | --guide light + dark value (value matches design) | Foundations §03 Colour; F-COL-9 | desktop/src/design/tokens.json themes.light/dark.guide; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-10 | --ink light + dark value (value matches design) | Foundations §03 Colour; F-COL-10 | desktop/src/design/tokens.json themes.light/dark.ink; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-11 | --ink-2 light + dark value (value matches design) | Foundations §03 Colour; F-COL-11 | desktop/src/design/tokens.json themes.light/dark.ink-2; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-12 | --ink-3 light + dark value (value matches design) | Foundations §03 Colour; F-COL-12 | desktop/src/design/tokens.json themes.light/dark.ink-3; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-13 | --accent light + dark value (value matches design) | Foundations §03 Colour; F-COL-13 | desktop/src/design/tokens.json themes.light/dark.accent; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-14 | --accent-ink light + dark value (value matches design) | Foundations §03 Colour; F-COL-14 | desktop/src/design/tokens.json themes.light/dark.accent-ink; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-15 | --accent-soft light + dark value (value matches design) | Foundations §03 Colour; F-COL-15 | desktop/src/design/tokens.json themes.light/dark.accent-soft; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-16 | --accent-rule light + dark value (value matches design) | Foundations §03 Colour; F-COL-16 | desktop/src/design/tokens.json themes.light/dark.accent-rule; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-17 | --amber light + dark value (value matches design) | Foundations §03 Colour; F-COL-17 | desktop/src/design/tokens.json themes.light/dark.amber; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-18 | --amber-soft light + dark value (value matches design) | Foundations §03 Colour; F-COL-18 | desktop/src/design/tokens.json themes.light/dark.amber-soft; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-19 | --danger light + dark value (value matches design) | Foundations §03 Colour; F-COL-19 | desktop/src/design/tokens.json themes.light/dark.danger; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-20 | --danger-soft light + dark value (value matches design) | Foundations §03 Colour; F-COL-20 | desktop/src/design/tokens.json themes.light/dark.danger-soft; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-21 | --success light + dark value (value matches design) | Foundations §03 Colour; F-COL-21 | desktop/src/design/tokens.json themes.light/dark.success; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-22 | --diff-add light + dark value (value matches design) | Foundations §03 Colour; F-COL-22 | desktop/src/design/tokens.json themes.light/dark.diff-add; styles/tokens.css | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-23 | History/edge "+12" stat colour uses --success (design literal oklch(.6 .11 155) is an inconsistency) | Foundations §03; F-COL-23 | lanes add duplicates: s1-history `history-added`, s1-preview `preview-add-ink` (both literal) | missing | t-d5-int-tok-e-legacy |
| FD-COL-24 | --tab selected-rail-row lift: light oklch(1 0 0 / .55), dark oklch(1 0 0 / .09) | Foundations §03 / Components Places rail; F-COL-24, C-PLACE-5 | absent (only legacy tab-selected hex) | missing | t-d5-int-tok-a-colour |
| FD-COL-25 | --tab-hover light/dark | F-COL-25 | tokens.json themes.*.tab-hover | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-26 | Links: accent text, underline on hover | Foundations §03 --accent "links"; F-COL-26 | styles/markdown.css:23-24 (ink + ink-2 underline, field hover) — differs | partial | t-d5-tok-link-accent |
| FD-COL-27 | ::selection uses --accent-soft | Foundations §03 --accent-soft "Selection"; F-COL-15 | no ::selection rule anywhere in desktop/src | missing | t-d5-int-tok-a-colour |
| FD-COL-28 | Legacy hex chrome palette (sidebar,text,muted,border,menu-highlight,chrome-*,overlay-surface…) retired from tokens and consumers | F-COL-27, ambiguity 9 | tokens.json themes.* legacy keys; ui.css:95-106,128-131,143-155; App.css:8 chrome-tint | partial | t-d5-int-tok-e-legacy |
| FD-COL-29 | Site monogram hues 1..6 | F-COL-28, C-CHIP-7 | tokens.json site-icon-hue-1..6 | partial | t-d5-qa-prim-foundations-spec |
| FD-COL-30 | Light/Dark parity: every theme key exists in both themes and resolves | Foundations §03 "each swatch light then dark" | tokens.json themes (no parity check in scripts/design-policy.mjs) | missing | t-d5-qa-prim-tokens-unit |
| FD-COL-31 | System appearance follows OS light/dark live | Foundations §03 | design/ThemeProvider.tsx; tests/ui/theme-motion.spec.ts "System: themed menus…" (emulates dark, asserts data-resolved-theme) | complete | — |
| FD-TINT-1a | Tint sand: h 65, a .1 | Foundations §02; F-TINT-1 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-1b | Tint sand swatch colour oklch(.65 .1 65) | Foundations §02; F-TINT-1, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-2a | Tint sage: h 150, a .09 | Foundations §02; F-TINT-2 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-2b | Tint sage swatch colour oklch(.6 .09 150) | Foundations §02; F-TINT-2, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-3a | Tint tide (default): h 240, a .13 | Foundations §02; F-TINT-3 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-3b | Tint tide (default) swatch colour oklch(.58 .13 240) | Foundations §02; F-TINT-3, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-4a | Tint iris: h 285, a .14 | Foundations §02; F-TINT-4 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-4b | Tint iris swatch colour oklch(.58 .14 285) | Foundations §02; F-TINT-4, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-5a | Tint rose: h 12, a .13 | Foundations §02; F-TINT-5 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-5b | Tint rose swatch colour oklch(.6 .13 12) | Foundations §02; F-TINT-5, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-6a | Tint graphite (Now): h 250, a .012 | Foundations §02; F-TINT-6 | desktop/src/design/tokens.json tints.hues; styles/tokens.css:579-585 | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-6b | Tint graphite (Now) swatch colour oklch(.55 .012 250) | Foundations §02; F-TINT-6, C-PLACE-22 | absent | missing | t-d5-int-tok-a-colour |
| FD-TINT-7 | [data-tint] on any container re-hues neutrals + accent | F-TINT-7 | styles/tokens.css:579-585; components/specimens/ControlsSpecimen.tsx (tint segmented) | partial | t-d5-qa-prim-foundations-spec |
| FD-TINT-8 | Swatch geometry 10/r3 (rail, tab), 12/r3.6 (tile), 14/r4.2 (Quick Look), 16/r4.8 (palette, page title); radius 30% of size | F-TINT-8, C-TAB-1, C-PLACE-4/17/22, C-PAGE-1 | absent (legacy swatch-width 54 / radius-swatch 5) | missing | t-d5-prim-swatch |
| FD-TINT-9 | PlaceSwatch primitive: Light + Dark, graphite reads neutral, selected double ring (creating tile) | C-PLACE-20/22 | absent | missing | t-d5-prim-swatch |
| FD-TINT-10 | Place switch: frame tint crossfade 320ms (blur unchanged) | Foundations §08/§09 "Place switch"; F-MO-15 | absent | missing | t-d5-prim-material |
| FD-MAT-1 | Translucent frame only (rail, strip, window edges): --frame at 80% light / 84% dark over blur | Foundations §09; F-MAT-1 | App.css:8 mac uses legacy --chrome-tint #eed9b240 | partial | t-d5-prim-material |
| FD-MAT-2 | macOS NSVisualEffectView .sidebar, behind-window, follows active state | Foundations §09; F-MAT-2 | src-tauri/tauri.macos.conf.json windowEffects ["sidebar"], followsWindowActiveState (no test) | partial | t-d5-prim-material |
| FD-MAT-3 | Windows 11 Mica Alt + same tint layer | Foundations §09 | absent (tauri.conf.json main window has no effects) | missing | t-d5-prim-material |
| FD-MAT-4 | ASSUME: Linux native = solid --frame (no compositor blur guaranteed) | Foundations §09 (silent on Linux) | tauri.conf.json decorations only; .app-shell background var(--frame) | missing | t-d5-prim-material |
| FD-MAT-5 | Web fallback backdrop-filter blur(40px) saturate(1.4) | Foundations §09; F-MAT-3 | absent | missing | t-d5-prim-material |
| FD-MAT-6 | Inactive window: vibrancy off, solid --frame | Foundations §09; F-MAT-4 | absent in CSS (native followsWindowActiveState only on mac) | missing | t-d5-prim-material |
| FD-MAT-7 | Reduce Transparency / Increase Contrast: solid --frame, no blur | Foundations §09; F-MAT-4 | absent (no prefers-reduced-transparency / prefers-contrast rules) | missing | t-d5-prim-material |
| FD-MAT-8 | Rail text on translucency uses ink / ink-2 only, never ink-3 | Foundations §09; F-MAT-5 | rail is legacy (features/shell/Rail.tsx, --muted); rail rebuild in t-s1-rail | in-flight:t-s1-rail | t-s1-rail (+ t-d5-qa-prim-foundations-spec asserts) |
| FD-MAT-9 | Never stack two blurs; cards/popovers/sheets opaque | Foundations §09 "Where" | palette backdrop blur 3px (ui.css:175) over frame — would stack once frame blurs | missing | t-d5-prim-material |
| FD-MAT-10 | Multiwindow: each window resolves its own active/inactive material | Foundations §09 inactive window | single window only | missing | t-d5-prim-material |
| FD-TYPE-1 | Title 20/1.3/600/-1.5% | Foundations §04; F-TYPE-3 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-2 | Heading 1 17/1.35/600 | Foundations §04; F-TYPE-4 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-3 | Heading 2 15/1.4/600 | Foundations §04; F-TYPE-5 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-4 | Heading 3 13/1.5/600 | Foundations §04; F-TYPE-6 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-5 | Prose 13/1.6/400 | Foundations §04; F-TYPE-7 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-6 | Label 12/1.45/400·500 | Foundations §04; F-TYPE-8 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-7 | Caption 11/1.4/500 | Foundations §04; F-TYPE-9 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-8 | Mono 12/1.5 | Foundations §04; F-TYPE-10 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-9 | Mono small 11 tabular | Foundations §04; F-TYPE-11 | desktop/src/design/tokens.json type-* | partial | t-d5-qa-prim-foundations-spec |
| FD-TYPE-10 | Font stacks sans (SF Pro, Segoe UI Variable) / mono (SF Mono, Cascadia); legacy font-sans/font-mono differ | F-TYPE-1/2 | tokens sans/mono yes; legacy font-sans/font-mono still used (ui.css .keyboard-shortcut, legacy chrome) | partial | t-d5-int-tok-e-legacy |
| FD-TYPE-11 | Place page title 28/600/-.022em | F-TYPE-13, C-PAGE-1 | absent | missing | t-d5-int-tok-b-dimensions |
| FD-TYPE-12 | Task view title follows Title 20/1.3/600/-.015em | F-TYPE-17, C-TREE-14 | task-view-title-size 22, tracking -.018em | partial | t-d5-int-tok-b-dimensions |
| FD-TYPE-13 | Legacy type tokens (caption 10, title 25, semibold 550…) retired | F-TYPE-16 | tokens.json foundation font-size-* | partial | t-d5-int-tok-e-legacy |
| FD-SP-1 | space-1..12 4pt scale | Foundations §05; F-SP-1..8 | desktop/src/design/tokens.json foundation | partial | t-d5-qa-prim-foundations-spec |
| FD-SP-2 | rhythm tokens turn 32 / answer 14 / work-row 4 / work-block 10 / indent 20 | Foundations §05; F-SP-9..13 | desktop/src/design/tokens.json foundation | partial | t-d5-qa-prim-foundations-spec |
| FD-SP-3 | column 680, row-h 28, hit 32, panel 300 | Foundations §05; F-SP-14..16 | desktop/src/design/tokens.json foundation | partial | t-d5-qa-prim-foundations-spec |
| FD-SP-4 | hit 32 applied to icon-only controls on coarse pointers (touch) | Foundations §05 "row-h · hit 28 · 32" | --hit defined but no var(--hit) consumer | missing | t-d5-int-tok-b-dimensions |
| FD-RAD-1 | Radius chip 6 / control 8 / card 12 / bubble 16 / dock 20 | Foundations §05; F-RAD-1..5 | desktop/src/design/tokens.json radius-* | partial | t-d5-qa-prim-foundations-spec |
| FD-RAD-2 | Menu radius 10 token | F-RAD-6 | absent at HEAD; s1-menus adds radius-menu 10 | in-flight:t-s1-menus | t-s1-menus |
| FD-RAD-3 | Legacy radius-md 9 retired (.hover-preview, .app-menu, .select-menu) | F-RAD-7 | ui.css:128,143,153 | partial | t-d5-int-tok-e-legacy |
| FD-SH-1 | sh-1 / sh-2 / sh-3 light + dark | Foundations §05 Depth; F-SH-1..3 | tokens.json themes.*.sh-1..3 | partial | t-d5-qa-prim-foundations-spec |
| FD-SH-2 | Depth order: popovers sh-2, sheets/Quick Look sh-3; legacy shadow/palette-shadow retired | F-SH-4/5 | ui.css .app-menu/.select-menu palette-shadow, .hover-preview shadow | partial | t-d5-int-tok-e-legacy |
| FD-MARK-1 | Running 6px accent dot in 14px box + mono clock | Foundations §06; F-MARK-1 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-2 | Queued 8px hollow ring 1.5 | Foundations §06; F-MARK-2 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-3 | Done check 14 ink-3 | Foundations §06; F-MARK-3 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-4 | Your call 6px amber dot | Foundations §06; F-MARK-4 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-5 | Failed triangle-alert 14 danger; red dot in dense rows | Foundations §06; F-MARK-5 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-6 | Stopped circle-slash → ban per Q34 | Foundations §06; F-MARK-6 | components/ui/StatusMark.tsx; ui.css:60-73 | n/a-decided | — (Q34: keep ban); spec still asserts data-icon=ban |
| FD-MARK-7 | Paused pause 14 ink-3 | Foundations §06; F-MARK-7 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-8 | Incomplete circle-dashed 14 ink-3 | Foundations §06; F-MARK-8 | components/ui/StatusMark.tsx; ui.css:60-73 | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-9 | Incomplete in dense rows draws ink-3 (impl colours it --mark-failed; design never shows red) | F-MARK-8 | ui.css:71 | partial | t-d5-int-tok-b-dimensions |
| FD-MARK-10 | Status marks never animate except header breath; words carry meaning (aria-label) | Foundations §06 | StatusMark role=img aria-label; no animation | partial | t-d5-qa-prim-primitives-spec |
| FD-MARK-11 | Breathing dot primitive: cf-breathe 2.8s on header "N running" only | Foundations §06/§08; F-MO-10, C-NAV-2 | absent (no keyframes) | missing | t-d5-prim-motion |
| FD-STEP-1 | Step category search → search | Foundations §07; F-ICON-3 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-2 | Step category read → book-open | Foundations §07; F-ICON-4 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-3 | Step category edit → pencil-line ⇒ pencil (Q34); impl FilePen | Foundations §07; F-ICON-5 | components/ui/Icon.tsx icons map | partial | t-d5-int-tok-d-icons |
| FD-STEP-4 | Step category create → file-plus-2 ⇒ file-plus (Q34) | Foundations §07; F-ICON-6 | components/ui/Icon.tsx icons map | n/a-decided | — (Q34 plain file icons); asserted by t-d5-qa-prim-foundations-spec |
| FD-STEP-5 | Step category run → terminal | Foundations §07; F-ICON-7 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-6 | Step category test → flask-conical | Foundations §07; F-ICON-8 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-7 | Step category browse → globe (impl Compass) | Foundations §07; F-ICON-9 | components/ui/Icon.tsx icons map | partial | t-d5-int-tok-d-icons |
| FD-STEP-8 | Step category transfer → arrow-left-right | Foundations §07; F-ICON-10 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-9 | Step category communicate → message-square (impl MessageCircle) | Foundations §07; F-ICON-11 | components/ui/Icon.tsx icons map | partial | t-d5-int-tok-d-icons |
| FD-STEP-10 | Step category coordinate → network | Foundations §07; F-ICON-12 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-11 | Step category plan → list-checks (impl Map) | Foundations §07; F-ICON-13 | components/ui/Icon.tsx icons map | partial | t-d5-int-tok-d-icons |
| FD-STEP-12 | Step category wait → hourglass | Foundations §07; F-ICON-14 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-STEP-13 | Step category work → wrench | Foundations §07; F-ICON-15 | components/ui/Icon.tsx icons map | partial | t-d5-qa-prim-foundations-spec |
| FD-ICON-1 | Glyph sizes 11/12/13/14/15/16 | F-ICON-1 | tokens icon-micro/xs/sm/md, file-chip-icon, composer-plus-size | partial | t-d5-qa-prim-foundations-spec |
| FD-ICON-2 | Stroke: lucide default 2 (impl --icon-stroke 1.6) | F-ICON-2, ambiguity 13 | ui.css:14 stroke-width var(--icon-stroke)=1.6 | partial | t-d5-int-tok-b-dimensions |
| FD-ICON-3 | Chrome icon square-terminal ⇒ terminal (nearest) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — terminal exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-4 | Chrome icon file-code-2 ⇒ file-code (Q34) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — fileCode exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-5 | Chrome icon file-json ⇒ file-code (nearest) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent mapping | missing | t-d5-int-tok-d-icons |
| FD-ICON-6 | Chrome icon file-x-2 ⇒ file-x (Q34) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — fileMissing exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-7 | Chrome icon file-lock-2 ⇒ file-lock (Q34) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — fileLock exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-8 | Chrome icon text-quote ⇒ quote | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent | missing | t-d5-int-tok-d-icons |
| FD-ICON-9 | Chrome icon folder-tree | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent (in set) | missing | t-d5-int-tok-d-icons |
| FD-ICON-10 | Chrome icon chevrons-up-down | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent (in set) | missing | t-d5-int-tok-d-icons |
| FD-ICON-11 | Chrome icon chevrons-down-up ⇒ fold-vertical | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent | missing | t-d5-int-tok-d-icons |
| FD-ICON-12 | Chrome icon columns-2 (Open in split) ⇒ no set icon; ASSUME use layout-grid? keep `split` | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — split=PanelLeft | missing | t-d5-int-tok-d-icons |
| FD-ICON-13 | Chrome icon app-window (Move to new window) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent at HEAD; s1-menus adds appWindow | in-flight:t-s1-menus | t-s1-menus |
| FD-ICON-14 | Chrome icon link (Copy link) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent at HEAD; s1-menus adds link | in-flight:t-s1-menus | t-s1-menus |
| FD-ICON-15 | Chrome icon archive | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent at HEAD; s1-history adds archive | in-flight:t-s1-history | t-s1-history |
| FD-ICON-16 | Chrome icon rotate-cw (Run again) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent (in set) | missing | t-d5-int-tok-d-icons |
| FD-ICON-17 | Chrome icon scroll-text (Open log) | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent (in set) | missing | t-d5-int-tok-d-icons |
| FD-ICON-18 | Chrome icon message-circle-reply ⇒ reply | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent | missing | t-d5-int-tok-d-icons |
| FD-ICON-19 | Chrome icon maximize-2 ⇒ expand | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — expand exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-20 | Chrome icon ungroup ⇒ no set icon; ASSUME text-only menu row | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — absent | missing | t-d5-int-tok-d-icons |
| FD-ICON-21 | Chrome icon square (Stop) ⇒ CSS 10px r2 square | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — composer-only CSS; Icon stop=CircleStop | missing | t-d5-prim-stop-glyph |
| FD-ICON-22 | Chrome icon clock-3 ⇒ clock | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — clock exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-23 | Chrome icon file-diff ⇒ diff | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — diff exists | n/a-decided | — (Q34 nearest set icon); asserted by t-d5-qa-prim-foundations-spec |
| FD-ICON-24 | Chrome icon list-checks for tab kinds | Components (mask URLs); F-ICON-16, Q34 | components/ui/Icon.tsx — checklist exists | partial | t-d5-qa-prim-foundations-spec |
| FD-ICON-25 | panel-right maps to PanelLeftIcon (mirrored glyph); set has no panel-right ⇒ mirror via data-mirror | F-ICON-17 | Icon.tsx panelRight: PanelLeftIcon | partial | t-d5-int-tok-d-icons |
| FD-ICON-26 | Stopped glyph circle-slash vs ban | F-MARK-6, C-NOTE-1, C-TOOL-7, Q34 | Icon ban → BanIcon | n/a-decided | — (Q34: Designer keep ban) |
| FD-ICON-27 | Icon registry specimen shows every name at 13/14/16 in Light + Dark | Foundations §07 | App.tsx Design system "Icon family" (IconButtons, legacy page) | partial | t-d5-prim-foundations-specimen |
| FD-MO-1 | ease cubic-bezier(.2,.8,.2,1) / spring (.34,1.3,.64,1) | F-MO-1/2 | tokens ease, spring | partial | t-d5-qa-prim-foundations-spec |
| FD-MO-2 | fast 120 / base 200 / slow 320 / spring 380 | F-MO-3..6 | tokens dur-* | partial | t-d5-qa-prim-foundations-spec |
| FD-MO-3 | Legacy durations (duration 220, duration-fast 140, overlay 240, directional 280, work 300) retired | F-MO-3/4 | tokens.json; ui.css legacy consumers | partial | t-d5-int-tok-e-legacy |
| FD-MO-4 | Rises: row enter 4px, send 6px, toast 8px (impl motion-travel 3px) | F-MO-4/12, ambiguity 15 | motion-travel 3px; latest-rise 4px | partial | t-d5-int-tok-c-motion |
| FD-MO-5 | Fold chevron-right 0→90° at base | F-MO-8 | motion-ask-open 90deg used by work/thinking/earlier; motion-chevron-open 180deg for dropdown chevron-down | partial | t-d5-int-tok-c-motion |
| FD-MO-6 | cf-shimmer shared keyframe (2.4s linear ∞, ink-2→ink→ink-2, 250%) | Foundations §08; F-MO-9 | three private copies: latest-pill.css:22, thinking.css:14, task-log.css:25 | partial | t-d5-prim-motion |
| FD-MO-7 | Shimmer on the one live step in view (work step title) | Foundations §08; C-WORK-6, C-NAV-4 | work-step.css has none | missing | t-d5-prim-motion (primitive); wiring in conversation area |
| FD-MO-8 | cf-breathe keyframe 2px→4.5px accent-soft 2.8s | Foundations §08; F-MO-10 | absent | missing | t-d5-prim-motion |
| FD-MO-9 | Reduced motion: everything still, clock alone moves (incl. shimmer/breathe stop) | Foundations §08; F-MO-11 | tokens.css:798 reduced block; tests/ui/theme-motion.spec.ts "reduced motion disables every shared transition and keyframe" | partial | t-d5-qa-prim-primitives-spec |
| FD-MO-10 | Popover: fade from .98 scale at origin, fast (impl surface-enter .985 + 3px, 240ms) | F-MO-17, C-MENU-10 | ui.css:132 surface-enter; s1-menus restyles menus | partial | t-d5-int-tok-c-motion |
| FD-MO-11 | Quick Look: fade from .96 scale over 20% scrim, base | F-MO-16 | absent | missing | t-d5-prim-quicklook |
| FD-MO-12 | Question paging 120ms crossfade, no slide | F-MO-13 | tray (conversation area) | partial | t-d5-qa-prim-foundations-spec (token); wiring conversation area |
| FD-MO-13 | Tab close: width → 0, neighbours slide (base) | F-MO-14 | ui.css tab-enter only | missing | t-d5-int-tok-c-motion (token motion-tab-collapse); wiring shell area |
| FD-MO-14 | Place switch: strip 6px horizontal slide toward the rail (slow) | F-MO-15 | absent | missing | t-d5-int-tok-c-motion (token); wiring shell/places area |
| FD-MO-15 | Animated icons play once on change: check, triangle-alert, circle-slash(ban), pause, chevron-right, panel-right, arrow-up, square, archive, sparkles, copy | Foundations §08 icon list; F-MO-18 | Icon.tsx isAnimated={false}; motionByName all none except arrow/chevron | missing | t-d5-int-tok-d-icons |
| FD-MO-16 | Copy → check for 1.2s (impl copiedFeedbackMs 1500) | F-MO-18 | tokens interaction.copiedFeedbackMs 1500; CopyButton.tsx | partial | t-d5-int-tok-c-motion |
| FD-MO-17 | Tooltip delay 500ms; warm chain instant | F-MO-19, C-OVL-3 | Tooltip.tsx; tests/ui/tabs.spec.ts + theme-motion.spec.ts (tooltip visible after hover) | complete | — |
| FD-MO-18 | Spring only for tray rising / bubble landing ("never bounce") | Foundations §08 rules | spring token; tray-shell.css (conversation) | partial | t-d5-qa-prim-foundations-spec |
| FD-MO-19 | Press 80ms (impl extra, not in design) | F-MO-7 | dur-press | partial | t-d5-qa-prim-foundations-spec (keep impl extra; assert token only) |
| FD-FOC-1 | Keyboard focus ring: 2px accent + 4px accent-soft halo (D6); impl halo 6px | D6; C-CTRL-10 | ui.css:11 focus-ring-width 2 / focus-halo-width 6 | partial | t-d5-int-tok-b-dimensions |
| FD-FOC-2 | No ring on mouse click/typing (pointer modality) | D6 | design/inputModality.ts; ui.css:10; tests/ui/composer-constant.spec.ts | complete | — |
| FD-FOC-3 | Field focus: surface bg, 1px accent ring + 4px halo, accent caret | C-CTRL-10 | ui.css:86 surface bg only; global ring 2px/6px; field-focus-ring tokens unused | partial | t-d5-int-tok-b-dimensions |
| FD-FOC-4 | Legacy chrome scopes draw outline in legacy --focus-ring colour | ui.css:105 | ui.css:105 | partial | t-d5-int-tok-e-legacy |
| FD-FOC-5 | Forced-colors: ring stays visible (transparent outline) | ASSUME (design silent) | ui.css:8-9; no test | partial | t-d5-qa-prim-primitives-spec |
| FD-DIS-1 | Disabled control opacity .6, field bg, ink-3 (impl .4) | C-CTRL-6 | opacity-control-disabled .4; theme-motion spec asserts token value | partial | t-d5-int-tok-b-dimensions |
| FD-DIS-2 | Legacy opacity-disabled .65 on bare button:disabled / menu items | — | ui.css:5,149 | partial | t-d5-int-tok-e-legacy |
| PR-BTN-1 | Button primary rest: 28h pad 12 r8 accent/accent-ink 12/500 (impl pad 14) | C-CTRL-1 | Button.tsx; ui.css:23-24 control-pad-primary 14 | partial | t-d5-int-tok-b-dimensions |
| PR-BTN-2 | Button primary hover brightness 1.06 (impl 1.07) | C-CTRL-1 | ui.css:30 | partial | t-d5-int-tok-b-dimensions |
| PR-BTN-3 | Button raised rest/hover/pressed | C-CTRL-2 | ui.css:25,32,34 | partial | t-d5-qa-prim-primitives-spec |
| PR-BTN-4 | Button quiet rest/hover/pressed/focus-visible | C-CTRL-3 | ui.css:26,33,35; tests/ui/theme-motion.spec.ts "shared hover, press, selection and disabled states" (Light+Dark) | complete | — |
| PR-BTN-5 | Button ghost ink-2, hover field + ink (impl ink-3) | C-CTRL-4, Q18 | ui.css:28 | partial | t-d5-int-tok-b-dimensions |
| PR-BTN-6 | Button danger rest/hover | C-CTRL-5 | ui.css:29-31 | partial | t-d5-qa-prim-primitives-spec |
| PR-BTN-7 | Button loading aria-busy | impl | Button.tsx | partial | t-d5-qa-prim-primitives-spec |
| PR-BTN-8 | Buttons inside legacy chrome scopes (.sidebar, .workspace-tabbar…) re-skinned with legacy palette | ui.css:95-106 | ui.css:95-106 | partial | t-d5-int-tok-e-legacy |
| PR-BTN-9 | 30h tray/best-match buttons vs 28 control (ASSUME: 28 control default, 30 is a size variant) | C-TRAY-4, ambiguity 10 | button-height 30 legacy, control-height 28 | missing | t-d5-prim-button-size |
| PR-IB-1 | IconButton 28 r8 14px icon ink-2; hover field+ink; pressed field-2 | C-CTRL-7 | Button.tsx IconButton; ui.css:36-41 | partial | t-d5-qa-prim-primitives-spec |
| PR-IB-2 | IconButton accessible name + tooltip (icon-only only) | C-OVL-3 | IconButton useTooltip; theme-motion spec Copy tooltip | complete | — |
| PR-IB-3 | IconButton touch: no tooltip; hit area 32 on coarse pointers | ASSUME (Foundations hit 32) | Tooltip skips touch; no hit expansion | partial | t-d5-int-tok-b-dimensions |
| PR-IB-4 | IconButton disabled | C-CTRL-6 | ui.css:22 | partial | t-d5-int-tok-b-dimensions |
| PR-SEG-1 | Segmented track field pad 2 r9; item 24h pad 10 r7; selected surface+sh-1 500 | C-CTRL-8 | Segmented.tsx; ui.css:44-47 | partial | t-d5-qa-prim-primitives-spec |
| PR-SEG-2 | Segmented selection by click | C-CTRL-8 | tests/ui/settings-models.spec.ts (aria-checked after click) | complete | — |
| PR-SEG-3 | Segmented keyboard: arrows move+select, one tab stop, focus ring | C-CTRL-8 | Segmented.tsx move(); no test | partial | t-d5-qa-prim-primitives-spec |
| PR-SEG-4 | Segmented hover/pressed field-2 / disabled | C-CTRL-8 | ui.css:46,22 | partial | t-d5-qa-prim-primitives-spec |
| PR-SEG-5 | ASSUME: Segmented at ≤600px wraps or scrolls, never overflows | design silent | none | missing | t-d5-qa-prim-primitives-spec |
| PR-FLD-1 | Field 30h pad 10 r8 field, ink-3 placeholder 12 | C-CTRL-9 | TextInput.tsx; ui.css:84-85 | partial | t-d5-qa-prim-primitives-spec |
| PR-FLD-2 | Field focus surface + 1px ring + 4px halo | C-CTRL-10 | see FD-FOC-3 | partial | t-d5-int-tok-b-dimensions |
| PR-TAG-1 | Tag "Suggested" plain: text only 11/500 ink-3 (impl accent tone with accent-soft fill) | C-CTRL-11 | Chip.tsx Tag tones accent/neutral/danger/key | partial | t-d5-prim-tag-plain |
| PR-TAG-2 | Tag neutral 18h pad 6 r5 field ink-2 | C-CTRL-12 | ui.css:53,55 | partial | t-d5-qa-prim-primitives-spec |
| PR-TAG-3 | Tag danger danger-soft/danger | C-CTRL-13 | ui.css:56 | partial | t-d5-qa-prim-primitives-spec |
| PR-KBD-1 | Kbd hint 18h pad 5 r5 surface inset .5 line mono 500 11 ink-3 (tag key is ink-2) | C-CTRL-14 | Chip.tsx Tag key; ui.css:57 | partial | t-d5-prim-kbd |
| PR-KBD-2 | KeyboardShortcut uses legacy font-sans / caption 10 / inherit colour | C-CTRL-14, C-MENU-5 | KeyboardShortcut.tsx; ui.css:118 | partial | t-d5-prim-kbd |
| PR-KBD-3 | Platform spelling (⌘ on macOS, Ctrl on Linux/Windows) | Interactions shortcuts | design/keyboard.ts formatShortcut; no test of KeyboardShortcut output | partial | t-d5-prim-kbd |
| PR-MENU-1 | Menu container surface r10 sh-2 pad 5; widths 240 tab / 200 group | C-MENU-1 | Menu.tsx; ui.css:143 legacy at HEAD | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-2 | Menu row 28h pad 0 10 r6 gap 10 13px; icon 14 ink-2 | C-MENU-2 | ui.css:147 (32h, 12px) at HEAD | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-3 | Menu row hover/keyboard highlight field-2 (not accent) | C-MENU-3 | ui.css:149 menu-highlight at HEAD | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-4 | Submenu chevron-right 12 ink-3 | C-MENU-4 | Menu.tsx SubTrigger | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-5 | Menu shortcut right-aligned 12 ink-3 | C-MENU-5 | KeyboardShortcut in Menu | in-flight:t-s1-menus | t-s1-menus (+ t-d5-prim-kbd for the primitive) |
| PR-MENU-6 | Separator .5px line margin 4 8 | C-MENU-6 | ui.css:150 | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-7 | Destructive row danger text, no icon | C-MENU-7 | ui.css:150 chrome-danger at HEAD | in-flight:t-s1-menus | t-s1-menus |
| PR-MENU-8 | Menu disabled row | — | ui.css:149 legacy opacity-disabled | partial | t-d5-int-tok-e-legacy |
| PR-MENU-9 | Menu keyboard: Shift+F10/ContextMenu key opens; first item focus; Esc returns focus | Interactions | Menu.tsx; tests/ui/theme-motion.spec.ts (dropdown keyboard); responsive.spec.ts "themes nested menus" | complete | — |
| PR-MENU-10 | Menu popover motion .98 fast | C-MENU-10 | ui.css surface-enter | partial | t-d5-int-tok-c-motion |
| PR-MENU-11 | Select (theme/role pickers) rows 28, field-2 highlight, surface r10 sh-2 (impl 32, menu-highlight, overlay-surface, radius-md) | C-MENU-*, ambiguity 9 | Select.tsx; ui.css:126-131 | partial | t-d5-prim-select-restyle |
| PR-MENU-12 | Native macOS app menu remains the system menu (not themed) | ASSUME | src-tauri menu (owned by native writer) | missing | t-d5-nat-window-menu |
| PR-MENU-13 | ASSUME: menus at ≤600px clamp to viewport (collisionPadding) and stay keyboard reachable | design silent | Menu.tsx collisionPadding; responsive.spec.ts | complete | — |
| PR-PREV-1 | HoverPreview/HoverCard 300w surface r12 sh-2 pad 12 14 14, kind/state row | C-PREV-1..4 | at HEAD 240w legacy; s1-preview adds HoverCard + preview-card-* tokens | in-flight:t-s1-preview | t-s1-preview |
| PR-PREV-2 | Legacy .hover-preview CSS (border, radius-md 9, overlay-surface, shadow) retired after s1-preview | C-PREV-1, F-RAD-7 | ui.css:153-155 | partial | t-d5-int-tok-e-legacy |
| PR-PREV-3 | Hover preview open delay (650, design silent) | F-MO-20 | interaction.previewOpenDelay; s1-preview changes | in-flight:t-s1-preview | t-s1-preview |
| PR-TIP-1 | Tooltip 24h pad 0 8 r6 surface sh-2 11px (impl pad-y 4, no fixed height) | C-OVL-3 | ui.css:89 | partial | t-d5-int-tok-b-dimensions |
| PR-TIP-2 | Tooltip shortcut in ink-3 | C-OVL-3, C-TAB-11 | s1-menus adds .tooltip-shortcut | in-flight:t-s1-menus | t-s1-menus |
| PR-TIP-3 | Tooltip only icon-only controls + truncated text; never on touch; keyboard focus only; Esc/scroll/press dismiss | C-OVL-3 | Tooltip.tsx; tests/ui/tabs.spec.ts:141-154 | complete | — |
| PR-TIP-4 | Tooltip Light + Dark themed surface + axe | C-OVL-3 | no themed assertion on .tooltip | partial | t-d5-qa-prim-primitives-spec |
| PR-TOAST-1 | Toast primitive: bottom-centre 40h r12 surface sh-2 pad 0 6 0 14 gap 12; accent dot or icon; ghost + field 26h actions | C-OVL-1/2 | absent at HEAD; lane-private ClosingToast (s1-menus) + ArchiveToast (s1-history) duplicate it | missing | t-d5-prim-toast |
| PR-TOAST-2 | Toast timing 6s close / 10s delete / launch archive; always offers Undo where structural | C-OVL-1, S-IX-10 | s1-menus toastDuration 6000 | missing | t-d5-prim-toast |
| PR-TOAST-3 | Toast enter fade + 8px rise base; reduced motion still | Foundations §08 | absent | missing | t-d5-prim-toast |
| PR-TOAST-4 | ASSUME: one toast at a time, newest replaces older (older keeps its Undo in ⌘Z history); hover/focus pauses the timer | design open question (shell #22) | absent | missing | t-d5-prim-toast |
| PR-TOAST-5 | ASSUME: toast is aria-live polite region; actions reachable by keyboard (F6 to landmark), Esc dismisses when focused | design silent | absent | missing | t-d5-prim-toast |
| PR-TOAST-6 | ASSUME: narrow ≤600 toast width clamps to viewport − 24, text ellipsizes before actions | design silent | absent | missing | t-d5-prim-toast |
| PR-TOAST-7 | ASSUME: multiwindow: toast shows only in the window where the action happened | design silent | absent | missing | t-d5-prim-toast |
| PR-TOAST-8 | Closing toast adopts primitive | C-OVL-1 | lane:s1-menus features/tabs/closing/ClosingToast.tsx | in-flight:t-s1-menus | t-d5-prim-toast-adopt-closing |
| PR-TOAST-9 | Archive toast adopts primitive | C-OVL-2 | lane:s1-history features/history/ArchiveToast.tsx | in-flight:t-s1-history | t-d5-prim-toast-adopt-archive |
| PR-QL-1 | Quick Look sheet shell: 420w canvas r14 sh-3 over 20% scrim; header swatch 14 + 17/600 title + "Space to close" 11; footer .5 line + primary | C-OVL-4 | absent (assets/Lightbox, PreviewSheet are file overlays; scrim #19151199) | missing | t-d5-prim-quicklook |
| PR-QL-2 | Quick Look Space toggles, Esc closes, focus trapped and returned | C-OVL-4, I-IKY-23 | absent | missing | t-d5-prim-quicklook |
| PR-QL-3 | Scrim token 20% (light/dark) | C-OVL-4 | asset-lightbox-scrim #19151199, palette-backdrop #19151126 | missing | t-d5-int-tok-a-colour |
| PR-QL-4 | ASSUME: narrow ≤600 Quick Look width = viewport − 32, scrolls inside | design silent | absent | missing | t-d5-prim-quicklook |
| PR-QL-5 | Quick Look reduced motion: no scale, fade only (or none) | Foundations §08 | absent | missing | t-d5-prim-quicklook |
| PR-SRCH-1 | Search field 40h r12 surface sh-1 pad 0 14; search 15 + 13 placeholder ink-3 + "⌘F" 11 | C-INPUT-3 | absent (overview-filter closest; s1-history has own 44h search) | missing | t-d5-prim-search-field |
| PR-SRCH-2 | Search field focus (keyboard ring), clear with Esc, ⌘F focuses | C-INPUT-3 | absent | missing | t-d5-prim-search-field |
| PR-SRCH-3 | ASSUME: narrow ≤600 search field full width, hint hidden | design silent | absent | missing | t-d5-prim-search-field |
| PR-FILT-1 | Filter tabs 26h pad 10 r7 12px; selected field fill 500; rest ink-2; hover field | C-INPUT-4 | absent | missing | t-d5-prim-filter-tabs |
| PR-FILT-2 | Filter tabs keyboard (arrows, one tab stop), focus ring | C-INPUT-4 | absent | missing | t-d5-prim-filter-tabs |
| PR-FILT-3 | ASSUME: narrow ≤600 filter tabs scroll horizontally with edge fade | design silent | absent | missing | t-d5-prim-filter-tabs |
| PR-CHIP-1 | File chip 20h pad 0 7 0 5 gap 5 r6 field; icon 12 ink-2; name 500; dir ink-3 middle-truncated | C-CHIP-1/6 | Chip.tsx; features/conversation/assets/FileChip.tsx, paths.ts | partial | t-d5-qa-prim-chips |
| PR-CHIP-2 | File chip with stats +N success / −N danger mono 11 | C-CHIP-2 | FileChip.tsx | partial | t-d5-qa-prim-chips |
| PR-CHIP-3 | Chip hover field-2 / keyboard focus ring / press | C-CHIP-3 | ui.css:52 | partial | t-d5-qa-prim-chips |
| PR-CHIP-4 | File chip missing (file-x, "not found", ink-3) / outside (file-lock, "outside this workspace") | C-CHIP-4/5, Q34 | FileChip.tsx | partial | t-d5-qa-prim-chips |
| PR-CHIP-5 | Link chip monogram tile 14 r4 hue, name 500, domain ink-3; text link 12px mark | C-CHIP-7/8 | LinkChip.tsx | partial | t-d5-qa-prim-chips |
| PR-CHIP-6 | Real favicon only for already-contacted domains | C-CHIP-9 | LinkChip.tsx / AssetContext favicons | partial | t-d5-qa-prim-chips |
| PR-CHIP-7 | Model chip 28h pad 8 r8 12 ink-2 chevron-down 11 | C-COMP-3 | composer/ModelPicker.tsx; tests/ui/composer-model.spec.ts (popover only) | partial | t-d5-qa-prim-chips |
| PR-CHIP-8 | Chip context menu (file chip: Open / Reveal / Copy path) | Interactions | FileActions.ts | partial | t-d5-qa-prim-chips |
| PR-DOT-1 | Needs-you amber 6px dot / failed red dot primitive shared by tabs, rail, tree, inbox | C-TAB-4/5, C-PLACE-6, C-TREE-6 | StatusMark dense; tab.css .tab-dot (own copy) | partial | t-d5-qa-prim-primitives-spec |
| PR-DOT-2 | Running never shows in rows/tabs (silent) | Foundations §08 rules | rule; tab/rail lanes | partial | t-d5-qa-prim-primitives-spec |
| PR-SCRIM-1 | One scrim token used by Quick Look, palette backdrop, task sheet backdrop, lightbox (ASSUME lightbox keeps darker 60%) | C-OVL-4 | palette-backdrop, asset-lightbox-scrim, task-backdrop-enter | partial | t-d5-int-tok-a-colour |
| PR-STOP-1 | Stop square 10px r2 currentColor glyph shared (composer, tab ⌥ close) | F-ICON-18 | composer.css only; Icon stop = CircleStop | partial | t-d5-prim-stop-glyph |
| PR-SPEC-1 | Foundations specimen (ramps light+dark side by side, tints+swatches, type, space, radii, depth, marks, step icons, motion loops, materials) | Foundations §01-§09 | App.tsx Design system page (legacy swatches) + ControlsSpecimen.tsx (roles, marks) | partial | t-d5-prim-foundations-specimen |
| PR-SPEC-2 | Primitives specimen for new primitives (toast, quick look, search, filters, kbd, plain tag, swatch, shimmer, breathe, select) | Components | absent | missing | t-d5-qa-prim-specimen |
| PR-SPEC-3 | Two rhythms specimen (conversation vs work rhythm) | Foundations §01 | absent | missing | t-d5-prim-foundations-specimen |

#### Open questions

1. Icon stroke: design masks lucide at stroke 2, tokens say 1.6 (ambiguity 13). ASSUME design wins: `icon-stroke` 2 in wave B; if the owner objects, revert one token.
2. Live-step shimmer (C-WORK-6) and header breath (C-NAV-2): this area ships the shared `cf-shimmer`/`cf-breathe` primitive; applying it to WorkStepView and ConversationBar belongs to the conversation writer (t-d5-cv-*), which should depend on t-d5-prim-motion. ASSUME the work-row title shimmers (Foundations "the one live step in view").
3. Toast stacking and keyboard access are unspecified (shell open question 22). ASSUME one toast at a time, newest replaces, hover/focus pauses, aria-live polite, Esc dismisses when focused, window-local.
4. columns-2 ("Open in split") and ungroup have no glyph in the animated set. ASSUME by the Q34 rule: "Open in split" keeps the existing `split` glyph; "Ungroup" row draws no icon.
5. square (Stop) is absent from the set. ASSUME a CSS-drawn 10px r2 square primitive (StopGlyph) is the "nearest" per Q34; the arrow-up→square morph becomes a 120ms crossfade.
6. 30h buttons (tray, best match) vs 28 control: ASSUME `size="tray"` variant 30h; 28 remains the default.
7. Lightbox scrim: design only states 20% for Quick Look. ASSUME image lightbox keeps its darker scrim but expressed as a themed token; palette backdrop and task sheet use the 20% `scrim`.
8. Linux / Windows materials: design names Mica Alt for Windows and is silent on Linux. ASSUME Linux = solid --frame; Windows Mica only if Tauri `windowEffects: ["micaAlt"]` (native writer owns tauri.conf.json, integrator patch here).
9. --success vs literal oklch(.6 .11 155) for history/preview stats (ambiguity 5). ASSUME --success; lanes s1-history/s1-preview each introduced a literal token which wave E folds into --success.
10. Incomplete mark coloured --mark-failed in dense rows (F-MARK-8) is not in the design. ASSUME ink-3 like the non-dense mark.
11. Animated icons need the library's imperative play handle; if @animateicons/react 0.12 offers none for a glyph, ASSUME the glyph stays static (absent, not broken).

#### Counts

- complete: 9
- partial: 138
- missing: 63
- in-flight: 17
- n/a-decided: 10
- total rows: 237

### 7.2 Shell chrome

Scope: Shell design page chrome and the Shell rows of Interactions, EXCEPT tab-kind contents (file/diff/terminal/web/history/settings/inbox panes) and EXCEPT places (place rows, Home tab, place switcher, All places, tints). Pinned Home tab, place-row menus/drag, ⌘0/⌃1–9/⌘P/⌘⇧P/⌘[/Space HANDLERS are the places writer's; this file only owns their chord recognizers (row SH-256..259, 265, 273).

Verified against HEAD 39d440c4f (`/home/santosh/codeaf-design-plan/desktop`) and the lane worktrees under `/home/santosh/codeaf-workspace/.claude/worktrees/` (read 2026-10-09). Lane state at read time: preview cf0536bd7 (+dirty), overview b803d4121, newtab 3a4ec2702 (committed during this pass), menus/split/rail/history/terminal/files uncommitted. "in-flight" = present only in a lane worktree, unverified; never complete.

Source shorthand: SH = `codeaf Shell.dc.html` §id; IX = `codeaf Interactions.dc.html` table/row; S-*/C-*/F-* = inventory IDs in design-shell.md / design-components.md. Lane short names: L-rail (a99c430e), L-menus (a320e518), L-split (ad64d2e1), L-overview (a4590d55), L-newtab (a2d4b3e6), L-preview (a7359f65), L-history (ab15dab6), L-term (aa899088). Paths are relative to `desktop/`.

#### 1. Window frame, page chrome, native chrome

| Cov ID | Design item (state/input) | Source (page §id + inventory ID) | Current code path(s) (HEAD file or lane:path) | Status | Task |
|---|---|---|---|---|---|
| SH-001 | Page rules: equal-width tabs that compress; state only for needs-you (amber) / failed (red); close on hover + active; pane title line only when split | SH intro; S-0-1, S-0-2 | `src/features/tabs/{Tab,TabStrip,SplitTab,PaneGrid}.tsx` | complete (shell-tabs.spec "tabs compress from 190px to 112px…", tabs.spec "tab close sits in a fixed slot…", shell-tabs.spec "a split is one merged tab…") | — |
| SH-002 | Window frame: `--frame` ground, rail 232 + main column padded 0 8px 8px 0, strip 46 | SH 2a/2h; S-0-3, HLP WIN/MAIN | `src/App.css` `.app-shell`, `.content-pane` (`--shell-card-inset`) | partial (rail 232 and strip 46 asserted in shell-tabs.spec "the strip, a tab and the content card match the shell design"; 8px right/bottom inset not asserted) | t-d5-sh-native-chrome-test |
| SH-003 | Content card radius 10, `--canvas`, `--sh-1` | S-0-4, HLP CARD | `App.css .content-card`, `tabs/workspace.css .workspace-pane` | complete (shell-tabs.spec "the strip, a tab and the content card match the shell design") | — |
| SH-004 | macOS traffic lights (overlay title bar, hidden title) at (16,18) | S-0-5, S-2b-2 | `src-tauri/tauri.macos.conf.json` (`titleBarStyle: Overlay`, `trafficLightPosition`), `tokens.json nativeWindow` | partial (configured; no test ties conf ↔ tokens ↔ `--native-controls-inset`) | t-d5-sh-native-chrome-test |
| SH-005 | Rail toggle (panel-left, 26px, ink-3) right-aligned in rail head | S-0-5, HLP RAIL0 | L-rail:`src/features/shell/RailToggle.tsx`, token `rail-toggle-size` | in-flight:t-s1-rail | — |
| SH-006 | Native material: macOS `sidebar` vibrancy behind window, follows active state | F-MAT-2 | `tauri.macos.conf.json windowEffects` | partial (configured, untested) | t-d5-sh-native-chrome-test |
| SH-007 | Browser/Linux fallback material: blurred `--frame` at 80% light / 84% dark, `backdrop-filter: blur(40px) saturate(1.4)` on rail + strip | F-MAT-1, F-MAT-3 | none (no tokens; L-rail peek uses `palette-blur` 3px) | missing | t-d5-int-tok-a-colour, t-d5-prim-material, t-d5-int-sh-app |
| SH-008 | Inactive window, Reduce Transparency, Increase Contrast → solid `--frame`, no blur | F-MAT-4 | none | missing | t-d5-int-tok-a-colour, t-d5-prim-material |
| SH-009 | Rail text on translucency uses ink / ink-2 only, never ink-3 | F-MAT-5 | L-rail:`src/features/shell/rail.css` | in-flight:t-s1-rail | — |
| SH-010 | ASSUME: Linux keeps the system-decorated title bar; no traffic-light gutter; strip starts at the 8px inset; no custom window buttons | design silent (draws macOS only); ambiguity 18 | `tauri.conf.json decorations:true`, `html[data-environment=desktop]` | partial (no test) | t-d5-sh-native-chrome-test |
| SH-011 | ASSUME: empty strip space and rail head drag the window; tabs, buttons, menus are no-drag | design silent; ambiguity 18 | `TabStrip.tsx` `data-tauri-drag-region` on `.workspace-tabbar`; but `.workspace-tab-actions` (the flex:1 spacer) is `-webkit-app-region: no-drag`, so empty strip space does NOT drag | partial | t-d5-sh-drag-regions, t-d5-sh-native-chrome-test |
| SH-012 | ASSUME: double-click on empty strip = OS zoom (macOS title-bar convention) | design silent | none (spacer is no-drag) | missing | t-d5-sh-drag-regions, t-d5-sh-native-chrome-test |
| SH-013 | ASSUME: Focus mode / peek keep native traffic lights visible; a revealed strip keeps the native-controls gutter | ambiguity 18; SH 2h focus mode | L-rail:`useShellFrame.ts` (no traffic-light handling) | researched: `docs/research/d5-focus-traffic-lights.md`, native command pending | t-d5-sh-focus-native-lights |
| SH-014 | Native min 800×560; browser usable from 320px | AGENTS responsive contract | `tauri.conf.json`, `App.css` media queries | complete (responsive.spec "layouts stay usable from 320px browser to native minimum at ${width}px") | — |
| SH-015 | Light + Dark on shell chrome | SH root `theme` prop | `design/ThemeProvider.tsx` | complete (theme-motion.spec "${theme}: themed menus, palette, keyboard focus and accessible controls"; shell-tabs.spec "…every tab kind and state, light and dark") | — |
| SH-016 | Close Window ⌘⇧W closes the focused window only (macOS File menu) | IX Flows "Multiple windows" (implied) | `src-tauri/src/menu.rs` `window-close` | partial (no test) | t-d5-sh-native-menu-items, t-d5-sh-native-menu-test |

#### 2. Rail (non-place rows)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-020 | Rail 232px, no fill (frame shows through), rows 32 r8 13px ink-2 | SH 2h "Rail"; S-R-1, S-2h-12, HLP RAIL0 | HEAD `features/shell/Rail.tsx` = old sidebar; L-rail restyle | in-flight:t-s1-rail | — |
| SH-021 | Rail row selected = `--tab` fill + `--sh-1`, ink 500 (tab-like lift) | F-COL-24, C-PLACE-5 | no `tab` theme colour in tokens (HEAD or L-rail); L-rail uses `NavigationItem` | missing | t-d5-int-tok-a-colour, t-d5-sh-rail-rows |
| SH-022 | Rail row hover = `--tab-hover` fill | C-PLACE-8 | L-rail `rail.css` | in-flight:t-s1-rail | — |
| SH-023 | Rail rows keyboard-reachable; ring only on keyboard focus | IX Flows "Accessibility"; S-IX-13 | L-rail test "the rail, the strip toggle and Focus mode are accessible in light and dark" | in-flight:t-s1-rail | — |
| SH-024 | Inbox row: inbox 14 ink-3 + "Inbox" + amber 6px dot when anything needs you | SH 3a; S-R-2, C-PLACE-1 | none (L-rail DQ R3 ships Workspace/Activity/Settings/Design system only) | missing | t-d5-sh-rail-attention-model, t-d5-sh-rail-rows, t-d5-sh-rail-test |
| SH-025 | Inbox click → Inbox in the current place's strip, focused on the oldest needs-you item | IX Shell "Rail · Inbox"; S-R-9 | none | missing | t-d5-sh-rail-inbox-wire, t-d5-sh-rail-test |
| SH-026 | Inbox ⌘-click / middle → new tab | IX "Rail · Inbox"; S-R-9 | none | missing | t-d5-sh-rail-inbox-wire, t-d5-sh-rail-test |
| SH-027 | Inbox hover shows the count | IX "Rail · Inbox"; S-R-9 | none | missing | t-d5-sh-rail-rows, t-d5-sh-rail-test |
| SH-028 | Inbox / Now right-click: none ("—"). ASSUME the webview's default context menu is suppressed on rail rows | IX "Rail · Inbox/Now" right-click "—" | none | missing | t-d5-sh-rail-inbox-wire, t-d5-sh-rail-test |
| SH-029 | Now row: circle-dashed 14 + "Now" + count 11 ink-3 | SH 3a; S-R-3, C-PLACE-2 | none | missing | t-d5-sh-rail-attention-model, t-d5-sh-rail-rows, t-d5-sh-rail-test |
| SH-030 | Now click switches the window to Now (graphite, unplaced tabs) | IX "Rail · Now"; S-R-10 | none | missing | t-d5-sh-rail-now-wire, t-d5-sh-rail-test |
| SH-031 | Now ⌘-click / middle → new window on Now | IX "Rail · Now"; S-R-10 | none (no multiwindow) | missing | t-d5-sh-rail-now-wire, t-d5-sh-rail-test |
| SH-032 | ASSUME (rail silent; IX Flows "Settings" = a tab): Settings entry is one rail row that opens/focuses the single Settings tab | IX Flows "Settings"; S-IX-12 | L-rail `kinds/SettingsPane.tsx`, `openKind.ts`; test "Settings is a tab: the rail item opens it once…" | in-flight:t-s1-rail | — |
| SH-033 | Legacy rail items not in the design (BrandMark address search, Activity page, Design system page, "Find anything ⌘K", ThemeSelect) retire once Inbox/Now/Settings land | SH 3a rail (Inbox · Now · Places · All places); S-R-1..8 | HEAD `Rail.tsx`, `App.tsx pages`; L-rail keeps them (DQ R3) | partial | t-d5-sh-rail-legacy-retire, t-d5-int-sh-app, t-d5-sh-rail-test |
| SH-034 | ⌘S collapses / restores the rail | SH 2h; S-R-16, S-IX-18 | L-rail `useShellFrame.ts`, test "the toggle and ⌘S collapse the rail…" | in-flight:t-s1-rail | — |
| SH-035 | ⌘B kept as alias beside ⌘S (L-rail DQ R2) | design lists only ⌘S | HEAD `App.tsx` ⌘B; L-rail `keyboard.ts` `'s' or 'b'` | in-flight:t-s1-rail | — |
| SH-036 | Collapsed rail: traffic lights + rail toggle move into the strip | SH 2b/2d; S-2b-2, S-2d-2 | HEAD `strip.css` mac `--native-controls-inset`; L-rail `RailToggle placement` | in-flight:t-s1-rail | — |
| SH-037 | Peek: hovering the top 8px of a collapsed window brings the rail back as an overlay | SH 2h; S-R-17; L-rail DQ R4 | L-rail `useShellFrame.ts`, test "hovering the top 8px peeks…" | in-flight:t-s1-rail | — |
| SH-038 | Focus mode ⌘⇧F hides rail + strip; top 8px reveals; key or ⌘S exits | SH 2h; S-R-17, S-IX-18 | L-rail tests "Focus mode (⌘⇧F)…", "Focus mode: ⌘S leaves it…" | in-flight:t-s1-rail | — |
| SH-039 | Collapse has a real transition; reduced motion removes it | Foundations motion | `App.css` grid transition | complete (theme-motion.spec "navigation is still; action motion is bounded; collapse has a real transition", "reduced motion disables every shared transition and keyframe") | — |
| SH-040 | ≤600px: rail becomes a modal drawer with focus trap and dismissal | AGENTS responsive; design silent | `App.tsx` `.sidebar-drawer` | complete (responsive.spec "narrow navigation traps focus, themes nested menus, dismisses, and preserves desktop preference") | — |
| SH-041 | ASSUME ≤850px: rail compacts to 190px (`sidebar-width-compact`) | design silent | `App.css @media (max-width: 850px)` | partial (no assertion of the width) | t-d5-sh-rail-test |
| SH-042 | ASSUME coarse pointer: rail rows ≥40px hit target | design silent | none | missing | t-d5-tok-sh-touch, t-d5-sh-touch-targets, t-d5-sh-touch-test |
| SH-043 | ASSUME rail collapsed / Focus mode are per window (not mirrored to a second window on the same place) | IX Flows "Multiple windows" silent on chrome state | L-rail `useShellFrame` (component state) | missing | t-d5-sh-window-local-state |
| SH-044 | ASSUME the ≤600 drawer shows the same Inbox/Now rows | design silent | none | missing | t-d5-sh-rail-test |

#### 3. Tab strip and tab

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-050 | Strip 46px: pinned · 1×16 hairline · tabs · "+" · spacer · overview button | SH 3a strip; S-T-1, HLP STRIP | `TabStrip.tsx`, `strip.css` | complete (shell-tabs.spec "the strip, a tab and the content card match the shell design"; tabs.spec "pinning, groups and keyboard context menus retain visible selection") | — |
| SH-051 | "+" (30px, ink-3) opens the New-tab field | IX "+ (new tab)"; S-T-4 | HEAD `new` opens a conversation; L-newtab `reducers/newtab.ts` | in-flight:t-s1-newtab | — |
| SH-052 | Overview button (layout-grid) at the strip's right edge | S-T-5 | HEAD `TabStrip.tsx` "All tabs"; L-overview test "the grid icon opens the overview…" | in-flight:t-s1-overview | — |
| SH-053 | Active tab: `--canvas` + `--sh-1`, × always visible | SH 2h; S-T-6 | `Tab.tsx`, `tab.css` | complete (shell-tabs.spec "the strip, a tab and the content card match the shell design") | — |
| SH-054 | Inactive tab: no fill, ink-2, no × until hover | S-T-7 | `tab.css` | complete (shell-tabs.spec same test; tabs.spec "tab close sits in a fixed slot: shown on hover and on the active tab, never changing width") | — |
| SH-055 | Hover: `--tab-hover` fill + × | S-2h-4, S-3j-2 | `tab.css` | complete (tabs.spec "tab close sits in a fixed slot…") | — |
| SH-056 | ASSUME pressed: `--field-2` fill for `dur-press` (80ms), as rows (L-newtab NT14) | design silent | none | missing | t-d5-sh-tab-chrome, t-d5-sh-strip-test |
| SH-057 | Keyboard: roving ←/→/Home/End, ring only on keyboard focus, Shift F10 opens the tab menu | IX Flows "Accessibility"; S-IX-13 | `TabItem.tsx navigate`, `design/inputModality.ts`, `components/ui/Menu.tsx` | complete (tabs.spec "pinning, groups and keyboard context menus retain visible selection") | — |
| SH-058 | Needs you: 6px amber dot replaces the type icon; inactive title lifts to full ink | SH 2h "State"; S-T-8, C-TAB-4 | `Tab.tsx TabGlyph`, `TabItem.tsx stateOfMark` ← `conversation/tabSummary.ts` | partial (specimen dot asserted in shell-tabs.spec "…every tab kind and state, light and dark"; live path + ink lift untested) | t-d5-sh-tab-state-test |
| SH-059 | Failed: red dot replaces the icon | S-2h-5, C-TAB-5 | same | partial (specimen only) | t-d5-sh-tab-state-test |
| SH-060 | Running is silent; tabs never carry spinners, counts or badges | SH 3j footnote; S-3j-23, C-TAB-6 | `stateOfMark` drops `working` | partial (rule in code; no live assertion) | t-d5-sh-tab-state-test |
| SH-061 | Screen reader hears state in words ("Needs you" / "Failed") | IX Flows "Accessibility" | `Tab.tsx aria-description` | partial (untested) | t-d5-sh-tab-state-test |
| SH-062 | Kinds × states sheet (rest/hover/active/needs-you/failed; Settings and New tab n/a) | SH 3j; S-3j-1..9, S-3j-15..22 | `specimens/TabsSpecimen.tsx` | complete (shell-tabs.spec "the Design system page shows every tab kind and state, light and dark") | — |
| SH-063 | Specimens: Pinned (inbox + dot), Group, Collapsed group, Split, Compressed | S-3j-10..14 | `TabsSpecimen.tsx` | complete (same test: badge count 1, group radius 10, collapsed count "3", pinned width 30) | — |
| SH-064 | Long title fades over last 20px with a mask, no ellipsis | SH 3l; S-3l-9, C-TAB-9 | `tab.css` | complete (shell-tabs.spec "the strip, a tab and the content card match the shell design") | — |
| SH-065 | × in a fixed 20px slot; width never changes on hover | S-3l-10, C-TAB-10 | `Tab.tsx .workspace-tab-close-slot` | complete (tabs.spec "tab close sits in a fixed slot…") | — |
| SH-066 | Full title in a 500ms tooltip (where no hover preview opens: active and compressed tabs) | SH 3l "Long title · full name"; S-3l-11 | none on `Tab.tsx` select button | missing | t-d5-sh-tab-chrome, t-d5-sh-strip-test |
| SH-067 | Double-click a tab renames it | IX Flows "Rename"; S-3l-13 | `Tab.tsx onDoubleClick` → `startRename` | complete (shell-tabs.spec "double-clicking a tab renames it") | — |
| SH-069 | Equal widths 190 → 112px, then scroll under a 40px right mask | SH 2h; S-2b-10, S-2h-3 | `TabStrip.tsx`, `strip.css` | complete (shell-tabs.spec "tabs compress from 190px to 112px, then the strip scrolls under a mask with a +N menu") | — |
| SH-070 | "+N ⌄" overflow menu lists every tab | SH 2b; S-2b-6 | `TabStrip.tsx` `overflowItems` | complete (shell-tabs.spec compress test; tabs.spec "many top tabs scroll under a mask with a +N menu and keep narrow-screen actions reachable") | — |
| SH-071 | Active tab always scrolled into view and holds ≥112px | S-2h-3 | `TabStrip.tsx scrollIntoView` | partial (untested) | t-d5-sh-strip-test |
| SH-072 | Compressed icon-only 44px tab. ASSUME live at ≤600px for inactive tabs (active keeps title ≥112) | SH 3j "Compressed"; S-3j-14 | `Tab.tsx compressed` prop, token `tab-compressed-width` (specimen only) | partial | t-d5-sh-strip-compressed, t-d5-sh-strip-test |
| SH-073 | Close motion: width → 0, neighbours slide (200ms); entry fades; reduced motion none | F-MO-14 | `ui.css tab-enter` only | partial | t-d5-sh-tab-close-motion, t-d5-sh-strip-test |
| SH-074 | Click a tab focuses it | IX "Tab" click; S-IX-1 | `TabItem.tsx` | complete (tabs.spec "top tabs preserve isolated drafts across closing, reopening and reload") | — |
| SH-075 | ⌘-click (Ctrl-click) a strip tab. ASSUME: toggles selection for grouping (soft fill), Esc clears | SH 2h "⌘-selecting"; S-2h-8; ambiguity 6 | none | missing | t-d5-sh-tab-multiselect, t-d5-sh-group-test |
| SH-076 | ASSUME middle-click a strip tab closes it (work keeps running, same toast as ×) | IX "Tab" ⌘-click/middle; U7 | none | missing | t-d5-sh-tab-multiselect, t-d5-sh-group-test |
| SH-077 | Drag reorders within the strip | IX "Tab" drag; S-2g-5 | HEAD `hosts/dragHost.ts reorder`; L-split test "the outer quarter of a tab reorders instead of grouping" | in-flight:t-s1-split | — |
| SH-078 | Drag onto a tab → "Group" target (inset accent ring, accent-soft), groups both | SH 2g; S-2g-2 | L-split `dragHost.ts`; test "dragging onto another tab shows the Group target and groups both" | in-flight:t-s1-split | — |
| SH-079 | Drag onto content edge → left/right/bottom split zones + "Split right" pill | SH 2g; S-2g-3, S-2g-5 | L-split `SplitZones.tsx` | in-flight:t-s1-split | — |
| SH-080 | Drag ghost (floating tab under the strip) | S-2g-4 | L-split `dragHost.ts` | in-flight:t-s1-split | — |
| SH-081 | Hover preview disappears on drag | S-2g-7 | L-preview `usePreviewTrigger.ts` | in-flight:t-s1-preview | — |
| SH-082 | Drag a tab out of the strip → new window holding that tab | IX Flows "Multiple windows"; S-2g-6 | none | missing | t-d5-sh-tab-tearoff, t-d5-sh-window-test |
| SH-083 | ASSUME keyboard reorder: Alt+Shift+←/→ moves the focused tab one place; SR "Moved to position N of M" (mirrors Q28) | design silent | none | missing | t-d5-sh-tab-keyboard-move, t-d5-sh-group-test |
| SH-084 | Pinned tab: 30px icon-only; ⌘W leaves it open | SH 2h "Pinned"; S-T-2 | `Tab.tsx pinned`; L-rail test "⌘W leaves a pinned tab open" | in-flight:t-s1-rail | — |
| SH-085 | Pinned Inbox tab: first slot, the only pinned tab with a dot | SH 3j/3l/2b; S-3j-10, S-3j-21, S-2b-2 | L-menus `reducers/closing.ts ensure-inbox`; test "the Inbox carries a dot only when something needs you" | in-flight:t-s1-menus | — |
| SH-086 | Inbox tab dot reflects needs-you across ALL conversations (world `attention`), not only open tabs | SH 3j "where everything that needs you lands"; arch "Inbox" row | L-menus computes from open tabs only | missing | t-d5-sh-rail-attention-model, t-d5-sh-inbox-tab-dot, t-d5-sh-rail-test |
| SH-087 | ASSUME coarse pointer: × always shown on the active tab with a 24px hit; long-press (500ms) opens the tab menu | design silent | none | missing | t-d5-tok-sh-touch, t-d5-sh-touch-targets, t-d5-sh-touch-test |
| SH-088 | ≤600px strip: actions stay reachable | AGENTS responsive | `strip.css @media 600` | complete (tabs.spec "many top tabs scroll under a mask with a +N menu and keep narrow-screen actions reachable") | — |
| SH-089 | ASSUME right-click on empty strip: New tab ⌘T · Reopen closed tab ⌘⇧T · Show all tabs ⌘⇧\ | design silent | none | missing | t-d5-sh-strip-menu, t-d5-sh-strip-test |
| SH-090 | ASSUME a tab shown in two windows on one place carries the same state glyph live | IX Flows "Multiple windows" | none | missing | t-d5-sh-window-test |

#### 4. Groups (strip)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-100 | Group capsule: `--tab-hover`, radius 10, 12px medium label, members | SH 2b/3g/3j; S-2b-3, S-3g-2, S-3j-11 | `GroupCapsule.tsx`, `group.css` | complete (shell-tabs.spec "…every tab kind and state…" radius 10; tabs.spec "groups have distinct names and support overview moves, rename and reload") | — |
| SH-101 | Click label → collapses to "Label N"; active member stays visible | SH 2h; S-2h-9, S-3g-18 | `GroupCapsule.tsx`, `reducers/groups.ts collapse-group` | complete (tabs.spec "pinning, groups and keyboard context menus retain visible selection" asserts aria-expanded false) | — |
| SH-102 | Collapsed group shows an amber dot when a member needs you | SH 2b/3j; S-2b-4, S-3j-12 | `TabStrip.tsx needsYou` | partial (untested) | t-d5-sh-tab-state-test |
| SH-103 | Group label right-click: Rename · Open as split · Collapse · Ungroup · Close N tabs (danger) | SH 3g; IX "Group label"; S-3g-14, S-3g-17 | L-menus `groupMenuItems`; test "the group label menu renames, collapses, ungroups and closes its tabs in danger ink" | in-flight:t-s1-menus | — |
| SH-104 | ASSUME "Close N tabs" keeps running work running (as ×) and shows the closing toast | ambiguity 8 | L-menus `close-group` | in-flight:t-s1-menus | — |
| SH-105 | Drag the group label moves the whole group | IX "Group label" drag; S-3g-18 | HEAD `groupDropProps` accepts drops only | missing | t-d5-sh-group-drag, t-d5-sh-group-test |
| SH-106 | ASSUME keyboard: Alt+Shift+←/→ on a focused group label moves the group | design silent | none | missing | t-d5-sh-group-drag, t-d5-sh-group-test |
| SH-107 | Drop a tab on a group label joins the group | SH 2h | `hosts/dragHost.ts move-group` | complete (tabs.spec "dragging onto a collapsed group label groups the tab and preserves its draft") | — |
| SH-108 | ⌘-select tabs then ⌘G groups them | SH 2h; IX Shortcuts; S-2h-7, S-IX-22 | menu entry "New group… ⌘G" only (L-menus) | missing | t-d5-sh-group-selected-key, t-d5-int-sh-keymap, t-d5-sh-group-test |
| SH-109 | Tasks opened from a conversation join its group automatically | SH 2h; S-2h-10 | `reducers/tabs.ts open-task` ignores `groupId` | missing | t-d5-sh-task-joins-group, t-d5-sh-group-test |
| SH-110 | Suggestion pill top-centre (r99, surface, sh-2): layers + "Group the 3 bench tabs as Benchmarks?" + accent "Group" + × | SH 2b; S-2b-7 | none | missing | t-d5-tok-sh-pill, t-d5-sh-group-suggest-pill, t-d5-sh-group-test |
| SH-111 | Suggestion only for ≥3 tabs on one repo or topic, at most once per session per set (ASSUME "topic" = same workspace root; × dismisses for the session) | SH 2h; S-2b-8, S-2b-9 | none | missing | t-d5-sh-group-suggest-model, t-d5-sh-group-test |
| SH-112 | ASSUME ≤600px: pill wraps inside card width − 16px | design silent | none | missing | t-d5-sh-group-suggest-pill, t-d5-sh-group-test |
| SH-113 | Group "Open as split" (≤2×2) | IX "Group label"; S-3h-16 | HEAD `split-group` reducer + `menuHost.tsx`; L-menus | in-flight:t-s1-menus | — |

#### 5. Context menus (tab / group / strip)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-120 | Menu surface r10 sh-2 pad 5; rows 28 r6; hover `--field-2`; hairline separators; shortcuts right ink-3; destructive = danger text | Components "Shell · menus"; C-MENU-1..7 | L-menus `components/ui/Menu.tsx`, tokens `menu-item-height 28px` | in-flight:t-s1-menus | — |
| SH-121 | Tab menu header = the full title | SH 3g; S-3g-3 | L-menus test "the tab menu lists the designed items with shortcuts, in the designed surface" | in-flight:t-s1-menus | — |
| SH-122 | Open in split ▸ | S-3g-4 | L-menus `menuHost.tsx` | in-flight:t-s1-menus | — |
| SH-123 | Add to group ▸ (groups, check on current) + "New group… ⌘G" | S-3g-5, S-3g-13 | L-menus | in-flight:t-s1-menus | — |
| SH-124 | Pin tab / Unpin tab | S-3g-6 | L-menus | in-flight:t-s1-menus | — |
| SH-125 | Duplicate | S-3g-7 | L-menus | in-flight:t-s1-menus | — |
| SH-126 | Copy link ⌘⇧C | S-3g-8 | L-menus draws it DISABLED (no link scheme) — conflicts with repo law "absent, not broken" | partial | t-d5-sh-decide-shell-questions, t-d5-sh-menu-backed-items |
| SH-127 | Move to new window | S-3g-9 | L-menus draws it DISABLED | partial | t-d5-sh-menu-backed-items, t-d5-sh-window-test |
| SH-128 | Close tab ⌘W | S-3g-10 | L-menus | in-flight:t-s1-menus | — |
| SH-129 | Close and stop ⌥⌘W, listed only for running work | S-3g-16, S-3l-8 | L-menus test "the explicit paths: right-click lists both closes for running work…" | in-flight:t-s1-menus | — |
| SH-130 | Close other tabs (3g shows ⌥⌘W, which is Close and stop elsewhere) — ASSUME no shortcut | S-3g-11; ambiguity 7 | L-menus lists it with no shortcut | in-flight:t-s1-menus | — |
| SH-131 | Close tabs to the right | S-3g-12 | L-menus test "pin, duplicate, split, close others and close to the right act on the strip" | in-flight:t-s1-menus | — |
| SH-132 | Open submenu row keeps `--tab-hover` fill | S-3g-19 | L-menus | in-flight:t-s1-menus | — |
| SH-133 | Menus fully keyboard operable (Shift F10 / ContextMenu key, arrows, Esc restores focus) | IX Flows "Accessibility" | `components/ui/Menu.tsx` | complete (tabs.spec "pinning, groups and keyboard context menus retain visible selection") | — |
| SH-134 | Extras not in 3g: "Rename tab", "Separate split" | IX Flows "Rename … or use the menu" | L-menus `menuHost.tsx` | in-flight:t-s1-menus | — |
| SH-135 | Nested menus themed and usable at narrow widths | AGENTS responsive | `Menu.tsx` | complete (responsive.spec "narrow navigation traps focus, themes nested menus, dismisses…") | — |
| SH-136 | ASSUME touch: long-press opens tab / group menus | design silent | none | missing | t-d5-sh-touch-targets, t-d5-sh-touch-test |
| SH-137 | "+N" overflow menu: New tab, Reopen closed tab, every tab | CM8 | HEAD `overflowItems`; L-menus | in-flight:t-s1-menus | — |
| SH-138 | Overview card right-click = the same tab menu | IX "Overview card"; S-3h-15 | L-overview `OverviewCard.tsx` own ContextMenu (Pin, Move to group, Close) | partial | t-d5-sh-overview-tab-menu, t-d5-sh-overview-test |

#### 6. Close vs close-and-stop, toasts, undo

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-140 | × closes; work keeps running; tooltip "Close · keeps running ⌘W" | SH 3l step 1; IX "Tab · X"; S-3l-2, S-IX-3 | L-menus `closing/*`; test "the close button keeps work running, shows its tooltip…" | in-flight:t-s1-menus | — |
| SH-141 | ⌥ held over a running tab: × becomes stop square; tooltip "Close and stop ⌥⌘W" | SH 3l step 2; S-3l-3, C-TAB-11 | L-menus `altHeld.ts`, `useCloseStopKey.ts` | in-flight:t-s1-menus | — |
| SH-142 | Idle tab: ⌥ does nothing | S-3l-6 | L-menus `running.ts` | in-flight:t-s1-menus | — |
| SH-143 | ⌥⌘W closes and stops | IX Shortcuts; S-3l-14 | L-menus `useCloseStopKey.ts` | in-flight:t-s1-menus | — |
| SH-144 | Toast "<b>Config stack</b> closed and still running" · Stop it · Undo; 6s; bottom | SH 3l step 3; S-3l-4, C-OVL-1 | L-menus `closing/ClosingToast.tsx`; tests "after closing running work a 6s toast…", "Stop it stops the closed work…" | in-flight:t-s1-menus | — |
| SH-145 | Closed-but-running work listed under "Running in the background"; click reopens where it was | SH 3l; S-3l-5, S-3l-7 | L-menus `InboxPane.tsx`, `background.ts` | in-flight:t-s1-menus | — |
| SH-146 | ONE toast primitive (bottom-centre, 40h r12 surface sh-2, 6s) shared by closing and auto-archive toasts | C-OVL-1, C-OVL-2; audit D8 (two impls, 6s vs 12s) | L-menus `ClosingToast.tsx`; L-history `ArchiveToast.tsx` | missing | t-d5-tok-sh-toast, t-d5-prim-toast, t-d5-int-tok-f-exports, t-d5-prim-toast-adopt-closing, t-d5-sh-toast-test |
| SH-147 | ASSUME toast: `role=status`; actions reachable by keyboard; timer pauses on hover / focus | ambiguity 22; IX Flows "Accessibility" | none shared | missing | t-d5-prim-toast, t-d5-sh-toast-test |
| SH-148 | ASSUME one toast at a time; a newer one replaces the older | ambiguity 22 | none | missing | t-d5-prim-toast, t-d5-sh-toast-test |
| SH-149 | ASSUME ≤600px: toast width clamps to viewport − 16px | design silent | none | missing | t-d5-prim-toast, t-d5-sh-toast-test |
| SH-150 | Toast enter = fade + 8px rise (200ms); reduced motion = fade only | F-MO-4 | none | missing | t-d5-tok-sh-toast, t-d5-prim-toast, t-d5-sh-toast-test |
| SH-151 | Launch toast "Archived 6 tabs idle for more than 12h" · Review · Restore all | SH 4c; S-4c-8 | L-history `ArchiveToast.tsx` | in-flight:t-s1-history | — |
| SH-152 | ASSUME a toast shows only in the window where the action happened | design silent | none | missing | t-d5-prim-toast, t-d5-sh-window-test |
| SH-153 | ⌘Z undoes the last structural action (close, move, regroup, pin, reorder, archive), 20 steps per window | IX Flows "Undo"; S-3l-15, S-IX-22 | HEAD `closed` list (≤20) + `reopen` only | missing | t-d5-sh-undo-stack, t-d5-sh-undo-keys, t-d5-int-sh-workspace, t-d5-sh-undo-test |
| SH-154 | ASSUME ⌘Z inside a text field stays text undo | design silent | none | missing | t-d5-int-sh-keymap, t-d5-sh-undo-test |

#### 7. Hover previews (lane t-s1-preview)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-160 | Hover an inactive tab 500ms → 300px text card | SH 2h "Preview"; S-3k-7; L-preview Q-P1 | L-preview `preview/usePreviewTrigger.ts`; test "a 500ms hover opens a 300px text card…" | in-flight:t-s1-preview | — |
| SH-161 | Active tab opens no card | L-preview Q-P2 | L-preview | in-flight:t-s1-preview | — |
| SH-162 | Moving across neighbours swaps instantly, no animation | S-3k-7 | L-preview test "moving across neighbouring tabs swaps the card at once…" | in-flight:t-s1-preview | — |
| SH-163 | Click, press, Esc close it; stays shut until pointer leaves | S-3k-7 | L-preview test "a click, a press and Escape close the card…" | in-flight:t-s1-preview | — |
| SH-164 | Act from the preview: Allow all / Review | SH 3k; S-3k-9 | L-preview test "Allow all in the card answers every permission on the engine…" | in-flight:t-s1-preview | — |
| SH-165 | Card bodies by kind (conversation last reply, task question, terminal lines, diff head, web screenshot) | S-3k-2..6 | L-preview `preview/bodies.tsx`, `content.ts` | in-flight:t-s1-preview | — |
| SH-166 | Keyboard focus-visible opens the card; its buttons are not in the Tab order | L-preview Q-P3 | L-preview | in-flight:t-s1-preview | — |
| SH-167 | No card on split segments or collapsed group pills | L-preview Q-P10 | L-preview | in-flight:t-s1-preview | — |
| SH-168 | ASSUME ≤600px: card stays inside the viewport (8px collision padding) | design silent | none | missing | t-d5-sh-preview-narrow-touch, t-d5-sh-preview-narrow-test |
| SH-169 | ASSUME touch (`hover: none`): no preview cards | design silent | none | missing | t-d5-sh-preview-narrow-touch, t-d5-sh-preview-narrow-test |
| SH-170 | Preview specimens Light + Dark | Components "Shell · preview" | L-preview test "the Design system page shows every preview card" | in-flight:t-s1-preview | — |

#### 8. Overview ⌘⇧\ grid + filmstrip (lane t-s1-overview)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-180 | Opens with ⌘⇧\ (Ctrl Shift A) or the grid button; Done / Esc close | SH 2h; S-3h-13, S-3h-17 | HEAD `TabOverview.tsx` (old dialog; tabs.spec "platform tab shortcuts…open overview"); L-overview test "the overview shortcut opens it, Done and Escape close it…" | in-flight:t-s1-overview | — |
| SH-181 | Top bar: traffic-light gutter, "Search N tabs", Grid \| Filmstrip, Done | SH 3h; S-3h-2; L-overview OV9 | L-overview `TabOverview.tsx` | in-flight:t-s1-overview | — |
| SH-182 | Sections per group with "Open as split", then "Other tabs" | S-3h-3, S-3h-8, S-3h-16; OV3 | L-overview `overview-model.ts` | in-flight:t-s1-overview | — |
| SH-183 | Card anatomy: kind, state ("4 running", "step 7"), title, then the one piece that matters; footer age · model | S-3h-4..12, S-3h-18 | L-overview `OverviewCard.tsx`: kind preview / draft / answer / first line (OV4, OV5); known age · canonical conversation or task model (OV15), shared catalog/pin labels or truthful actual ID; no invented step/model | partial | t-d5-sh-overview-card-body, t-d5-sh-overview-test; `overview-model.spec.ts` 4/4 |
| SH-184 | Needs-you card carries its primary action (Allow all / Review) | S-3h-6; audit G4 | none in L-overview | missing | t-d5-sh-overview-card-body, t-d5-sh-overview-test |
| SH-185 | Card click opens; close on hover; drag regroups | IX "Overview card"; S-3h-15 | L-overview tests "close shows on hover…", "dragging a card onto another section regroups the tab" | in-flight:t-s1-overview | — |
| SH-186 | Card ⌘-click / middle = "Background tab" (every card is already a tab) | IX "Overview card"; OV14 | L-overview: same as click | in-flight:t-s1-overview | — |
| SH-187 | Type to filter | S-3h-14 | L-overview test "typing filters the cards and Enter opens the first match" | in-flight:t-s1-overview | — |
| SH-188 | Keyboard cursor ← → ↵ Esc; ⌘W closes the centre/cursor card | S-3h-17, S-3i-7; OV6 | L-overview tests | in-flight:t-s1-overview | — |
| SH-189 | Filmstrip: live half-scale panes, neighbours scale .86 / opacity .9, 100px edge fade, "Group · 1 of 4" | SH 3i; S-3i-1..8 | L-overview `OverviewFilmstrip.tsx` | in-flight:t-s1-overview | — |
| SH-190 | Pinch out opens the overview | SH 2h; S-3h-13; OV2 (not wired) | none | missing | t-d5-sh-overview-pinch |
| SH-191 | Overview fits 320px | AGENTS responsive | L-overview test "the overview fits 320px with no page overflow" | in-flight:t-s1-overview | — |
| SH-192 | ASSUME the overview lists only this window's tabs (its place) | IX Flows "Multiple windows" | none | missing | t-d5-sh-window-test |
| SH-193 | Overview Light + Dark, accessible | — | L-overview test "specimen ${scheme}: grid and filmstrip are themed and accessible" | in-flight:t-s1-overview | — |

#### 9. Split panes (lanes t-s1-split, t-s1-menu)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-200 | Split = one merged tab, a segment per pane, focused segment filled | SH 2h; S-2h-11, S-3b-2, S-2d-3 | `SplitTab.tsx` | complete (shell-tabs.spec "a split is one merged tab with a segment per pane, a 40px pane title and a focus ring") | — |
| SH-201 | 40px pane title line only while split | S-3b-3 | `PaneGrid.tsx PaneHeader` | complete (same test) | — |
| SH-202 | Focused pane 1.5px accent-soft ring | S-3b-4 | HEAD `data-focused` only; L-split test "…the focused ring is 1.5px" | in-flight:t-s1-split | — |
| SH-203 | Up to a 2×2 grid | S-2d-12 | L-split test "…a 2x2 stops at four" | in-flight:t-s1-split | — |
| SH-204 | Drop zones left / right / bottom with "Split right" pill | S-2g-3, S-2g-5 | L-split `SplitZones.tsx` | in-flight:t-s1-split | — |
| SH-205 | Click focuses a pane; ⌘⌥←/→ moves focus | IX "Split pane"; S-2d-8 | L-split `usePaneKeys.ts` | in-flight:t-s1-split | — |
| SH-206 | Pane menu: Close pane · Swap · Maximize | IX "Split pane"; S-2d-9 | L-split `PaneGrid.tsx paneMenu`; test "the pane menu swaps, maximizes without unmounting, restores, and closes" | in-flight:t-s1-split | — |
| SH-207 | Divider resizes on hover; double-click equalizes; ratio persists | S-2d-10 | L-split `SplitHandles.tsx` | in-flight:t-s1-split | — |
| SH-208 | Pane controls appear on hover in the title line | S-2d-11 | L-split | in-flight:t-s1-split | — |
| SH-209 | Unfocused pane: 36px compact field; expands over 200ms; needs-you one-line tray | SH 3b; S-3b-5..8 | L-split `CompactComposer.tsx` | in-flight:t-s1-split | — |
| SH-210 | ASSUME ⌘W with a split active closes the whole merged tab (one close per split; "Close pane" is the pane menu's) | ambiguity 10 | `SplitTab.tsx onClose` → `closeTab` | partial (no test) | t-d5-sh-strip-test |
| SH-211 | ASSUME ≤600px: only the focused pane shows full-size; merged-tab segments switch panes; dividers hidden | design silent | none | missing | t-d5-sh-split-narrow, t-d5-sh-split-narrow-test |
| SH-212 | ASSUME coarse pointer: divider hit target 16px | design silent | token `pane-resize-hit 8px` | missing | t-d5-tok-sh-touch, t-d5-sh-touch-targets, t-d5-sh-touch-test |
| SH-213 | Split specimens Light + Dark | Components "Shell · split" | L-split test "the Design system page shows the drag targets, the pane header and the compact composer…" | in-flight:t-s1-split | — |

#### 10. New-tab field ⌘T (lane t-s1-newtab)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-220 | ⌘T / "+" opens the New tab (plus icon, × ) with one centred field | SH 3f; S-3f-2, S-3f-14 | L-newtab `kinds/newtab/NewTabPane.tsx` (commit 3a4ec2702, unmerged) | in-flight:t-s1-newtab | — |
| SH-221 | Field geometry, hint "↵ to start a conversation", caption "Type a question, a file, a URL, or a command." | S-3f-3, S-3f-12 | L-newtab test "the field, its rows and the caption have the design geometry" | in-flight:t-s1-newtab | — |
| SH-222 | First row "Ask “…” in a new conversation ↵" | S-3f-4, S-3f-15 | L-newtab tests | in-flight:t-s1-newtab | — |
| SH-223 | Start: "Open file… ⌘O" — row exists; ⌘O key not wired (NT5) | S-3f-7 | L-newtab `rows.ts` | partial | t-d5-int-sh-keymap, t-d5-sh-newtab-start-rows, t-d5-sh-newtab-test |
| SH-224 | Start: "New terminal ⌃`" (drawn only while terminal is backed, NT4) | S-3f-6 | L-newtab `rows.ts` (gated off) | missing | t-d5-sh-newtab-start-rows, t-d5-sh-newtab-test |
| SH-225 | Matching files with dim path | S-3f-9 | L-newtab `useFileMatches.ts` | in-flight:t-s1-newtab | — |
| SH-226 | Open-tab rows with amber dot and ⌘ number (NT Q30) | S-3f-10, S-3f-16 | L-newtab | in-flight:t-s1-newtab | — |
| SH-227 | Recently closed rows "closed 1h ago" (L-newtab shows "closed", no age — NT3) | S-3f-11 | L-newtab `rows.ts` | partial | t-d5-sh-newtab-closed-age, t-d5-sh-newtab-test |
| SH-228 | Pasting / typing a URL opens a web tab (L-newtab treats it as a question — NT1) | S-3d-9, S-3f-13 | L-newtab | missing | t-d5-sh-newtab-url, t-d5-sh-newtab-test |
| SH-229 | "From history" rows (archived marked), "See all N in History ⌘↵" (NT2 not drawn) | SH 4c; S-4c-2..7 | none | missing | t-d5-sh-newtab-history, t-d5-sh-newtab-test |
| SH-230 | Esc clears, then closes the empty field | L-newtab NT8 | L-newtab test "Escape clears the text, then closes the empty field…" | in-flight:t-s1-newtab | — |
| SH-231 | One highlight for hover and keyboard; press field-2 | L-newtab NT14 | L-newtab | in-flight:t-s1-newtab | — |
| SH-232 | Fits 320px | NT15 | L-newtab test "stays inside the card at 320px" | in-flight:t-s1-newtab | — |
| SH-233 | Light + Dark, accessible | — | L-newtab test "is accessible and uses shared controls, light and dark" | in-flight:t-s1-newtab | — |

#### 11. Command palette ⌘K

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-240 | ⌘K palette: no design page draws it; 3f/3k say "one field … jumps to a tab" (the ⌘T field). ASSUME ⌘K opens/focuses the New-tab field and the 4-page "Go to X" palette retires | SH 3f/3k "New tab is a field, not a page" | HEAD `components/CommandPalette.tsx`, `App.tsx` ⌘K; L-rail `palette` id | partial | t-d5-sh-decide-shell-questions, t-d5-sh-palette-retire, t-d5-sh-palette-test |
| SH-241 | Rail "Find anything ⌘K" and the address-style search button retire with the palette | SH 3a rail | HEAD/L-rail `Rail.tsx` | partial | t-d5-sh-palette-retire, t-d5-sh-rail-legacy-retire, t-d5-sh-palette-test |

#### 12. Keyboard shortcut table (every chord in IX "Shortcuts" + shell chords elsewhere)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-250 | ⌘T new-tab field | IX Shortcuts | `design/keyboard.ts tabActionShortcut`, `useTabKeys.ts` | complete (tabs.spec "platform tab shortcuts create, close, reopen, navigate and open overview") | — |
| SH-251 | ⌘W close (keeps running) | IX Shortcuts | same | complete (same test) | — |
| SH-252 | ⌥⌘W close and stop | IX Shortcuts | L-menus `useCloseStopKey.ts` | in-flight:t-s1-menus | — |
| SH-253 | ⌘⇧T reopen closed tab | IX Shortcuts | `useTabKeys.ts` | complete (same test) | — |
| SH-254 | ⌃Tab recent-tab switcher (held; Esc cancels) | IX Shortcuts; SH 2h | `useTabKeys.ts` | complete (tabs.spec "held Control Tab previews recent tabs, Escape cancels, release commits") | — |
| SH-255 | ⌘1–9 jump to tab (9 = last), composer or not | IX Shortcuts | HEAD `useTabKeys.ts` (untested); L-rail test "⌥⌘1–3 pick pinned models; ⌘1–9 always jump to tabs…" | in-flight:t-s1-rail | — |
| SH-256 | ⌘0 place Home — chord recognizer (handler: places) | IX Shortcuts | none | missing | t-d5-int-sh-keymap |
| SH-257 | ⌃1–9 pinned place — recognizer | IX Shortcuts | none | missing | t-d5-int-sh-keymap |
| SH-258 | ⌘P go to place — recognizer | IX Shortcuts | none | missing | t-d5-int-sh-keymap |
| SH-259 | ⌘⇧P all places — recognizer | IX Shortcuts | none | missing | t-d5-int-sh-keymap |
| SH-260 | ⌘Y History | IX Shortcuts | L-rail `history` id (only when backed) + L-history | in-flight:t-s1-rail | — |
| SH-261 | ⌘S toggle rail | IX Shortcuts | L-rail | in-flight:t-s1-rail | — |
| SH-262 | ⌘⇧F focus mode | IX Shortcuts | L-rail | in-flight:t-s1-rail | — |
| SH-263 | ⌘⇧\ tab overview (Ctrl Shift A) | IX Shortcuts | `keyboard.ts isOverviewShortcut` | complete (tabs.spec "platform tab shortcuts create, close, reopen, navigate and open overview") | — |
| SH-265 | ⌘[ up a level on Home — recognizer; ASSUME on Home only, elsewhere ⌘[ stays task Back | IX Shortcuts | `conversation/Breadcrumb.tsx historyDirection` (task Back) | missing | t-d5-int-sh-keymap |
| SH-266 | ⌘⇧K Tasks panel | IX Shortcuts | L-rail `tasks` id | in-flight:t-s1-rail | — |
| SH-267 | ⌥⌘1–3 pinned models (Ctrl Alt 1–3) | IX Shortcuts | L-rail | in-flight:t-s1-rail | — |
| SH-268 | ⌘/ all models | IX Shortcuts | HEAD `ModelPicker.tsx`; L-rail `models` id | in-flight:t-s1-rail | — |
| SH-269 | ⌘↑ / ⌘↓ previous / next message (in chat); ⌘↑ no longer opens the overview | IX Shortcuts | L-rail `turn-previous`/`turn-next`; test "…⌘↑ does not open it" | in-flight:t-s1-rail | — |
| SH-270 | Esc: close the overlay (switcher, overview, menus) | IX Shortcuts | `useTabKeys.ts`, `Menu.tsx` | complete (tabs.spec "held Control Tab previews recent tabs, Escape cancels, release commits") | — |
| SH-271 | ⌘G group selected tabs | IX Shortcuts | none | missing | t-d5-int-sh-keymap, t-d5-sh-group-selected-key, t-d5-sh-group-test |
| SH-272 | ⌘⌥←/→ move focus between panes | IX Shortcuts | L-split `usePaneKeys.ts` | in-flight:t-s1-split | — |
| SH-273 | Space Quick Look — recognizer outside writing fields (handler: places) | IX Shortcuts | none | missing | t-d5-int-sh-keymap |
| SH-274 | ⌘Z undo structural action | IX Shortcuts | none | missing | t-d5-int-sh-keymap, t-d5-sh-undo-keys, t-d5-sh-undo-test |
| SH-275 | ⌘, Settings tab | IX Flows "Settings" | L-rail test "⌘, opens the Settings tab once, from any page" | in-flight:t-s1-rail | — |
| SH-276 | ⌘O Open file… | SH 3f | none | missing | t-d5-int-sh-keymap, t-d5-sh-newtab-start-rows, t-d5-sh-newtab-test |
| SH-277 | ⌃` New terminal | SH 3f | L-term "the new terminal key opens a shell under the conversation" (own handler) | in-flight:t-s1-terminal | — |
| SH-278 | ⌘⇧C Copy link | SH 3g | menu hint only (L-menus, disabled) | missing | t-d5-int-sh-keymap, t-d5-sh-menu-backed-items |
| SH-279 | ⌘N new window on Now | IX Flows "Multiple windows" | none | missing | t-d5-int-sh-keymap, t-d5-sh-window-new, t-d5-sh-window-test |
| SH-281 | ⌘⇧] / ⌘⇧[ (Ctrl PgDn / PgUp) adjacent tabs | AGENTS platform contract | `keyboard.ts sequentialTabDirection`, `menu.rs` | complete (tabs.spec "platform tab shortcuts create, close, reopen, navigate and open overview") | — |
| SH-282 | Linux spellings (Ctrl, Ctrl Shift A, Ctrl Alt 1–3) | L-rail DQ R1 | L-rail `keyboard.ts` | in-flight:t-s1-rail | — |
| SH-283 | macOS menu bar mirrors shell chords: has New Tab, Close Tab, Reopen, Next/Previous, Show All Tabs; now also Close and Stop ⌥⌘W, Toggle Sidebar ⌘S, Focus Mode ⌘⇧F, History ⌘Y, Settings… ⌘, New Window ⌘N, delivered to the focused window; not yet exercised on macOS | AGENTS "native accelerators" | `src-tauri/src/menu.rs`, `src/lib/desktopTabs.ts`, `desktopMenuRoute.ts`, `menu_route.rs` | partial | t-d5-sh-native-menu-items, t-d5-sh-native-menu-test |
| SH-284 | Native menu actions reach the FOCUSED window (today `emit_to("main")`) | IX Flows "Multiple windows" | `menu.rs handle` | partial | t-d5-sh-native-menu-items, t-d5-sh-native-menu-test |

#### 13. Multiwindow (renderer side; native = fixed t-d5-nat-*)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| SH-300 | Each window shows one place: a window boots from its label / place key and loads that workspace | IX Flows "Multiple windows"; S-IX-4 | one window `main`; workspace in localStorage | missing | t-d5-sh-window-boot, t-d5-int-sh-app, t-d5-sh-window-test |
| SH-301 | The same place in two windows mirrors live (tab added/closed/regrouped in A appears in B) | IX Flows "Multiple windows" | none | missing | t-d5-sh-window-test (sync itself: t-d5-prim-workspace-sync) |
| SH-302 | ASSUME window-local: active tab, recent ids, selection, overview open, undo stack, rail collapsed/focus; shared: tabs, groups, closed list | design silent | `model.ts WorkspaceState` is one blob | missing | t-d5-sh-window-local-state, t-d5-int-sh-workspace, t-d5-sh-window-test |
| SH-303 | ⌘N / File ▸ New Window opens a window on Now | IX Flows "Multiple windows"; S-IX-4 | none | missing | t-d5-sh-window-new, t-d5-int-sh-app, t-d5-sh-window-test |
| SH-307 | Relaunch restores the open windows and their places | IX Flows "Relaunch"; S-IX-5 | none | missing | t-d5-sh-window-restore, t-d5-sh-window-test |
| SH-308 | Relaunch restores tabs, groups, splits; saved conversations re-attach once | IX Flows "Relaunch" | `model.ts readWorkspace`, `useBackgroundSessions` | complete (tabs.spec "top tabs preserve isolated drafts across closing, reopening and reload"; tabs-sessions.spec "reload attaches each saved tab exactly once") | — |
| SH-309 | Relaunch restores scroll positions | IX Flows "Relaunch"; audit B16 | none | missing | t-d5-sh-scroll-restore, t-d5-int-sh-workspace, t-d5-sh-window-test |
| SH-310 | Undo stack is per window | IX Flows "Undo" | none | missing | t-d5-sh-undo-stack, t-d5-sh-undo-test |
| SH-311 | ASSUME closing a window (red light / ⌘⇧W) keeps its place's tabs in the store and running work running | design silent | none | missing | t-d5-sh-window-test |
| SH-312 | ASSUME a new window has the same min size, material and chrome as `main` | design silent | none | missing | t-d5-sh-window-test (native: t-d5-nat-window-open) |

#### Open questions

1. Pinned first slot: Home tab (2h, 3a–4c) or Inbox (2b, 2g, 3j, 3l)? ASSUME both: Home (places writer) first, then the pinned Inbox (L-menus); the collapsed-rail strip (2b) shows the Inbox as its first pinned tab after the traffic lights and rail toggle. Recorded by t-d5-sh-decide-shell-questions.
2. ⌥⌘W: "Close other tabs" (3g) vs "Close and stop" (3l, IX). ASSUME Close and stop; Close other tabs has no shortcut (as L-menus already does).
3. "Copy link" and "Move to new window" while unbacked: disabled (L-menus) or absent (repo law)? ASSUME absent until backed; Move to new window becomes live with t-d5-nat-tab-move-window; Copy link stays absent until a `codeaf://` link scheme is decided.
4. Strip tab ⌘-click: "Background tab" (IX) vs "⌘-selecting" for ⌘G (2h). ASSUME ⌘-click toggles selection on strip tabs (links and rows keep ⌘-click = background tab); middle-click closes.
5. ⌘K: no design. ASSUME ⌘K opens the New-tab field (one field), the 4-page palette retires.
6. Toast stacking / keyboard (ambiguity 22). ASSUME one toast at a time, newest replaces; role=status; timer pauses on hover/focus; Esc dismisses while focused.
7. Group suggestion dismissal: once per session per set (2h) vs "Not now hides for 30 days" (IX filing suggestion). ASSUME × dismisses for the session only; no 30-day memory for tab groups.
8. ⌘W with a split active: whole split or focused pane? ASSUME whole split (merged tab has one close); "Close pane" lives in the pane menu.
9. Compressed (icon-only) tab: when is it live? ASSUME at ≤600px for inactive tabs only.
10. Focus mode on macOS: native traffic lights cannot be hidden through stable Tauri 2 API. ASSUME they stay visible; the content keeps clear of them only when the strip is revealed (research t-d5-sh-focus-native-lights).
11. Window-local vs shared workspace fields (two windows on one place). ASSUME active tab, recents, selection, overview, undo and rail state are per window; tab set, groups and closed list are shared. If t-d5-prim-workspace-sync already fixes this split, t-d5-sh-window-local-state reduces to its test.
12. ⌘B alias (L-rail R2): keep beside ⌘S? ASSUME keep until the designer says otherwise.
13. Pinch-out: no reliable webview event (L-overview OV2). ASSUME not built; keys and grid button remain (research t-d5-sh-overview-pinch).
14. Inbox / Now right-click is "—". ASSUME suppress the webview's default context menu on those rows (nothing opens).

#### Counts

Rows: 227.

| Status | Rows |
|---|---|
| complete | 35 |
| partial | 27 |
| missing | 72 |
| in-flight | 93 |
| n/a-decided | 0 |

In-flight by lane: t-s1-history 1, t-s1-menus 24, t-s1-newtab 10, t-s1-overview 11, t-s1-preview 10, t-s1-rail 22, t-s1-split 14, t-s1-terminal 1.

Tasks: 68 in ~/.codex/codeaf-design-run/d5-inventory/tasks-shell.json (every partial/missing row is covered by at least one task; in-flight rows get follow-ups only where the lane leaves a gap).

### 7.3 Tab kinds

Scope: the contents and states of every tab kind other than the conversation (file, diff, terminal/job, engine job, web, history, settings, inbox, new tab, task), the Components "Edge states" rows and the Interactions rows that reach those kinds.
Code verified 2026-10-09 against HEAD 39d440c4f (`/home/santosh/codeaf-design-plan`) and the lane worktrees under `/home/santosh/codeaf-workspace/.claude/worktrees/` (L-files = agent-a451c7c04660863b4, committed abba872c6, not in HEAD; L-term = agent-aa8990887568558b7, uncommitted; L-hist = agent-ab15dab692b3b743f, uncommitted; L-rail = agent-a99c430e0f6cc71ca, uncommitted; L-menus = agent-a320e518fe154e07c, uncommitted; L-newtab = agent-a2d4b3e6b5845bad3, committed 3a4ec2702, not in HEAD).
At HEAD, `kinds/{file,diff,web,terminal,settings,history,inbox}.ts` are `placeholderPane` ("<Kind> is not available yet.", `backed:false`), and `kinds/newtab.ts` is a ConversationPane stand-in. Lane work is unverified by definition (in-flight).
Tab-strip chrome (glyph states, close, groups, previews, overview) belongs to the shell writer. Here it appears only where a kind's body or its kind-specific menu depends on it.

| Cov ID | Design item (state/input) | Source (page §id + inventory ID) | Current code path(s) (HEAD file or lane:path) | Status | Task |
|---|---|---|---|---|---|
| **File tab** | | | | | |
| TF-01 | File tab body: header (icon, name, dir), the whole file as numbered lines on canvas | Shell 3e S-3e-1/2/4, S-K-3 | HEAD `kinds/file.ts` placeholder; L-files `features/files/{FilePane,FileSurface,FileHeader,FileLines}.tsx` | in-flight:t-s1-files | — |
| TF-02 | Changes / File segmented toggle; Changes is first; the choice persists across a reload | S-3e-2/3/4 | L-files `FileHeader.tsx`, `view-state.ts` `file.view`; lane test "the toggle shows the whole file, the choice survives a reload…" | in-flight:t-s1-files | — |
| TF-03 | Toggle keyboard: arrow keys move inside the segmented control, ring shows only on keyboard focus | S-IX-13, D6 | L-files uses shared `Segmented`; no lane test drives it by keyboard | in-flight:t-s1-files | t-d5-tab-files-test |
| TF-04 | File can't be shown: `[file] logo.psd  Too large to show · 3.4 MB  Open in ⌄` (one muted line, card r12 surface sh-1) | Components Edge C-EDGE-1, Q1 | L-files `FileSurface.tsx` `refusalLine` prints "Too large to show, 3.4 MB" (comma, not " · ") | partial | t-d5-tab-files-strings |
| TF-05 | Binary file → "Binary file" + Open in ⌄ | Q1 | L-files `FileSurface.tsx` | in-flight:t-s1-files | — |
| TF-06 | Open in ⌄ menu: lists the editors found on the ENGINE machine, default first ("Photoshop  default"), highlighted row field-2, separator, `Copy path ⌘⇧C` | C-EDGE-2, Components Edge caption | L-files `EditorHandoff.tsx`: Open in editor (local only), Copy path, Copy relative path; nothing enumerates editors | partial | t-d5-tab-files-editors-route, t-d5-tab-files-open-with-native, t-d5-tab-files-editors-menu |
| TF-07 | Open in editor ↗ hands off to the editor when the engine is local | S-3e-8 | L-files `EditorHandoff.tsx` + Tauri `open_path`; lane test "Open in editor appears only when the engine is on this machine" | in-flight:t-s1-files | — |
| TF-08 | Browser build (no desktop shell): handoff is the Open in menu (Copy path only) | ASSUME: no native shell means no open; copy only | L-files test "without the desktop shell the handoff is the Open in menu" | in-flight:t-s1-files | — |
| TF-09 | File outside git: subtitle "~/scratch · not in git", no Changes/File toggle, no ± counts | C-EDGE-3, Q2 | L-files hides the toggle and counts (test "a file outside git has the file view only"), but the dir line is the plain dir and has no " · not in git" | partial | t-d5-tab-files-strings |
| TF-10 | Header/tab icon by type: file-code-2, file-json, file-text, image | S-3e-10, S-3j-17 | L-files `FileHeader.tsx` always `fileCode`; `kinds/file.ts` icon fixed | done | t-d5-int-tab-kind-slots, t-d5-tab-files-type-icon |
| TF-11 | Image file in the File view | S-3j-17 (image icon) | ASSUME: a raster image (png, jpg, gif, webp, avif, bmp, ico) up to 16 MiB (the engine's own read cap, stated once in `imageFile.ts`) renders in the File view only, read through `/files`, fitted inside the body with its aspect ratio kept (no zoom, no toolbar); SVG and anything the engine does not label as a raster image, oversize files, malformed size or base64, and undecodable bytes get the can't-be-shown line and the Open in menu. `FileSurface.tsx` + `imageFile.ts` | implemented, awaiting designer review of the fit | t-d5-tab-files-image-view |
| TF-12 | Deleted file in File view → "This file was deleted." | ASSUME: one muted line | L-files `FileSurface.tsx` | in-flight:t-s1-files | — |
| TF-13 | File chip click → preview sheet; ⌘-click / middle → file tab; right-click Open in editor · Reveal · Copy path · Copy relative path; hover = full path | Interactions Conversation "File chip", S-3e-9 | L-files `conversation/assets/FileChip.tsx`; lane test "a plain click on a file chip opens the preview sheet; Command-click…" | in-flight:t-s1-files | — |
| TF-14 | Copy path key ⌘⇧C copies the focused file's path | C-EDGE-2 | L-files `keyboard.ts` `copyPathShortcut`; lane test "the Open in menu shows the copy-path key…" | in-flight:t-s1-files | — |
| TF-15 | File re-reads when the conversation changes it | ASSUME: the open tab re-reads when the conversation's changes list reports the path again; there's no live watch | L-files `useWorkView.ts` reads once per mount | missing | t-d5-tab-files-refresh |
| TF-16 | Narrow window (≤600): header wraps (toggle and handoff to a second row), dir fades first; ≤850 unchanged | ASSUME: AGENTS responsive law, design silent | L-files `files.css` has no media rule | missing | t-d5-tab-files-narrow |
| TF-17 | Light + Dark accessibility contract for file/diff tab | AGENTS contract, S-IX-13 | no axe pass in `files-tabs.spec.ts` | missing | t-d5-tab-files-test |
| TF-18 | File tab in a split pane / second window: same body, own scroll | ASSUME: kind body is window-agnostic | L-files `FilePane` keyed per pane | in-flight:t-s1-files | — |
| **Diff tab** | | | | | |
| TD-01 | Diff header: name, dir, "+12" "−3", toggle, Open in editor | S-3e-2, S-K-4 | L-files `FileHeader.tsx` | in-flight:t-s1-files | — |
| TD-02 | Hunk header "@@ 84,10 +84,16 @@ func (l *Lexer) next() Token" (Q32: file/diff tabs keep it) | S-3e-5, Q32 | L-files `DiffBody.tsx`; specimen test asserts the hunk text | in-flight:t-s1-files | — |
| TD-03 | Dual line-number gutter, removed rows danger-soft with "-", added "+" | S-3e-6 | L-files `DiffBody.tsx`, `diffRows.ts` (+`diffRows.test.ts`); lane test "header, two line-number columns, red and green fills" | in-flight:t-s1-files | — |
| TD-04 | Collapsed run "⋯ 40 unchanged lines" opens on click (and on ↵ when focused) | S-3e-7 | L-files `DiffBody.tsx`; lane test "folds that open" | in-flight:t-s1-files | — |
| TD-05 | Diff base gone: "Compared with the latest commit. The commit this conversation started on is no longer in history." | C-EDGE-4, Q24 | L-files `FileSurface.tsx` `baseGoneNote`; lane test "when the start commit is gone…" | in-flight:t-s1-files | — |
| TD-06 | Base = conversation start commit, fallback latest commit | D3, Q23 | HEAD `internal/remote` workview + bridge `/changes`, `/diff`; tests `internal/remote/workview_start_test.go` TestDiffBaseIsTheStartCommit, TestDiffStartUnreachableFallsBack, TestDiffStartOutsideGit; `internal/desktopbridge/workview_start_test.go` TestOpeningAConversationRecordsItsDiffStart | complete | — |
| TD-07 | No changes → one muted line; very large diff cut | ASSUME: "No changes." / "The diff is cut at 5000 lines." | L-files `FileSurface.tsx` | in-flight:t-s1-files | — |
| TD-08 | ⌘-click a changed-file chip opens a diff tab in the background | Interactions File chip | L-files `FileChip.tsx` | in-flight:t-s1-files | — |
| **Terminal tab** | | | | | |
| TT-01 | Terminal header: title, "~/codeaf · job" or "· terminal", state words, buttons stop, copy, ellipsis | Shell 3c S-3c-2, S-K-6 | HEAD `kinds/terminal.ts` placeholder; L-term `features/terminal/TerminalHeader.tsx`; lane test "a job tab shows its header… design geometry" | in-flight:t-s1-terminal | — |
| TT-02 | Output on `--term` field, monospace, ANSI hues ~60% chroma only here | S-3c-3, Q3 | L-term `TerminalScreen.tsx` (xterm), `ansi.ts`, `theme.ts`; lane test "ANSI colours are token hues at about 60% chroma, in light and dark" | in-flight:t-s1-terminal | — |
| TT-03 | Running: header "Running · 2m 14s" with accent dot; tab silent | S-3c-4 | L-term `state.ts`, `TerminalHeader.tsx` | in-flight:t-s1-terminal | — |
| TT-04 | Exited: "exit 0 · 2m ago"; failed exit → red glyph in header, red tab dot | S-3c-5, S-3c-9 | L-term `state.ts` (test "the glyph tone follows the state and the exit code") | in-flight:t-s1-terminal | — |
| TT-05 | Interactive shell: typing reaches the PTY; resize reaches the engine | S-3c-8, Q7 | L-term `bindings.ts`; lane tests "the new terminal key opens a shell…", "the screen size reaches the engine" | in-flight:t-s1-terminal | — |
| TT-06 | "Ask codeaf about this output" field → new conversation with the output attached, in a new tab | S-3c-6/7, Q5 | L-term `AskField.tsx`; lane test "Ask codeaf about this output starts a new conversation…" | in-flight:t-s1-terminal | — |
| TT-07 | Stop (square) ends a running job; log stays until Remove | S-3c-2, Q4 | L-term `TerminalHeader.tsx`; lane test "Stop ends a running job and its log stays until Remove job" | in-flight:t-s1-terminal | — |
| TT-08 | Copy (copy icon) copies selection else recent output; "Copied" feedback | S-3c-2 | L-term `TerminalHeader.tsx` | in-flight:t-s1-terminal | — |
| TT-09 | Ellipsis menu while running: the design does not specify it. ASSUME: Copy output, then Remove; a disabled Stop item must be absent, not disabled (repo law) | S-3c-2, CLAUDE.md "absent, not broken" | L-term menu lists `Stop` with `disabled: !canStop` | partial | t-d5-tab-term-menu |
| TT-10 | Finished job menu: `Open log` (scroll-text), `Run again` (rotate-cw) │ `Close tab ⌘W`, `Remove job` (highlighted) | C-EDGE-6 | L-term header menu: Run again (icon play), Copy output, Remove; no Open log, no Close tab, wrong icon | partial | t-d5-tab-term-menu |
| TT-11 | Finished job TAB right-click carries the same items (the specimen is "Finished job · tab menu") | C-EDGE-6 | L-menus tab menu has no kind-specific items | missing | t-d5-int-tab-kind-slots, t-d5-tab-term-tab-menu |
| TT-12 | Finished job tab in strip: square-terminal + name + "exit 0" 11px ink-3 on tab-hover | C-EDGE-5 | no kind meta slot on `Tab.tsx` | missing | t-d5-int-tab-kind-slots, t-d5-tab-term-tab-meta |
| TT-13 | Run again starts the same command in a new tab; absent for running jobs and shells | C-EDGE-6 | L-term tests "a finished job offers Run again…", "a running job and a shell offer no Run again" | in-flight:t-s1-terminal | — |
| TT-14 | Limit "… earlier output trimmed to the last 512 KB" (mono 12 ink-3, first line) | C-EDGE-7, Q6 | L-term `useTerminalFeed.ts` `trimmedLine`; lane test "a log older than the engine keeps says so on its first line" | in-flight:t-s1-terminal | — |
| TT-15 | Limit "16 terminals are open in this conversation. Close one to start another." | C-EDGE-8, Q6 | L-term `state.ts` `limitSentence`; lane test "the limit line shows when the engine refuses a terminal" | in-flight:t-s1-terminal | — |
| TT-16 | Terminal the engine no longer has (bridge restart) says so | ASSUME: one muted line, Remove offered | L-term lane test "a terminal the engine no longer has says so" | in-flight:t-s1-terminal | — |
| TT-17 | ⌃` opens a new terminal under the conversation | 3f S-3f-6 | L-term `keys.ts`, lane test; central registry entry missing (`design/keyboard.ts` is integrator-only) | partial | t-d5-int-tab-keys |
| TT-18 | Narrow window ≤600: meta hides, state words + icons stay, ask field full width | ASSUME: responsive law | L-term css has no media rule | missing | t-d5-tab-term-narrow |
| TT-19 | Light + Dark accessibility | contract | L-term lane test "the tab is accessible in light and dark" | in-flight:t-s1-terminal | — |
| TT-20 | Provider keys never reach the PTY environment | arch "Security" | `internal/env/env.go` strips only codeaf-owned keys | missing | t-d5-be-pty-env-strip (fixed; referenced only) |
| **Engine jobs (model's background jobs) in a job tab** | | | | | |
| TJ-01 | Engine background job opens as a job tab: same kind and glyph as a terminal job ("same for an interactive shell and a job log") | S-3c-8, S-3j-19; Components system note "Background job finished · nightly-bench" | no list/kill route (`jobs.go` registry unexported); only `jobUpdate` events | missing | t-d5-be-jobs-export, t-d5-be-jobs-routes (fixed), t-d5-tab-jobs-client, t-d5-tab-jobs-pane |
| TJ-02 | Job tab header: name, "~/codeaf · job", "Running · elapsed" / "exit N · ago", Stop, Copy, More | S-3c-2/4/5 | none for engine jobs | missing | t-d5-tab-jobs-pane |
| TJ-03 | Job log on the terminal field, read-only, survives reload (engine log file) | S-3c-3, Q4 | none | missing | t-d5-tab-jobs-pane |
| TJ-04 | Stop a running engine job | S-3c-2 | none (`POST /sessions/{id}/jobs/{jobId}/stop` planned) | missing | t-d5-tab-jobs-pane |
| TJ-05 | "Ask codeaf about this output" on an engine job | S-3c-6, Q5 | none | missing | t-d5-tab-jobs-pane |
| TJ-06 | Open a job tab from the conversation ("Background job finished · nightly-bench" note, a job row); ⌘-click = background | Components §2.7/§3.5, S-IX-2 | none | missing | t-d5-tab-jobs-open |
| TJ-07 | Jobs rollup live across conversations (world `jobs` records) keeps a job tab current without its conversation's SSE | arch world stream | none | missing | t-d5-tab-jobs-client |
| TJ-08 | Open log for a finished engine job → the saved log as a read-only tab | C-EDGE-6 | none | missing | t-d5-tab-term-menu |
| **Web tab** | | | | | |
| TW-01 | Web tab body: 48px header row, page on its own light sheet inside the card (margin 0 8 8, r8) | Shell 3d S-3d-1/6, S-K-5 | HEAD `kinds/web.ts` placeholder `backed:false` | missing | t-d5-tab-web-host, t-d5-tab-web-pane |
| TW-02 | Back / forward / reload 28px icon buttons; forward ink-3 when there is no forward history | S-3d-2 | none | missing | t-d5-tab-web-address |
| TW-03 | Address field: max 520, h30, r8, field fill, 12px; favicon/monogram 14px r4; host ink, path ink-3 ellipsis | S-3d-3 | none | missing | t-d5-tab-web-model, t-d5-tab-web-address |
| TW-04 | Address editing: click selects the URL, ↵ navigates, Esc restores; only http/https accepted, a bare host gets https | ASSUME: design silent, conservative scheme rule from arch | none | missing | t-d5-tab-web-model, t-d5-tab-web-address |
| TW-05 | Chat-plus (message-square-plus) starts a conversation with the page attached | S-3d-4, S-3k-12 | none | missing | t-d5-tab-web-chat-plus |
| TW-06 | Open externally (external-link) → default browser | S-3d-5 | `design/native.ts` `openUrl` exists (HEAD) | partial | t-d5-tab-web-address |
| TW-07 | Loading: nothing on the tab, a 2px progress line along the top of the card | S-3d-7, S-3j-23 | HEAD `tabs/LoadingLine.tsx` specimen only (`shell-tabs.spec.ts` "…every tab kind and state" asserts 2px) | partial | t-d5-tab-web-events, t-d5-tab-web-pane |
| TW-08 | Tab icon: real favicon if fetched, else monogram (coloured r-square, letter) | S-3d-8, S-3j-22 | HEAD `Tab.tsx` `KindIcon`/`monogramHue` specimen only | partial | t-d5-tab-web-events |
| TW-09 | Tab title follows the page title; address follows in-page navigation; URL persists across relaunch | S-IX-5, ASSUME | none; `view-state.ts` has no web field | missing | t-d5-int-tab-kind-slots, t-d5-tab-web-events |
| TW-10 | Native child webview per web pane, bounds follow the sheet rect (resize, split, rail collapse); hidden when its tab is not shown | arch "Web tab" | none | missing | t-d5-nat-web-webview, t-d5-prim-native-web-ts (fixed), t-d5-tab-web-host |
| TW-11 | Overlays over the native webview (menus, hover preview, tooltip, toast, overview, drag zones, Quick Look) never sit under the page | ASSUME: hide the webview while a DOM overlay intersects the sheet; show the sheet fill + title | none | missing | t-d5-nat-web-overlay-hide (fixed), t-d5-tab-web-occlusion |
| TW-12 | Error / offline / blocked / certificate / non-http page states | ASSUME: one muted line on the sheet ("This page can't be shown here." / "You're offline." / "This site doesn't allow being shown inside codeaf.") + "Open in browser"; nothing red | none | missing | t-d5-tab-web-states |
| TW-13 | New-window requests (target=_blank, window.open) | ASSUME: open as a background web tab right after this one; never a native window; http/https only | none | missing | t-d5-tab-web-newwindow |
| TW-14 | Find in page (⌘F while the web pane has focus) | ASSUME: slim find field replaces the address row; Esc closes; "n of m" | none | missing | t-d5-tab-web-find-research, t-d5-tab-web-find |
| TW-15 | Keys while the native webview holds focus (⌘L address, ⌘R reload, ⌘W, ⌘T, ⌘F) | ASSUME: the native app menu accelerators deliver them, because webview keystrokes never reach the renderer | none | missing | t-d5-tab-web-keys |
| TW-16 | Pasting a URL into the new-tab field opens a web tab | S-3d-9, S-3f-13 | L-newtab: "A URL is a question for now" | missing | t-d5-sh-newtab-url |
| TW-17 | Link chip: click → default browser; ⌘-click / middle → web tab in codeaf; right-click Copy link; hover full URL | Interactions "Link chip", S-3d-10 | HEAD `conversation/assets/LinkChip.tsx` opens externally only | partial | t-d5-tab-web-open, t-d5-cv-link-chip |
| TW-18 | Hover preview / overview card for web: screenshot + title + URL | S-3k-6/8 | none | missing | t-d5-tab-web-preview (text card; screenshot is an open question) |
| TW-19 | Web kind absent where it cannot work (browser build, no native web capability) | CLAUDE.md "absent, not broken" | HEAD `backed:false` | partial | t-d5-nat-capabilities (fixed), t-d5-tab-web-pane |
| TW-20 | Web tab moved to another window / opened in split: its webview follows | ASSUME: close and reopen at the same URL in the new host | none | missing | t-d5-tab-web-host |
| TW-21 | Narrow window ≤600: nav buttons stay, chat-plus and external fold into ⋯, address flexes | ASSUME | none | missing | t-d5-tab-web-address |
| TW-22 | Web pane Light + Dark (sheet is always light), keyboard reach, ring on focus | S-3d-6 sheet `data-theme=light`, S-IX-13 | none | missing | t-d5-tab-web-test |
| TW-23 | Web tab needs-you / failed (3j columns drawn) | S-3j-6 | ASSUME: failed = page could not load (red glyph); needs-you never set | missing | t-d5-tab-web-states |
| **History tab** | | | | | |
| TH-01 | ⌘Y opens History; a second press focuses it rather than adding a tab; several can be open | 4a S-4a-1/23, I-IKY-10 | HEAD `kinds/history.ts` placeholder; L-hist `features/history/HistoryPane.tsx`; lane test "Control Y opens History and a second press selects it" | in-flight:t-s1-history | — |
| TH-02 | Header "History · 1,284 conversations", search field "Search what you discussed, decided or changed ⌘F", filters All/Decisions/Files/Tasks/Open | S-4a-3/4/5, C-INPUT-3/4 | L-hist `HistoryPane.tsx`, `history.css`; lane test "geometry follows the design" | in-flight:t-s1-history | — |
| TH-03 | List grouped Today/Tuesday/Last week, sticky headers, rows say something, live glyph + "Open" | S-4a-6..12, C-HIST-1..3 | L-hist `HistoryList.tsx`, `HistoryRow.tsx`; lane test "the list groups by time and each row says something" | in-flight:t-s1-history | — |
| TH-04 | Open rows' live state for conversations not attached on this bridge (other window, closed-but-running) | S-4a-7/8, arch world stream | L-hist `attachedStates()` only sees this bridge's attached sessions | partial | t-d5-tab-hist-world-live |
| TH-05 | Recap pane: header, What we discussed, Decided (with who), Outcome + file chips, Continue / Read conversation | S-4a-13..17 | L-hist `RecapPane.tsx`, `internal/session/recap.go`; lane test "selecting a row shows its living recap" | in-flight:t-s1-history | — |
| TH-06 | Continue replaces History with the live conversation, composer focused; ⌘-click keeps History | S-4a-18/20, S-4e-5 | L-hist lane test "Continue replaces the History tab…" | in-flight:t-s1-history | — |
| TH-07 | Read conversation: read-only in the same tab, recap on top, Back returns | S-4a-19 | L-hist `ReadView.tsx`; lane test | in-flight:t-s1-history | — |
| TH-08 | Search: tab title "History · lexer", best match card, Decisions/Discussed/Files groups, highlight, Esc clears, archived included | 4b S-4b-1..11, C-HIST-4 | L-hist `SearchResults.tsx`, `HistoryPane.tsx`; lane test "search answers in plain words" | in-flight:t-s1-history | — |
| TH-09 | Best match: Read recap / Jump to message | I-IPH-33/34 | L-hist lane test "Read recap goes to the recap; Jump to message…" | in-flight:t-s1-history | — |
| TH-10 | ↑↓ moves, ↵ Continue, ⌘F focuses the field | I-IPH-31/32, S-4a-22 | L-hist lane tests "the arrow keys and Enter…", "Control F focuses…" | in-flight:t-s1-history | — |
| TH-11 | Thousands of rows virtualized, fixed height, paging | S-4a-24 | L-hist lane test "thousands of rows stay virtual" | in-flight:t-s1-history | — |
| TH-12 | Row right-click: Continue · Read · Add to place · Archive · Delete | I-IPH-26..30, S-4a-21 | no `ContextMenu` in `features/history/*` | missing | t-d5-tab-hist-row-menu, t-d5-tab-hist-add-to-place |
| TH-13 | Row ⌘-click / middle opens in a new tab | I-IPH-25 | L-hist (Continue ⌘-click tested; row ⌘-click unverified) | in-flight:t-s1-history | t-d5-tab-hist-test |
| TH-14 | Delete: archive first; inline confirm naming counts ("Delete 14 chats?"); toast with Undo for 10s | I-IFL-18..20, S-IX-10 | no delete route or UI | missing | t-d5-tab-hist-delete-route, t-d5-tab-hist-delete-ui |
| TH-15 | Auto-archive at 12h idle (not pinned/running/needs-you); one toast next launch "Archived 6 tabs idle for more than 12h" Review / Restore all | 4c S-4c-8/9, C-OVL-2 | L-hist `host.tsx`, `ArchiveToast.tsx`; lane tests "tabs idle for 12 hours archive themselves…", "Review opens History…", "Escape dismisses the toast" | in-flight:t-s1-history | — |
| TH-16 | Tasks never get their own row; "Task in [conversation]" in search; Tasks filter | S-4a-25 | L-hist `model.ts`, Go filters | in-flight:t-s1-history | — |
| TH-17 | Empty / failed: one dim line | Components Edge caption | L-hist lane test "the empty and failed states say one dim line" | in-flight:t-s1-history | — |
| TH-18 | Narrow window: list, then recap with Back | responsive law | L-hist lane test "a narrow window shows the list, then the recap with Back" | in-flight:t-s1-history | — |
| TH-19 | Light + Dark accessibility | contract | L-hist lane test "History passes the accessibility contract in …" | in-flight:t-s1-history | — |
| TH-20 | Recap writer = "Titles and summaries" role; manual pages updated | Q8, manual law | L-hist `internal/session/recap.go`, `internal/manual/chat/history.md` | in-flight:t-s1-history | — |
| TH-21 | ⌘T searches history inline: "From history" rows, "· archived", "See all 14 in History ⌘↵" | 4c S-4c-2..7 | not in L-newtab or L-hist | missing | t-d5-sh-newtab-history |
| **Settings tab** | | | | | |
| TS-01 | Settings is a tab (⌘,), opened once from the rail or the key | I-IFL-26, S-K-7 | HEAD `kinds/settings.ts` placeholder, page outside tabs; L-rail `kinds/SettingsPane.tsx`, `settings.ts` backed; lane tests "⌘, opens the Settings tab once", "Settings is a tab: the rail item opens it once…" | in-flight:t-s1-rail | — |
| TS-02 | Model per role: one row per role, saves at once, receipt "Saved · applies to the next call", effort when supported | D5, Q9, Q10 | HEAD `features/settings/SettingsPage.tsx`; tests `settings-models.spec.ts` "changing a role saves at once, shows the receipt, offers effort, and can be reset", `model-roles.spec.ts` | complete | — |
| TS-03 | Pinned models: three slots, take a catalog model, composer picker follows | D4, C-INPUT-2 | HEAD `SettingsPage.tsx`, `useModelSettings.ts`; tests `settings-models.spec.ts` "a pinned slot takes a catalog model…", "the picker shows the three pinned labels…" | complete | — |
| TS-04 | Provider key status (source + present), never the secret | arch Settings, code-bridge 6.7 | no route, no UI | missing | t-d5-be-key-status (fixed), t-d5-tab-set-key-status |
| TS-05 | Appearance: theme; "reduce motion follows the OS" | I-IFL-26 | `ThemeSelect` primitive exists, not on the settings page | missing | t-d5-tab-set-appearance |
| TS-06 | Engine connection: local/remote, state, Retry | I-IFL-26 | `engine_connection` native command exists; no section | missing | t-d5-tab-set-engine |
| TS-07 | Permissions section | I-IFL-26 | no engine route for approval policy (`Meta.Approval` only) | missing | t-d5-tab-set-permissions-research |
| TS-08 | Settings tab has rest/hover/active only (no needs-you/failed) | S-3j-8 | by construction (kind sets no state) | in-flight:t-s1-rail | — |
| TS-09 | Tab title. 3j draws "Models", but the page grows past models. ASSUME: the title reads "Settings" | S-3j-8 vs I-IFL-26 | L-rail label "Settings" | in-flight:t-s1-rail | — |
| TS-10 | One centred column, no horizontal overflow at 320px, Light + Dark | responsive law | test `settings-models.spec.ts` "settings is one centred column, accessible in light and dark, with no horizontal overflow at 320px" | complete | — |
| TS-11 | Hover/overview card: "Pinned: … Default effort: …" | 3h Shell-ann line 243 | L-rail `SettingsPreview` | in-flight:t-s1-rail | — |
| TS-12 | New sections keyboard-reachable, accessible in Light + Dark, at 320px | contract | none | missing | t-d5-tab-set-test |
| **Inbox tab** | | | | | |
| TI-01 | Inbox (pinned) body: "Running in the background" (closed-but-running work, click reopens where it was) | 3l S-3l-7, S-K-10 | HEAD `kinds/inbox.ts` placeholder; L-menus `kinds/inbox/InboxPane.tsx`, `closing/background.ts`; lane test "closed-but-running work lists in the pinned Inbox and a click reopens the tab" | in-flight:t-s1-menus | — |
| TI-02 | Stopping the closed work clears it | S-3l-4/5 | L-menus lane test "stopping the closed work clears it from the Inbox" | in-flight:t-s1-menus | — |
| TI-03 | Inbox dot only when something needs you | S-3j-21/23 | L-menus lane test "the Inbox carries a dot only when something needs you" | in-flight:t-s1-menus | — |
| TI-04 | Needs-you list across ALL conversations, not only open tabs ("where everything that needs you lands") | S-3j-21, arch Inbox | L-menus lists only open tabs' summaries (`api.background.needsYou`) | partial | t-d5-be-attention, t-d5-prim-world-client (fixed), t-d5-tab-inbox-feed |
| TI-05 | Needs-you row: amber dot, conversation title, place, the question in one line, age; oldest first | ASSUME from 3l row anatomy + I-ISH-1 "oldest" | L-menus row = dot + title + "needs you" | partial | t-d5-tab-inbox-rows |
| TI-06 | Rail Inbox click opens Inbox focused on the oldest needs-you item | I-ISH-1 | none | missing | t-d5-tab-inbox-rows |
| TI-07 | Answer in place: the row opens the question card; answering attaches the conversation lazily (`POST /sessions {sessionFile}` then `/answer`); the row leaves | arch Inbox "answering attaches lazily"; S-3k-9 (act without switching) | none | missing | t-d5-tab-inbox-answer |
| TI-08 | Row click opens/focuses that conversation tab with the question focused in the tray; ⌘-click = background | I-ISH-1/2, I-IFL-13 | L-menus `select` only for open tabs | partial | t-d5-tab-inbox-open |
| TI-09 | Empty: one muted line "Work that needs you, or keeps running after you close its tab, lands here." | DS emptiness law | L-menus `InboxPane.tsx` | in-flight:t-s1-menus | — |
| TI-10 | ASSUME: failed work is not listed in the Inbox. Failed shows on the tab and the rail, and work that finishes leaves the Inbox | 3l "until it finishes or needs you" | L-menus lists running/waiting only (unasserted) | missing | t-d5-tab-inbox-test |
| TI-11 | Keyboard ↑↓/↵ through rows, Light + Dark, narrow ≤600 | S-IX-13, responsive law | L-menus rows are buttons; arrows not wired | missing | t-d5-tab-inbox-rows, t-d5-tab-inbox-test |
| TI-12 | Inbox mirrors live in a second window | I-IFL-9 | world stream shared (planned) | missing | t-d5-tab-inbox-feed |
| **New tab (⌘T)** | | | | | |
| TN-01 | One centred field, rows, caption "Type a question, a file, a URL, or a command." | 3f S-3f-1..12, C-NEWT-1..5 | HEAD `kinds/newtab.ts` stand-in; L-newtab `kinds/newtab/NewTabPane.tsx`, `rows.ts`; lane tests "the field, its rows and the caption have the design geometry" | in-flight:t-s1-newtab | — |
| TN-02 | First row "Ask “fix…” in a new conversation ↵" turns the tab into a conversation | S-3f-4/15 | L-newtab lane tests "Enter on the first row creates a session…" | in-flight:t-s1-newtab | — |
| TN-03 | Start rows: New terminal ⌃`, Open file… ⌘O | S-3f-6/7 | L-newtab `rows.ts` shows terminal only when `input.terminal` (false until L-term lands); ⌘O unbound | partial | t-d5-sh-newtab-start-rows, t-d5-int-tab-keys |
| TN-04 | Matching files from the engine open a file tab | S-3f-9 | L-newtab lane test "matching files come from the engine and open a file tab" | in-flight:t-s1-newtab | — |
| TN-05 | Open-tab rows (amber dot, "open tab", jump key) and recently closed ("closed 1h ago") | S-3f-10/11 | L-newtab lane tests "arrows move, Enter on an open tab row jumps…", "a closed tab row brings the tab back" | in-flight:t-s1-newtab | — |
| TN-06 | Pasted URL row → web tab | S-3f-13 | L-newtab treats a URL as a question | missing | t-d5-sh-newtab-url |
| TN-07 | From history section | 4c | none | missing | t-d5-sh-newtab-history |
| TN-08 | Esc clears then closes; 320px fit; Light + Dark | contract | L-newtab lane tests "Escape clears the text…", "stays inside the card at 320px", "is accessible… light and dark" | in-flight:t-s1-newtab | — |
| TN-09 | New tab kind has no needs-you/failed | S-3j-9 | by construction | in-flight:t-s1-newtab | — |
| **Task tab** | | | | | |
| TK-01 | Task tab body = conversation components on the task page (route.taskId) | S-K-2, S-3j-3 | HEAD `kinds/task.ts` → `ConversationPane`; test `conversation-tasks.spec.ts` "a task notice opens the task in the same tab; Back and Ctrl+[ return" | complete | — |
| TK-02 | ⌘-click a task row opens a background task tab | S-IX-23 | HEAD `Workspace.tsx` `openTaskTab`; test `conversation-tasks.spec.ts` "modifier-click on a task row opens a background tab"; `model.test.ts` "open and open-task add a tab, foreground or background" | complete | — |
| **Cross-kind** | | | | | |
| TX-01 | Unbacked kinds are never opened; the placeholder line never shows in the live app | CLAUDE.md law | HEAD `kinds/Placeholder.tsx` + `backed` flag; no test proves the live app never opens one | partial | t-d5-tab-web-pane (last unbacked kind) |
| TX-02 | Kind-specific glyph/meta/menu slots (file type icon, web favicon, job "exit 0", finished-job menu) | S-3j-17/22, C-EDGE-5/6 | `kinds/slots.ts` has only icon/pane/preview | missing | t-d5-int-tab-kind-slots |
| TX-03 | Relaunch restores each kind's view (file view, web URL, terminal reattach, job id) | I-IFL-7/8 | L-files `file`, L-term reattach; web/job missing | partial | t-d5-int-tab-kind-slots, t-d5-tab-web-events, t-d5-tab-jobs-pane |

Not rows here, because other writers own them: the task-notice right-click menu (t-d5-cv), tasks joining their conversation's group, the tab-strip glyph states of every kind, the 3h overview cards, the 3k preview cards for conversation, task, terminal and diff (t-d5-sh with t-s1-preview), and the shared Toast primitive that `ArchiveToast` and `ClosingToast` should move onto (t-d5-sh).

#### Open questions

1. **Web page screenshots for hover previews and overview cards (S-3k-6/8).** Tauri v2 child webviews have no stable capture API. ASSUME: a web preview is a text card (title, host and path) until native capture exists. t-d5-tab-web-preview ships the text card only.
2. **Web error, offline, blocked and certificate wording (TW-12).** The design does not draw them. ASSUME: one muted line on the sheet plus "Open in browser". Nothing red inside the pane. The tab glyph turns red only when the page failed to load. The strings live in one constant file so the designer can respell them.
3. **New-window requests from a page (TW-13).** ASSUME: a background web tab placed right after the opener, in the same place. Never a native window, never a non-http(s) scheme.
4. **Find in page and keys while the native webview holds focus (TW-14/15).** The webview has no IPC by rule (arch "Web tab"), and its keystrokes never reach the renderer. ASSUME: ⌘L, ⌘R, ⌘F, ⌘W and ⌘T reach the app through native app-menu accelerators (the existing `desktop-tab-action` path). Find uses a native find command, if t-d5-tab-web-find-research confirms one exists on both WKWebView and WebKitGTK. Otherwise find stays absent.
5. **"Chat-plus starts a conversation with the page attached" (TW-5).** ASSUME: a new conversation tab opens focused, with the page URL and title as a link attachment in the composer, NOT sent. The page body is never scraped (no invented content, and the model can fetch it).
6. **Failed work in the Inbox (TI-10).** ASSUME: not listed. 3l says background work stays in the Inbox "until it finishes or needs you". Failed shows on the tab, rail and notification.
7. **Answer in place inside the Inbox (TI-07).** The design shows acting from a preview (3k), not an Inbox card layout. ASSUME: the Inbox row expands into the same question card the tray draws, reusing the tray component read-only. Answering attaches the conversation lazily.
8. **"Open log" for a finished job (C-EDGE-6).** For a PTY job the log is the tab itself. ASSUME: "Open log" appears only for engine jobs that keep a log file, and opens that log read-only in the job tab's field (full log, not the 512 KB tail). This needs `t-d5-be-jobs-routes` to serve `GET /sessions/{id}/jobs/{jobId}/log`. If that route is not in the backend writer's contract, the item stays absent.
9. **Editors list for "Open in ⌄" (TF-06).** ASSUME: a local engine lists the OS handlers registered for the file's type, default first, and opening one goes through a confined native command. A remote engine lists nothing, so the menu holds Copy path and Copy relative path.
10. **Settings tab title (TS-09).** 3j draws "Models", and Interactions makes Settings a page with five sections. ASSUME: "Settings".
11. **Permissions in Settings (TS-07).** No engine route reads or writes the approval policy. ASSUME: the section stays absent until t-d5-tab-set-permissions-research settles the engine contract.
12. **Open-tab jump key in the new-tab rows (3f "⌥⌘3" vs ⌘1–9, Q30 open).** Not decided here. The new-tab rows show whatever the central keyboard registry assigns.
13. **Delete undo (TH-14).** "Toast with Undo for 10s" implies soft delete. ASSUME: delete moves the conversation to a trash folder under `$CODEAF_HOME`, and the trash is purged after the undo window when the next launch sweeps. Only archived conversations can be deleted ("archived first").

#### Counts

| Status | Rows |
|---|---|
| complete | 6 |
| partial | 18 |
| missing | 51 |
| in-flight:t-s1-files | 17 |
| in-flight:t-s1-history | 17 |
| in-flight:t-s1-terminal | 13 |
| in-flight:t-s1-newtab | 6 |
| in-flight:t-s1-rail | 4 |
| in-flight:t-s1-menus | 4 |
| n/a-decided | 0 |
| **Total** | **136** |

### 7.4 Conversation

HEAD 39d440c4f (`/home/santosh/codeaf-design-plan/desktop`). Paths relative to `desktop/` unless prefixed `internal/` or a lane.
Lanes read: s1-rail (`agent-a99c430e0f6cc71ca`, rewires ModelPicker to ⌥⌘1-3 and turnScroll to a shortcut registry), s1-split (`agent-ad64d2e18339b086b`, CompactComposer), s1-files (`agent-a451c7c04660863b4`, committed abba872c6 not merged: FileChip ⌘-click/middle → file/diff tab), s1-menus (`agent-a320e518fe154e07c`, Menu.tsx/Tooltip.tsx edits).
No `conversation-audit.md` exists in `/home/santosh/.codex/codeaf-design-run/` (only an empty log), so this matrix comes from the inventories plus my own reading of the source and tests.
Tests named are Playwright specs under `tests/ui/` (Chromium + WebKit) unless marked "node". The 29 node `*.test.ts` are run by no npm script or CI, so a row backed only by a node test is `partial`.

| Cov ID | Design item (state/input) | Source (page §id + inventory ID) | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| **Header** ||||||
| CV-001 | Header 52px: title (medium) on clean canvas, no bar or blur | Conv 1a; I-C1a-2, I-C1f-5, C-NAV-1 | features/conversation/ConversationBar.tsx, conversation-bar.css | partial (padding 22/12 vs 20/10; title shown only when titleSource set; no test asserts title) | t-d5-cv-header-bar, t-d5-cv-test-header |
| CV-002 | Header "N running" with breathing accent dot (cf-breathe 2.8s, the only breathing dot) | 1a; I-C1a-3, I-CL-2, C-NAV-2 | ConversationBar.tsx (static StatusMark) | missing (no breathe keyframes) | t-d5-int-tok-c-motion, t-d5-cv-header-bar |
| CV-003 | Header "N need you" = pending question count | 1a; I-C1a-4, Q13 | ConversationBar.tsx; ConversationView.tsx barCounts | complete (conversation-task-panel-design.spec "the header names what is running and what needs you…") | — |
| CV-004 | Click "N running" / "N need you" → Tasks opened pre-filtered (Running / Needs you) | Int Conversation; I-ICV-33 | ConversationBar.tsx (counts are spans) | missing | t-d5-cv-header-bar, t-d5-cv-header-wire |
| CV-005 | Tasks toggle button (panel-right 28px, hover field), aria-pressed | 1a/1b; I-C1a-5, I-C1b-2, I-ICV-34 | ConversationBar.tsx | complete (conversation-tasks.spec "panel shows counts and nested rows, closes, and reopens from the header toggle") | — |
| CV-006 | ⌘⇧K toggles the Tasks panel | Int Conversation + Shortcuts; I-ICV-34, I-IKY-16 | none | missing | t-d5-int-cv-keys, t-d5-cv-header-wire |
| CV-007 | Double-click the header title → rename (same as tab rename) | Int Flows; I-IFL-16 | ConversationBar.tsx (plain span) | missing | t-d5-cv-header-bar, t-d5-cv-header-wire |
| CV-008 | Auto-title ~6 words, regenerated once after the first answer unless renamed | Int Flows; I-IFL-17 | engine titleChanged → tabs `title` reducer (rank-guarded, manual wins) | partial (manual-wins rank exists; "regenerated once" is engine-side and untested) | t-d5-cv-test-header |
| CV-009 | Using chip SLOT in the header (chip itself is places) | Int Conversation; I-ICV-31, C-NAV-1 | none | missing | t-d5-cv-header-bar (slot); chip = places writer |
| CV-010 | Header counts on narrow windows (≤600px) | ASSUME: counts hide ≤600px, the toggle stays; the title truncates with a tooltip | conversation-bar.css `@media (max-width:600px)` | partial (no test) | t-d5-cv-test-header |
| CV-011 | Header in task view: back arrow, trail, counts, no panel toggle | 1c; I-C1c-2..4, Q11 | TaskRoute.tsx, Breadcrumb.tsx | complete (conversation-task-panel-design.spec "the task view header is the back arrow, the trail and the counts: no panel toggle") | — |
| CV-012 | Header title keyboard: focus-visible ring, Enter starts rename | ASSUME: the title is a button only when renaming is possible; F2/Enter renames | none | missing | t-d5-cv-header-bar |
| **Transcript: turns, folding, bubbles** ||||||
| CV-020 | User bubble: literal text, right, radius 16, ≤80% | Comp §2.1; C-MSG-1, I-C1a-10 | UserMessage.tsx | complete (conversation.spec "first send creates the session, shows literal words…") | — |
| CV-021 | Long bubble clamp at 8 lines + 45% mask + "Show more"/"Show less" | 1b; I-C1b-5/6, I-C1f-10, C-MSG-4 | UserMessage.tsx, user-message.css | partial (no test) | t-d5-cv-test-messages |
| CV-022 | Sending: 60% opacity until recorded (plain text only) | Q14; C-MSG-5 | UserMessage.tsx `data-sending` | partial (no test asserts opacity) | t-d5-cv-test-messages |
| CV-023 | Copy on hover/focus-within (26px), no edit | Int; I-ICV-3, I-C1f-35, C-MSG-3 | UserMessage.tsx RowActions + CopyButton | partial (no test on the bubble's Copy) | t-d5-cv-test-messages |
| CV-024 | Edit pencil on a sent message | Comp §2.1 specimen; D1 | — | n/a-decided (D1: Copy only) | — |
| CV-025 | Right-click user bubble → "Copy" | Int; I-ICV-2 | none | missing | t-d5-cv-menu-message |
| CV-026 | Message actions stay visible on touch | 1f; I-C1f-35, I-C1f-40 | styles/ui.css `(hover:none),(pointer:coarse)` | partial (no test) | t-d5-cv-test-messages |
| CV-027 | Bubble with paste card ("Pasted text" · "214 lines", 48px preview, mask 25%) | 1b; I-C1b-9..11, I-C1f-11 | UserMessage.tsx + composer/PasteCard.tsx variant sent | partial (no render test in bubble) | t-d5-cv-test-messages |
| CV-028 | Bubble with attachments (64px thumbs, file chip on surface) | Comp §2.1; C-MSG-2 | blocks/AttachmentView.tsx | complete (conversation-work.spec "a picture sent with the message goes to the engine and shows in the bubble") | — |
| CV-029 | Folded turn row: question ink-2 + digest ink-3, 30px | 1a/1b/1e; I-C1a-7..9, I-C1e-61, C-NAV-7 | blocks/TurnViewV2.tsx | complete (conversation.spec "folding a turn shows the digest and survives reload…") | — |
| CV-030 | Folded turn hover fill / focused shows chevron-down | 1e/1f; I-C1e-59/60, I-C1f-34, C-NAV-8 | TurnViewV2.tsx, conversation.css | complete (conversation-flow-states.spec "a folded turn warms on hover and shows its unfold chevron…") | — |
| CV-031 | Click folded turn expands in place; scroll held (scrollTop compensated) | 1f Folding; I-C1f-23, I-ICV-4 | turnScroll.ts useFoldAnchor | partial (no test asserts the held top line) | t-d5-cv-test-folding |
| CV-032 | Right-click folded turn → "Copy answer", "Open in new tab" | Int; I-ICV-5/6 | none | missing | t-d5-cv-menu-message |
| CV-033 | >12 turns → "N earlier turns" group (chevron-right) toggles | 1e history; I-C1e-56/57, C-NAV-6 | EarlierTurns.tsx, folding.ts | partial (node folding.test.ts only) | t-d5-cv-test-folding |
| CV-034 | "Earlier messages summarized" divider between hairlines | 1e; I-C1e-58, C-NOTE-4 | EarlierTurns.tsx, SystemNote compaction | partial (no Playwright test) | t-d5-cv-test-folding |
| CV-035 | ⌘↑ / ⌘↓ step between user messages with 72px offset; Esc returns to latest | 1f Jump; I-C1f-24/25, I-IKY-19/20 | turnScroll.ts useTurnJump (lane rewires to shortcut registry) | in-flight:t-s1-rail | t-d5-cv-test-folding (after lane) |
| CV-036 | Assistant answer prose / Markdown (GFM, code, tables, lists) | Comp §2.5/2.6; C-ANS-2..9 | components/ui/Markdown.tsx, styles/markdown.css | complete (markdown.spec "assistant Markdown renders GFM structure…") | — |
| CV-037 | Interim update with "Update" eyebrow, "— cut off" | Comp §2.3; C-ANS-1 | blocks/BlockViews.tsx | complete (conversation-work.spec "an interim update is set off from the final answer"; conversation-flow-design.spec "a cut update ends on the line of its words…") | — |
| CV-038 | Answer footer on hover: Copy + "Worked 42s" opens work | Comp §2.6; C-ANS-10 | BlockViews.tsx | complete (conversation-flow-design.spec "the answer names its work on hover and the link opens the work block") | — |
| CV-039 | Turn telemetry "Worked 1m 4s" (settled) | 1b; I-C1b-7, I-C1b-12 | work/format.ts, WorkBlockView.tsx | complete (conversation-live-polish.spec "a retried call is not counted failed, and a question wait is not worked time") | — |
| CV-040 | Turn cost/tokens on chat turns | Design silent (ambiguity 24); Q15 | none | n/a-decided (Q15: drawn only when the engine sends it; 1b draws no cost on chat turns) | — |
| CV-041 | Code block horizontal scroll with 24px right mask while more to the right | 1f Horizontal; I-C1f-13 | components/ui/useMoreToRight.ts, Markdown.tsx | partial (tables tested: conversation-flow-states.spec "an answer table fades its right edge…"; code blocks untested) | t-d5-cv-test-messages |
| CV-042 | Code block copy (24px) in head | Comp §2.6; C-ANS-6 | Markdown.tsx + CopyButton | partial (no test) | t-d5-cv-test-messages |
| **Work block, steps, thinking, steer** ||||||
| CV-050 | Work block folded line "Worked 42s · thought 6s · N steps · N calls · 1 failed" (failed in danger) | Comp §3.1; C-WORK-1 | work/WorkBlockView.tsx, format.ts | complete (conversation-work.spec "work folds to one line, opens to titled steps…") | — |
| CV-051 | Work block line hover: field fill, ink | Int; I-ICV-10, C-WORK-2 | workBlock.css | partial (no test) | t-d5-cv-test-work |
| CV-052 | Click work line expands/folds | Int; I-ICV-8 | WorkBlockView.tsx | complete (conversation-work.spec "work folds to one line…") | — |
| CV-053 | Right-click work line → "Copy log" | Int; I-ICV-9 | none | missing | t-d5-cv-menu-work |
| CV-054 | Live work header "Working" + mono clock | Comp §3.1; C-WORK-3 | WorkBlockView.tsx Summary | partial (no direct assertion) | t-d5-cv-test-work |
| CV-055 | Step settled: 28px, ink-3, duration right | 1a; I-C1a-12, C-WORK-4, Q19 | work/WorkStepView.tsx, work-step.css (`step-row-height` 26) | partial (row height 26 vs 28; ink not asserted) | t-d5-tok-cv-geometry, t-d5-cv-test-work |
| CV-056 | Live step: accent dot, ink, command under it | 1a/1c; C-WORK-5 | WorkStepView.tsx | complete (conversation-flow-design.spec "a running step is one row with its command beneath…") | — |
| CV-057 | Live step title shimmer (cf-shimmer 2.4s, only the live step) | 1a/1c, 1f; I-C1a-15, I-C1c-16, I-CL-1, C-WORK-6, C-NAV-4 | none in work-step.css | missing | t-d5-cv-step-shimmer |
| CV-058 | Step settles: shimmer stops, clock freezes, ink drops to ink-3 | Comp; C-WORK-7 | WorkStepView.tsx | partial (no test) | t-d5-cv-step-shimmer, t-d5-cv-test-work |
| CV-059 | Failed step: ink-2 + red 6px dot + "failed · 4.1s" | 1a; I-C1a-14, I-IFL-22, Q19, C-TOOL-6 | WorkStepView.tsx, StepMark.tsx | complete (conversation-flow-design.spec "…a failed call carries its time in danger ink") | — |
| CV-060 | Permission receipt inline in work ("allowed once") | 1a; I-C1a-13 | tray/ReceiptLine.tsx, model/receipts.ts | complete (conversation-flow-design.spec "…a receipt reads "verdict · who · when"…") | — |
| CV-061 | Refused by policy: struck-through command + "refused" (shows as a receipt) | 1c; I-C1c-14, I-IFL-23, C-TOOL-8 | ToolCallRow.tsx, tool-call.css | partial (strikethrough unverified, no test) | t-d5-cv-test-work |
| CV-062 | Tool rows forming ("Preparing…" .55) / waiting on you (never folds) / stopped | Comp §3.3; C-TOOL-2/3/7 | ToolCallRow.tsx | partial (no test per state) | t-d5-cv-test-work |
| CV-063 | Click step row expands calls; edit diff (no hunk header, Q32); bash output footer "exit 1" + "Show full output" | Comp §3.3; I-ICV-11, C-TOOL-13/14, Q32 | CallDetail.tsx, DiffView.tsx, TerminalBlock.tsx | complete (conversation-work.spec "work folds… an edit shows its diff and a command its terminal") | — |
| CV-064 | Right-click step → "Copy command or output" | Int; I-ICV-12 | none | missing | t-d5-cv-menu-work |
| CV-065 | Step row hover fill | Int; I-ICV-13 | work.css | partial (no test) | t-d5-cv-test-work |
| CV-066 | Thinking live: accent dot + shimmer "Thinking", last 3 italic lines masked; no clock (Q33) | Comp §3.4; C-THINK-1, Q33 | work/ThinkingView.tsx, thinking.css | complete (conversation-flow-design.spec "…steps, thinking and notices wear the design type and height") | — |
| CV-067 | Thinking collapsed "Thought for 6s"; nothing after reload | Comp §3.4; C-THINK-2/3 | ThinkingView.tsx | partial (no test) | t-d5-cv-test-work |
| CV-068 | Steer landing line in work block: landing clause ink-3 + corner-down-right + text | 1a; I-C1a-16, C-STEER-1, I-X-10 | blocks/Steer.tsx, model/live.ts | partial (render untested) | t-d5-cv-test-work |
| CV-069 | Steer consumed: landing clause fades, text ink-2; fell through → next turn's message | Comp §2.2; C-STEER-2/3 | model/live.ts onSteerConsumed/onSteerFell | partial (no test) | t-d5-cv-test-work |
| CV-070 | Work-guide notes (info, job finished, watch fired) | Comp §3.5; C-NOTE-5..7 | NoteItem.tsx, AsideRow.tsx | complete (conversation-records.spec "a session note to the model is one quiet line…") | — |
| **Chips** ||||||
| CV-080 | File chip rest/stats/missing/outside | Comp §5.1; C-CHIP-1..6 | assets/FileChip.tsx | complete (conversation-live-polish.spec "a file chip is one line, and shows the directory relative to the workspace") | — |
| CV-081 | File chip click → preview sheet | Int; I-ICV-14 | FileChip.tsx, PreviewSheet.tsx | complete (conversation-work.spec "a workspace path becomes a file chip that previews through the engine…") | — |
| CV-082 | File chip ⌘-click/middle → file tab | Int; I-ICV-15 | lane s1-files: FileChip.tsx openInTab | in-flight:t-s1-files | — |
| CV-083 | File chip right-click: Open in editor / Reveal / Copy path / Copy relative path | Int; I-ICV-16..19 | assets/FileActions.ts fileMenu | partial (no test) | t-d5-cv-test-chips |
| CV-084 | File chip hover 500ms full-path tooltip (designer tooltip, not native title) | Int; I-ICV-20, I-C1f-39 | FileChip.tsx `title=` | partial | t-d5-cv-chip-tooltips |
| CV-085 | Link chip click → default browser | Int; I-ICV-21 | assets/LinkChip.tsx open → native openUrl | partial (chip render tested, the open itself untested) | t-d5-cv-test-chips |
| CV-086 | Link chip ⌘-click/middle → web tab in codeaf | Int; I-ICV-22 | LinkChip.tsx (modifier clicks fall through to the browser) | missing | t-d5-cv-link-chip |
| CV-087 | Link chip right-click → "Copy link" | Int; I-ICV-23 | none | missing | t-d5-cv-link-chip |
| CV-088 | Link chip hover full URL tooltip | Int; I-ICV-24 | LinkChip.tsx `title=` | partial | t-d5-cv-link-chip |
| CV-089 | Generated image promoted above the answer | Comp; — | assets/ImageFigure.tsx | complete (conversation-work.spec "a generated image is promoted above the answer…") | — |
| **Task notice, task panel, expanded, task view** ||||||
| CV-100 | Task notice done/running (sh-2, live step)/your call | Comp §4.5; C-TNOT-1..3 | TaskNotice.tsx | partial (conversation-records.spec 400px checks the title; states untested) | t-d5-cv-test-tasks |
| CV-101 | Task notice click → task in this tab | Int; I-ICV-25, C-TNOT-4 | TaskNotice.tsx | complete (conversation-tasks.spec "a task notice opens the task in the same tab; Back and Ctrl+[ return") | — |
| CV-102 | Task notice ⌘-click/middle → background tab | Int; I-ICV-26 | TaskNotice.tsx | complete (conversation-tasks.spec "a task notice opens a background tab on modifier-click or middle-click, and its menu matches the row") | — |
| CV-103 | Task notice/row right-click: Open in new tab · Pause/Resume · Stop | Int; I-ICV-27..29 | TaskRow.tsx and TaskNotice.tsx via tasks/taskMenu.ts | partial (notices: conversation-tasks.spec "a child task notice offers Pause while running and Resume once that row is paused"; rows still have no right-click test) | t-d5-cv-test-tasks |
| CV-104 | Task row ⌘-click → background tab | Int; I-ICV-26 | TaskRow.tsx | complete (conversation-tasks.spec "modifier-click on a task row opens a background tab") | — |
| CV-105 | Tasks panel 280px: "Tasks" + "5 of 14", maximize-2, x | 1c; I-C1c-19..21, C-TREE-1 | TaskPanel.tsx | complete (conversation-tasks.spec "panel shows counts and nested rows, closes…") | — |
| CV-106 | Progress strip 3px five segments | 1c; I-C1c-22, C-TREE-2 | tasks/TaskProgress.tsx | partial (no segment assertion) | t-d5-cv-test-tasks |
| CV-107 | Tree rows: group chevron + n/m, connector rule, selected/running/needs-you/queued ring/failed parent/paused/ancestor path | 1c; I-C1c-23..37, C-TREE-3..10, Q22 | tasks/TaskTree.tsx, TaskRow.tsx, TaskMark.tsx, rowText.ts | partial (fills tested by conversation-task-panel-design.spec "a row's rest, hover, press and selected fills…"; glyph states, paused, path prefix only in node tests) | t-d5-cv-test-tasks |
| CV-108 | Row hover actions: open, pause, ⋯ holding Stop | 1f; I-C1f-33 | TaskRow.tsx | complete (conversation-task-panel-design.spec "hover shows open, pause and a more menu holding Stop…") | — |
| CV-109 | Finished fold "Finished 4" | 1c; I-C1c-38, C-TREE-11 | TaskPanel.tsx | complete (conversation-live-polish.spec "with every task finished…" and "with live work the Finished fold stays closed") | — |
| CV-110 | Panel list masks 12/28 only on overflow; header and strip fixed | 1f; I-C1f-7 | task-panel.css, tasks/useScrollEdges.ts | complete (conversation-task-panel-design.spec "the list masks an edge only while…") | — |
| CV-111 | Panel open/close: no transition (column); narrow sheet slides and fades | Q12 | ConversationView.tsx useTaskPanel (planSplit 1100) | partial (sheet tested: conversation-task-panel-design.spec "a narrow pane opens the panel as a sheet…"; slide/fade untested) | t-d5-cv-test-tasks |
| CV-112 | Sheets lock the scroll behind them | 1f; I-C1f-27 | task-panel backdrop | partial (unverified) | t-d5-cv-test-tasks |
| CV-113 | Expanded tasks: header "$0.71 spent · 94 steps", minimize-2, back | 1d; I-C1d-2..4 | ExpandedTasks.tsx, tasks/TasksTable.tsx | complete (conversation-tasks-expanded.spec "expand opens the table and Back leaves it"; conversation-task-panel-design.spec "the expanded view can be left from its right-hand collapse control…") | — |
| CV-114 | Filters All/Needs you/Running/Done with counts | 1d; I-C1d-5..8 | TasksTable.tsx | complete (conversation-tasks-expanded.spec "filter tabs show their counts and narrow the rows") | — |
| CV-115 | Search in expanded table | 1d; I-C1d-9 | TasksTable.tsx Search | partial (no test) | t-d5-cv-test-tasks |
| CV-116 | Expanded rows: now-line, model, steps, cost (Q15), status, age; countdown row; Waits on; 48px bottom mask | 1d; I-C1d-11..21, I-C1f-8 | TasksTable.tsx, tasksTableModel.ts | partial (node tasksTableModel.test; countdown row and mask untested) | t-d5-cv-test-tasks |
| CV-117 | Detail aside: status, inline question card, facts grid, instructions, Open task | 1d; I-C1d-22..27 | detail/TaskDetailPane.tsx | complete (conversation-tasks-expanded.spec "selecting a row fills the detail pane…", "Open task goes to the task view") | — |
| CV-118 | Escape leaves expanded; reload keeps route; 400px | 1d | ExpandedTasks.tsx | complete (conversation-tasks-expanded.spec "Escape leaves the expanded view", "reload keeps the expanded route…", "400px dark…") | — |
| CV-119 | Task view: title, status line "Running step 7 · 2m 14s · DS Flash · $0.06" | 1c; I-C1c-6, I-C1c-9, C-TREE-14 | tasks/TaskHead.tsx | partial (no test of the line; title size token 22 vs 20) | t-d5-cv-test-tasks |
| CV-120 | Task view Pause + ⋯ overflow (Stop) | 1c; I-C1c-7/8 | TaskHead.tsx | partial (pause through the panel row tested in conversation-tasks.spec "a running task row pauses…"; the head controls are untested) | t-d5-cv-test-tasks |
| CV-121 | Instructions card 3-line clamp + Edit (amend) | 1c; I-C1c-10/11, C-TREE-15 | tasks/InstructionsCard.tsx | complete (conversation-tasks.spec "the Instructions card shows the task, not the engine brief…") | — |
| CV-122 | Work log "Work log · 7 commands · 1 refused", live mono shimmer row | 1c; I-C1c-12..16, C-TREE-16 | tasks/WorkLog.tsx, LogStep.tsx, task-log.css | partial (no test) | t-d5-cv-test-tasks |
| CV-123 | Note to task bubble + "Read at step 5" receipt | 1c; I-C1c-17, C-TREE-17 | tasks/TaskNotes.tsx, logLines.ts ("Delivered at its next step"/"Read") | complete (conversation-tasks.spec "a note sent from the task view reaches the task…") | — |
| CV-124 | "Read at step N" exact wording | Q15 | logLines.ts | n/a-decided (Q15: drawn only when the engine sends it) | — |
| CV-125 | Note from worker ("Note from worker" eyebrow, 85%) | Comp §4.4; C-TREE-18 | none ("Note from" absent) | missing | t-d5-cv-task-worker-note |
| CV-126 | Waits on + dependency | Comp §4.4; C-TREE-19 | TaskView.tsx, tasks/TaskLinks.tsx | partial (no test) | t-d5-cv-test-tasks |
| CV-127 | Task composer "Note to this task"; refused note keeps draft; ended task cannot take notes | 1c; I-C1c-18, C-TREE-20 | tasks/TaskComposer.tsx | complete (conversation-tasks.spec "a note the engine refuses…", "the task view follows the panel…") | — |
| CV-128 | Task panel button inside task view | Q11 | — | n/a-decided (Q11: hidden) | — |
| CV-129 | Task cost per task when engine sends | Q15 | tasksTableModel.costText | complete (conversation-tasks-expanded.spec "selecting a row fills the detail pane; facts … appear only when present") | — |
| **Decision tray / questions** ||||||
| CV-140 | Tray card (surface r18 sh-2, max 640) with source line + pager "2 of 5" | 1a; I-C1a-18..20, C-TRAY-1 | tray/DecisionTray.tsx, TrayHeader.tsx | complete (conversation-questions.spec "several questions page through one card…") | — |
| CV-141 | Pager ← → keys while the tray is focused | Int Composer; I-ICO-14, I-IKY-30 | none (no keydown in TrayHeader) | missing | t-d5-cv-tray-keys |
| CV-142 | Pager arrow disabled at the end = 40% opacity | 1e/1f; I-C1e-18, I-C1f-38 | TrayHeader.tsx (disabled Button) | partial (no test) | t-d5-cv-test-tray |
| CV-143 | Permission single: "Allow once" ↵ primary / "Deny" / "Always allow…"; no pager | 1e; I-C1e-2..7, C-TRAY-4 | tray/AnswerForms.tsx, QuestionCardV2.tsx | partial (fills tested in conversation-question-states.spec "tray answers change fill…"; ↵ and Always untested) | t-d5-cv-test-tray |
| CV-144 | "and say why" toggles the "Say why (optional)" field | Q16; C-EDGE-9 | QuestionCardV2.tsx | complete (conversation-question-states.spec "keyboard focus draws the one shared ring…" opens Say why) | — |
| CV-145 | Choice with suggested pick + countdown "Picks Suggested in 12s" + Hold + "Choose" ↵ | 1e; I-C1e-8..15, C-TRAY-6/7 | tray/ChoiceForm.tsx, clock.ts | complete (conversation-questions.spec "a question with a deadline counts down and Hold stops its clock") | — |
| CV-146 | Typing in the card holds the countdown | 1e; I-C1e-16, I-ICO-17 | QuestionCardV2.tsx stopClock | partial (no test) | t-d5-cv-test-tray |
| CV-147 | Held clock wording | Comp C-TRAY-7 (impl "On hold — take your time") | ChoiceForm.tsx | partial (impl wording not in design; ASSUME keep) | t-d5-cv-test-tray |
| CV-148 | Irreversible: no clock, no Always; "Keep it" (quiet first) / "Remove" (danger-soft) / "Tell it…"; no reason field | 1e; I-C1e-17..20, Q17, C-TRAY-12 | AnswerForms.tsx, answers.ts | partial (node tray.test.ts only) | t-d5-cv-test-tray |
| CV-149 | Clarification: field takes focus, ⌘↵ sends, "Later" folds to header count, "You decide" | 1e; I-C1e-21..27, Q18 | tray/ClarifyForm.tsx, CardFooter.tsx | partial (no test of ⌘↵/Later/You decide; ink-2 per Q18 unverified) | t-d5-cv-test-tray |
| CV-150 | Batch: "Allow 3 git actions?" one card; Allow all / Deny all / "One by one" → pager inside card | 1a/1e; I-C1e-28..30, C-TRAY-8 | tray/BatchCard.tsx | partial (batch-is-one-card tested in conversation-questions.spec; One by one untested) | t-d5-cv-test-tray |
| CV-151 | "Doesn't block this reply" note / "The reply is waiting on this" | 1a/1e; I-C1a-26, I-C1e-3 | NonBlockingNote.tsx, CardFooter.tsx | complete (conversation-questions.spec "…the composer blocks only on a blocking question") | — |
| CV-152 | "Holding up <tasks>" one muted line in the tray foot | Q20; C-EDGE-10 | CardFooter.tsx | partial (no test) | t-d5-cv-test-tray |
| CV-153 | Tray body capped at 40% height, 16px bottom mask, source + actions pinned | 1f; I-C1f-9 | tray-shell.css (`tray-body-max-height` 40vh) | partial (no test) | t-d5-cv-test-tray |
| CV-154 | Compact tray 40px when >1 viewport from bottom, 200ms; "Review" / returning to bottom re-expands | 1b/1f; I-C1b-15/16, I-C1f-21/22, I-ICO-18 | tray/TrayCompact.tsx | complete (conversation-question-states.spec "the tray shrinks to its 40px line only when…") | — |
| CV-155 | Receipts: pending / answered "you · 14:02" / auto-picked "picked by codeaf after 30s" / withdrawn | 1e; I-C1e-31..35, C-TRAY-9..11 | ReceiptLine.tsx, model/receipts.ts | partial (withdrawn: conversation-records.spec; answered: flow-design; the "picked by codeaf" wording is absent from source) | t-d5-cv-receipt-autopick, t-d5-cv-test-tray |
| CV-156 | Click receipt → opens that question in the tray | Int; I-ICV-36 | ConversationView.tsx focusQuestion | partial (no test) | t-d5-cv-test-tray |
| CV-157 | Tray rising spring 380 | Comp; C-TRAY-13 | tray-shell.css | partial (unverified) | t-d5-cv-test-tray |
| CV-158 | Tray mouse click no ring; keyboard ring 2px + 4px halo | D6; 1f | tray css | complete (conversation-question-states.spec "inline tray fields show no ring on a mouse click", "keyboard focus draws the one shared ring…") | — |
| CV-159 | Tray at 320px | ASSUME (design silent): full width, actions wrap | tray-shell.css | complete (conversation-questions.spec "320px …: no horizontal overflow, composer reachable, accessible") | — |
| CV-160 | Unfocused split pane needs-you mini-tray (38px, "Review") | Comp split; C-SPLIT-4 | none | missing | t-d5-cv-pane-mini-tray |
| **Queue** ||||||
| CV-170 | Queued row rest: clock-3 + text, 30px r10 | 1a/1e; I-C1a-27, I-C1e-37, C-COMP-4 | blocks/QueuedRows.tsx | complete (queued-rows.spec "queued rows show two…") | — |
| CV-171 | Hover: fill, grip replaces clock, pencil + x | 1e; I-C1e-38, I-C1a-28 | QueuedRows.tsx, queued.css | partial (no assertion on grip or fill) | t-d5-cv-test-queue |
| CV-172 | Editing: 34px field with ring, "Esc" hint, "Save" | 1e; I-C1e-39/40 | blocks/QueuedEdit.tsx | complete (queued-edit-reorder.spec "a queued row edits in the engine: Esc keeps it, Save sends the change") | — |
| CV-173 | Click a queued row → edit inline | Int; I-ICO-19 | none (pencil only) | missing | t-d5-cv-queue-menu |
| CV-174 | Right-click queued row: Edit · Move up/down · Send now · Remove | Int; I-ICO-20..23 | none | missing | t-d5-cv-queue-send-now-engine, t-d5-cv-queue-send-now-client, t-d5-cv-queue-menu |
| CV-175 | Drag reorders; drop takes the target row's place; no drop line; only visible rows accept | Q27; I-ICO-24 | QueuedRows.tsx | complete (queued-edit-reorder.spec "dragging a row to another place reorders the engine queue") | — |
| CV-176 | ⌥↑/⌥↓ moves focused row, keeps focus, "Moved to position N of M" | Q28; I-ICO-25 | QueuedRows.tsx nudge | complete (queued-edit-reorder.spec "Alt+Up and Alt+Down move the focused row and keep focus on it") | — |
| CV-177 | Overflow "N more queued" (two rows visible) | 1e; I-C1e-41 | QueuedRows.tsx | complete (queued-edit-reorder.spec "queue order comes from the engine: two rows, then "N more queued"") | — |
| CV-178 | Remove really removes from the engine | Q25 | useConversation.removeQueue | complete (conversation.spec "a queued message waits above the composer… removing it takes it back from the engine") | — |
| CV-179 | Change refused (409): row leaves, muted "that message has already been sent", no toast | Q26; C-EDGE-11, C-COMP-8 | QueuedEdit/useConversation | complete (queued-edit-reorder.spec "a message whose turn has started refuses the change, says so and drops the row") | — |
| CV-180 | Editing a queued message with a pasted-text card edits the whole stored text | Q29 | QueuedEdit.tsx | partial (no test) | t-d5-cv-test-queue |
| CV-181 | "2 queued" chip in the running composer when queue rows are collapsed (1b) | 1b; I-C1b-18/19 | none | missing (ASSUME: shows while the dock is compact, i.e. scrolled away; click returns to the bottom and expands the rows) | t-d5-cv-queue-chip |
| CV-182 | Queue at narrow width / touch | ASSUME: actions always visible on touch (row-actions rule); drag disabled on coarse pointers, menu keeps Move up/down | queued.css | missing | t-d5-cv-queue-menu, t-d5-cv-test-queue |
| **Composer** ||||||
| CV-190 | Idle: "Ask codeaf" placeholder, "DS Flash" chip, inactive send (field-2) | 1e; I-C1e-43, C-COMP-1 | Composer.tsx | complete (conversation.spec "a fresh tab is quiet: centred composer, no engine call, Send disabled when blank") | — |
| CV-191 | Ready: send turns accent | 1e; I-C1e-44 | Composer.tsx | complete (composer-constant.spec "the composer looks identical at rest, hover, click and typing") | — |
| CV-192 | Running empty → Stop (square); with text → Steer pill + hint "Queue ⌥↵" | 1e; I-C1e-45/46, I-ICO-5..7 | Composer.tsx | complete (conversation.spec "while running: Stop when empty, Steer with text, Queue beside it") | — |
| CV-193 | ⌥↵ queue instead | 1e; I-C1e-47, I-ICO-9 | Composer.tsx onKeyDown | complete (conversation.spec "while running…") | — |
| CV-194 | Right-click Send/Stop/Steer → "Queue instead (⌥↵)" | Int; I-ICO-8 | none | missing | t-d5-cv-menu-composer |
| CV-195 | Enter sends, Shift+Enter newline, IME never sends | Comp §7 | Composer.tsx | complete (conversation.spec "Enter sends, Shift+Enter adds a line, composition never sends") | — |
| CV-196 | Focus: no ring on click/typing; 2px ring + 4px halo on keyboard only | D6; I-ICO-1 | Composer.tsx useKeyboardFocus | complete (composer-constant.spec "keyboard focus draws the ring on the field alone") | — |
| CV-197 | Right-click composer → "Paste as plain text" | Int; I-ICO-2 | none | missing | t-d5-cv-menu-composer |
| CV-198 | Drop files/images to attach; chips; limits; refused send keeps them | Int; I-ICO-3, C-COMP-2 | composer/useFileDrop.ts, AttachmentTray.tsx | complete (composer-attachments.spec "picker, paste and drop add chips…", "a refused send keeps the attachments") | — |
| CV-199 | Paste >12 lines → card (x removes), sent as tagged text | 1e; I-C1e-52/53, I-ICO-4 | composer/PasteCard.tsx, pastedText.ts | complete (composer-paste.spec "a paste over 12 lines becomes a removable card…") | — |
| CV-200 | Paste card geometry 260w, 32h preview, mask from 20% | Comp; C-INPUT-1 | paste-card.css (240/30/25%) | partial | t-d5-tok-cv-geometry |
| CV-201 | Typed text grows to 8 lines then scrolls under a 28px top fade | 1e/1f; I-C1e-55, I-C1f-12 | Composer.tsx useAutosize | complete (composer-paste.spec "typed text caps at 8 lines and scrolls") | — |
| CV-202 | Composer max width 680 | Comp §7; C-COMP-1 | tokens composer-max-width 608 | partial | t-d5-tok-cv-geometry |
| CV-203 | ↑ in empty composer recalls the last message; Esc blurs | impl extra | Composer.tsx | partial (no test; design silent: keep) | t-d5-cv-test-composer |
| CV-204 | Blocked by a blocking question: "The reply is waiting on your answer above" | 1e | ConversationDock.tsx BLOCKED | complete (conversation-questions.spec "…the composer blocks only on a blocking question") | — |
| CV-205 | Compact composer in an unfocused split pane (36px r18, "Reply to <title>") | Comp split; C-SPLIT-5, C-COMP-7 | lane s1-split composer/CompactComposer.tsx | in-flight:t-s1-split | — |
| CV-206 | Empty start: centred composer, "What are we building?", "Ask codeaf, or type @ to reference a file", no suggestions | 1e; I-C1e-62..64 | EmptyStart.tsx | complete (composer-constant.spec "the start screen asks what we are building and hints at @ references") | — |
| CV-207 | Empty start in the place's tint | 1e; I-C1e-62 | none (tint is a places concern) | missing (follows the place tint once places land) | t-d5-cv-empty-tint |
| CV-208 | @ file reference picker: typing @ opens matches from the engine | 1e; I-C1e-65 | none (findEngineFiles unused) | missing | t-d5-cv-at-query, t-d5-cv-at-picker |
| CV-209 | @ picker keyboard: ↑/↓ move, ↵/Tab insert, Esc closes, picker closes on scroll | ASSUME (picker not drawn): menu rules 28px rows, field-2 hover; ↵ inserts a file chip token `@path` | none | missing | t-d5-cv-at-picker |
| CV-210 | @ picker empty/no matches/engine unreachable | ASSUME: nothing renders when no matches (emptiness law); errors silent | none | missing | t-d5-cv-at-picker |
| CV-211 | @ picker narrow (≤600) and touch | ASSUME: full composer width, opens above the composer, rows 36px on coarse pointers | none | missing | t-d5-cv-at-picker |
| **Model chip and picker** ||||||
| CV-220 | Model chip "DS Flash ▾" 28px | 1e; I-C1e-43, C-COMP-3 | composer/ModelPicker.tsx | complete (composer-model.spec "the model chip says Flash and opens the quick-swap popover…") | — |
| CV-221 | Popover: pinned segmented, Effort (hidden without levels, Q9), list with shortcuts, "All models… ⌘/" | 1e; I-C1e-48..51, C-INPUT-2, Q9 | ModelPopover.tsx | complete (composer-model.spec "with routing and effort the popover shows pinned segments, Effort, shortcuts…") | — |
| CV-222 | Popover hover fill, sh-2, Esc returns focus, arrows, scroll closes | 1f; I-C1f-26 | ModelPopover.tsx | complete (composer-model.spec "hover is a fill only…", "arrows move between controls…") | — |
| CV-223 | ⌥⌘1–3 switch pinned models (order per D4) | 1e; I-ICO-11, I-IKY-17, D4, Q30 | lane s1-rail ModelPicker.tsx via useShortcuts | in-flight:t-s1-rail | — |
| CV-224 | ⌘/ opens all models | I-ICO-12, I-IKY-18 | lane s1-rail | in-flight:t-s1-rail | — |
| CV-225 | Segment label for a non-default pinned model = model-name tail | Q31 | modelOrder.ts | partial (node modelOrder.test only) | t-d5-cv-test-composer |
| CV-226 | Popover width 290 | Comp; C-INPUT-2 | model-popover-width 300 | partial | t-d5-tok-cv-geometry |
| CV-227 | A swap applies to the next message | 1e; I-C1e-48 | useConversationModel.ts | partial (no test that the next /turn carries the model) | t-d5-cv-test-composer |
| **Scroll and anchoring** ||||||
| CV-240 | Top fade: mask 40→96px, on once scrollTop>0, 120ms | 1f; I-C1f-5 | conversation-view.css | complete (conversation-scroll.spec "the top fade turns on only once content sits above the edge"; conversation-flow-states.spec "the top edge fades in over 120ms") | — |
| CV-241 | Bottom dock gradient; scroll padding = dock + 24 | 1f; I-C1f-6, I-C1a-17 | conversation-view.css | partial (no test) | t-d5-cv-test-scroll |
| CV-242 | Stick to bottom within 48px; streaming follows; scroll up stops following | 1f; I-C1f-15 | useStickToBottom.ts | complete (conversation-flow-states.spec "late growth after a follow … does not unanchor the reader") | — |
| CV-243 | Latest pill: shows only unanchored with live/new below; "Latest · Working 1m 12s" shimmer | 1b/1f; I-C1b-14, I-C1f-16/18, C-NAV-5 | LatestPill.tsx | complete (conversation-scroll.spec "the Latest pill appears only when unanchored…", "a running turn offers Latest with the live Working label…") | — |
| CV-244 | Latest pill enter 200ms fade + 4px rise; click smooth-scrolls and re-anchors; hides at bottom | 1f; I-C1f-17/19/20, I-ICV-35 | latest-pill.css, useStickToBottom.jump | partial (click/hide tested by the conversation-scroll.spec test; enter motion untested) | t-d5-cv-test-scroll |
| CV-245 | Opens at the bottom (anchored) on mount | CL; I-CL-5 | useStickToBottom.ts | complete (conversation-scroll.spec "the Latest pill appears only…") | — |
| CV-246 | Relaunch restores scroll position per conversation | Int Flows; I-IFL-7 | none (re-opens anchored at the bottom) | missing (ASSUME: restore the saved top only when the reader was unanchored; anchored panes reopen at the bottom) | t-d5-int-cv-view-state, t-d5-sh-scroll-restore |
| CV-247 | Running work re-attaches after relaunch | Int Flows; I-IFL-8 | useConversation attach; tabs-sessions | complete (tabs-sessions.spec "reload attaches each saved tab once") | — |
| **Notices, errors, offline** ||||||
| CV-260 | Provider failure: turn footer red glyph + "Retry" (draft kept) | Int Flows; I-IFL-21, C-NOTE-2 | blocks/TurnFooter.tsx, ErrorItem.tsx | complete (conversation.spec "a failed turn keeps the draft and offers Retry") | — |
| CV-261 | Stopped turn footer: circle-slash (ban per Q34) + "Stopped" | Comp §2.7; C-NOTE-1, Q34 | TurnFooter.tsx | partial (no test) | t-d5-cv-test-notices |
| CV-262 | Interrupted turn (engine restart/crash mid-turn) | Design silent (ambiguity 21) | model/steps.ts maps Interrupted → stopped | partial (ASSUME: reads as "Stopped", same as a person's Stop, per Q22's settled ink; untested) | t-d5-cv-test-notices |
| CV-263 | Retrying: accent dot + text + mono countdown | Comp §3.5; C-NOTE-3 | TurnFooter.tsx "Retrying" (no time), SystemNote time prop | partial (no countdown even when the engine sends a delay) | t-d5-cv-live-notices |
| CV-264 | Compacting / compacted while live | Comp C-NOTE-4 | model/live.ts (no compacting handler); settled divider exists | partial (ASSUME: the "Earlier messages summarized" divider appears when the compacted event arrives; nothing new during compacting) | t-d5-cv-live-notices |
| CV-265 | Engine offline: one muted line "Reconnecting to the engine…" under the header | Int Flows; I-IFL-3 | EngineNotice.tsx ("codeaf engine is not running" + Retry, in the dock) | partial (wrong wording and position) | t-d5-cv-offline-notice |
| CV-266 | Past 30s: "Can't reach the engine · Retry", nothing red | I-IFL-5/6 | EngineNotice.tsx | missing | t-d5-cv-offline-notice |
| CV-267 | Offline: composer stays editable; sends queue locally and go out on reconnect | I-IFL-4 | useConversation FailedSend keeps the draft only | missing | t-d5-cv-offline-queue |
| CV-268 | No modal error dialogs | I-IFL-24 | — | partial (law holds by inspection; no test) | t-d5-cv-test-notices |
| **Cross-cutting interaction law** ||||||
| CV-280 | Hover = fill only 120ms; press darkens 80ms; disabled 40% | 1f; I-C1f-28..31, I-C1f-38 | styles/ui.css | complete (theme-motion.spec "shared hover, press, selection and disabled states") | — |
| CV-281 | Tooltips: icon-only + truncated text, 500ms (instant when warm), sh-2 11px, leave on scroll | 1f; I-C1f-39, C-OVL-3 | components/ui/Tooltip.tsx | complete (theme-motion.spec themed keyboard focus test: `getByRole('tooltip',{name:'Copy'})`) | — |
| CV-282 | Copy feedback ("Copied" status + check icon) | Comp; — | components/ui/CopyButton.tsx | partial (no test) | t-d5-cv-test-messages |
| CV-283 | Screen readers hear status in words ("needs you", "running 12s"); colour never alone | Int Flows; I-IFL-29 | StatusMark labels, aria | partial (contracts helpers only) | t-d5-cv-test-notices |
| CV-284 | Every row keyboard-reachable (folded turns, steps, receipts, queue, task rows) | I-IFL-27 | various | partial (queue and task rows tested; steps and receipts not) | t-d5-cv-test-work |
| CV-285 | Light + Dark contrast of conversation surfaces | — | — | complete (conversation-contrast.spec, five specimens per scheme) | — |
| CV-286 | Narrow layouts 320/400/600/850 for transcript, dock, panel | CLAUDE.md responsive law; ambiguity 15 | conversation css, planSplit 1100 | partial (320 tray, 400 notice/expanded tested; 600/850 transcript untested) | t-d5-cv-test-scroll |
| CV-287 | Multiwindow: the same conversation open in two windows mirrors live | Int Flows; I-IFL-9 | one SSE per active conversation per window | missing (ASSUME: each window holds its own SSE for its active conversation; answers/queue edits converge through snapshots) | t-d5-cv-test-multiwindow |
| CV-288 | Native: ⌘ on macOS, Ctrl on Linux for every conversation key | — | design/keyboard.ts isMac | partial (Linux mapping untested for conversation keys) | t-d5-cv-test-composer |
| CV-289 | Notification click focuses that question in the tray | Int Flows; I-IFL-13 | focusKey path exists in ConversationView | missing (no native hook) | t-d5-cv-notify-focus |
| **Wire / data path** ||||||
| CV-300 | Renderer consumes incremental snapshots (`GET /sessions/{id}?since=`), merges the entries tail, fetches omitted tool outputs via `/tools/{callId}` | arch A3 | useConversation.ts readEngine full snapshot | missing | t-d5-prim-snapshot-merge, t-d5-cv-incremental-wire |
| CV-301 | Background conversation summaries from the world stream rather than polling full snapshots every 2s | arch (world stream) | useBackgroundSessions.ts (2s full poll) | missing | t-d5-prim-world-background |

#### Open questions

1. **Send now semantics** (I-ICO-22): no screen draws it. Assumption: if work is running, the message is steered into the running turn; otherwise it is submitted. The bridge does this atomically (t-d5-cv-queue-send-now-engine).
2. **"2 queued" chip** (1b, I-C1b-19): it has no drawn behaviour. Assumption: it replaces the queue rows while the dock is compact, and clicking it returns to the bottom, which expands the rows.
3. **Folded turn "Open in new tab"** (I-ICV-6): the target is not specified. Assumption: a new conversation tab on the same session file, scrolled to that turn.
4. **@ picker** (I-C1e-65): it is not drawn. Assumption: menu geometry (28px rows, field-2 hover), the list opens above the composer, ↵ or Tab inserts the relative path as literal text, no matches draws nothing, and it uses the existing `GET /files/find`.
5. **Interrupted turn** (ambiguity 21): it is not drawn. Assumption: it reads "Stopped", the same as a person's Stop (Q22 gives settled ink).
6. **Compacting while live**: the design draws only the settled divider. Assumption: the divider appears when the compacted event arrives, and nothing extra shows during compacting.
7. **Model chip label**: 1a says "Flash" and 1b/1c/1e say "DS Flash". The code and composer-model.spec use "Flash" for the default chip, and D4 names "DS Flash". Assumption: keep the current tested behaviour until the designer answers. The pinned order for ⌥⌘1–3 follows D4 and is in flight in t-s1-rail. Q30 (⌘1–3 against tab jumps) is still open; the lane moves the model keys to ⌥⌘, which would resolve the conflict.
8. **Batch secondary label**: "Deny" (1a/1d) or "Deny all" (1e). The code uses "Deny all" on the batch card. Keep it.
9. **Header counts click**: which surface opens? Assumption: the expanded Tasks view, pre-filtered (Running, or Needs you).
10. **Scroll restore on relaunch** (I-IFL-7): assumption is that only unanchored positions are saved and anchored panes reopen at the bottom.
11. **Offline sends with attachments**: assumption is that they are not held in the outbox. The draft and attachments stay, as with a refused send.
12. **Node unit tests**: the 29 `*.test.ts` files run under no npm script or CI. Rows backed only by them are marked partial. Wiring `node --test` into `npm run check` touches `package.json` (integrator-only) and belongs to the qa/int writer. It is not a cv task.
13. **Background polling**: CV-301 replaces the 2s full-snapshot poll in `useBackgroundSessions.ts` with world rows. If the shell/world writer also claims that file, t-d5-prim-world-background should be merged into theirs.
14. **Held countdown wording**: the code's "On hold — take your time" is not in the design. Assumption: keep it, since the design shows no held state.

#### Counts

| Status | Rows |
|---|---|
| complete | 73 |
| partial | 73 |
| missing | 34 |
| in-flight | 5 (t-s1-rail ×3, t-s1-files ×1, t-s1-split ×1) |
| n/a-decided | 4 (D1, Q11, Q15 ×2) |
| **total** | **189** |

### 7.5 Places

Scope: Places page 6a 6c 6d 6e 6f 8a–8g 9a–9e 10a; Interactions "Places, History" + place rows of "Shell", "Flows" and "Shortcuts"; place specimens in Components. Inventory IDs are from `~/.codex/codeaf-design-run/d5-inventory/design-places.md` (P-*). The audit (`places-audit.md`, P01–P83) was re-checked against HEAD 39d440c4f and against the lane worktrees.

Verified at HEAD:
- `desktop/src/features/places/` does not exist, and neither does `internal/placegraph/`.
- `data-tint` is set only in `components/specimens/ControlsSpecimen.tsx` (a specimen).
- `tokens.json` `tints.hues` has six h/a pairs, and `tokens.css:579-585` emits `[data-tint=…]`. `scripts/check-design.mjs:36` asserts only that the default tint exists.
- There are no swatch colour tokens.
- `tabKinds` (`tabs/kinds/types.ts`) has no `home` kind.
- HEAD `Rail.tsx` is the pre-v3 page nav. The rail lane (t-s1-rail) restyles it with `items[]`, adds ⌘S/⌘B collapse, and peeks from the TOP 8px (`useShellFrame.ts`). DESIGN-QUESTIONS R3 in the lane says ⌘0/⌘P/⌘⇧P/⌃1–9 are unbound "until places exist".
- `Menu.tsx`: HEAD has action/separator. The menus lane adds `submenu` but has no swatch row.
- `ConversationBar.tsx` has only the running/need-you counts.
- `DESIGN-QUESTIONS.md` at HEAD has no places decision, so no row is n/a-decided.

Status key: complete, partial, missing, in-flight:<lane>, n/a-decided.

#### A. Model and rules (6d, 6e, 8g)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| PL-001 | Place record {id,name,parents[],context,policy,manager?}: a graph with many parents, cycles blocked, manager reserved | 6e; P-6e-1,3,5 | none (`session.Place` is an unrelated folder layout) | missing | t-d5-be-pg-store; t-d5-pl-selectors |
| PL-002 | Membership {chatId,placeId,addedBy you/ai,at}: a chat is unplaced, in one place or in several | 6e; P-6e-2,4; P-0-2 | none (`Meta.Places` = referred folders) | missing | t-d5-be-pg-membership; t-d5-pl-selectors |
| PL-003 | Renderer place data: snapshot plus live `places` records from the world stream | arch | none | missing | t-d5-prim-places-client; t-d5-pl-store |
| PL-004 | Context = union over all places plus ancestors ≤2 levels, membership-only (not strip) | 6e; P-6e-6 | `internal/session/placescontext.go` (folders only) | missing | t-d5-be-pg-resolve; t-d5-be-pg-context-inject |
| PL-005 | Conflicts: nearest common ancestor decides; with no common ancestor, ask once and remember | 6e; P-6e-7,8 | none | missing | t-d5-be-pg-resolve; t-d5-pl-test-using |
| PL-006 | Sources and memory add up and never conflict | 6e; P-6e-9 | none | missing | t-d5-be-pg-resolve |
| PL-007 | Memory fills itself: decisions are promoted into the place and each one can be removed | 6e; P-6e-14 | engine memory is per workspace | missing | t-d5-pl-decisions (ASSUME: not built in d5; Home shows a memory list only if the engine sends one) |
| PL-008 | Per-chat source budget; show what was trimmed | 6e; P-6e-21 | none | missing | t-d5-be-pg-resolve; t-d5-pl-using-popover |
| PL-009 | Adding a place mid-run applies from the next turn | 6e; P-6e-22 | none | missing | t-d5-be-pg-context-inject |
| PL-010 | Teams and sharing out of scope | 6e; P-6e-23 | — | n/a-decided (design itself scopes it out, 6e "Teams and sharing are out of scope for now") | — |
| PL-011 | Scale: 20–200 places, depth 2–3, 50 children under one parent | 6e; P-6e-24; P-X-9 | none | missing | t-d5-pl-test-fixture; t-d5-pl-test-scale |
| PL-012 | Places are optional; quick chats stay unplaced (Now, then History) | 6e; P-6e-15 | none | missing | t-d5-pl-root-home; t-d5-pl-rail-sections |
| PL-013 | Relaunch: windows, places, tabs and scroll restore; running work re-attaches | IX Flows; P-IX-28 | single localStorage key `codeaf.desktop.workspace.v1` (`tabs/model.ts`) | partial | t-d5-prim-workspace-sync; t-d5-pl-window-place |

#### B. Tint (9d, 6e, P-0-3..5)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-020 | Six tint hue tokens (h/a) for tide/iris/rose/sand/sage/graphite | 9d; P-0-4; P-CMP-14 | `design/tokens.json` `tints.hues`; `styles/tokens.css:579-585` | partial (exists; only the default is asserted by `check-design.mjs`, values are not) | t-d5-int-tok-a-colour |
| PL-021 | Swatch colours per tint (lightness differs per tint), Light and Dark | 9d; P-0-5 | none | missing | t-d5-int-tok-a-colour; t-d5-prim-swatch |
| PL-022 | Tint swatch component: 10/12/14/16px, r = .3·size, optional status glyph | P-0-6; P-CMP-2 | none | missing | t-d5-prim-swatch |
| PL-023 | Window frame takes the place tint (`data-tint` on the shell); Go to swaps it; Now and the root are graphite | 6a, 9a, 9d; P-0-3; P-6d-4 | `data-tint` not set in the app shell | missing | t-d5-pl-window-tint; t-d5-int-pl-app-mount |
| PL-024 | Native: macOS overlay titlebar/vibrancy and Linux decorated window under a tint. ASSUME: CSS frame tint only; OS title unchanged | P-X-15 | `src-tauri/tauri.macos.conf.json` | missing | t-d5-pl-window-tint; t-d5-pl-test-window |
| PL-025 | Tint marks a family: top-level picks, children inherit, a child override is followed by its own children | 9d; P-9d-5; P-6e-25 | none | missing | t-d5-pl-selectors |
| PL-026 | Several parents: keep the tint of the parent it was created in; never blend | 9d; P-9d-6 | none | missing | t-d5-pl-selectors |
| PL-027 | New top-level place gets an unused colour. ASSUME: deterministic least-used tint, no model call | 9d; P-9d-7 | none | missing | t-d5-pl-selectors |
| PL-028 | Tint palette specimen (mini frames, "Graphite · Now", inheritance example) | 9d; P-9d-2,3 | none | missing | t-d5-pl-specimens |
| PL-029 | Graphite is not offered in the 5-swatch pickers (create, menu). ASSUME: graphite = "no tint chosen", reachable only by having no tint | 8f; P-8f-4 | none | missing | t-d5-pl-inline-create; t-d5-int-pl-menu-swatches |

#### C. Rail (6a, 10a, IX Shell rows, CMP rail rows)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-040 | Rail frame 232px: lights + toggle, Inbox, Now, Pinned, Open, All places at the bottom | 6a; P-0-7; P-SH-2 | HEAD `features/shell/Rail.tsx` (page nav); lane t-s1-rail restyles it with `items[]` | partial | t-d5-int-pl-rail-slot; t-d5-pl-rail-sections |
| PL-041 | Rail toggle ⌘S (⌘B kept) and the collapse animation | 9c; P-IX-41 | t-s1-rail: `shell/RailToggle.tsx`, `useShellFrame.ts`, lane DESIGN-QUESTIONS R2 | in-flight:t-s1-rail | — |
| PL-042 | Rail row anatomy: 32px, gap 10, r8, 13px, lead, name, muted " · parent", dot, 11px meta | P-0-6; P-CMP-2; P-6a-9 | lane rows are 30px/12px tab-shaped (D-02) | missing | t-d5-tok-pl-rail; t-d5-pl-rail-row |
| PL-043 | Row hover: fill only; Open rows show ×; Pinned rows show none | 10a; P-10a-3; P-X-1 | none | missing | t-d5-pl-rail-row |
| PL-044 | Row selected: `--tab` lift + sh-1 + ink + 500 weight; `aria-current` | P-0-6; P-X-4 | none | missing | t-d5-pl-rail-row |
| PL-045 | Row focus-visible ring; ↑/↓ roving focus through the rail list; Enter = Go to | IX Accessibility; P-IX-34; P-X-3 | none | missing | t-d5-pl-rail-sections; t-d5-pl-test-rail |
| PL-046 | Row disabled. ASSUME: no disabled place row; archived places never appear in the rail | — | none | missing | t-d5-pl-rail-sections |
| PL-047 | Inbox row: inbox icon, amber dot when anything anywhere needs you, hover shows count | 6a; P-6a-3; P-IX-3 | lane t-s1-menus builds a local-tab Inbox pane only | missing | t-d5-pl-rail-sections |
| PL-048 | Inbox row click opens Inbox in the current place's strip at the oldest item; ⌘-click/middle-click opens a new tab | P-IX-1,2 | t-s1-menus `kinds/inbox/InboxPane.tsx` (local) | in-flight:t-s1-menus (pane) / missing (row) | t-d5-pl-rail-sections |
| PL-049 | Now row: circle-dashed, count of unplaced open tabs; click switches the window to Now (graphite, no Home); ⌘-click opens a new window on Now | 6a; P-6a-4; P-IX-4,5 | none | missing | t-d5-pl-rail-sections; t-d5-pl-navigation |
| PL-050 | Pinned section: user order, never auto-closes, a pinned parent does not pull in its children | 10a; P-10a-10,15 | none | missing | t-d5-pl-selectors; t-d5-pl-rail-sections |
| PL-051 | Open section: places you've gone to, newest at the top, flat, parent muted | 10a; P-10a-9,10; P-6a-8 | none | missing | t-d5-pl-selectors; t-d5-pl-rail-sections |
| PL-052 | Open header "Close all" (right side) | 10a; P-10a-4; P-CMP-15 | none | missing | t-d5-pl-rail-sections |
| PL-053 | All places row: folder-tree, "⌘⇧P" meta; click opens the root Home as a tab; ⌘-click opens a new tab | 6a; P-6a-10; P-IX-11 | none | missing | t-d5-pl-rail-sections; t-d5-int-pl-home-kind |
| PL-054 | Two marks per row: tint square plus a dot only for amber needs-you or red failed; running never shows | 10a; P-10a-16; P-CMP-2; P-SH-3 | `StatusMark` exists (tabs) | missing | t-d5-pl-place-dot; t-d5-pl-rail-row |
| PL-055 | A dot on a parent rolls up from its children | 10a; P-10a-17 | none | missing | t-d5-be-pg-status-rollup; t-d5-pl-selectors |
| PL-056 | Status dot hover names it ("2 need you in Config parser"); click = row; screen-reader words | 10a; P-10a-18; P-IX-10 | none | missing | t-d5-pl-place-dot |
| PL-057 | Failed (red) dot on a place (described, not drawn) | P-X-11 | none | missing | t-d5-pl-place-dot; t-d5-pl-test-rail |
| PL-058 | Close: hover ×; tooltip "Close Marketing · 4 tabs ⌘⇧W"; closes its tabs in this window and leaves the rail; ⌘P / All places restores the tabs | 10a; P-10a-6,11 | none | missing | t-d5-pl-rail-row; t-d5-pl-navigation |
| PL-059 | ⌘⇧W closes the current place | 10a; P-10a-11 | none | missing | t-d5-pl-keys |
| PL-060 | Closed but running: row stays in Open, muted, "closed · still running" with its glyph, and leaves by itself when the work finishes or you answer | 10a; P-10a-5,12; P-CMP-15 | t-s1-menus has the tab version (`closing/running.ts`) | missing | t-d5-pl-selectors; t-d5-pl-rail-row |
| PL-061 | Going idle: an Open place untouched for 12h closes itself; Pinned never auto-close | 10a; P-10a-13; P-6d-7 | none | missing | t-d5-pl-idle-close |
| PL-062 | Pinned places' idle tabs still archive (toast "Archived 6 tabs idle for more than 12h · Review · Restore all") | 10a; P-10a-13; P-CMP-23 | t-s1-history auto-archive | in-flight:t-s1-history | t-d5-pl-idle-close (place side only) |
| PL-063 | Empty rail: Inbox, Now, "Places you open show here. Pin the ones you live in.", All places | 10a; P-10a-8 | none | missing | t-d5-pl-rail-sections |
| PL-064 | 0 places: Inbox and Now only, no empty "Places" header, All places without ⌘⇧P meta | 6d; P-6d-1; 8e P-8e-2 | none | missing | t-d5-pl-rail-sections |
| PL-065 | Drag to pin: lifted ghost (sh-2), 2px accent insertion line, drop across the section line pins or unpins | 10a; P-10a-7,14; P-X-6 | `tabs/hosts/dragHost.ts` is tab-only | missing | t-d5-pl-rail-dnd |
| PL-066 | Drag within Pinned reorders; keyboard equivalent (ASSUME: Alt+↑/↓ like Q28, plus menu Pin/Unpin) | 10a; P-10a-14 | none | missing | t-d5-pl-rail-dnd |
| PL-067 | Drop a file, link or tab onto a rail row adds it to that place; drop-target highlight (ASSUME: the tile "Add here" recipe on the row) | P-IX-9; P-6e-12; P-X-5 | none | missing | t-d5-pl-filing; t-d5-pl-rail-dnd |
| PL-068 | Rail row right-click: Go to · Quick Look · Pin/Unpin · Rename · Tint · Close · Close all others | P-IX-8 | t-s1-menus Menu primitive | missing | t-d5-pl-rail-menu |
| PL-069 | Rail row ⌘-click/middle-click opens the place in a new window | P-IX-7 | none | missing | t-d5-pl-rail-sections; t-d5-pl-navigation |
| PL-070 | Pinned rows take ⌃1–9 in order (9c also numbers Open rows) | 10a; P-10a-14; P-9c-5; P-IX-37 | none (lane R3: unbound) | missing | t-d5-pl-keys; t-d5-pl-decisions |
| PL-071 | Rail stays at ~5–10 rows with 200 places | 6d; P-6d-6 | none | missing | t-d5-pl-test-scale |
| PL-072 | Narrow ≤600: the rail with place sections lives in the existing drawer `<dialog>`; ≤850 compact widths (190/154) | P-X-10 | `App.tsx` drawer; `responsive.spec.ts` "narrow navigation traps focus…" | partial | t-d5-pl-rail-sections; t-d5-pl-test-window |
| PL-073 | Touch/mobile (pointer:coarse): ASSUME no hover × on touch; close via long-press menu; rows ≥32px | — | none | missing | t-d5-pl-rail-row |
| PL-074 | Rail specimen states (typical day, hover close, closed running, dragging, empty) | 10a; P-CMP-15 | none | missing | t-d5-pl-specimens |

#### D. Collapsed rail, switcher, peek (9c, 9e)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-080 | Collapsed: lights + toggle move into the strip | 9c; P-9c-2; P-9e-5 | t-s1-rail `RailToggle placement` | in-flight:t-s1-rail | — |
| PL-081 | Home tab gains chevrons-up-down and opens the place switcher | 9c; P-9c-2; P-CMP-12; P-IX-12 | none | missing | t-d5-pl-home-tab; t-d5-pl-switcher |
| PL-082 | Switcher popover 280px: Inbox (dot), Now ⌃0, Pinned ⌃1…, Open with parents and dots, All places ⌘⇧P; current row `--field-2`; ↑↓/↵/Esc | 9c; P-9c-3; P-CMP-13 | none | missing | t-d5-pl-switcher |
| PL-083 | ⌃0 = Now; ⌃1–9 jump between places | 9c; P-9c-4,5 | none | missing | t-d5-pl-keys |
| PL-084 | Edge peek: hovering the LEFT 8px for 300ms peeks the full rail (lane uses the TOP 8px) | 9e; P-9c-6; D-04 | t-s1-rail `useShellFrame.ts` (top 8px) | partial | t-d5-pl-rail-peek |
| PL-085 | Needs-you while collapsed: Home-tab swatch gets an amber glyph; the switcher names which place | 9e; P-9c-7; P-9e-6 | none | missing | t-d5-pl-home-tab; t-d5-pl-switcher |
| PL-086 | Linux: ⌃n would collide with ⌘n = Ctrl n. ASSUME: Linux uses Alt+0–9 for places | — | `design/keyboard.ts` maps ⌘→Ctrl | missing | t-d5-pl-keys; t-d5-pl-decisions |

#### E. Strip and Home tab (6a, 8a, 9b, IX, CMP)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-090 | Home tab: pinned first slot, swatch + place name, 30px, active = canvas + sh-1; then a 1×16 hairline | P-0-8,9; P-6a-11; P-SH-1 | `tabs/TabStrip.tsx` (pinned tabs, no Home) | missing | t-d5-pl-home-tab; t-d5-int-pl-home-kind |
| PL-091 | Home tab hover / focus-visible / pressed states | P-CMP-27 | none | missing | t-d5-pl-home-tab |
| PL-092 | Home never closes (no ×, ⌘W skips it); ⌘0 focuses it | P-IX-14; P-SH-1; P-IX-36 | none | missing | t-d5-int-pl-home-kind; t-d5-pl-keys |
| PL-093 | Home tab right-click = place menu | P-IX-13 | none | missing | t-d5-pl-home-tab; t-d5-pl-place-menu |
| PL-094 | Now has no Home | P-6e-26 | none | missing | t-d5-int-pl-workspace-key |
| PL-095 | Switching place swaps the strip to that place's saved tabs; the tabs you left are kept and running work keeps running | 6d; P-6d-4; 8g P-8g-2 | single `WorkspaceState` | missing | t-d5-int-pl-workspace-key; t-d5-pl-navigation |
| PL-096 | After Go to: tint, saved tabs, land on Home (9a: the Reading strip comes back with a web tab) | 9a; P-9a-1..3 | none | missing | t-d5-pl-navigation; t-d5-pl-test-window |
| PL-097 | All places is a closable tab in the place it was opened from; it stays behind after Go to; ⌘⇧P opens a fresh one | 9e; P-9e-2; P-8c-4 | none | missing | t-d5-int-pl-home-kind; t-d5-pl-keys |
| PL-098 | ⌘T in a place: the new chat belongs to the place; from Now it has no place until filed | 9e; P-9e-4; P-6d-3; P-IX-45 | t-s1-newtab ⌘T field | in-flight:t-s1-newtab (field) / missing (membership) | t-d5-int-pl-newtab-place |
| PL-099 | Same chat in two places: each strip has a live view, one run, one composer state; closing one view doesn't stop it | 6d; P-6d-8; P-6e-19 | `useBackgroundSessions` (two views untested) | partial | t-d5-pl-test-window |
| PL-100 | Hover preview shows "also open in Marketing" | 6e; P-6e-20; P-CMP-28 | t-s1-preview builds the preview card | in-flight:t-s1-preview (card) / missing (line) | t-d5-int-pl-preview-line |

#### F. Conversation header, Using chip and popover, membership note (6a, 6f, 6e, IX)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-110 | Header counts "4 running" / "5 need you" | 6a; P-6a-13; P-CMP-16 | `conversation/ConversationBar.tsx` | complete (`conversation-task-panel-design.spec.ts` "the header names what is running and what needs you") | — |
| PL-111 | Using chip closed: layers, "Using 3 places · 4 sources", chevron-down, h24 `--field` | 6f; P-6f-3; P-CMP-5 | none | missing | t-d5-pl-using-chip; t-d5-int-pl-conv-slots |
| PL-112 | Using chip single place: swatch + place name (9b "codeaf") | 9b; P-9b-3 | none | missing | t-d5-pl-using-chip |
| PL-113 | Using chip hidden when a chat has no place and no sources (emptiness law) | P-0-2 | none | missing | t-d5-pl-using-chip |
| PL-114 | Chip hover / focus / open (`--field-2` + chevron-up) / keyboard ↵ opens, Esc closes | 6f; P-6f-3 | none | missing | t-d5-pl-using-chip |
| PL-115 | Popover Places section: swatch chips, ancestor marked "· inherited" | 6f; P-6f-4 | none | missing | t-d5-pl-using-popover |
| PL-116 | Popover Instructions rows with their source place | 6f; P-6f-5 | none | missing | t-d5-pl-using-popover |
| PL-117 | Popover Sources rows (folder-git, file-text, URL) with their place | 6f; P-6f-6 | none | missing | t-d5-pl-using-popover |
| PL-118 | Popover Policy row with conflict outcome "Release wanted Flash · codeaf decided" | 6f; P-6f-7 | none | missing | t-d5-pl-using-popover; t-d5-be-pg-resolve |
| PL-119 | Trimmed-by-budget line in the popover. ASSUME: "N sources trimmed" muted row | 6e; P-6e-21 | none | missing | t-d5-pl-using-popover |
| PL-120 | "Add to a place…" files this chat into another place (picker) | 6f; P-6f-8 | none | missing | t-d5-pl-using-popover; t-d5-pl-place-picker |
| PL-121 | Drop files on the popover adds them to this chat only | P-IX-16 | none | missing | t-d5-pl-using-popover; t-d5-be-pg-sources |
| PL-122 | Transcript line "Now also using Release: brand-voice.md · Undo"; Undo reverts | 6f; P-6f-9,10; P-6e-11; P-CMP-6 | `conversation/SystemNote.tsx` (generic) | missing | t-d5-pl-membership-note; t-d5-int-pl-conv-slots |
| PL-123 | Using popover narrow ≤600: ASSUME a bottom sheet at full width | — | none | missing | t-d5-pl-using-popover |
| PL-124 | AI offers at most one existing place after the first reply | 6e; P-6e-16; P-6d-3 | none | missing | t-d5-pl-decisions |

#### G. Place Home page (8a, 8b, 8g, 9a, 9b, CMP)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-130 | Page anatomy: 680 column, breadcrumb, 28px title + 16px swatch + ⋯ menu, 11px section labels, bottom fade | 8a; P-8a-4,5; P-CMP-18,19 | none | missing | t-d5-tok-pl-page; t-d5-pl-home-page |
| PL-131 | Breadcrumb ("All places › codeaf › Marketing"): click Go to; ⌘-click new window; focus ring; `aria-current` | 8b; P-8b-5; P-IX-23 | `conversation/Breadcrumb.tsx` (task route only) | missing | t-d5-pl-breadcrumb |
| PL-132 | ⌘[ in the Home tab goes up a level | 8g; P-8g-7; P-IX-42 | none | missing | t-d5-pl-keys |
| PL-133 | "Since yesterday" recap; shown only with text. ASSUME: no model call in d5; render the engine-provided text or nothing | 8a, 9a; P-8a-6; P-9a-4; P-CMP-20 | none | missing | t-d5-pl-home-since; t-d5-pl-decisions |
| PL-134 | Attention rows rolled up from descendants: "title · in <place>", "needs you" / "running · 2m", amber first; click opens that chat | 8a; P-8a-7 | none | missing | t-d5-pl-home-attention |
| PL-135 | Places tiles section, 4-column grid, "New place" tile last | 8a; P-8a-8; P-CMP-3; P-8g-4 | none | missing | t-d5-pl-tile-grid |
| PL-136 | Tile rest: swatch, name, meta "28 chats" / "6 chats · also in Software" / "47 places · 212 chats" | P-8a-8,10; P-CMP-7 | none | missing | t-d5-pl-tile; t-d5-pl-selectors |
| PL-137 | Tile amber/red dot rolled up from descendants | P-8a-9 | none | missing | t-d5-pl-tile |
| PL-138 | Tile hover fill / focus-visible ring | P-X-3 | none | missing | t-d5-pl-tile |
| PL-139 | Tile selected: 2px accent ring (no halo) | P-CMP-8; 8c | none | missing | t-d5-pl-tile |
| PL-140 | Tile drop target: "Add here" | P-CMP-9; P-X-5 | none | missing | t-d5-pl-tile; t-d5-pl-tile-dnd |
| PL-141 | Tile click / ↵ Go to; ⌘-click / ⌘↵ new window; Space Quick Look; arrow keys move across the grid | P-IX-17,18,20; 8g P-8g-3 | none | missing | t-d5-pl-tile-grid |
| PL-142 | Tile right-click menu: Go to ↵ · Quick Look Space · Open in new window ⌘↵ · Rename · Tint ▸ swatches · Add to another place… · Pin to rail · Archive · Delete place… (red) | 8f; P-8f-5..16; P-IX-19; P-CMP-26 | t-s1-menus Menu primitive | missing | t-d5-pl-place-menu |
| PL-143 | Tile grid narrow: ASSUME auto-fit minmax(160px,1fr); 2 columns ≤600, 1 column ≤360 | — | none | missing | t-d5-pl-tile-grid |
| PL-144 | Chats section: rows 44px, title + digest + relative time; running dot replaces the icon; click opens or focuses its tab | 8a; P-8a-11..13 | t-s1-history `HistoryRow.tsx` (in flight) | missing | t-d5-pl-chat-list |
| PL-145 | Home composer: "Start something in codeaf"; model label; ↵ opens a new tab right after Home, focused | 8a, 9b; P-8a-14; P-9b-2,5; P-IX-22 | `conversation/Composer.tsx` (generic) | partial | t-d5-pl-home-composer |
| PL-146 | ⌘↵ in the Home composer opens it in the background and stays on Home | 9b; P-9b-6 | none | missing | t-d5-pl-home-composer |
| PL-147 | Home never turns into a chat; new chat auto-titled; header shows a single place chip | 9b, 9e; P-9e-3; P-9b-3,7 | auto-title exists in the engine | partial | t-d5-pl-home-composer; t-d5-pl-using-chip |
| PL-148 | Empty place: "Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know." | 8b; P-8b-7; P-CMP-22 | none | missing | t-d5-pl-empty-place |
| PL-149 | Empty place actions "Add files or links" (paperclip) and "Write instructions" (text) | 8b; P-8b-8,9 | none | missing | t-d5-pl-empty-place |
| PL-150 | Inherited-context line "Uses Marketing's context: brand-voice.md, codeaf.dev" | 8b; P-8b-10 | none | missing | t-d5-pl-empty-place |
| PL-151 | Empty-place composer "Start the first chat in Launch site" | 8b; P-8b-11 | none | missing | t-d5-pl-home-composer |
| PL-152 | New place joins the top of Open and the strip holds only Home | 8b; P-8b-3,4 | none | missing | t-d5-pl-navigation; t-d5-pl-test-home |
| PL-153 | Instructions = plain prose on Home; Notes "(plain for now)" with "Add a note, or drop a file or link…". ASSUME: a quiet section under Chats, hidden when empty, save on blur | 6e; P-6e-13; P-CMP-21 | none | missing | t-d5-pl-instructions |
| PL-154 | Sources list on Home with remove. ASSUME: a quiet section; not drawn | 6e; P-6e-12 | none | missing | t-d5-pl-home-sources |
| PL-155 | Rename. ASSUME: inline edit of the Home title, which the menu item focuses; ↵ saves, Esc reverts | 8f; P-8f-9 | tab rename dialog in `Workspace.tsx` | missing | t-d5-pl-rename |
| PL-156 | Home loading state. ASSUME: no skeleton; sections appear when data arrives; nothing invented | P-X-8 | none | missing | t-d5-pl-home-page |
| PL-157 | Home page narrow (≤600/850): column fills the width with 16px padding; composer stays docked | — | none | missing | t-d5-pl-home-page; t-d5-pl-test-home |
| PL-158 | 9a Inside Reading: Open gains Reading at the top; Q3 report leaves (MRU cap / closed) | 9a; P-9a-2 | none | missing | t-d5-pl-selectors; t-d5-pl-test-window |

#### H. All places root Home, first launch, inline create (8c, 8e, 8f)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-170 | All places = root Home; graphite; rail row selected | 8c; P-8c-1..3; P-8g-1 | none | missing | t-d5-pl-root-home |
| PL-171 | Header "All places", "Search places" field, "5 at the top level · 58 in all" | 8c; P-8c-5; P-8f-17 | none | missing | t-d5-pl-root-home |
| PL-172 | Top-level tiles "N places · M chats" | 8c; P-8c-6,7 | none | missing | t-d5-pl-root-home; t-d5-pl-selectors |
| PL-173 | "Not in any place · 38" unplaced list | 8c; P-8c-8,9; P-8g-8 | none | missing | t-d5-pl-root-home |
| PL-174 | AI cluster line "5 of these look like they belong in Reading · Move them" | 8c; P-8c-10,11 | none | missing | t-d5-pl-suggestions; t-d5-pl-decisions |
| PL-175 | Suggested place card "Launch week · 7 chats across Marketing and Release · Create / Not now" | P-CMP-4; P-IX-26 | none | missing | t-d5-pl-suggestions |
| PL-176 | "Not now" hides a suggestion for 30 days | P-IX-26 | none | missing | t-d5-pl-suggestions |
| PL-177 | First launch: graphite; one sentence "Places hold work that belongs together…"; tiles "Name a place" and "Open a folder or repo"; "Not in any place · 2" | 8e; P-8e-1..8 | none | missing | t-d5-pl-first-launch |
| PL-178 | Open a folder or repo → native picker → place named after the folder, with that folder as its source, tint auto | 6d; P-6d-2a; P-8e-7; P-IX-27 | none | missing | t-d5-nat-dialog; t-d5-be-pg-from-folder; t-d5-pl-first-launch |
| PL-179 | After the first place: "Now's matching tabs move into it with one click" | 6d; P-6d-2 | none | missing | t-d5-pl-first-launch |
| PL-180 | Inline create tile: 5 swatches + name field + "↵ create · Esc cancel" | 8f; P-8f-2,3; P-CMP-10; P-IX-21 | none | missing | t-d5-pl-inline-create |
| PL-181 | Inline create: empty name refused; focus returns to the New place tile after Esc | — | none | missing | t-d5-pl-inline-create |
| PL-182 | Search places field: filters tiles and lists. ASSUME: case-insensitive over name + ancestor path | 8c; P-8c-5 | none | missing | t-d5-pl-root-home |

#### I. ⌘P go-to palette (6c, 6d, IX)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-190 | ⌘P opens the go-to palette over the current place (scrim .18, 620×560) | 6c; P-6c-1,19; P-IX-38 | `components/CommandPalette.tsx` (⌘K pages) | missing | t-d5-pl-palette-view; t-d5-pl-keys |
| PL-191 | Search field "Go to a place, or create one" + count "58 places" | 6c; P-6c-4,15 | none | missing | t-d5-pl-palette-view |
| PL-192 | Recent section: swatch, name · parent, dot, relative time, recent first | 6c; P-6c-5,6; P-6c-20 | none | missing | t-d5-pl-palette-model |
| PL-193 | All section: tree with chevrons, "N inside", leaf "N chats", expand/collapse (←/→) | 6c; P-6c-7..14 | none | missing | t-d5-pl-palette-model; t-d5-pl-palette-view |
| PL-194 | ↵ open, ⌘↵ open in new window, ⌘N new place (footer hints) | 6c; P-6c-16..18 | none | missing | t-d5-pl-palette-view |
| PL-195 | Typing a name that matches nothing: "Create '<name>'" row (ASSUME), ↵ or ⌘N creates under the root | 6d; P-6d-2b | none | missing | t-d5-pl-palette-model |
| PL-196 | Row hover / selected (`--field`) / focus; ↑↓ roving; Esc closes and restores focus | 6c; P-IX-46 | none | missing | t-d5-pl-palette-view |
| PL-197 | Scale: 200 places, Reports with 47 children; virtualised list | 6c; P-6c-20; P-6d-6 | none | missing | t-d5-pl-palette-model; t-d5-pl-test-scale |
| PL-198 | Pick mode (Add to a place… / Add to another place… / History Add to place) | 6f; 8f; P-IX-24 | none | missing | t-d5-pl-place-picker |
| PL-199 | Palette narrow ≤600: full-width sheet (ASSUME, like the ⌘K palette) | — | `CommandPalette` narrow behaviour exists | missing | t-d5-pl-palette-view |
| PL-200 | Palette loading. ASSUME: shows the cached snapshot; no spinner | P-X-8 | none | missing | t-d5-pl-palette-view |

#### J. Quick Look (8d, 8g)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-210 | Space on a selected tile (and rail row, palette row via menu) opens a read-only Home sheet (520, sh-3, scrim .2) | 8d; P-8d-2; P-CMP-17; P-IX-43 | none | missing | t-d5-pl-quicklook |
| PL-211 | Header swatch + name + "Space to close"; recap; chats; "Places: Papers, Books, Talks" | 8d; P-8d-3..6 | none | missing | t-d5-pl-quicklook |
| PL-212 | "Go to Reading" (primary) / "Open in new window" | 8d; P-8d-7,8 | none | missing | t-d5-pl-quicklook |
| PL-213 | Space/Esc closes; focus is trapped and then restored; tabs and tint unchanged | 8d; P-8d-9; P-IX-46 | none | missing | t-d5-pl-quicklook |
| PL-214 | Quick Look narrow: ASSUME full-width sheet ≤600 | — | none | missing | t-d5-pl-quicklook |

#### K. Organizing: drag, menus, filing, archive/delete/undo (8f, 8g, 6d, 6e, IX)

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-220 | Drag a chat onto a tile or rail row adds it; ⌥ moves it | 8g; P-8g-6; P-IX-20 | none | missing | t-d5-pl-dnd-model; t-d5-pl-tile-dnd |
| PL-221 | Drag a place onto a tile: 8g says add, ⌥ move; IX says nest, ⌥ second parent (D-11). ASSUME: plain = add parent (non-destructive), ⌥ = move | 8g; P-IX-20 | none | missing | t-d5-pl-dnd-model; t-d5-pl-decisions |
| PL-222 | A drop that would create a cycle is refused (no drop state; SR announces) | 6e; P-6e-3 | none | missing | t-d5-pl-dnd-model |
| PL-223 | Fill a place: drop a file, folder, URL or tab onto the rail row or Home → source (tab → membership) | 6e; P-6e-12; P-BE-12 | `composer/useFileDrop.ts` (chat only) | missing | t-d5-pl-filing |
| PL-224 | Keyboard equivalent for every drag: "Add to another place…" / Pin menu | P-IX-34 | none | missing | t-d5-pl-place-menu; t-d5-pl-rail-menu |
| PL-225 | Add to another place… (second parent) | 8f; P-8f-12 | none | missing | t-d5-pl-place-menu; t-d5-pl-place-picker |
| PL-226 | Pin to rail / Unpin (toggle label) | 8f; P-8f-13 | none | missing | t-d5-pl-place-menu; t-d5-pl-rail-menu |
| PL-227 | Tint row in the menu: 5 inline swatches with a selected ring | 8f; P-8f-10 | `Menu.tsx` has no swatch row | missing | t-d5-int-pl-menu-swatches |
| PL-228 | Archive a place: archived first; it leaves the rail, stays in History | 8f; P-8f-15; P-X-12; P-IX-32 | none | missing | t-d5-pl-place-actions; t-d5-be-pg-archive-delete |
| PL-229 | Delete place…: inline confirm naming counts; chats keep other places or become unplaced; children move up; toast with Undo for 10s | 6d; P-6d-10; P-8f-16; P-IX-32 | none | missing | t-d5-pl-delete-confirm |
| PL-230 | Merge a place (promised in 6d, never drawn). ASSUME: omitted in d5 | P-X-13 | — | missing | t-d5-pl-decisions |
| PL-231 | ⌘Z undoes the last structural place action (close, archive, move, delete, filing), up to 20 per window | P-IX-33,44 | none | missing | t-d5-pl-undo |
| PL-232 | History row menu "Add to place" | P-IX-24 | t-s1-history row (no menu item) | missing | t-d5-tab-hist-add-to-place |
| PL-233 | History row click, recap, ⌘-click, ↑↓ ↵ | P-IX-25 | t-s1-history | in-flight:t-s1-history | — |
| PL-234 | Stale place: untouched 60 days → quiet merge/archive suggestion on Home and in ⌘P | 6d; P-6d-11; P-6e-18 | none | missing | t-d5-pl-suggestions |
| PL-235 | New-place suggestion only when ≥5 chats cluster | 6e; P-6e-17; P-6d-2c | none | missing | t-d5-pl-decisions |
| PL-236 | Place goes quiet: reopening restores nothing until you ask (vs close restores tabs) | 6d; P-6d-7 | none | missing | t-d5-pl-idle-close |

#### L. Multiwindow, keyboard, notifications, journeys

| Cov ID | Design item | Source | Current code | Status | Task |
|---|---|---|---|---|---|
| PL-250 | Each window shows one place; a window is a view of a place | 6d; P-6d-9; P-IX-29 | single window | missing | t-d5-nat-window-open; t-d5-pl-window-place |
| PL-251 | Same place in two windows: same tab set, live, no forks | 6d; P-6d-9; P-X-17 | none | missing | t-d5-prim-workspace-sync; t-d5-pl-test-window |
| PL-252 | Open in new window from palette ⌘↵, menu, Quick Look, ⌘-click on rail row/tile/breadcrumb | P-X-16 | none | missing | t-d5-pl-navigation |
| PL-253 | ⌘N opens a new window on Now (IX) vs ⌘N = new place inside ⌘P (6c). ASSUME: ⌘N inside the palette = new place; elsewhere = new window | P-IX-29; D-07 | none | missing | t-d5-pl-keys; t-d5-nat-window-menu |
| PL-254 | Notifications only for needs-you/failed, one per question, grouped per place; click focuses | P-IX-30; P-X-18 | none | missing | t-d5-nat-notify; t-d5-pl-notify-group |
| PL-255 | Dock badge = count of needs-you items | P-IX-31 | none | missing | t-d5-nat-notify |
| PL-256 | Something needs you in another place: amber on its rail row, listed in Inbox, answer without switching | 6d; P-6d-5 | none | missing | t-d5-pl-rail-sections; t-d5-be-attention |
| PL-257 | Keyboard shortcut map: ⌘0, ⌃0–9, ⌘P, ⌘⇧P, ⌘⇧W, ⌘[, Space, ⌘↵, ⌘Z, platform labels | IX Shortcuts; P-IX-36..46 | `design/keyboard.ts` (tab keys only) | missing | t-d5-pl-keys |
| PL-258 | Screen readers announce place status in words; colour never carries meaning alone | P-IX-34 | none | missing | t-d5-pl-place-dot; t-d5-pl-test-rail |
| PL-259 | Light + Dark for every place surface | P-X-14 | tokens are theme-aware | missing | t-d5-pl-test-rail; t-d5-pl-test-home; t-d5-pl-test-window |
| PL-260 | Reduced motion: no animation on tint swap, drag ghost or overlays | — | global reduced-motion rules | missing | t-d5-pl-test-window |
| PL-261 | Journey: first place via ⌘P → type → ⌘N | 6d; P-6d-2b | none | missing | t-d5-pl-test-palette |
| PL-262 | Journey: new chat inside a place belongs to it and uses its context | 6d; P-6d-3 | none | missing | t-d5-pl-test-using |

#### Open questions (each with the conservative assumption the tasks use)

1. ⌘P vs ⌘⇧P (6c caption calls the palette "All places (⌘P)"). ASSUME: ⌘P = go-to palette; ⌘⇧P = a fresh All places root-Home tab in the current strip.
2. ⌘N clash (D-07). ASSUME: inside the palette ⌘N creates a place; anywhere else ⌘N is the native New Window on Now (t-d5-nat-window-menu).
3. ⌃1–9 numbering: Pinned only (10a, IX) or Pinned then Open (9c). ASSUME: Pinned first, then Open, up to 9 (matches the drawn switcher); ⌃0 = Now. On Linux ASSUME Alt+0–9, because Ctrl+digit is already ⌘+digit there.
4. Peek: left 8px for 300ms (Places 9e) vs top 8px (Shell 2h, rail lane). ASSUME: with the rail collapsed, the left edge peeks the rail; the top edge stays for Focus mode (strip). t-d5-pl-rail-peek edits the rail lane's `useShellFrame.ts` after t-s1-rail lands. If the shell writer also claims that file, the integrator orders the two.
5. Drag semantics for places (D-11). ASSUME: plain drop = add (membership / second parent), ⌥ = move. A cycle is refused.
6. Merge (6d) is not drawn anywhere. ASSUME: omitted in d5; Delete and Archive only.
7. Delete semantics vs "Delete 14 chats?". ASSUME: deleting a place never deletes chats; the confirm names "N chats will become unplaced · M places move up".
8. "Since …" recap, AI cluster suggestions, the AI filing offer after the first reply, and memory promotion have no fixed backend task. ASSUME: render only what the engine sends (nothing otherwise). t-d5-pl-decisions records them, and the 60-day stale rule is a pure client date rule.
9. Pinned/Open scope. ASSUME: Pinned is global; Open is per window (close is "in this window"), persisted with the window's workspace.
10. Close (restores tabs) vs idle (restores nothing). ASSUME: explicit close stashes the tab set for restore; 12h idle archives the tabs (history lane) and the place reopens on Home only.
11. Counts ("N inside", "N chats"). ASSUME: "inside" = direct child places; "chats" = chats in the place and its descendants (deduplicated), from t-d5-be-pg-status-rollup.
12. Rail row (32/13) vs the rail lane's tab-shaped items (30/12). ASSUME: Places numbers for place rows. Settings/Activity/Design system stay reachable via ⌘, and ⌘K, and the integrator decides the bottom footer.
13. Generic undo ring and toasts: if the shell writer ships a generic per-window undo/toast, t-d5-pl-undo and t-d5-prim-toast register into it rather than keep their own ring. The integrator dedupes.
14. Renderer data layer: t-d5-prim-places-client is assumed to own `features/places/client.ts` (typed routes, `/sessions/{id}/using`, world `places` records). t-d5-pl-store adds only the React hook `usePlaces.ts`. If the prim task already ships that hook, t-d5-pl-store closes as a no-op.
15. Narrow and mobile are not drawn. ASSUME: rail in the ≤600 drawer; tiles auto-fit; palette, Quick Look and the Using popover become full-width sheets ≤600; no hover-only affordance on touch (× via menu).
16. Missing icons (folder-input, text, folder-git-2). ASSUME: folder-plus, align-left, folder-git (or folder), added in t-d5-int-pl-icons.

#### Counts

Total rows: 179.

| Status | Rows |
|---|---|
| complete | 1 (PL-110) |
| partial | 8 (PL-013, PL-020, PL-040, PL-072, PL-084, PL-099, PL-145, PL-147) |
| in-flight | 7: PL-041 and PL-080 (t-s1-rail), PL-062 and PL-233 (t-s1-history), PL-048 (t-s1-menus, pane only), PL-098 (t-s1-newtab, field only), PL-100 (t-s1-preview, card only). The place half of the last three has its own d5 task. |
| n/a-decided | 1 (PL-010) |
| missing | 162 |

### 7.6 Engine, bridge and native capabilities

Read 2026-10-09 against HEAD `39d440c4f` (`/home/santosh/codeaf-design-plan`) plus the lane worktrees named below (read-only).
`B/` = `internal/desktopbridge/`, `S/` = `internal/session/`, `T/` = `desktop/src-tauri/`.
"complete" is used ONLY where the code was opened and a named test asserts it. History-lane work
(`t-s1-history`, worktree `agent-ab15dab692b3b743f`: `B/history.go` with `/history`, `/history/search`,
`/history/archive`, `/history/{id}`, `/history/{id}/messages`; `S/recap.go`, `S/readconversation.go`; `Meta.Recap`;
`Config.Recaps`) is uncommitted and is marked in-flight, never complete. The files lane (`t-s1-files`,
`agent-a451c7c04660863b4`, commit `abba872c6`) adds `native::host_name` to `T/src/lib.rs`/`native.rs` and `hostName()` to
`desktop/src/design/native.ts`; tasks touching those files depend on it.

Vocabulary law: Go keeps `session.Place`/`PlaceRef` for conversation folders; the person-facing place is package
`internal/placegraph` (`placegraph.Node`, `placegraph.Membership`). `internal/manual/chat/places.md` already uses
"place" for v3's full-screen pages, so the desktop place page is a NEW page (`desktop-places.md`), never an edit that
blends the two meanings.

#### 1. Places (place graph, membership, context)

| Cov ID | Design item (state/input) | Source (page §id + inventory ID) | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-PL-01 | Place record `{id, name, parents[], context, policy, manager?}` create/read/update | Places §6e P-6e-1, P-BE-8 | none (`S/place.go:60` `session.Place` is one conversation's folder, not this) | missing | t-d5-be-pg-store, t-d5-be-pg-routes |
| BE-PL-02 | Graph not tree: many parents allowed, cycles blocked (409 naming the loop) | §6e P-6e-3 | none | missing | t-d5-be-pg-store |
| BE-PL-03 | One store `$CODEAF_HOME/desktop/places.json`, flock + atomic rename + generation counter; unreadable file never overwritten | arch-decisions "canonical data path" | none (`internal/filelock` exists, unused for this) | missing | t-d5-be-pg-store |
| BE-PL-04 | `manager` reserved, kept round-trip, never interpreted | §6e P-6e-5 | none | missing | t-d5-be-pg-store |
| BE-PL-05 | Tint: fixed six (tide, iris, rose, sand, sage, graphite); top-level picks; children inherit FIRST parent; graphite = no tint chosen; never blended | §6e P-6e-25, §9d P-9d-4, P-9d-5, P-9d-6, §8f P-8f-4 | none | missing | t-d5-be-pg-store |
| BE-PL-06 | New top-level place gets an unused tint. ASSUME: deterministic least-used, no model call (audit Q-04) | §9d P-9d-7 | none | missing | t-d5-be-pg-store |
| BE-PL-07 | Rename, set tint (menu) | §8f P-8f-9, P-8f-10 | none | missing | t-d5-be-pg-routes |
| BE-PL-08 | Add to another place (second parent); move (⌥) replaces parent | §8f P-8f-12, §8g P-8g-6, P-IX | none | missing | t-d5-be-pg-routes |
| BE-PL-09 | Create by name from ⌘P (`⌘N new place`), from inline tile (name + tint), under a parent | §6c P-6c-18, §6d P-6d-2b, §8f P-8f-2, P-8f-3 | none | missing | t-d5-be-pg-routes |
| BE-PL-10 | Membership `{chatId, placeId, addedBy: you\|ai, at}` add/remove | §6e P-6e-2, P-BE-7 | partial analogue only: `S/places.go:82` `PlaceRef` (folders per chat, ≤16), not on the bridge | missing | t-d5-be-pg-membership, t-d5-be-pg-routes |
| BE-PL-11 | A chat has zero or several places; "Not in any place · N" unplaced list | §6e P-6e-4, §8c P-8c-8, §8e P-8e-8 | `S/world.go:432` `ReadWorld` lists every conversation (no place join) | missing | t-d5-be-pg-membership, t-d5-be-pg-routes |
| BE-PL-12 | Drop a chat on a tile adds it; ⌥ moves it (membership move) | §8g P-8g-6, P-X-5 | none | missing | t-d5-be-pg-membership |
| BE-PL-13 | Undo of a membership add (the line's "Undo") and structural undo token (10s toast) | §6f P-6f-10, Shell IX S-IX-10, S-3l-15 | none | missing | t-d5-be-pg-membership, t-d5-be-pg-archive-delete |
| BE-PL-14 | New chat started in a place (⌘T in place, Home composer ↵/⌘↵) belongs to it from creation | §6d P-6d-3, §9e P-9e-4, §9b P-9b-5, P-9b-6 | `B/bridge.go:504` `POST /sessions {sessionFile}` only | missing | t-d5-be-session-workspace |
| BE-PL-15 | Chat in a place whose first source is a folder runs in that folder; other folder sources attached via `ReferPlace(path, PlaceSaid)` | arch-decisions "Per-conversation workspace" | one workspace per bridge (`cmd/codeaf/desktop_bridge.go:47-56,73`); Tauri passes no `--workspace` (`T/src/lib.rs:63`) | missing | t-d5-be-session-workspace |
| BE-PL-16 | Context = union over every membership + ancestors up to 2 levels; independent of which strip shows the tab | §6e P-6e-6, P-6d-8 | none | missing | t-d5-be-pg-resolve |
| BE-PL-17 | Conflict (default model, permissions, contradictory instructions) decided by nearest common ancestor | §6e P-6e-7, P-6e-8 | none | missing | t-d5-be-pg-resolve |
| BE-PL-18 | No common ancestor: the chat asks once and remembers. ASSUME: a "your pick" policy row in the Using bundle; the pick is stored per chat+key | §6e P-6e-8; places-design ambiguity 18 | none | missing | t-d5-be-pg-resolve, t-d5-be-pg-using-route |
| BE-PL-19 | Sources and memory never conflict: they add | §6e P-6e-9 | none | missing | t-d5-be-pg-resolve |
| BE-PL-20 | Per-chat source budget; trimmed list returned | §6e P-6e-21 | `S/placescontext.go:67,77` budget exists for attached-folder instruction files only | missing | t-d5-be-pg-resolve |
| BE-PL-21 | Using bundle for the chip/popover: places (direct + inherited, marked), instructions `{text, from}`, sources `{kind,label,from}`, policy `{key,value,from,conflict{wanted,decidedBy}}`, counts "N places · M sources" | §6f P-6f-3..P-6f-7, P-6f-12, §6e P-6e-10, P-BE-6 | none | missing | t-d5-be-pg-using-route |
| BE-PL-22 | Place context reaches the model in message[0] beside `# Attached folders`; recomposed only when the store generation moved | arch-decisions; §6e P-6e-6 | `S/placescontext.go:109` `attachedBlock` + `S/memory.go:2117` head composition (folders only) | partial | t-d5-be-pg-context-inject |
| BE-PL-23 | Adding a place mid-run applies from the next turn | §6e P-6e-22 | none | missing | t-d5-be-pg-context-inject |
| BE-PL-24 | Line "Now also using <place>: <source> · Undo" posted when membership changes; survives reload (journaled) | §6e P-6e-11, §6f P-6f-9 | none (`S/connect.go:617` ambient note is the nearest pattern) | missing | t-d5-be-pg-change-line, t-d5-be-pg-context-inject |
| BE-PL-25 | Manual law: chat manual page, `prompts/system.md` and probe table describe place context | root CLAUDE.md "THE MANUAL LAW" | `internal/manual/chat/places.md` uses "place" for v3 pages (collision) | missing | t-d5-be-pg-manual |
| BE-PL-26 | Add source to a place: file, folder, repo, URL, tab (→ conversation ref); remove; list | §6e P-6e-12, P-BE-12, §8b P-8b-8 | none | missing | t-d5-be-pg-sources |
| BE-PL-27 | Drop on the Using popover adds to THIS chat only | P-IX Using chip, P-BE-12 | `S/places.go:144` `ReferPlace`, `internal/remote/places.go:233`; not on bridge | partial | t-d5-be-pg-sources |
| BE-PL-28 | Instructions as plain prose on Home ("Write instructions") | §6e P-6e-13, §8b P-8b-9 | none | missing | t-d5-be-pg-routes |
| BE-PL-29 | Notes on Home (plain for now) | P-BE-3, CMP "Notes" | none | missing | t-d5-be-pg-routes |
| BE-PL-30 | Policy (default model) applied to a chat created in the place. ASSUME: model only, at creation; permissions shown, not enforced, until decided | §6e P-6e-7, §6f P-6f-7 | `cmd/codeaf/desktop_bridge.go:90` model = conversation role | missing | t-d5-be-pg-resolve, t-d5-be-session-workspace |
| BE-PL-31 | First place from a folder/repo: name = folder (repo root name), source = folder, tint assigned; idempotent | §6d P-6d-2a, §8e P-8e-7, Shell IX S-IX-9 | none | missing | t-d5-be-pg-from-folder, t-d5-nat-dialog |
| BE-PL-32 | "Now's matching tabs move into it with one click" after first place | §6d P-6d-2 | none | missing | t-d5-be-pg-from-folder |
| BE-PL-33 | Palette data: total count, recents by lastVisitedAt, tree with "N inside", "N chats" | §6c P-6c-4..P-6c-15, P-6c-20, P-BE-2 | none | missing | t-d5-be-pg-routes, t-d5-be-pg-status-rollup |
| BE-PL-34 | Status roll-up per place incl. descendants (needs-you, failed, running), DAG-safe; hover text "2 need you in Config parser" | §10a P-10a-16, P-10a-17, P-10a-18, §8a P-8a-9, P-BE-1 | `S/world.go:173,187` per-project counts (workspace buckets, not places) | missing | t-d5-be-pg-status-rollup |
| BE-PL-35 | Place Home: chats with one-line digest + state, attention rows rolled up with origin place, child tiles with "also in" | §8a P-8a-7, P-8a-8, P-8a-10..P-8a-13, P-BE-3, §8d P-8d-5, P-8d-6 | history lane `Meta.Recap` + `B/history.go` rows | in-flight:t-s1-history | t-d5-be-pg-home-digest |
| BE-PL-36 | "Since <last visit>" recap. ASSUME: no model call; composed from existing chat recaps or omitted (emptiness law) | §8a P-8a-6, §9a P-9a-4, §8d P-8d-4 | none | missing | t-d5-be-pg-home-digest |
| BE-PL-37 | Root Home: "N at the top level · M in all", top-level tiles "N places · M chats" | §8c P-8c-5..P-8c-7, P-BE-4 | none | missing | t-d5-be-pg-routes, t-d5-be-pg-status-rollup |
| BE-PL-38 | Rail: pinned (ordered, never auto-close), open (MRU), close per place, reorder | §10a P-10a-9, P-10a-10, P-10a-11, P-10a-14, P-10a-15, P-BE-1 | none (tabs only, localStorage) | missing | t-d5-be-pg-rail |
| BE-PL-39 | Closed but running: row stays muted until the work finishes or you answer, then leaves | §10a P-10a-5, P-10a-12 | none | missing | t-d5-be-pg-rail |
| BE-PL-40 | Open place idle 12h closes itself; reopening restores nothing until asked | §10a P-10a-13, §6d P-6d-7 | none | missing | t-d5-be-pg-rail |
| BE-PL-41 | Idle tabs archive at 12h (also inside pinned places) | §10a P-10a-13, Shell §4d S-4d-7 | history lane auto-archive | in-flight:t-s1-history | — |
| BE-PL-42 | Archive a place (and unarchive) | §8f P-8f-15, P-X-12 | none (`S/place.go:327` archives conversations only) | missing | t-d5-be-pg-archive-delete |
| BE-PL-43 | Delete: children move to the deleted place's parents; chats keep other places or become unplaced; counts for "Delete 14 chats?"; Undo 10s; nothing lost | §6d P-6d-10, §8f P-8f-16, S-IX-10 | none | missing | t-d5-be-pg-archive-delete |
| BE-PL-44 | Merge a place | §6d P-6d-10, P-X-13 | none; no Merge item drawn | missing | t-d5-be-pg-merge-decision |
| BE-PL-45 | Places idle 60 days get a quiet merge/archive suggestion; "Not now" snoozes 30 days | §6d P-6d-11, §6e P-6e-18, P-BE-10 | none | missing | t-d5-be-pg-suggest-stale |
| BE-PL-46 | Cluster suggestion (≥5 chats), "5 of these look like they belong in Reading · Move them", one filing line after the first reply (existing places only) | §6e P-6e-16, P-6e-17, §8c P-8c-10, P-8c-11, §6d P-6d-2c | none | missing | t-d5-be-pg-suggest-decision |
| BE-PL-47 | Memory promoted from a place's chats; removable | §6e P-6e-14, P-BE-11 | memory is per workspace (`S/memory*.go`) | missing | t-d5-be-pg-memory-decision |
| BE-PL-48 | Same chat in two places: one run, one composer state | §6d P-6d-8, §6e P-6e-19 | `B/bridge.go:513-518` reuses an open conversation with the same file | complete (`TestResumeDeduplicatesSameTranscriptAndKeepsCanonicalPlan`) | — |
| BE-PL-49 | Place changes reach every window live (`places` records on the world stream) | arch-decisions; §6d P-6d-9 | none | missing | t-d5-be-pg-routes, t-d5-be-world-stream |
| BE-PL-50 | Scale: 200 places, a 47-child parent, depth 2-3; resolve and list stay bounded | §6e P-6e-24, P-X-9, §6c P-6c-20 | none | missing | t-d5-be-pg-store, t-d5-be-pg-resolve |
| BE-PL-51 | Teams and sharing | §6e P-6e-23 | — | n/a-decided (the design itself rules it out: "Teams and sharing are out of scope for now", P-6e-23) | — |
| BE-PL-52 | Loading/error of every place read. ASSUME: routes answer `{error}` sentences; nothing invented while loading | P-X-8, places ambiguity 22 | — | missing | t-d5-be-pg-routes |
| BE-PL-53 | Concurrent writers (two windows, engine child reading) never lose an edit: generation check, 409 on a stale `ifGeneration` | arch-decisions | none | missing | t-d5-be-pg-store, t-d5-qa-be-places-routes |

#### 2. Web tab (native child webview)

| Cov ID | Design item (state/input) | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-WEB-01 | Page in a native child webview on its own sheet inside the card | Shell §3d S-3d-1, S-3d-6 | `desktop/src/features/tabs/kinds/web.ts` placeholder; `T/Cargo.toml` lacks `unstable` | missing | t-d5-nat-web-webview |
| BE-WEB-02 | Back, forward, reload buttons | §3d S-3d-2 | none | missing | t-d5-nat-web-webview, t-d5-prim-native-web-ts |
| BE-WEB-03 | Address field follows the page URL; tab title follows `document.title` | §3d S-3d-3 | none | missing | t-d5-nat-web-events |
| BE-WEB-04 | Loading: 2px progress line only | §3d S-3d-7 | none | missing | t-d5-nat-web-events |
| BE-WEB-05 | Real favicon when fetched, else monogram | §3d S-3d-8 | `B/favicon.go:59-114` only for domains the conversation contacted | partial | t-d5-be-web-favicon, t-d5-nat-web-events |
| BE-WEB-06 | Open externally (external-link) | §3d S-3d-5 | `T/src/native.rs:79` `open_url` | complete (`urls_need_http_scheme_and_host`) | — |
| BE-WEB-07 | Chat-plus: start a conversation with the page attached. ASSUME: attaches `{url, title}` as text; the app never fetches the page body itself | §3d S-3d-4 | none | missing | t-d5-prim-native-web-ts |
| BE-WEB-08 | A page's window.open / target=_blank opens a codeaf web tab, never a second native window | ASSUME (design silent) | none | missing | t-d5-nat-web-events |
| BE-WEB-09 | Menus, popovers, palette, Quick Look, overview and hover previews draw ABOVE the page (native view hidden or snapshotted while they are open) | Shell §3g, §3h, §3k (all overlay the card) | none | missing | t-d5-nat-web-overlay-hide |
| BE-WEB-10 | Webview label `web-*` has no capability and no IPC; own data directory; http/https only; file:, tauri:, ipc: refused | arch-decisions "Security" | none | missing | t-d5-nat-web-webview, t-d5-nat-capabilities |
| BE-WEB-11 | Pasting a URL in ⌘T opens a web tab | §3d S-3d-9, S-3f-13 | new-tab lane field | in-flight:t-s1-newtab | t-d5-prim-native-web-ts (native half) |
| BE-WEB-12 | ⌘-click / middle on a link chip opens a web tab; plain click opens the browser | §3d S-3d-10 | `open_url` exists; no web tab | missing | t-d5-prim-native-web-ts |
| BE-WEB-13 | Web pane resizes with split (2-4 panes), window resize, rail collapse, zoom/DPR | Shell §3b, §2d | none | missing | t-d5-prim-native-web-ts |
| BE-WEB-14 | Web tab in a second window; moving a web tab to a new window recreates its webview there | S-3g-9, S-IX-4 | none | missing | t-d5-nat-web-webview, t-d5-nat-tab-move-window |
| BE-WEB-15 | Failed load / certificate / blank page. ASSUME: platform error page inside the sheet plus a `web://loading {failed:true}` event; nothing turns red | Shell ambiguity 14 | none | missing | t-d5-nat-web-events |
| BE-WEB-16 | Browser dev mode (no Tauri). ASSUME: pane shows the URL and "Open in browser" only | ASSUME (design silent) | none | missing | t-d5-prim-native-web-ts |
| BE-WEB-17 | Keyboard inside a page: app shortcuts (⌘T, ⌘W, ⌘L) still reach the app while the webview has focus | ASSUME (design silent) | `T/src/menu.rs` accelerators (macOS only) | missing | t-d5-nat-web-webview |

#### 3. Jobs (engine background jobs and person-started jobs)

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-JOB-01 | Person-started job: start, list, stream, output, close | Shell §3c S-3c-1..S-3c-5 | `B/terminal_routes.go:16-161` | complete (`TestJobKeepsItsLogAndExitStatus`) | — |
| BE-JOB-02 | Close keeps a finished job's log; "Remove" drops it | DESIGN-QUESTIONS Q4 | `B/terminal_routes.go` `close`/`remove` | complete (`TestJobKeepsItsLogAndExitStatus`, `TestClosingAnInteractiveTerminalDropsIt`) | — |
| BE-JOB-03 | Engine background jobs listed (bash background, renders, watches) | Shell §3c "nightly-bench … job", §6a P-6a-12 | `S/jobnotice.go:111` `JobNotice` reaches the bridge only as `jobUpdate` events | partial | t-d5-be-jobs-export, t-d5-be-jobs-routes |
| BE-JOB-04 | Stop an engine job (square) | §3c S-3c-2 | `internal/remote/client.go:1624` `Agent.Cancel("job:N")`; no route | partial | t-d5-be-jobs-routes |
| BE-JOB-05 | Read an engine job's log (tail) | §3c S-3c-3 | `JobNotice.LogPath` under the session folder (two-roots rule) | missing | t-d5-be-jobs-routes |
| BE-JOB-06 | Reload/relaunch still lists live engine jobs | Shell IX S-IX-5 | registry unexported (`S/jobs.go:475`) | partial | t-d5-be-jobs-export |
| BE-JOB-07 | Cross-conversation running list (jobs roll-up on the world stream) | §8a P-8a-7, §6a P-6a-13 | `S/world.go:173` `Project.Running` | partial | t-d5-be-jobs-routes |
| BE-JOB-08 | Job/terminal pane | §3c | terminal lane | in-flight:t-s1-terminal | — |

#### 4. Terminal (PTY)

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-TERM-01 | Interactive PTY: start, input, resize, stream, close | Shell §3c | `B/terminal.go`, `B/terminal_routes.go` | complete (`TestTerminalSpawnInputStreamAndPlainOutput`, `TestTerminalResize`, `TestCloseKillsTheProcessGroupByItsOwnPid`) | — |
| BE-TERM-02 | User's shell, interactive, not login, in the project folder | DESIGN-QUESTIONS Q7 | `B/terminal.go:131-160` | complete (`TestTerminalRunsInTheWorkspace`) | — |
| BE-TERM-03 | A chat in a place: the terminal opens in that place's folder | §6a P-6a-12, Q7 | follows the bridge's one workspace | missing | t-d5-be-session-workspace |
| BE-TERM-04 | 512KB scrollback | Q6 | `B/terminal.go:18-25` | complete (`TestScrollbackIsBoundedAndReplays`) | — |
| BE-TERM-05 | 16 live terminals per conversation; one muted line when hit | Q6 | `B/terminal.go:18-25` limit, no test asserts the 17th refusal | partial | t-d5-qa-be-terminal-limit |
| BE-TERM-06 | Ask codeaf about this output | §3c S-3c-6, Q5 | `GET …/output`, `engine-client.ts` ask helper | complete (`terminal-client.spec.ts` "ask codeaf attaches the selection or the recent plain output through the attachment path") | — |
| BE-TERM-07 | PTY environment carries no provider key (`OPENROUTER_API_KEY`, `OPENAI_API_KEY`, …) and no codeaf token | arch-decisions "Security" | `internal/env/env.go:83-96` strips codeaf-owned secrets only | partial (`TestTerminalDoesNotInheritEngineSecrets` covers the token only) | t-d5-be-pty-env-strip |
| BE-TERM-08 | Terminals survive a bridge restart. ASSUME: not required (design silent); logs die with the bridge | code-bridge §6.4 | in memory only (`B/terminal.go:288-302`) | missing | t-d5-be-terminal-persist-decision |
| BE-TERM-09 | Terminal pane UI | §3c | terminal lane | in-flight:t-s1-terminal | — |

#### 5. Files

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-FILE-01 | Read text (1MB) and bytes (16MB) | Shell §3e | `B/workview.go`, `B/files.go` | complete (`TestWorkViewRoutesRelayTheEngine`, `TestReadFileCarriesDataAndInlineFlag`) | — |
| BE-FILE-02 | Diff against the commit the conversation started on | §3e, DESIGN-QUESTIONS D3, Q23 | `B/bridge.go:535-537` | complete (`TestOpeningAConversationRecordsItsDiffStart`) | — |
| BE-FILE-03 | Fuzzy find for ⌘T and @ | §3f S-3f-13 | `B/workview.go` find | complete (`TestWorkViewRoutesRelayTheEngine`) | — |
| BE-FILE-04 | Directory listing (tree, folder sources, @ picker browse) | Places P-6e-12, Shell §3f | `internal/remote/browse.go:17` `ListDir` not in `Connection` | partial | t-d5-be-files-list |
| BE-FILE-05 | Open in editor / reveal confined to engine-reported roots, not a renderer-supplied workspace | arch-decisions "Security" | `T/src/native.rs:68-77` trusts the renderer's `workspace` | partial (`refuses_symlink_escape`, `refuses_missing_relative_and_dotdot` test the confine, not the root's origin) | t-d5-be-roots-route, t-d5-nat-openpath-roots |
| BE-FILE-06 | "Open in ⌄" lists the editors found on the engine machine, default first, plus Copy path | DESIGN-QUESTIONS Q1 | files lane `EditorHandoff.tsx` offers only "Open in editor" | missing | t-d5-tab-files-open-with-native |
| BE-FILE-07 | Write/rename/delete a file from the UI. ASSUME: none in v1 (design draws no editing) | code-bridge §6.5 | no route | missing | t-d5-be-files-write-decision |
| BE-FILE-08 | File and diff panes | §3e | files lane | in-flight:t-s1-files | — |

#### 6. History

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-HIST-01 | List conversations, newest first, thousands of rows (cursor) | Shell §4a, §4d S-4d-8 | lane:`B/history.go` `GET /history` | in-flight:t-s1-history | — |
| BE-HIST-02 | Search in your own words, ranked, no model call | §4b, §4d S-4d-5 | lane:`B/history.go` `/history/search` | in-flight:t-s1-history | — |
| BE-HIST-03 | Archive / restore | §4c | lane:`B/history.go` `/history/archive` | in-flight:t-s1-history | — |
| BE-HIST-04 | Living recap written by the "Titles and summaries" role | §4d S-4d-2, DESIGN-QUESTIONS Q8 | lane:`S/recap.go`, `Meta.Recap` | in-flight:t-s1-history | — |
| BE-HIST-05 | Read a closed conversation without attaching an engine | §4d S-4d-6 | lane:`S/readconversation.go`, `/history/{id}/messages` | in-flight:t-s1-history | — |
| BE-HIST-06 | Reopen (continue) a conversation | §4d S-4d-6 | `B/bridge.go:504-571` | complete (`TestResumeDeduplicatesSameTranscriptAndKeepsCanonicalPlan`) | — |
| BE-HIST-07 | A history row/place Home row names the chat's places | §8a P-8a-11, §6d P-6d-7 ("still in ⌘P and History") | none | missing | t-d5-be-pg-routes |

#### 7. Settings

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-SET-01 | Model per role (8 roles) | Shell IX S-IX-12, D5 | `B/modelroles.go`, `internal/config/desktoproles.go:45-67` | complete (`TestAChosenModelIsPersistedAndReadBackByTheEngineSource`, `TestTheConversationRoleMovesOpenChatsLive`) | — |
| BE-SET-02 | Pinned models | D4 | `B/modelpinned.go` | complete (`TestAPinnedChoiceIsPersistedAndRefusedWhenItIsNotThreeCatalogModels`) | — |
| BE-SET-03 | Catalog with fallback when unreachable | D5 | `B/modelroles.go:89-110` | complete (`TestAnUnreachableCatalogFallsBackToTheModelsInUse`) | — |
| BE-SET-04 | Provider key status `{source, present}`, never the value | S-IX-12 "engine connection" | `internal/config/apikey.go:135` `APIKeySourceAt`; no route | missing | t-d5-be-key-status |
| BE-SET-05 | Engine connection facts (local or forwarded, model, version) | S-IX-12 | `/health` only | partial | t-d5-be-key-status |
| BE-SET-06 | Permissions (tool approval mode) | S-IX-12 | `internal/config/settings.go:2120` `ToolApprovalModeAt`; no route | missing | t-d5-be-settings-permissions |
| BE-SET-07 | Set or replace the provider key from the app. ASSUME: not in v1; the row names where the key comes from | S-IX-12 | none | missing | t-d5-be-key-set-decision |
| BE-SET-08 | Settings pane | S-IX-12 | rail lane `SettingsPane.tsx` | in-flight:t-s1-rail | — |

#### 8. Inbox and attention

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-INB-01 | Questions and approvals of an attached conversation | Conversation tray | `Snapshot.questions`, `POST …/answer` | complete (`TestQuestionAnswerUsesCanonicalIdentityAndOfferedKey`) | — |
| BE-INB-02 | Cross-conversation needs-you feed (every place, attached or not) | §6d P-6d-5, §6a P-6a-3 | `S/world.go:281` `SessionRow.NeedsPerson`; no route | missing | t-d5-be-attention |
| BE-INB-03 | Answer in Inbox without switching (lazy attach then `/answer`) | §6d P-6d-5 | `POST /sessions` spawns an engine first | partial | t-d5-be-attention, t-d5-qa-be-world |
| BE-INB-04 | Failed items (red) in the feed | §10a P-10a-16, P-X-11 | none | missing | t-d5-be-attention |
| BE-INB-05 | System notification when backgrounded: needs-you and failed only, one per question, grouped per place; never done/running | Shell IX S-IX-6, P-X-18 | none | missing | t-d5-nat-notify |
| BE-INB-06 | Notification click focuses that question. ASSUME: focuses the app window and the renderer opens the newest needs-you item (desktop notification plugins do not deliver click payloads on Linux) | S-IX-6 | none | missing | t-d5-nat-notify |
| BE-INB-07 | Dock badge = needs-you count, nothing else | S-IX-7 | none | missing | t-d5-nat-notify |
| BE-INB-08 | Local-tab Inbox pane | Shell IX | menus lane `InboxPane.tsx` | in-flight:t-s1-menus | t-d5-prim-world-client (data source) |

#### 9. Multiwindow

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-WIN-01 | Open a place in a new window (⌘↵ palette/menu/Quick Look, ⌘-click rail/tile/breadcrumb) | §6c P-6c-17, §8f P-8f-7, §8d P-8d-8, P-X-16, S-R-11 | one window `main` (`T/tauri.conf.json`) | missing | t-d5-nat-window-open, t-d5-prim-native-windows-ts |
| BE-WIN-02 | ⌘N opens a new window on Now (menu item New Window) | S-IX-4 | `T/src/menu.rs` tab items only | missing | t-d5-nat-window-menu |
| BE-WIN-03 | Linux/Windows: no app menu; New Window reached by the renderer's ⌘N/Ctrl+N. ASSUME | ASSUME (design silent) | `menu.rs` is `#[cfg(target_os="macos")]` | missing | t-d5-prim-native-windows-ts |
| BE-WIN-04 | Same place in two windows mirrors live: same tab set, no fork | §6d P-6d-9, P-X-17, S-IX-4 | tabs in one `localStorage` key (`desktop/src/features/tabs/model.ts:14`) — windows would overwrite each other | missing | t-d5-be-workspace-store, t-d5-be-workspace-routes, t-d5-prim-workspace-sync |
| BE-WIN-05 | Move tab to new window (menu) and tear-off drag. ASSUME: the new window shows the same place, focused on that tab (a window is a view of a place) | Shell §3g S-3g-9, S-3g-15, §2g S-2g-6 | none | missing | t-d5-nat-tab-move-window, t-d5-prim-native-windows-ts |
| BE-WIN-06 | Every window has the same IPC rights as `main` (`w-*`) and nothing more | arch-decisions | `T/capabilities/default.json` windows `["main"]` | missing | t-d5-nat-capabilities |
| BE-WIN-07 | Relaunch restores windows, places, tabs, splits, scroll; running work re-attaches | S-IX-5 | tabs only, one window, localStorage | partial | t-d5-sh-window-restore, t-d5-prim-workspace-sync |
| BE-WIN-08 | Window title names the place. ASSUME: "<place> — codeaf", "Now — codeaf"; frame tint is renderer-drawn | P-X-15 | title fixed "codeaf" | missing | t-d5-nat-window-open |
| BE-WIN-09 | Closing a window never stops work. ASSUME | ASSUME (design silent) | bridge is process-global (`T/src/lib.rs:19-23`) | missing | t-d5-nat-window-open |
| BE-WIN-10 | Active tab/split are per window; the tab SET is per place | §6d P-6d-9 | none | missing | t-d5-prim-workspace-sync |
| BE-WIN-11 | One world stream + one active-conversation stream per window (6-connection budget) | arch-decisions "Bridge rules" | `useBackgroundSessions.ts:5-13` polls every 2s | missing | t-d5-prim-world-client, t-d5-prim-world-background |

#### 10. Incremental snapshots

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-SNAP-01 | `GET /sessions/{id}?since=<entryCount>` returns header + entries tail | arch A3 | full snapshot every time (`B/bridge.go:584`) | missing | t-d5-be-incremental-snapshot, t-d5-int-be-snapshot-ring |
| BE-SNAP-02 | Tool `Output` over a cap omitted (fetched by `/tools/{callId}`) | arch A3 | `GET …/tools/{callId}` exists (`TestToolOutputFetchesOnlyNamedCanonicalCallStub`) but snapshots carry every output | partial | t-d5-be-incremental-snapshot |
| BE-SNAP-03 | Ring stores events + snapshot headers, not transcripts; memory bounded | arch A3 | ring of 2048 records with full snapshots (`B/bridge.go:30,333`) | missing | t-d5-int-be-snapshot-ring |
| BE-SNAP-04 | Renderer merges a tail into its held snapshot | arch A3 | `engine-client.ts:121` `snapshotFrom` replaces | missing | t-d5-prim-snapshot-merge |
| BE-SNAP-05 | Background tabs fed by the world stream, not 2s full-snapshot polls | arch-decisions | `useBackgroundSessions.ts:13` | missing | t-d5-prim-world-background |

#### 11. Session lifecycle and world stream

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-LIFE-01 | `POST /sessions/{id}/detach` releases a view | arch-decisions | no `delete(b.sessions…)` anywhere | missing | t-d5-be-session-detach |
| BE-LIFE-02 | Idle reaping of engine children with no observer and no running work | arch-decisions | children live until the bridge exits | missing | t-d5-be-session-detach |
| BE-LIFE-03 | View disconnect never stops a turn; replay survives | Shell IX S-IX-3 | `B/bridge.go` observers | complete (`TestViewDisconnectDoesNotStopTurnAndReplaySurvives`) | — |
| BE-LIFE-04 | Engine offline: renderer reconnects; Rust respawns the bridge | S-IX-8 | `T/src/lib.rs:78-87` clears the cached connection | partial | t-d5-qa-be-lifecycle |
| BE-LIFE-05 | World stream `GET /events?after=N` with `world`, `attention`, `places`, `workspace`, `jobs` records, gap → reset | arch-decisions | none | missing | t-d5-be-world-stream |
| BE-LIFE-06 | World rows: title, running, needs-you, failed, task counts, updatedAt, open | arch-decisions, §8a P-8a-12 | lane:`B/history.go:268` `attachedStates` | missing | t-d5-be-world-rows |
| BE-LIFE-07 | Relaunch: running work re-attaches. ASSUME: work in an engine child ends when the app quits; reopening re-attaches the conversation, never the process | S-IX-5 | engine children are the bridge's children | partial | t-d5-be-session-detach |

#### 12. Workspace (tab sets per place)

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-WS-01 | Engine-owned tab set per place and for Now, versioned | arch-decisions; P-BE-9 | `localStorage` `codeaf.desktop.workspace.v1` | missing | t-d5-be-workspace-store |
| BE-WS-02 | `GET/PUT /workspaces/{key}` with `If-Match` → 412 on a stale version | arch-decisions | none | missing | t-d5-be-workspace-routes |
| BE-WS-03 | `workspace` records on the world stream (other windows update) | arch-decisions | none | missing | t-d5-be-workspace-routes |
| BE-WS-04 | localStorage v1 imported once into `now` | arch-decisions | `desktop/src/features/tabs/model.ts:59-62` | missing | t-d5-prim-workspace-sync, t-d5-int-be-tabs-persistence |
| BE-WS-05 | Switching place keeps the tabs you left; running work keeps running | §6d P-6d-4 | none | missing | t-d5-prim-workspace-sync |

#### 13. Security acceptance (explicit)

| Cov ID | Design item | Source | Current code path(s) | Status | Task |
|---|---|---|---|---|---|
| BE-SEC-01 | Bearer token compared in constant time on every route; 401 sentence without detail | arch "Security" | `B/bridge.go:492-495` | complete (`TestUnauthorizedCannotOpenOrSend`, `TestModelRoutesNeedTheToken`, `TestTerminalRoutesNeedTheEngineToken`) | — |
| BE-SEC-02 | Every NEW route (places, world, workspaces, settings, jobs, files list, using, detach, roots) also refuses without the token | arch "Security" | — | missing | t-d5-qa-be-security |
| BE-SEC-03 | Token held in renderer memory only: never in localStorage, URLs, query strings or logs | `desktop/AGENTS.md:147` | rule only, no test | partial | t-d5-qa-be-security |
| BE-SEC-04 | A request whose `Origin` is present and not in the native allow-list is refused 403 before the token check | arch "Security" | CORS headers only (`B/bridge.go:481-491`); foreign Origin with token is served | missing | t-d5-be-origin-guard |
| BE-SEC-05 | `Host` must be a loopback literal (DNS-rebinding guard) | ASSUME (hardening; same law) | none | missing | t-d5-be-origin-guard |
| BE-SEC-06 | Bridge listens on loopback only | code-bridge §1 | `cmd/codeaf/desktop_bridge.go:39-46` | partial (no test) | t-d5-qa-be-security |
| BE-SEC-07 | PTY env: provider key variables stripped | arch "Security" | see BE-TERM-07 | partial | t-d5-be-pty-env-strip |
| BE-SEC-08 | CSP adds only what web/multiwindow need (nothing: web is a native view, windows share the origin); forwarded `localhost` URL normalised to 127.0.0.1 so CSP never blocks it | arch "Security"; code-bridge §3 | `T/tauri.conf.json` csp; `T/src/lib.rs:37` accepts `http://localhost:` | partial | t-d5-nat-csp |
| BE-SEC-09 | Web webview has no IPC (no capability names `web-*`, no `remote` URL capability), separate data dir, http/https only | arch "Security" | none | missing | t-d5-nat-web-webview, t-d5-nat-capabilities |
| BE-SEC-10 | `open_path`/`reveal_path` confine to engine-reported roots | arch "Security" | renderer-supplied root | partial | t-d5-nat-openpath-roots |
| BE-SEC-11 | `open_url` only http(s) with a host, one argv entry, no shell | code-bridge §3 | `T/src/native.rs:28-50` | complete (`urls_need_http_scheme_and_host`) | — |
| BE-SEC-12 | Capabilities least privilege: `main` + `w-*` only; exact permission list | arch "Security" | `main` only, 3 permissions | partial | t-d5-nat-capabilities |
| BE-SEC-13 | Provider key never crosses to the renderer; key route returns source only | code-bridge §3 | true today (no route at all) | partial | t-d5-be-key-status |
| BE-SEC-14 | Workspace payloads validated (size cap, schema version, JSON only) before they reach disk | arch-decisions | none | missing | t-d5-be-workspace-routes |
| BE-SEC-15 | Renderer never chooses a conversation's working directory or a terminal's cwd by path: it sends a `placeId`, the bridge resolves the folder from the store | arch "Per-conversation workspace" | n/a today (one workspace) | missing | t-d5-be-session-workspace |
| BE-SEC-16 | Web-tab favicon fetch: public addresses only, size and time capped | code-bridge §3 | `B/favicon.go:116-130` (contacted domains) | partial | t-d5-be-web-favicon |
| BE-SEC-17 | Tauri invoke list equals the registered handlers (no stray or missing command) | ASSUME (hardening) | 5 commands | missing | t-d5-qa-nat-config |

#### Open questions

1. **Delete a place: unplace or delete chats?** P-6d-10 says "Nothing is lost", S-IX-10 confirm says "Delete 14 chats?". ASSUME: delete only unplaces chats (they stay in History); the confirm counts affected chats as "14 chats leave this place".
2. **Pinned/Open rail scope (global vs per window).** ASSUME global (stored in `places.json`), because windows mirror places; Close removes the place from the rail for every window.
3. **Ancestor depth "up to 2 levels".** ASSUME two levels above EACH direct membership (parent and grandparent), deduplicated; inherited ancestors count in "N places".
4. **Ask-once conflicts.** ASSUME no question card in the transcript: the Using bundle carries an unresolved policy row with both values; a pick is stored per (chat, key) in `places.json`; until picked, the chat's own current setting holds.
5. **Policy keys.** ASSUME v1 policy = `model` only, applied when a chat is CREATED in the place; `permissions` is stored and shown but not enforced until the designer decides.
6. **"Since <last visit>" writer.** ASSUME no model call; built from existing recaps of chats changed since `lastVisitedAt`, else omitted.
7. **Merge, cluster suggestions, filing offer, memory promotion.** ASSUME not built in d5 beyond a decision task each; the 60-day stale suggestion is a pure date rule and is built.
8. **Move tab to new window / tear-off.** ASSUME the new window shows the same place focused on that tab (no forked tab set). If the designer means "a new window on Now", only `tabmove.rs`'s target key changes.
9. **Notification click target.** ASSUME focus the window; the renderer opens the newest needs-you item for that place.
10. **Relaunch and running work.** ASSUME quitting the app ends engine children (work stops); relaunch re-attaches conversations by transcript.
11. **Provider key entry in Settings.** ASSUME read-only source in v1.
12. **Web-tab favicon for any URL.** ASSUME a bridge route that fetches `/favicon.ico` for a web tab's host under the same public-address rules; the webview's own favicon event wins when present.
13. **Manual vocabulary.** The chat corpus already uses "place" for v3 pages. ASSUME a new `desktop-places.md` page whose headings say "desktop place" and which the probe table reaches for "what is the Using chip", "why does this chat know about brand-voice.md".
14. **Chat-only source drop (Using popover).** ASSUME a folder becomes `ReferPlace(path, PlaceSaid)` on that chat; a file becomes an attachment on the next message; a URL is pasted into the composer.

#### Counts

| Status | Rows |
|---|---|
| complete | 19 |
| partial | 25 |
| missing | 105 |
| in-flight | 13 |
| n/a-decided | 1 |
| **total** | **163** |

Tasks: 85 in ~/.codex/codeaf-design-run/d5-inventory/tasks-backend.json (1 composite t-d5-be-pg, 6 decision/research, 11 QA incl. one manual review, 10 integrator-only). Every partial/missing row above names at least one task that lists it in `covers`.
