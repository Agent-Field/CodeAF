# Workspace sync: one tab set per place, mirrored live

Design: Places 6d "Same place, two windows — both windows show the same tab set, live. No forked tab sets. A
window is just a view of a place." Interactions "Relaunch — windows, places, tabs, splits and scroll positions
restore." Architecture: `PLACES-ARCHITECTURE.md` §3.4 (this file records what was built and where it differs).

## What is shared and what is not

| Shared by every window on the place (engine) | Kept by one window (localStorage, under its own name) |
|---|---|
| tabs, with each pane's content: draft, route, folds, what it points at | which tab it shows (`activeId`) |
| groups (name, collapsed), pins, order | recency order for Ctrl+Tab (`recentIds`) |
| splits: panes, layout, divider ratios | which pane of each split it focuses (`split.focus`) |
| the closed list (Reopen), the tab counter | scroll position per pane |

The engine refuses a document carrying `activeId`, `recentIds`, `scroll`, `focus` or `split.focus`, by name.

A draft is one composer per conversation: typing into a pane copies the words to every other pane of the same
conversation in that tab set (Places 6e "one composer state"). Across two different places' tab sets this is
not done yet (see Gaps).

## Every tab kind, and what else a window does on its own

| Kind / writer | What mirrors | Two windows at once |
|---|---|---|
| conversation, task | `sessionFile`, draft, title, `route` (task view, Back/Forward), folds, task panel | engine titles arrive in both windows; the second `title` is a no-op and is never queued. A route change in one window moves the same tab in the other (a tab is one view; see questions) |
| file, diff | `path`/`file` (path + Changes/File) | none beyond the tab itself |
| terminal | the tab; the terminal it shows is bound by pane id in `codeaf.desktop.terminals.v1` (same origin, so both windows read one binding) | both windows attach the same engine terminal; the terminal lane decides whether two attaches are allowed. A terminal exiting closes its tab in both: the second close is a no-op |
| web | `target.url` (and `target.shot`, which nothing writes today) | a data-URL screenshot written into `shot` would meet the 256 KiB refusal, said in words |
| settings, history, inbox, newtab | the tab | History's idle archive runs once per window mount (12 h idle, never the window's active tab, `codeaf.desktop.history-activity`); a second window's archive of the same tabs is a no-op |
| home (Places shell) | the place's pinned Home | `home-ensure` is a no-op on an existing Home; two first Homes converge on one |

Window-local and NOT synced, by design: the overview's grid/filmstrip choice (`TabOverview` `viewKey`),
appearance (`codeaf-theme`, already mirrored by its own storage event), and everything in "kept by one window".

## Engine

- `internal/workspacestore` — one `<key>.json` per place in an injected directory; `cmd/codeaf` uses
  `<folder of places.json>/workspaces` (`~/.codeaf/desktop/workspaces`). Keys are `now` or a place-graph id
  `pl_<16 hex>`; nothing else becomes a file name.
- Every write: per-key file lock (`internal/filelock`, 10 s, then busy), re-read, revision compare-and-swap,
  temp + fsync + rename. A read takes no lock and creates nothing.
- A damaged file reads as revision 0 with `damaged: true` and is left in place; the next write moves it to
  `<key>.json.damaged-<time>` (never deleted) and restarts the revision from the clock. A file from a newer build
  is refused and left alone.
- Limits (change `store.go`, `limits.ts` and this list together): 256 KiB document, 400 tabs, 50 closed,
  100 groups, 4 panes per split, 128-byte ids, 4 KiB titles.

| Route (token required; a present non-shell `Origin` is 403) | Answer |
|---|---|
| `GET /api/engine/workspaces/{key}` | `{key, revision, updatedAt?, writer?, workspace, damaged?}`; nothing saved: revision 0, `workspace: null` |
| `GET …/{key}?after=N&wait=1` | long poll, held ≤ 25 s: answers as soon as the revision is not N, else the unchanged record |
| `PUT …/{key}` `{revision, writer, workspace}` | 200 record; 409 `{code:"conflict", current}`; 400 `invalid`; 413 `too_large`; 503 `busy`; 409 `newer` |

Differences from §3.4/§3.5: the expected revision is in the body, not `If-Match` (the bridge's CORS answer
allows only `Authorization` and `Content-Type`); and there is **no `workspace` record on the world stream**,
because `features/chat/world-client.ts` `parseWorldRecord` throws on any unknown record type and would drop
every window's world feed. When that reader skips unknown types, a PUT can publish `{key, revision, writer}`
there and the long poll can go.

## Renderer: `src/features/workspace-sync/`

