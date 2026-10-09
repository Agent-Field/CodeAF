# Places, native Web tabs and the engine-wide data path

Status: architecture proposal, 2026-10-09. It is grounded in `39d440c4f` and the
in-flight lane worktrees. It does not describe shipped behaviour; nothing here is built
until its PlanDB task (`t-d5-*`, project `p-6xuu`) lands. `DESIGN-COVERAGE.md` maps
every design item to a task. On conflict, the design files
(`.git/design-current/*.dc.html`) win over this document, and the code wins over both
on what exists today.

## 1. Bottom line

1. **A design "place" is a new engine concept.** It does not extend `session.Place`.
   `session.Place` (`internal/session/place.go:60`) is one conversation's folder.
   `session.PlaceRef` (`internal/session/places.go:82`) is a folder one conversation
   refers to. `session.PlacesRoot()` is `~/.codeaf/v3/projects`, where conversations
   live. None of them is a named, tinted, many-to-many container. The design's place
   is a new Go package, `internal/placegraph`, stored in one file outside the
   conversation tree. Its node type is `placegraph.Place`, always package-qualified,
   and never `session.Place`. The coordinator lane `t-d5-places-store` has built the
   store (`~/codeaf-places-store`, `dfdd4b22c`, not yet merged); §3.1 describes that
   shape.
2. **The engine owns every piece of state two windows must agree on**: the place
   graph, membership, rail pins and the open list, and each place's tab set. The
   renderer's `localStorage` becomes a one-time migration source only.
   "Same place, two windows: both windows show the same tab set, live" (Places 6d)
   cannot be met by per-webview storage.
3. **Place context reaches the model through the existing message[0] door.** The
   child engine resolves a chat's places at turn start, only when the store's
   generation has moved, and composes them next to the existing `# Attached folders`
   block (`internal/session/placescontext.go`). There is no second resolver, no
   per-turn rebuild, and no independent AI in the desktop.
4. **There is one engine-wide SSE stream** (`GET /api/engine/events`) carrying small
   typed records: `world`, `attention`, `places`, `workspace`, `jobs`. It replaces the
   2-second full-snapshot polling of background tabs and feeds the rail dots, Inbox,
   Home, the switcher and notifications. Per-conversation SSE stays, and only the
   focused conversation holds one.
5. **The Web tab is a native child webview**, one per web pane, positioned over the
   pane's rectangle by Rust. That webview has no Tauri capability, no IPC and no
   bridge token, and it accepts only http/https. The renderer sends a rectangle and a
   URL, and receives title, favicon, URL and loading events.
6. **Native multiwindow** is a `window_open {placeKey}` command creating
   `WebviewWindow` labels `w-<n>`. Every window reads the same engine workspace store
   for its place, and the capability scope covers `main` and `w-*` only.

## 2. Reference inventory: what exists today (verified)

| Piece | Where | Shape | Gap for this design |
|---|---|---|---|
| Transport | `cmd/codeaf/desktop_bridge.go:29-115`, `desktop/src-tauri/src/lib.rs:48-89` | Loopback HTTP on `127.0.0.1:0`, a 32-byte bearer token, and one JSON line `{url, token, model}` printed on stdout | One `--workspace` per bridge process. Tauri passes none, so the process cwd is used |
| Auth | `internal/desktopbridge/bridge.go:479-495` | The token is compared in constant time. CORS headers are set only for the four native origins | A request with a valid token and **no** Origin is served. A foreign Origin is not refused. It is only denied CORS |
| Conversation open | `bridge.go:504-571` `POST /sessions {sessionFile}` | One child `codeaf engine` per conversation, stdio, `remote.Dial` | No workspace parameter. Sessions are never removed from `b.sessions`, so engine children are never reaped |
| Conversation state | `bridge.go:67-89` `Snapshot` | The full transcript (`entries[]`, including tool `Output`), tasks, questions, queue, usage and `seq` | Full re-serialisation on every task, title or question event. The ring keeps 2048 records, many of them full snapshots |
| Conversation stream | `bridge.go:773-826` `GET /sessions/{id}/events?after=N` | SSE `Record{seq,type,event?,snapshot?}`, with a 15s heartbeat. A gap sends a full snapshot | Only the active tab subscribes. Background tabs poll `GET /sessions/{id}` every 2s (`useBackgroundSessions.ts:13`) |
| Files/diff/locate/favicon/terminals | `internal/desktopbridge/{files,workview,favicon,terminal,terminal_routes}.go` | Per-conversation routes, all typed in `features/chat/engine-client.ts` | No directory listing (`remote.Client.ListDir` exists but is not plumbed). Terminals are in memory only |
| Models | `internal/desktopbridge/{modelroles,modelpinned}.go` | 8 roles, catalog, pinned. Every role defaults to `deepseek/deepseek-v4.1-flash` (`bridge.go:29`, D4) | Provider-key status (`config.APIKeySourceAt`) is not exposed |
| Cross-conversation data | `internal/session/world.go:130-437` `ReadWorld`, `SessionRow{ID,Title,Presence,Live,Tasks,Places,Archived…}`, `NeedsPerson()` | On disk under `~/.codeaf/v3/projects/<bucket>/<id>/` | Not on the bridge. History lane `t-s1-history` adds `internal/desktopbridge/history.go` (uncommitted) |
| Referred folders | `internal/session/places.go:128-238` `Places/ReferPlace/RemovePlace/SetPlaceMode`; `placescontext.go:109-284` `attachedBlock` | ≤16 per conversation in `meta.json`. Composed into message[0] only on a deliberate act | No bridge route. Not in `Snapshot` |
| Background jobs | `internal/session/jobs.go:261,475`, `jobnotice.go:111-141` | Reach outside code only as `EventJobUpdate{Job *JobNotice}` | No exported list, no stop, no snapshot field |
| Tab model | `desktop/src/features/tabs/{types,model,helpers}.ts`, `reducers/*`; `storageKey='codeaf.desktop.workspace.v1'` | `WorkspaceState{tabs,groups,activeId,closed,nextNumber,recentIds}` and `Tab = Pane & {pinned, groupId?, split?}` | One workspace, kept in one `localStorage` key. Two windows would overwrite each other's tabs. No place key |
| Tab kinds | `features/tabs/kinds/types.ts:5` | `conversation, task, file, diff, web, terminal, settings, history, newtab, inbox` | No `home` kind and no `places` (All places) kind. `web` is a placeholder (`backed:false`) |
| Native | `src-tauri/src/{lib,native,menu}.rs`; `capabilities/default.json` (`windows:["main"]`); `Cargo.toml` (`macos-private-api`, `tauri-plugin-shell`) | `engine_health`, `engine_connection`, `open_path`, `reveal_path`, `open_url` | One window. No `unstable` (no child webviews). No dialog or notification plugins. `open_path` trusts a renderer-supplied workspace string |

## 3. Data shapes

### 3.1 `internal/placegraph` (new package; store built by `t-d5-places-store`)

These are the shapes the store lane landed (`internal/placegraph/types.go` at `dfdd4b22c`). They are
quoted so the bridge, the child engine and the renderer agree on one spelling.

```go
type Place struct {
    ID, Name     string
    Parents      []string        // ordered; the FIRST parent decides an inherited tint; none = under the root
    Tint         Tint            // tide|iris|rose|sand|sage|graphite; "" inherits
    Archived     bool
    Context      Context         // {Instructions string; Sources []Source}
    Policy       Policy          // {Model, Permissions string}: the fields that can conflict
    Manager      json.RawMessage // reserved by the design (6e), kept verbatim, read by nobody
    CreatedAt, ArchivedAt, LastOpenedAt time.Time
}
type Source struct { ID string; Kind SourceKind; Ref, Label string; AddedBy AddedBy; At time.Time }
type Membership struct { ChatID, PlaceID string; AddedBy AddedBy /* you|ai */; At time.Time }
type State struct {
    Version     int
    Revision    uint64   // +1 per structural commit; TouchOpened does not move it
    Places      []Place
    Memberships []Membership
    Pinned      []string // the rail's Pinned section in the person's order (⌃1–9)
}
// Every mutation returns a Receipt; Store.Undo(receiptID) reverts it only when the
// graph still sits at that receipt's AfterRevision (ErrRevisionConflict otherwise). 20 receipts are kept.
```