- `controller.ts` — every change is a real `WorkspaceAction`, applied at once with the pure `workspaceReducer`
  and queued. Saves are debounced (400 ms; a burst of typing is one write) and compare-and-swap. A 409 carries
  the current tab set; the queue is replayed over it:
  - ids minted by a reducer (new tab, group, split, the tab after the last one closes) are recorded the first
    time and replayed identically, so ids are stable through any number of rebases;
  - a change already present (its save landed but the answer was lost) is recognised and not made twice;
  - pin and collapse replay as the value they produced, Reopen as the tab it reopened;
  - a draft for a tab another window closed is kept on the closed tab (Reopen brings it back);
  - anything else another window overtook is counted in `status.overtaken`, never dropped in silence.
  - An action that changes only window-local fields (select, split focus) is never queued: no echo.
- Offline: the window keeps working, the queue is kept locally (so a reload keeps it), the status says
  "codeaf engine is not running", retries double from 2 s to 30 s, and `online`/visibility retry at once.
- `windowStore.ts` — the window's name is its native label (`win-main`, `win-w-3`), so relaunch restores focus,
  scroll and queue; a browser tab keeps a session name. A window that closed with unconfirmed changes leaves them
  behind; the next window on that place takes them over only when Web Locks prove the old window is gone. With
  no Web Locks nothing is taken over and nothing is deleted.
- `useWorkspaceSync.ts` — `{state, dispatch, status, retry, acknowledge, scrollOf, setScroll, handoff}`.

## Integration seam (root `Workspace.tsx`, after the Places shell `d45b4aa28` lands)

This lane does not edit `Workspace.tsx`, `TabStrip`, `model.ts` or the reducers. The swap is:

```tsx
// replaces: useReducer(workspaceReducer, undefined, () => initialFor(place, title)), the `adopted` ref,
// the localStorage save effect and the `storage` event effect
const sync = useWorkspaceSync({ key: place === 'now' ? 'now' : (place as WorkspaceKey), initial: () => initialFor(place, placeTitle ?? 'Home') });
const { state, dispatch } = sync;
```

- `initialFor` keeps reading the candidate's per-place localStorage keys, so the first window on each place
  imports what was there once; the keys are never deleted.
- The candidate's `adopt` action and `sharedShape` are not needed. `sharedShape` cannot be reused as is: it
  serialises `tabs` with `split.focus` inside, so one window focusing a pane would move the other's.
  `workspace-sync/shared.ts` `sharedText` is the corrected projection.
- `home-ensure` keeps working: on an existing Home it is a no-op (nothing queued); two windows creating a
  place's first Home at once converge on one, because the slice drops any other Home.
- Scroll: the conversation view calls `sync.setScroll(pane.id, top)` and restores from `sync.scrollOf(pane.id)`;
  that wiring belongs to the conversation lane and is not done here.
- Show `status` with the existing quiet line patterns: `offline`/`refused` with `status.error`, and
  `overtaken > 0` once ("Another window changed these tabs first; N of your changes no longer applied"), then
  `acknowledge()`.

## Move to new window on the same place: the contradiction

Interactions asks for "Move to new window" and "Dragging a tab out of the strip makes a new window"; Places 6d and
Interactions "Multiple windows" say a window shows one place and the same place in two windows mirrors live. A
new window opened on the **same** place shows the same tab set, so the moved tab cannot leave the source window
without leaving the new one too. The candidate's two-phase handoff (open a copy in the target, close the original
in the source) under mirroring ends with both windows holding the copy and the original gone: the tab's id changes
and nothing moves.

What is built: `sync.handoff(tabId)` — moves only the source window's focus off the tab (to its neighbour), writes
nothing, and answers `{key, tabId}`; the new window opens on the same key with `focus: tabId` and shows that tab.
It is a focused-view handoff, not a move. **Unknown, for the designer:** whether "Move to new window" should
instead open the tab in a window on another place (Now, or a chosen one) — that needs a destination the design
does not name, so no chooser was invented. The native call (`openPlaceWindow` with a focus tab id instead of a
pane payload) is the shell lane's to wire.

## Gaps, stated

- No world-stream `workspace` record (above); one long poll per window instead.
- Drafts are one composer within a place's tab set, not across two places that show the same conversation.
- Two windows typing into the same draft at the same moment: the window that saves last wins and counts the
  other's words in `status.overtaken`; the window whose words were replaced is not told.
- Folds and the task route mirror with the tab (Architecture §3.4 lists only focus, scroll, hover and menus as
  window-local). If a designer wants a fold or a task view in one window not to move the other, those fields move
  to `WindowLocal` in `shared.ts` and the engine's refusal list.
- A tab kind or view field this build does not know is normalised on read (`kindOrDefault`, `cleanView`); two
  different codeaf builds editing one tab set at once could flatten each other's newer fields.
- Web Locks in the Linux webview (WebKitGTK) are assumed, not verified; without them orphaned queues wait for
  their own window instead of being taken over.

## Tests

```sh
go test -race ./internal/workspacestore ./internal/desktopbridge -run 'Workspace|TwoWindows|LongPoll|Refusals|UnknownKeys'
npm --prefix desktop run workspace-sync:test        # controller, client, window copy (node)
make build && npm --prefix desktop run test:workspace-sync   # two real pages, real bridge, Chromium + WebKit, port 1771
```