- **Identity:** `ChatID` is `session.SessionRow.ID` (the conversation folder's name). It is stable
  across reopen and is what `ReadWorld` reports. The bridge never uses a `sessionFile` path as a
  membership key.
- **Store:** the caller injects the path. The bridge passes `home.Join("desktop", "placegraph.json")`
  (owned by `t-d5-be-pg-store`). Avoid the word `places` in the path: `v3/projects` is already the
  conversations' places root. Every call takes the exclusive lock (`internal/filelock`, 10s, then
  `ErrLocked`), re-reads, validates, then replaces the file by temp, fsync and rename. A damaged file
  is quarantined beside the original, never deleted (`Store.LastRecovery()`).
- **Laws in code:** many parents, cycles refused (`ErrCycle`). Sibling names are unique,
  case-insensitively. `DeletePlace`/`MergePlaces` move children to the removed place's parents and
  only unfile chats. An archived place keeps its memberships but reads as absent. Tint resolves through
  first parents, and a top-level place with no tint gets the least-used of five.
- **Not in the store yet**, so the bridge tasks add it:
  - the rail's **Open** list (newest first) and closed-but-running entries (`t-d5-be-pg-rail`);
  - per-place roll-ups (`t-d5-be-pg-status-rollup`);
  - conflict decisions and the source budget (`t-d5-be-pg-resolve`);
  - path and URL canonicalisation of sources (`t-d5-be-pg-sources`);
  - every HTTP route (`t-d5-be-pg-routes`).
- **Scale:** the design expects 20–200 places. The store's ceilings (`MaxPlaces` 2000,
  `MaxMemberships` 50000, `MaxFileBytes` 32 MiB) refuse runaway input. They are not budgets. Reads
  are in memory after one parse.

### 3.2 Context resolution (`placegraph.Resolve`)

`Resolve(sessionID) Bundle` returns, for one chat:

```go
type Bundle struct {
    Revision     uint64      // the store revision this bundle was resolved at
    Places       []PlaceUse  // direct memberships, then ancestors ≤2 levels; each {id,name,via:"direct"|"inherited"}
    Instructions []Sourced   // {text, place}
    Sources      []Sourced   // {source, place}
    Policy       []Decision  // {field:"model"|"permissions", value, wanted:[{place,value}], decidedBy:place|"you"}
    Trimmed      []Sourced   // over the per-chat source budget, named not quoted
    Ask          *Conflict   // a parentless conflict with no remembered ruling: the chat asks once
}
```

- Union of direct places and their ancestors up to 2 levels (6e). Sources and memory
  add up; they never conflict. A conflict on model, permissions or contradictory
  instructions is decided by the nearest common ancestor. With none, `Ask` is set,
  the chat asks once. The answer is stored as a ruling beside the graph (`t-d5-be-pg-resolve` owns where; the landed `State` has no field for it yet).
- **Budget:** reuse `attachedFileLimit` (4 KiB per file) and `attachedFilesBudget`
  (12 KiB total) from `placescontext.go`. Do not add a second pair of constants.
  Whatever is cut goes into `Trimmed` and is named in the Using popover.

### 3.3 How the model is told (child engine)

- The child `codeaf engine` shares the disk (`Connection.Local`). At **turn start**
  it `stat`s `placegraph.json`. If `State.Revision` moved, it calls `Resolve` and rebuilds
  the place block **once**. This applies "Adding a place mid-run applies from the next
  turn" (6e risks) and the message[0] cache law (`memory.go:2097`,
  `refreshSystemLocked`).
- Folder sources of a chat's places become `PlaceRef{Arrival: PlaceSaid}` through the
  existing `ReferPlace`, so the ground ladder (`taskstands.go`) and `attachedBlock`
  keep being the only readers of folders. Instructions and non-folder sources get a
  sibling block, `# Places this conversation belongs to`. It is composed in a new
  `internal/session/placegraphcontext.go` behind a `Config.PlaceContext` hook, so
  `internal/session` never imports `placegraph`; `cmd/codeaf` wires the hook. Each
  place gets its own heading naming which place said it. Several places' rules are
  never blended, the same law the attached-folder block already states
  (`t-d5-be-pg-context-inject`).
- Membership changes post a system line into the transcript ("Now also using
  Marketing: brand-voice.md", 6f) through the existing notice path. Undo is a
  membership removal.
- **Manual law:** the same change updates `internal/manual/chat/` (a page answering
  "what is a place / why is this chat using X"), `internal/session/prompts/system.md`
  and the probe table in `internal/manual/chat_test.go`. Task `t-d5-be-pg-manual`
  owns it.

### 3.4 Per-place tab sets (workspace store)

- `home.Join("desktop", "workspaces", "<placeId|now>.json")` holds the renderer's
  `WorkspaceState` for that place. It is validated by the same rules as
  `readWorkspace` (ported to a Go validator for shape and size, ≤256 KB), versioned,
  and written with `PUT` and `If-Match: <version>`. A 409 returns the current
  document, and the renderer rebases its pending action.
- Window-local only, never synced: which tab is focused **in this window**, scroll
  position, hover and menu state. Synced: tabs, groups, pins, splits, `closed`, and
  pane drafts keyed by conversation. "One composer state" for the same chat in two
  places (6e) means a draft belongs to the conversation, not the pane.
- `features/tabs/model.ts` keeps the reducer pure. Only its persistence adapter
  (`readWorkspace` / save effect) changes, behind `t-d5-prim-workspace-sync`. On
  first run the v1 `localStorage` key is imported into `now` and then left alone.

### 3.5 Engine-wide stream

`GET /api/engine/events?after=N` (SSE) uses the same ring and gap logic as the
per-conversation stream (`bridge.go:773-826`), with records typed as:

| type | payload | produced when |
|---|---|---|
| `world` | `{rows: [{session, title, place[], running, needsYou, failed, at, archived}], removed: []}` (delta rows only) | `ReadWorld` rescan on presence-file change (fsnotify, with a 2s debounce), plus live bridge sessions on title/turn/question events |
| `attention` | `{items: [{session, kind:"question"\|"failed", text, place[], at}]}` | question asked, withdrawn or answered; task failed |
| `places` | `{revision, places?, memberships?, pinned?, open?}` (changed collections only) | any `placegraph` write |
| `workspace` | `{key, version}` (the renderer fetches the document) | any workspace `PUT` |
| `jobs` | `{session, jobs: [{id,name,state,elapsed,exitCode}]}` | `EventJobUpdate` |

Background tabs stop polling full snapshots. A tab's title, running state and dot come
from `world` rows. A full snapshot is fetched only when a tab is focused.

### 3.6 Incremental conversation snapshots

- `GET /sessions/{id}?since=<entryCount>` returns the header (`title, running,
  needsPerson, questions, tasks, queue, usage, seq`) plus `entries[since:]`.
  `entries[i].Output` is replaced with `{truncated:true}` above 8 KB; the full body
  stays one `GET /tools/{callId}` away (existing route, 1 MiB cap).
- The ring stores events plus **snapshot headers**, not full transcripts. A gap
  answers with header plus `since` tail.
- `DisplayEntry` gets explicit JSON tags in a compatible spelling (capitalised fields
  stay accepted), so the wire stops depending on Go field names.

## 4. One canonical data path per feature

| Feature | Truth | Wire | Renderer owner |
|---|---|---|---|
| Conversation | child engine session | `/sessions/{id}` (+`since`), per-session SSE | `features/conversation/*` |
| Places graph, membership, rail | `placegraph.json` | `/api/engine/places*` + `places` records | `features/places/` (new) |
| Place context | `placegraph.Resolve` in the child engine | engine-internal; `GET /sessions/{id}/using` for the chip | `features/places/using/` |
| Tab sets | `desktop/workspaces/*.json` | `/api/engine/workspaces/{key}` + `workspace` records | `features/tabs/` persistence adapter |
| Cross-conversation rows, Inbox, dots | `ReadWorld` + live sessions | world SSE `world`/`attention` | `features/world/` (new) |
| History | `t-s1-history` routes | its routes + `world` rows | `features/history/` (lane) |
| Jobs | session job registry (export) / bridge PTYs | `/sessions/{id}/jobs`, `/terminals`, `jobs` records | `features/terminal/` (lane) + jobs list |
| Files | the conversation's workspace | existing routes + `/files/list` | `features/files/` (lane) |
| Settings | `config` desktop roles/pinned + key source | `/models*`, `/settings/key` | `SettingsPane` (lane) |
| Web pages | the native child webview | Tauri commands/events only | `features/web/` + the `nativeWeb.ts` adapter (coordinator lane `t-d5-native-web`) |
| Windows | Tauri window manager | `window_open`, `tab_move_to_window` | `lib/native/windows.ts` |

## 5. Bridge surface to add (narrow and typed)

| Route | Body → result | Backing |
|---|---|---|
| `GET /places` | → `State` plus the Open list (whole; small at design scale) | `Store.Snapshot` |
| `POST /places` | `{name, parent?, tint?}` → `{place, receipt}` | `CreatePlace` |
| `PATCH /places/{id}` | `{name?, tint?, context?, policy?}` → `{receipt}` | `Rename`/`SetTint`/`SetContext`/`SetPolicy` |
| `POST /places/{id}/parents` | `{add?:id, remove?:id}` → `{receipt}` (409 on cycle) | `AddParent`/`RemoveParent` |
| `POST /places/{id}/sources` | `{kind, ref, label?}` → `{receipt}` (paths canonicalised, repo-root snapped, the chosen path kept) | `SetContext` after canonicalising |
| `DELETE /places/{id}/sources/{sourceId}` | → `{receipt}` | `SetContext` |
| `POST /places/{id}/archive` / `restore` / `delete` / `merge` | → `{receipt, chats, children}` | `Archive`/`Restore`/`DeletePlace`/`MergePlaces` |
| `POST /places/undo/{receipt}` | → `{revision}` (409 when the graph moved on) | `Store.Undo` |
| `POST /places/{id}/chats` | `{chat, addedBy, from?}` → `{membership, receipt}` | `AddChat`/`MoveChat` |
| `DELETE /places/{id}/chats/{chat}` | → `{receipt}` | `RemoveChat` |
| `POST /places/{id}/pin` / `unpin`, `PUT /places/open` | `{index?}` / `{open}` → `{receipt}` | `Pin`/`Unpin`, open list (`t-d5-be-pg-rail`) |
| `POST /places/from-folder` | `{path}` → `{place, receipt}` (named after the repo or folder, one folder source) | `t-d5-be-pg-from-folder` |
| `GET /sessions/{id}/using` | → `Bundle` | `Resolve` |
| `GET/PUT /workspaces/{key}` | `WorkspaceState` + version | workspace store |
| `GET /events?after=N` | SSE | world ring |
| `POST /sessions` | `+ workspace?, place?` | the place's first folder source becomes the chat's workspace; its other folders are referred as `said` |
| `POST /sessions/{id}/detach` | → `{}` | drops the observer and reaps an idle child |
| `GET /sessions/{id}/jobs`, `POST …/jobs/{job}/stop` | → `JobRow[]` | exported job snapshot |
| `GET /sessions/{id}/files/list?path=` | → `{entries[], truncated}` (≤2000) | `remote.Client.ListDir` |
| `GET /settings/key` | → `{source, present}`, **never the value** | `config.APIKeySourceAt` |

The route table in `bridge.go` and the wiring in `cmd/codeaf/desktop_bridge.go` are
integrator-only edits. Feature code lives in new files (`places.go`, `world.go`,
`workspaces.go`, `jobs.go`, `settings.go`).

## 6. Security boundary

- **The token stays the gate.** In addition: a request whose `Origin` is present and
  not one of the four native origins gets a 403 before the token check. Browser dev
  mode keeps the Vite proxy, bound to loopback only.
- **The renderer never sees a provider key.** `GET /settings/key` returns a source
  name and a boolean. PTY environments drop `OPENROUTER_API_KEY`, `OPENAI_API_KEY`
  and every `*_API_KEY` the config layer reads, not only `CODEAF_*`.
- **Web tab webviews (`web-*` labels)** are absent from every capability file. They
  get no `invoke`, no event bridge and no access to `engine_connection`. Navigation
  allowed: `http:` and `https:` only. `file:`, `tauri:`, `javascript:`, `data:`
  top-level and custom schemes are refused in `on_navigation`. Downloads are refused
  with one muted line. `window.open` becomes a new web tab through the
  `web://new-window` event, never a native window. Each webview uses a data
  directory under the app data dir; it is persistent per user (assumption Q-W2).
- **The CSP of the app webview is unchanged** except for what the multiwindow and
  dialog features need. The web tab is not an iframe, so no `frame-src` is opened.
  `connect-src` adds `http://localhost:*` only if forwarded remote engines are kept.
- **`open_path`/`reveal_path`** confine to the roots the engine reports for open
  conversations (fetched by Rust from the bridge with its own token), not to a
  renderer-supplied workspace string.
- **Place sources** are canonicalised with the same `canonicalPath` and repo-root snap
  as `PlaceRef`. Referring a folder never widens where work may write
  (`taskoutside.go`'s one-writable-ground law is untouched).

## 7. Native Web tab: mechanics

The coordinator lane `t-d5-native-web` (`~/codeaf-native-web`) is building this: `web.rs` with the
security policy, viewport, lifecycle, navigation, WebPane and snapshots (`t-d5nw-*`). Command names
below are descriptive; the lane's typed bridge is canonical once it lands, and the `t-d5-*` web tasks
were reduced to verifying it against these rules.

1. `Cargo.toml` enables `tauri/unstable` (multi-webview). This is integrator-only.
2. `web_open {paneId, url, rect}` creates child webview `web-<paneId>` on the calling
   window with `WebviewBuilder::new(label, WebviewUrl::External(url))`, then
   `window.add_child(builder, position, size)`.
3. The renderer's `WebPane` measures its body with a `ResizeObserver` and calls
   `web_bounds {paneId, rect}`, throttled to animation frames. The pane draws the
   address row, the loading line (`LoadingLine.tsx`) and the states. The page pixels
   are the native view.
4. **Overlays:** a native view draws above the DOM. When any menu, hover preview,
   palette, Quick Look, toast or overview is open over a web pane, the renderer calls
   `web_hide {paneId}` and shows the last captured frame. Capture uses a
   `web_capture` snapshot where the platform supports it; otherwise the pane's
   neutral surface. On close it calls `web_show`. A test asserts no DOM overlay
   coexists with a visible web view (`t-d5-nat-web-overlay-hide`).
5. Events (`web://title|favicon|url|loading|new-window`, payload carrying `paneId`)
   update the pane's `TabSummary`. Tab title, monogram/favicon and the loading line
   come from these events, not from the bridge favicon route, which is limited to
   domains a conversation fetched.
6. Split panes, tab switches, overview and window minimise call `web_hide`/`web_show`.
   Closing the tab calls `web_close`. Back, forward, reload and stop map to commands.
   Find-in-page uses the platform's find where available; otherwise it is absent,
   not broken.
7. Outside Tauri (browser dev mode), the web pane shows one line, "Web pages open in
   the desktop app", and an Open in browser button using `openUrl` (existing).

## 8. Native multiwindow: mechanics

- `window_open {placeKey, tabId?}` builds `WebviewWindow` `w-<n>` with
  `index.html#place=<key>`, the same size tokens as `main`, and the macOS overlay
  title bar. Linux keeps its honest tinted fallback and never simulates traffic
  lights.
- `capabilities/default.json` widens `windows` to `["main", "w-*"]`, with the same
  permissions and nothing more.
- Each window boots, reads `GET /workspaces/<key>` and subscribes to the world
  stream. Two windows on one place therefore share tabs, groups and drafts live.
  Focus and scroll stay per window.
- `tab_move_to_window {tabId, from, toPlace?}` is a workspace-store transaction: the
  source window removes the tab, and the target window (new or existing) adds it.
  Running work is unaffected because tabs are views (6e).
- The native menu gains New Window (⌘N, Interactions) and window list items. Inside
  the ⌘P palette ⌘N means "new place"; the palette's key handler takes it first while
  the palette is open (resolves design conflict D-07 by focus scope).
- One SSE connection per window for the world stream, plus at most one for the
  focused conversation, stays inside the browser's six-per-host limit per webview.

## 9. Capability gaps for standalone operation

| Feature | Today | Missing to be standalone | Task(s) |
|---|---|---|---|
| Places | nothing | everything in §3 and §5 | `t-d5-be-pg-*`, `t-d5-pl-*` |
| Jobs list | events only | exported snapshot, stop, world `jobs` | `t-d5-be-jobs-export`, `t-d5-be-jobs-routes` |
| Terminal | in-memory PTYs per conversation | survive window reload (reattach by id); key-stripped env | `t-s1-terminal` (pane), `t-d5-be-pty-env-strip` |
| Files | read/find/stat/diff/locate | directory list (tree, @ picker), editor list for "Open in ⌄" | `t-d5-be-files-list`, `t-s1-files` |
| History | not on bridge | list/search/archive/recap (lane), auto-archive (lane) | `t-s1-history` |
| Settings | roles, pinned, catalog | key status without the secret | `t-d5-be-key-status`, `t-s1-settings` |
| Inbox | per-open-conversation questions only | engine-wide attention feed; answer without a visible tab | `t-d5-be-attention`, `t-d5-tab-inbox-*` |
| Multiwindow | one window | windows, capability scope, shared workspace store | `t-d5-nat-window-*`, `t-d5-be-workspace-*` |
| Session lifecycle | children never reaped | detach + idle reap | `t-d5-be-session-detach` |
| Workspace per chat | one per bridge | `POST /sessions {workspace}` | `t-d5-be-session-workspace` |

## 10. Open questions and the assumption shipped until answered

| # | Question | Conservative assumption |
|---|---|---|
| Q-P1 | ⌘N: new window (Interactions) or new place (6c palette footer)? | Focus-scoped: inside the palette it means new place; everywhere else, new window on Now. |
| Q-P2 | ⌘P vs ⌘⇧P (6c caption vs the rail's binding) | ⌘P opens the Go-to palette; ⌘⇧P opens the All places root Home tab. |
| Q-P3 | ⌃1–9: pinned only, or pinned then open (9c)? | Pinned first, then open, in rail order; ⌃0 = Now. |
| Q-P4 | Reopening a closed place: restore its tabs (10a) or nothing (6d "goes quiet")? | Closed by the person: tabs restored. Auto-closed after 12h idle: tabs archived, nothing restored until asked. |
| Q-P5 | Plain drop vs ⌥ drop (8g vs Interactions) | Plain drop adds (non-destructive); ⌥ moves. |
| Q-P6 | Merge (promised by 6d, absent from every menu) | The engine supports it (`MergePlaces`); the UI shows no Merge item until the designer places one (`t-d5-be-pg-merge-decision`). |
| Q-P7 | Counts "N chats / N inside": direct or descendants? | Tiles show descendants (chats in the place and its descendants, each counted once); the Home Chats list shows direct members only. |
| Q-P8 | Who writes "Since yesterday" and who suggests places/merges? | No model call from the desktop. Show only what the engine already has (child recaps from `t-s1-history`); otherwise draw nothing. Cluster suggestions are not built; the 60-day idle suggestion is a date rule. |
| Q-P9 | Which folder becomes a chat's working directory when its place has several? | The place's first folder source; the rest are referred `said`. With no folder source, the bridge's launch workspace. |
| Q-P10 | Draft of the same chat in two strips | One draft per conversation, synced through the workspace store with a 500ms debounce. |
| Q-W1 | Web tab page state across app restart | URL and title are restored; the page reloads; no session restore of forms. |
| Q-W2 | Web cookies | Persistent per user in the app data dir; a Clear site data item in Settings is out of scope until designed. |
| Q-W3 | Overlays over a web page | The page is hidden behind a captured frame while the overlay is open (§7.4). |
| Q-M1 | Quitting with running work in another window | macOS: closing the last window keeps the app (and engine) running; Linux/Windows: closing the last window quits after the existing close-and-stop confirmation rules for running tabs. |
| Q-S1 | Remote engine (`CODEAF_DESKTOP_CONNECTION`) with places | The place graph and the workspaces live with the engine (remote disk); the web tab and windows stay local. |
