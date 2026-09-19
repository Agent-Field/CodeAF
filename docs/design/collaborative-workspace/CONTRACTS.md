# Wave 1 contracts

Coordination baseline 18 September 2026, **reconciled 19 September 2026** (PlanDB notes n-91ee n-uijj n-ludz n-q2mx n-5r0l n-5sbo n-cw71 n-ux5g). The freeze is not an excuse to ship missing invariants: every finding below must have a test, not only a comment. Amend only through a PlanDB note and a CONTRACTS.md patch. Do not invent extra tables or tools. A Folders tab-bar place is **Folders-entry** (below), not this freeze.

Wave 1 work package: `docs/design/collaborative-workspace/issue-1.md` (branch-local). PUBLICATION-POLICY.md: no GitHub issues, comments, or PRs before owner verification. Journeys J01–J08. Pre-wave source: `610a32ba`.

## Lane ownership (disjoint)

| Lane | PlanDB | Owns (create/edit) | Must not edit |
|---|---|---|---|
| storage | `t-w1-storage` | `internal/workspace/*` | anything else |
| service | `t-w1-service` | `internal/wsapi/*` (new package) | workspace internals, tui3, session, cmd |
| tui | `t-w1-tui` | `internal/tui3/homepanel_folders.go`, `internal/tui3/folders.go`, their tests; `internal/tui3/homegrid.go` (append `panelFolders` to the iota **last** so existing IDs do not shift; insert the slot in `homePanelOrder` after recent; whisper); `internal/tui3/commands.go` (`/folders` rows only); `internal/tui3/homeslash.go` (`homeFate` for `folders`); `internal/tui3/tui3.go` (`Options.Folders` field); `internal/tui3/place_home.go` (`homeRowVerbs` folder case); `internal/tui3/home.go` (folder enter + pending start); `internal/tui3/app.go` (`case "folders"` only) | `internal/workspace`, `internal/wsapi`, `internal/session`, `cmd/codeaf` |
| wiring | `t-w1-wiring` | `cmd/codeaf/collections.go` (additive `--reason`); `cmd/codeaf/folders_adapter.go` (`var _ tui3.Folders`); `cmd/codeaf/chatv3.go` / `chatv3_local.go` (construct `wsapi.Service`, set `tui3.Options.Folders` and `session.Config.Folders`); `internal/session/tools_folders.go`; `internal/session/session.go` (`Config.Folders`); `internal/session/tools.go` and `internal/session/bashbelt.go` (append `foldersTools` the same way `memoryTools` is appended); `internal/session/prompts/system.md` (mention `folders` only if wired) | `internal/workspace` internals, `internal/tui3` except filling Options |
| proof | `t-w1-proof` | `internal/manual/chat/` pages that currently deny folder UI; `internal/manual/chat_test.go` probes; `internal/e2e/tuiwords_test.go` needles; `docs/design/collaborative-workspace/TRY.md`; optional untagged e2e helper. **Not** `internal/tui3/*_test.go` or workspace/wsapi tests unless a lane transfers them in a PlanDB note. | product logic; other packages' unit tests |

Integration (`t-w1-integrate`) applies lane branches onto `feat/collaborative-workspace-0918` in order: storage → service → real-store → tui → wiring → proof. Wiring's adapter must implement every TUI Folders method, including `RenameFolder`.

Storage draft lives in the storage worktree only (`origin.go`, `schema.go`, in-progress `store.go`). Coordinator checkout must not keep those uncommitted files after contracts land.

## Product names (person-facing)

| Surface | Spelling |
|---|---|
| Home panel heading | `folders` |
| Empty whisper | `logical groups of chats · /folders create Billing` (no ellipsis, never “no folders yet”) |
| Slash | `/folders` — **not** an alias of `/folder`. Optional later alias `/collections` is out of Wave 1. |
| `/folders create <name>` | make a logical folder |
| `/folders add <name-or-id>` | file the current chat here |
| `/folders rename <name-or-id> <new-name>` | rename a logical folder; the new name shows through every parent |
| Verb strip (`→` on a folders row) | `n` new chat here · `f` add current chat · `m` move this placement · `w` why here · `x` remove this placement |
| Tool name | `folders` |
| Root | virtual; never a `collections` row; never a CLI list entry |

Filesystem `/folder` `/place` `/dir` stay filesystem. Logical membership never changes cwd, repo, or `/attach`.

## `internal/workspace` (storage)

Keep existing `Open`, `Close`, `Create`, `Rename`, `Collections`, `Members`, `CollectionsFor`, `Add`, `Remove`, `Ref`, `Kind`, errors. Listing remains read-only and does **not** migrate. Writes call `ensureSchema`, which migrates v1→v2 or creates v2.

`PRAGMA user_version` after a successful Wave 1 write-path migration is **2**. Wave 2 write-path is **3** (see Wave 2). Wave 3 write-path is **4** (see Wave 3). Wave 4 write-path is **5** (see Wave 4). `application_id` stays `0x4146434c`. Wave 1 created no guidance/grant/delivery/execution tables; Wave 2 adds only the five named v3 tables below. Wave 3 adds `participants` and `deliveries`. Wave 4 adds `grants` and `execution_bindings`.

```go
const (
    OriginPerson         = "person"
    OriginSystemFallback = "system_fallback"
    OriginOrganizer      = "organizer"
    ActionAdd            = "add"
    ActionRemove         = "remove"
    LifecycleActive      = "active"
)

type Provenance struct {
    Origin, Reason, Actor, Evidence, IdempotencyKey string
    // ExpectedRevision is optional. Zero means “no precondition” (old CLI).
    // Non-zero must match the collection's revision inside the write txn.
    ExpectedRevision int
    // ExpectedFrom / ExpectedTo apply to Move only; zero means no check.
    ExpectedFrom, ExpectedTo int
}

// ErrConflict is a stale-write refusal. It wraps ErrInvalid and the error
// string contains the word "revision".

type MembershipEvent struct {
    CollectionID string
    Kind         Kind
    RefID, SessionID, Action, Origin, Reason, Actor, Evidence, At, IdempotencyKey string
}

type Collection struct {
    ID, Name, Purpose, Lifecycle string
    Revision                     int
    CreatedAt, UpdatedAt         string // RFC3339; empty on v1-read-before-write
}

func (s *Store) AddWith(ctx context.Context, id string, ref Ref, p Provenance) error
func (s *Store) RemoveWith(ctx context.Context, id string, ref Ref, p Provenance) error
func (s *Store) Move(ctx context.Context, fromID, toID string, ref Ref, p Provenance) error
func (s *Store) WhyHere(ctx context.Context, id string, ref Ref) (MembershipEvent, error)
func (s *Store) Events(ctx context.Context, id string, ref Ref) ([]MembershipEvent, error)
func (s *Store) SchemaVersion() int
func (s *Store) RootState(ctx context.Context) (revision int, purpose, updatedAt string, err error)
```

Mismatch of a non-zero expected revision returns `ErrConflict` wrapping `ErrInvalid` with the word `revision`. Zero expected revision keeps old CLI `Add`/`Remove` unconditional. `Move` checks ExpectedFrom against the source collection and ExpectedTo against the destination **in the same writer transaction** as both membership edits; one mismatch rolls the whole Move back.

`Add`/`Remove` keep their old signatures and mean person origin + empty reason (CLI compatibility). `Move` is add-destination + remove-source + two events in **one** writer transaction; other placements of the same ref stay. Cycle check remains in that transaction. A successful mutating write increments the touched collection revision(s) **and** `root_state.revision` in that same transaction. `WhyHere` is the latest event for that active or last edge. Root is not stored as membership. `RootState` reads `root_state` (creating the v2 row only on a write path / ensureSchema). Listing still does not migrate and does not write.

`Collection` JSON may grow omitempty fields; default CLI text is still `id  name`.

## `internal/wsapi` (service)

No import of `session`, `tui3`, `provider`, `run`. Imports `workspace` only.

```go
type Inventory interface {
    ConversationIDs() []string
    Title(id string) string
}

type Folder struct {
    ID, Name, Purpose, Lifecycle string
    Revision                     int
    ParentIDs                    []string // empty ⇒ child of Root
    MemberCount                  int      // unique conversation IDs, not paths
}

type Placement struct {
    CollectionID string
    Ref          workspace.Ref
    Title        string
    AlsoIn       []string // other collection names
}

type Why struct {
    Event workspace.MembershipEvent
}

type RootView struct {
    Folders     []Folder      // parentless collections
    Unfiled     []Placement   // conversations with no membership
    Revision    int           // root_state.revision
}

type Service struct{} // holds *workspace.Store, Inventory, clock

func Open(path string) (*Service, error)
func (s *Service) Close() error
func (s *Service) SetInventory(Inventory)
func (s *Service) CreateFolder(ctx context.Context, name string) (Folder, error)
func (s *Service) RenameFolder(ctx context.Context, id, name string) error
func (s *Service) AddPlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error
func (s *Service) RemovePlacement(ctx context.Context, collectionID string, ref workspace.Ref, p workspace.Provenance) error
func (s *Service) MovePlacement(ctx context.Context, fromID, toID string, ref workspace.Ref, p workspace.Provenance) error
func (s *Service) RootSnapshot(ctx context.Context) (RootView, error)
func (s *Service) FolderSnapshot(ctx context.Context, id string) (Folder, []Placement, error)
func (s *Service) PlacementsOf(ctx context.Context, ref workspace.Ref) ([]Folder, error)
func (s *Service) WhyHere(ctx context.Context, collectionID string, ref workspace.Ref) (Why, error)
```

Deduplicate conversation IDs in counts. Pass Provenance through to the store (do not drop Expected* or Origin). Expected-revision conflicts return `workspace.ErrConflict` (or `ErrInvalid` mentioning `revision`). `Open` failure is an error: the service is not constructed, and callers must not invent an empty successful RootView. Corrupt/foreign/future stores are errors, not empty folders. Use `workspace.Provenance` / `workspace.MembershipEvent` after storage lands — do not keep a second look-alike type in this package.

## TUI (`internal/tui3`)

- Append `panelFolders` as the last `homePanelID` iota value (do **not** insert it between existing IDs). Insert the slot after `panelRecent` in `homePanelOrder`. `keep: 2`, `least: 3`, `rest: 4`, `most: 8`, not pinned. Wave 1 shipped this as an eighth **home panel**. Folders-entry (below) promotes Folders to a dedicated tab-bar place; the panel may remain as enter-from.
- Snapshot is a memo filled on the home **beat** (`readHomeFolders`), never in `View` or on cursor move.
- `homeFolderRow = 246`, `homeFolderBack = 247`. Member chats reuse `homeSession` with `cell.panel == panelFolders`.
- `app.pendingFolder` string: set by `n` / `/folders` new; consumed after first-message `renew`/`startChatEnter` via `AddPlacement`. Esc clears it and creates no transcript.
- `Options.Folders` is `tui3.Folders`. DTOs are **exported** so `cmd/codeaf` can implement the interface (`FolderView`, `FolderPlacement`, `FolderWhy`, `FolderRoot`). They do **not** use `workspace.Ref` or `wsapi.Folder`. Wiring owns `cmd/codeaf/folders_adapter.go` mapping `*wsapi.Service` → `tui3.Folders`, with a compile-time `var _ tui3.Folders = (*foldersAdapter)(nil)`. `*wsapi.Service` does **not** satisfy `tui3.Folders` directly.
- Nil `Options.Folders` (open failed / store unavailable / corrupt): the panel heading still exists, but it is **not** an empty working workspace. Mutations (`n f e m w x`, `/folders create|add|nest`) must refuse with a visible failure, never silent success. Distinct from a working empty store, which uses the emptiness-law whisper.
- 80-col: sequential drill-in, `esc` back. No model on paint.

```go
type FolderView struct {
    ID, Name, Purpose, Lifecycle string
    Revision, MemberCount int
    ParentIDs []string
}
type FolderPlacement struct {
    CollectionID, RefID, Title, Kind string
    AlsoIn []string
}
type FolderWhy struct {
    Origin, Reason, Actor, Evidence, At string
}
type FolderRoot struct {
    Folders []FolderView
    Unfiled []FolderPlacement
    Revision int
}
type Folders interface {
    RootSnapshot(ctx context.Context) (FolderRoot, error)
    FolderSnapshot(ctx context.Context, id string) (FolderView, []FolderPlacement, error)
    CreateFolder(ctx context.Context, name string) (FolderView, error)
    RenameFolder(ctx context.Context, id, name string) error
    AddPlacement(ctx context.Context, collectionID, refID string) error
    AddFolderPlacement(ctx context.Context, parentID, childFolderID string) error
    RemovePlacement(ctx context.Context, collectionID, refID string) error
    MovePlacement(ctx context.Context, fromID, toID, refID string) error
    WhyHere(ctx context.Context, collectionID, refID string) (FolderWhy, error)
}
```

`Kind` on a placement is workspace's reference kind (`collection`, `conversation`). Empty Kind is a conversation. Nested/shared folders survive as `Kind=collection` members of FolderSnapshot; RootSnapshot may list only parentless folders. `AddFolderPlacement` is how the TUI nests one folder under another: the adapter calls `svc.AddPlacement` with `workspace.CollectionKind` and OriginPerson, never a conversation-ref fallback. `/folders nest <child> [in <parent>]` and the folder-row verb `e` (nest this folder) are the person-facing doors; a cycle is the existing store `ErrCycle`. `wsapi.Service` talks to a `store` interface matching `workspace.Store`. `Open` binds `*workspace.Store` directly (`AddWith`/`Move`/`RootState`); there is no production fake adapter. `cmd/codeaf` `foldersAdapter.RenameFolder` calls `svc.RenameFolder`.

## Session / CLI (wiring)

- `session.Config.Folders` is an interface with `List/File/Unfile/Move` methods wrapping `wsapi` (define the interface in `session` so `wsapi` does not import `session`). `File` takes a typed `workspace.Ref`; it does not hard-code `ConversationKind` for every call.
- Tool `folders` on the belt only when `Config.Folders != nil`. Actions: `list`, `file`, `unfile`, `move`. `file` with `child` nests that folder (`CollectionKind`) under `id`; without `child` it files this chat. Refuses cycles/unknown ids in result text.
- **Trusted origin at ingress.** The folders tool stamps `OriginOrganizer` (actor = this session) itself. Tool arguments must not carry `origin`; a model cannot claim `person`. Person-facing CLI and TUI verbs stamp `OriginPerson`. `system_fallback` is only for explicit recovery paths, never as a silent default.
- Conversation id for membership is `session.Place.ID()` (16 hex), never a UI path.
- CLI: `--reason` optional on `add`/`remove`; default output of old verbs unchanged.
- First-message order: mint transcript, then `AddPlacement`. Failure leaves the chat unfiled under Root; retry is idempotent.

## Tests the lanes owe before handoff

- storage: v1 list without migrate; first write → v2; cycle txn; events written; Root not stored; foreign/future/damaged refuse; **ExpectedRevision mismatch → ErrConflict**; **Move ExpectedFrom+ExpectedTo atomic (one mismatch rolls both edges back)**; **RootState.revision increments on mutating writes**; complexity ≤ 15.
- service: add/move/remove/idempotency; unique counts; dual placement; **Open/corrupt error is not an empty RootView**; Provenance expected-revision forwarded; after storage lands, adapter must call real AddWith/Move (no discarded provenance, no two-txn Move).
- tui: emptiness whisper; “also in”; selection/composer stability; 80-col sequential; no store read in View; **nil Folders mutations refuse** (not empty-success). Package tests live here, not in the proof lane.
- wiring: tool absent when service nil; present when wired; `/folder` unchanged; **compile-time `var _ tui3.Folders` on the adapter**; **tool-origin is organizer, never person**.
- proof: e2e/manual/probes/TRY only. **Not** `internal/tui3/*_test.go`. Committed harness covering J01–J08 actions by frozen names; probes “group chats in folders”, “is /folder a logical folder?”, “also in two folders”. Live tmux is `t-w1-live`, not this lane’s pass.

## Isolation

`CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only.

# Wave 2 contracts

Coordination freeze 19 September 2026. Wave 1 types, methods, iota values, origins, and person-facing Wave 1 spellings stay. Amend only through a PlanDB note and a CONTRACTS.md patch. Do not invent grant or execution tables, a keyword-only filer, or a private HTTP client. A Folders tab-bar place is **Folders-entry** (below), not this freeze. Participants and deliveries are Wave 3 (v4), not this freeze.

Wave 2 work package: `docs/design/collaborative-workspace/issue-2.md` (branch-local). PUBLICATION-POLICY.md: no GitHub issues, comments, or PRs before owner verification. Journeys J09–J18 plus affected J01–J08. Packages are not implemented in this freeze.

## Lane ownership (disjoint)

| Lane | PlanDB | Owns (create/edit) | Must not edit |
|---|---|---|---|
| schema | `t-w2-schema` | `internal/workspace` v2→v3 migration and tables `guidance`, `jobs`, `observations`, `placement_suppressions`, `proposed_actions`; lease + fencing methods | `internal/wsdiscover`, `internal/wsapi`, `internal/tui3`, `internal/session`, `cmd/codeaf`, `internal/roles`, `internal/provider` |
| discover | `t-w2-discover` | **New** `internal/wsdiscover/*`. `home.Join("v3", "discovery.db")`. Cursors, passages, FTS, vectors, rewind/delete. Embedder **interface** + test fake | workspace v3 tables, wsapi, session, tui3, cmd, provider production client |
| embed | `t-w2-embed` | `roles.RoleEmbed` constant + vocabulary; OpenAI-compatible `/embeddings` on `internal/provider` / `internal/lane`; availability inspect (no keys printed); bind the discoverer interface in `cmd/codeaf` only as needed to compile the adapter | workspace internals, tui3 panel files, session turn loop, wsapi validator |
| wsapi | no dedicated Wave 2 task at freeze; fill `internal/wsapi` against these signatures (coordinator may split one) | `InstructFolder`, `EffectiveGuidance`, `SearchEvidence`, `ValidateActionPlan`, `ApplyActionPlan`, `SuppressPlacement`, typed `ActionPlan`. Inject discoverer like Inventory — **do not import** `wsdiscover` if that couples membership to the index | `internal/workspace` internals beyond calling new Store methods; tui3; session; cmd |
| session | `t-w2-session` | `internal/session`: construct/use `Config.ConversationHistory` when `Memory` is nil; hybrid `search_conversations`; load `EffectiveGuidance` on the main turn; guidance checkpoint; register `RoleOrganize`; tool sentence that search is no longer lexical-only; organizer runner the tick calls | `internal/workspace` schema, `internal/wsdiscover` files, tui3 panel files except filling Options |
| tui | `t-w2-tui` | Folder detail instructions section; indexing progress (software); why-here organizer copy; quiet filing; delayed/degraded copy. Additive `tui3.Folders` methods and DTOs. Keep Wave 1 verbs | `internal/workspace`, `internal/wsapi`, `internal/session`, `cmd/codeaf` |
| tick / wiring | session + `cmd/codeaf` | After a journalled substantive user message, enqueue `observe_and_organize` (coalesce by source revision). Process on the existing standing pass (`standing.Interval` = 5m) and `codeaf tick`. Extend `cmd/codeaf/folders_adapter.go` + `v3SearchSeam` / `ConversationHistory` construction. Compile-time `var _ tui3.Folders` still holds | do not change `standing.Interval`; do not add a second daemon |
| proof | `t-w2-proof` | `internal/manual/chat/` denials of automatic organization and inherited instructions; probes; `internal/e2e/tuiwords_test.go` needles; TRY.md Wave 2 | product logic; `internal/tui3/*_test.go` |

Integration onto `feat/collaborative-workspace-0918` in order: schema → wsapi → discover → embed → session → tui → tick/wiring → proof. Discover and embed may land in parallel after schema’s Store methods exist or against this freeze’s signatures. Wiring’s adapter must implement every TUI Folders method, including the Wave 2 additives.

New functions in every Wave 2 package stay at cyclomatic complexity ≤ 15. No dummy production fallbacks: a missing embedder, a failed Open, or a down organizer is a labelled error/deferral, never zeros that claim success, never a fake `100%`, never an empty successful RootView, never membership from a BM25 score.

## Product names (person-facing)

Wave 1 names stay (`folders`, `/folders`, `n f e m w x`, emptiness whisper). Additive:

| Surface | Spelling |
|---|---|
| Folder-detail heading | `instructions` |
| Empty instructions whisper | `standing guidance for chats in this folder` (no “no instructions yet”) |
| Slash | `/folders instruct <name-or-id> <text>` — person origin; not an alias of `/folder` |
| Verb strip (folder row) | `i` instruct this folder. If that chord collides, rename the chord, not the action. |
| Why-here, organizer origin | origin `organizer` plus the evidence reason; no approval card |
| Embedder/organizer down | `discovery delayed` — never `checked` |
| Expansion-only retrieval | `degraded` |
| Indexing | software counters (passages/vectors); never a fake `100%` while work remains |
| Feature switch | profile row `workspace.organize` (default on once v3 exists). Off pauses automatic placements; manual folders, history, and chat stay |

Purpose text on a collection is a description, not an instruction. Agent-inferred observations are not adopted instructions.

## `internal/workspace` (schema v3)

Keep every Wave 1 method and type. Listing remains read-only and does **not** migrate. Writes call `ensureSchema`, which migrates v1→v2→v3 or v2→v3 or creates v3.

`PRAGMA user_version` after a successful Wave 2 write-path migration is **3**. `application_id` stays `0x4146434c`. Foreign/future/corrupt still refuse. Test v1-to-v3 and v2-to-v3. Wave 2 still does not add grant or execution tables. Participants and deliveries are Wave 3 (see Wave 3); grant/execution_bindings are Wave 4.

```go
const (
    JobPending    = "pending"
    JobLeased     = "leased"
    JobCompleted  = "completed"
    JobDeferred   = "deferred"
    JobFailed     = "failed"
    JobCancelled  = "cancelled"
    JobOrganize   = "observe_and_organize"
    GuidanceActive = "active"
    GuidanceSuperseded = "superseded"
)

// ScopeID empty means virtual Root. Root is still not a collections row.

type Guidance struct {
    ID, ScopeID, Text, Status, Origin, Actor, SourceRef, Supersedes string
    Revision int
    CreatedAt, UpdatedAt string // RFC3339
}

type Job struct {
    ID, Type, State, Owner, Fence, CauseID, CoalesceKey, ChatID, SourceRev, Error string
    Attempt int
    LeaseUntil, CreatedAt, UpdatedAt string
}

type Observation struct {
    ID, ChatID, SourceRev, Purpose, Body, Evidence, Model, PromptVersion string
    CreatedAt string
}

type Suppression struct {
    CollectionID, Kind, RefID, SessionID, EvidenceHash, Actor, At string
}

type Proposal struct {
    ID, ChatID, SourceRev, PlanJSON, Result, IdempotencyKey string
    CreatedAt string
}

func (s *Store) PutGuidance(ctx context.Context, g Guidance) (Guidance, error)
func (s *Store) ListGuidance(ctx context.Context, scopeID string) ([]Guidance, error)
func (s *Store) EnqueueJob(ctx context.Context, job Job) (Job, error) // coalesce on Type+CoalesceKey while pending/leased
func (s *Store) LeaseJob(ctx context.Context, types []string, owner string, until string) (Job, error)
func (s *Store) FinishJob(ctx context.Context, id, fence, state, detail string) error // completed|deferred|failed|cancelled
func (s *Store) HeartbeatJob(ctx context.Context, id, fence, until string) error
func (s *Store) PutObservation(ctx context.Context, o Observation) (Observation, error)
func (s *Store) PutProposal(ctx context.Context, p Proposal) (Proposal, error)
func (s *Store) Suppress(ctx context.Context, collectionID string, ref Ref, evidenceHash string, p Provenance) error
func (s *Store) IsSuppressed(ctx context.Context, collectionID string, ref Ref, evidenceHash string) (bool, error)
```

`LeaseJob` moves `pending` → `leased`, mints a fencing token, sets `Owner` + `LeaseUntil`. A mismatched fence refuses (`ErrConflict`, word `fence`). An expired lease returns to `pending`; expiry is not “worker dead” and does not mark external effects done. `FinishJob` requires the current fence. States allowed from `leased`: `completed`, `deferred`, `failed`, `cancelled`. There is no other job state.

A membership/guidance/suppression change, its audit event, and any outbox job this slice owns commit in **one** writer transaction. No model or network I/O inside that transaction.

Root guidance lives in `guidance` with empty `ScopeID`, not as a new `collections` row. `root_state.revision` still increments on mutating writes, including guidance and suppressions that affect Root.

## Job lease + fencing

Enqueue `observe_and_organize` only after the substantive user message is **already** in the journal. Coalesce by `CoalesceKey` = chat id + source revision: a second enqueue for the same key while `pending` or `leased` is a no-op success, not a second job. Organize **outside** the DB transaction; revalidate expected revisions before apply.

Tick and the in-window standing pass both call the same runner. Do not change `standing.Interval`. Bounded attempts, budget reservation, and fairness with foreground chat are required; exhausting the background budget defers the job (`deferred`) and stays visible. `workspace.organize` off cancels or defers automatic apply; it does not drop the journal or refuse manual `Add`/`Remove`.

## `internal/wsdiscover` (discovery.db)

Path: `home.Join("v3", "discovery.db")`. Rebuildable. Not membership truth. Journals remain authoritative.

Cursor identity is **not a byte offset**:

```go
type Cursor struct {
    SessionID    string
    Generation   int    // source generation; rewind/rewrite bumps this
    Ordinal      int64  // message/passage ordinal inside that generation
    ContentHash  string // hash of the indexed bytes
}

type Passage struct {
    SessionID, SourceRef, ContentHash, Speaker, Text string
    Generation int
    Ordinal    int64
    // Vector is empty until embedded. Model/Version/Dimension identify the generation.
    Model, Version string
    Dimension      int
    Vector         []float32
}

type Embedder interface {
    Embed(ctx context.Context, texts []string) (vectors [][]float32, model, version string, dim int, err error)
    Available(ctx context.Context) (model string, ok bool, err error)
}

type IndexProgress struct {
    Passages, Vectors int
    Cursor            Cursor
    Delayed, Degraded bool
    Detail            string // person-facing; empty when caught up
}
```

Crash after journal append and before index update is reconciled once (A13). Rewind or explicit source deletion invalidates derived rows for that generation; abandoned **proposals** in collections.db remain unless the source was deleted (A22). Unavailable ≠ deleted ≠ absent.

Vector lookup is brute-force cosine behind this interface for the first release. A model/version/dimension mismatch must not compare vectors. Tests may inject a fake Embedder; production must bind the shipped provider adapter or report unavailable — never a silent zero-vector success.

## `ActionPlan` (typed organizer output)

Organizer output is this type, not `map[string]any`, not SQL. `no-action` is first-class.

```go
const (
    PlanNoAction     = "no-action"
    PlanAdd          = "add"
    PlanRemove       = "remove"
    PlanMove         = "move"
    PlanCreateFolder = "create-folder"
)

type EvidenceRef struct {
    SourceRef, PassageHash, Quote string
}

type Action struct {
    Kind                         string
    CollectionID, FromID, ToID   string
    Ref                          workspace.Ref
    FolderName, Purpose          string
    ParentIDs                    []string
    ExpectedRevision             int // collection; zero = no check (forbidden on organizer apply)
    ExpectedFrom, ExpectedTo     int
    ExpectedRootRevision         int
    ExpectedGuidanceRevision     int
    Evidence                     []EvidenceRef
    Reason                       string
    IdempotencyKey               string
}

type ActionPlan struct {
    Kind, ChatID, SourceRev, Model, PromptVersion string
    Actions []Action // Kind==PlanNoAction ⇒ Actions empty
    Evidence []EvidenceRef
    Degraded bool
}
```

Validator (`wsapi.ValidateActionPlan`) refuses unless all of:

1. The plan is the typed struct (unknown `Kind` is invalid).
2. `no-action`: `Actions` empty; Apply records a `proposed_actions` row and writes **no** membership.
3. Every collection/ref ID exists (or is being created in an earlier action of the same plan, in order).
4. DAG: `create-folder` / nest cannot cycle; uses the existing store cycle check.
5. Freshness: organizer apply always sends non-zero expected revisions; mismatch → `workspace.ErrConflict` (word `revision`), no partial apply.
6. Same authority: applied membership is `OriginOrganizer` (actor = organizing session). The plan cannot stamp `person`. Automatic **remove** only of edges whose latest origin is `organizer` or `system_fallback`. Person placements stay.
7. Suppressions: key = collection id + object (`kind`,`ref_id`,`session_id`) + **evidence hash**. Identical evidence cannot re-add. New evidence (new hash) may reconsider with a new reason (J11).
8. `create-folder` runs an equivalent-name/purpose check first; weak overlap is `no-action`, not a folder per pair (J12). A cited relationship without a folder edge is `no-action` plus evidence, not a manufactured membership.
9. Similarity scores are never membership. A plan whose only support is lexical overlap, with no `RoleOrganize` result, is refused (**keyword-only filer refused**).
10. `Degraded` plans may cite and search; they must not apply new membership. Delayed/failed organize invents none (J17).

`ApplyActionPlan` validates, then applies in one writer transaction per action-set (membership + events + proposal row + optional job ack). Revalidate immediately before that transaction if organize ran outside it.

Evidence hash: hex SHA-256 of the canonical, sorted passage hashes in the action’s `Evidence`. Suppression stores that digest.

## `internal/wsapi` (service additives)

No import of `session`, `tui3`, `provider`, `run`. Still imports `workspace`. Discovery is an injected interface (same pattern as `Inventory`).

Keep every Wave 1 method. Additive:

```go
type InstructRequest struct {
    ScopeID, Text string
    Provenance    workspace.Provenance // person origin only
}

type GuidanceItem struct {
    ScopeID, Name, Text, Origin, Actor, SourceRef string
    Revision int
}

type GuidanceRev struct {
    ScopeID  string
    Revision int
}

type GuidanceSnapshot struct {
    RootRevision int
    Guidance     []GuidanceRev
}

type EffectiveGuidance struct {
    Items    []GuidanceItem // parents + ancestors + Root, deduped by ScopeID; Root once
    Snapshot GuidanceSnapshot
    Conflict bool // incompatible instructions; do not silently pick recency/path
}

type SearchQuery struct {
    Query, SessionID, ConversationID string
    Limit int
}

type SearchHit struct {
    Ref, SessionID, Passage, ScoreKind string // ScoreKind: bm25 | embed | expansion
    Degraded bool
}

type ApplyResult struct {
    PlanID string
    Applied []workspace.MembershipEvent // empty for no-action
}

type IndexView struct {
    Passages, Vectors int
    Delayed, Degraded bool
    Detail            string
}

func (s *Service) InstructFolder(ctx context.Context, req InstructRequest) (GuidanceItem, error)
func (s *Service) EffectiveGuidance(ctx context.Context, conversationID string) (EffectiveGuidance, error)
func (s *Service) SearchEvidence(ctx context.Context, q SearchQuery) ([]SearchHit, error)
func (s *Service) ValidateActionPlan(ctx context.Context, plan ActionPlan) error
func (s *Service) ApplyActionPlan(ctx context.Context, plan ActionPlan) (ApplyResult, error)
func (s *Service) SuppressPlacement(ctx context.Context, collectionID string, ref workspace.Ref, evidenceHash string, p workspace.Provenance) error
func (s *Service) IndexProgress(ctx context.Context) (IndexView, error)
```

`InstructFolder` requires `OriginPerson`. Inferred/organizer text is an Observation, not Guidance. Compatible refinements compose; incompatible items set `Conflict` and do not silently win. `EffectiveGuidance` loads known applicable instructions **directly** — not by similarity ranking (J13). Shared placement does not copy sibling chats or merge folders (J14).

`SearchEvidence` is hybrid: lexical candidates plus embedding (or labelled expansion) candidates, deduped by source id. Existing opaque `chat:` refs still open the cited exchange.

Person-remove of an organizer placement calls `RemovePlacement` **and** `SuppressPlacement` in one store transaction (wiring/TUI). Restart + tick with unchanged evidence must not re-add (J11).

## Roles, embed, spend

```go
const (
    RoleOrganize roles.Role = "organize" // registered; default TierLow
    RoleEmbed    roles.Role = "embed"    // PIN, not registered to a text tier
)
```

- `RoleOrganize` is declared in `internal/roles` vocabulary and **registered** from `internal/session` (owns the call). Default `TierLow`. Restructuring or instruction conflicts pass a high floor into `callRoleChecked`. **Never** `RoleAuditor`. Spend via `callRoleChecked` / `callPurpose(roles.RoleOrganize)`.
- `RoleEmbed` is a pin like `RoleImageGen` / `RoleSpeech`: in vocabulary, **not** `Register`’d (an embeddings endpoint is not a text tier). Configurable model ID through `roles.PinKey(RoleEmbed)`. Availability is inspected without printing keys. OpenAI-compatible `POST /embeddings` on the existing provider/lane. Spend tagged `callPurpose(roles.RoleEmbed)` / `provider.WithCallTag` on that request. No private HTTP client.
- Tests may fake the Embedder interface. Production must not ship a stub that returns success with empty vectors.

Expanded-query lexical fallback is **degraded capability** when the embedder is down, not a replacement. Hybrid search still runs; UI and hits say `degraded` / `discovery delayed`. It must not claim the workspace was checked and must not file.

## Session

`ConversationHistory` is constructed when `Memory` is nil (A18 / J15). `cmd/codeaf` splits `v3SearchSeam` from `v3Memory`: memory off leaves `Config.Memory` nil (no `remember`, no reflex) and still sets `Config.ConversationHistory` to a read-only history reader. `search_conversations` and the search place are present whenever that reader is. Nil history still means the verb is absent.

Keep `ConversationHistoryReader`. Hybrid search **adds** embedding/expansion candidates beside existing BM25; it does not replace lexical query semantics. Update the tool sentence `Search is lexical, not semantic` in the same change as the hybrid path. Historical text stays evidence, not instructions. Opaque `chat:` reads stay.

```go
type GuidanceSource interface {
    Effective(ctx context.Context, conversationID string) (EffectiveGuidance, error)
}

// Config.Guidance is nil when folders/wsapi are unavailable. The main turn
// still runs; it simply has no folder instructions. The DTO may be defined in
// session (same fields as wsapi.EffectiveGuidance) so wsapi does not import session.
```

Main-turn order (software, not the model):

1. Persist the user message in the journal.
2. Load `EffectiveGuidance` for current parents + Root (no ranking).
3. Main talk model answers; it may call `search_conversations` (hybrid).
4. **Guidance checkpoint** before affected mutations: recompute guidance; if the snapshot’s revisions/conflict bit changed, pause **only** the affected write/work commitment. Unrelated reads and tools continue (A7/A9 / J13).
5. Enqueue `observe_and_organize` (coalesce by source revision).

Affected mutations: membership file/unfile/move, `InstructFolder`, and work commitment (`StartTask` / equivalent). Not: search, list, read, why-here.

The `folders` tool does **not** gain `instruct`. A model cannot mint person-origin guidance.

Organizer path (tick/host, outside the collections writer txn): lease job → `RoleEmbed` new passages (or labelled degraded expansion) → lexical + vector candidates → `RoleOrganize` (evidence, hierarchy, suppressions, guidance consequences) → typed `ActionPlan` → `ValidateActionPlan` + `ApplyActionPlan`.

## TUI (`internal/tui3`)

Wave 1 `Folders` methods stay, in the same order. Additive methods and DTOs are exported so `cmd/codeaf` can implement them. Still no `workspace.Ref` / `wsapi.Folder` in this package. Snapshot still on the home **beat**, never in `View`. Preview still launches **no** AI. 80-col: instructions readable; no per-row model calls while scrolling.

```go
type FolderInstruction struct {
    ScopeID, Text, Origin, Actor, At string
    Revision int
}
type FolderIndex struct {
    Passages, Vectors int
    Delayed, Degraded bool
    Detail string
}
// Folders additive:
InstructFolder(ctx context.Context, id, text string) error
FolderGuidance(ctx context.Context, id string) ([]FolderInstruction, error)
IndexProgress(ctx context.Context) (FolderIndex, error)
```

Nil `Options.Folders` still refuses mutations, including `i` / `/folders instruct`. Why-here already carries `Origin`; organizer copy must show it. Quiet filing: a new organizer placement appears without an approval card. Indexing progress is software; a catch-up state must not draw `100%` while passages remain. Embedder down uses `discovery delayed` / `degraded`, never `checked`.

## Tick / wiring (`cmd/codeaf`)

- Construct `session.Config.ConversationHistory` even when `config.MemoryEnabledAt` is false.
- Point `v3SearchSeam` at that history reader, not at `v3Memory`.
- After journal append of a substantive user message, enqueue `JobOrganize` with `CoalesceKey` = chat id + source revision.
- `codeaf tick` and the in-window standing pass run the organizer runner. Organize outside the DB txn; revalidate; apply.
- `foldersAdapter` implements the additive TUI methods (`var _ tui3.Folders` still compiles). `InstructFolder` stamps `OriginPerson`. Person-remove of an organizer edge also suppresses.
- Bind the real Embedder when the pin/lane is available; otherwise leave discovery `Delayed` — do not bind a production stub.

## Tests the lanes owe before handoff

- schema: v1 list without migrate; v1→v3 and v2→v3 on first write; lease/fence mismatch refuses; expired lease returns to pending; coalesce; suppress key; Wave 2 schema does not create grant or execution tables (participants/deliveries wait for Wave 3 v4); complexity ≤ 15.
- discover: cursor identity is not a byte offset; crash/replay (A13); rewind/delete invalidates derived rows (A22); version/dimension mismatch does not compare vectors; fake Embedder in tests only.
- embed: `/embeddings` adapter; availability inspect without printing keys; spend tagged; pin not a text-tier Register; degraded path labelled, not a silent lexical filer.
- wsapi: `no-action` writes no membership; validator rules 1–10; InstructFolder person-only; EffectiveGuidance Root-once + conflict bit; SuppressPlacement blocks identical evidence; ApplyActionPlan expected-revision atomic.
- session: ConversationHistory set when Memory is nil; `search_conversations` present with memory off; hybrid adds candidates beside BM25; tool sentence updated; checkpoint pauses only affected mutations; RoleOrganize through callRoleChecked, never RoleAuditor.
- tui: instructions section + empty whisper; indexing not fake 100%; organizer why-here; delayed/degraded copy; no organize on hover; Wave 1 verbs intact.
- tick/wiring: enqueue after journal; coalesce; compile-time Folders; history seam split from memory.
- proof: delete denials of automatic organization and inherited instructions; state limits (no keyword-only file; suppressions; memory-off; delayed vs checked); TRY.md Wave 2; tuiwords needles. Live tmux is not this lane’s pass.

## Isolation

Same as Wave 1. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Measure scale in passages/vectors/memory/latency, not chat count alone (J18).

# Wave 3 contracts

Coordination freeze 19 September 2026. Wave 1 and Wave 2 types, methods, iota values, origins, and person-facing spellings stay. Amend only through a PlanDB note and a CONTRACTS.md patch. Grant and execution tables are Wave 4 (v5), not this freeze. Do not invent a manager subclass, a second messaging bus, or planner/critic product types. A Folders tab-bar place is **Folders-entry** (below), not this freeze.

Wave 3 work package: `docs/design/collaborative-workspace/issue-3.md` (branch-local). PUBLICATION-POLICY.md: no GitHub issues, comments, or PRs before owner verification. Journeys J19–J26 plus affected earlier journeys. Packages are not implemented in this freeze.

Owner clarification supersedes any mandatory group-chat-only reading: an existing ordinary chat coordinates independent chats. A new group chat is **optional**.

## Lane ownership (disjoint)

| Lane | PlanDB | Owns (create/edit) | Must not edit |
|---|---|---|---|
| schema | `t-w3-schema` | `internal/workspace` v3→v4 migration and tables `participants`, `deliveries`; actor mint; delivery ack | `internal/wscollab`, `internal/wsapi`, `internal/tui3`, `internal/session`, `cmd/codeaf`, `internal/enginehost` |
| collab | `t-w3-collab` | **New** `internal/wscollab/*`. One envelope/outbox for direct, fan-out, and shared-discussion. `JournalSeam` / `HostLocator` / `Store` interfaces + fake store in tests | workspace schema files, wsapi, tui3, session, cmd |
| wsapi | `t-w3-wsapi` | `CoordinateSelected`, `ManageFolder`, `Deliver`, `InviteToDiscussion`, `CreateDiscussion`, `InspectScope`, `PauseCoordination`. Inject collaborator like Inventory — **do not import** `wscollab` | `internal/workspace` internals beyond calling new Store methods; tui3; session; cmd |
| host | `t-w3-host` | `internal/enginehost`: wake/reconnect or durable pending; bind `wscollab.HostLocator`; `session.RegisterCollabRouter` from the host/cmd wire | tui3 panel files; workspace schema |
| session | `t-w3-session` | mailbox stays local; `RegisterCollabRouter` + `Config.Collab`; coordinator tools (read/discuss/organize; delegated execute is Wave 4); assignment law: representative text is `fromAgent` | `internal/workspace` schema, `internal/wscollab` files, tui3 panels |
| tui | `t-w3-tui` | Mark members as convenience; visible sent/request/reply with source links; participant labels; 80-col sequential. New `tui3.Collab` on `Options` | `internal/workspace`, `internal/wsapi`, `internal/session`, `cmd/codeaf` |
| proof | `t-w3-proof` | `internal/manual/chat/` inter-chat communication denial; probes; `internal/e2e/tuiwords_test.go` needles; TRY.md Wave 3 | product logic; `internal/tui3/*_test.go` |

Integration onto `feat/collaborative-workspace-0918` in order: schema → collab → wsapi → host → session → tui → proof. Schema and collab may land in parallel against this freeze (collab tests inject a fake `Store`). Wsapi waits on both doors. Host waits on collab. Session waits on wsapi+collab. Wiring’s adapters must implement every TUI Folders method (Wave 1+2 still compile) **and** `var _ tui3.Collab`.

New functions in every Wave 3 package stay at cyclomatic complexity ≤ 15. No dummy production fallbacks: a missing router, a retired host, or a nil `Config.Collab` is a labelled pending/absence, never a fake delivered line, never a coordinator transcript impersonating two speakers, never `fromPerson` minted from model text.

## Product names (person-facing)

Wave 1 and Wave 2 names stay. Additive:

| Surface | Spelling |
|---|---|
| Coordination | an ordinary chat; never a “manager” product object or a special collaboration mode |
| Optional separate chat | a **discussion** — not a “group chat” product entity. Creating one is optional. |
| Direct | `request` / `reply` with a source link |
| Fan-out | `sent` separately; one receipt per recipient |
| Joint | participant labels on a normal chat |
| Mark members | convenience; not a required ritual. Primary path is natural-language “coordinate these” |
| Offline recipient | waiting; the line appears **once** on resume |
| Delivery machinery | `accepted` / `recorded` / `processed` are store/test words, never painted. The person sees sent, request, reply |
| Pause | `pause coordination` — stops **new** autonomous decisions and launches. Closing a view does not pause. Stopping existing work is a separate `stop work` (Wave 4) |
| Archive | suppresses automatic wake-ups; history remains |
| Planner / critic | configurable role labels a person names, not product entities |
| Escalation | parents may join; **not** “always ask after two turns” as a ban on parent join |
| Empty participants | emptiness law: nothing, never `0 participants` |

No planner/critic product types, no manager subclass. A Folders tab-bar place is **Folders-entry** (below), not this freeze. Icons through `tokens` only.

## `internal/workspace` (schema v4)

Keep every Wave 1 and Wave 2 method and type. Listing remains read-only and does **not** migrate. Writes call `ensureSchema`, which migrates v1→v2→v3→v4 or any prefix of that, or creates v4.

`PRAGMA user_version` after a successful Wave 3 write-path migration is **4**. `application_id` stays `0x4146434c`. Foreign/future/corrupt still refuse. Test v1-to-v4 and v3-to-v4. Grant and execution tables (`grants`, `execution_bindings`) are Wave 4 (see Wave 4).

Only two new tables: `participants`, `deliveries`. Scope is **not** a third table: selected snapshots and folder-dynamic scope live on the coordinator’s participant row (`ScopeKind`, `FolderID`, `SnapshotJSON`).

Actor IDs are minted by software (`mintID`, 16-byte hex, same as Wave 2 jobs/guidance), **never** by the model. `PutParticipant` / `PutDelivery` mint when `ID` is empty. A tool argument named `actor_id` is ignored on write. The model cannot supply `from_person`.

```go
const (
    ParticipantActive   = "active"
    ParticipantPaused   = "paused"
    ParticipantArchived = "archived"
    ActorKindChat       = "chat"
    ActorKindRole       = "role"   // planner/critic are labels in Role, not kinds
    ActorKindFolder     = "folder"
    ScopeSelected       = "selected"
    ScopeFolderDynamic  = "folder-dynamic"
    DeliveryPending     = "pending"
    DeliveryAccepted    = "accepted"
    DeliveryRecorded    = "recorded"
    DeliveryProcessed   = "processed"
    PatternDirect       = "direct"
    PatternFanout       = "fan-out"
    PatternDiscussion   = "discussion"
)

// SnapshotJSON is a canonical JSON array of conversation IDs for ScopeSelected.
// Empty for folder-dynamic (descendants are resolved at read time, never snapshotted).

type Participant struct {
    ID, DiscussionID, ActorID, Kind, Role, SourceChatID, Status string
    ScopeKind, FolderID, SnapshotJSON                           string
    Origin, Actor                                               string
    CreatedAt, UpdatedAt                                        string // RFC3339
}

type Delivery struct {
    ID, CauseID, FromChatID, ToChatID, Pattern, State, Body string
    Origin, ActorID, DiscussionID, IdempotencyKey           string
    Attempt                                                 int
    CreatedAt, AcceptedAt, RecordedAt, ProcessedAt, UpdatedAt string
}

func (s *Store) PutParticipant(ctx context.Context, p Participant) (Participant, error)
func (s *Store) ListParticipants(ctx context.Context, discussionID string) ([]Participant, error)
func (s *Store) SetParticipantStatus(ctx context.Context, id, status string) error
func (s *Store) PutDelivery(ctx context.Context, d Delivery) (Delivery, error)
func (s *Store) GetDelivery(ctx context.Context, id string) (Delivery, error)
func (s *Store) ListDeliveries(ctx context.Context, causeID string) ([]Delivery, error)
func (s *Store) ListPendingDeliveries(ctx context.Context, toChatID string) ([]Delivery, error)
func (s *Store) ListChatTraffic(ctx context.Context, chatID string) ([]Delivery, error)
func (s *Store) AckDelivery(ctx context.Context, id, state string) (Delivery, error)
```

`AckDelivery` advances **one** step along `pending → accepted → recorded → processed`. A skip (pending→recorded, accepted→processed, pending→processed) refuses (`ErrInvalid`, word `state`). The three later facts are different timestamps: `AcceptedAt` is the queue, `RecordedAt` is the recipient journal, `ProcessedAt` is the finished invocation/turn. A row may be `accepted` with empty `RecordedAt`. Tests must not treat any one of them as “sent”.

`Delivery.ID` is the durable id written into the recipient journal — the same role as mailbox `deliveryID`, not a second scheme. Software mints it once at enqueue; retries reuse it. Fan-out: **one ID per recipient**, shared `CauseID`. Non-empty `IdempotencyKey` is unique; a second enqueue with the same key is a no-op success that returns the existing row.

A membership/guidance/participant/delivery-ack change this slice owns still commits in **one** writer transaction. No model or network I/O inside that transaction.

## `internal/wscollab` (one router)

New package. One durable envelope/outbox for **direct, fan-out, and shared-discussion**. Do not build three buses or three chat types. Routing destinations and participant/scope records express the difference.

Does **not** import `session`, `tui3`, `wsapi`, `provider`, `run`. Defines the interfaces it needs; tests inject a fake `Store` until schema lands. Production wire maps `workspace.Delivery` onto `Envelope`.

```go
type Envelope struct {
    DeliveryID, CauseID, FromChatID, ToChatID, Pattern, Body string
    Origin, ActorID, DiscussionID, IdempotencyKey            string
}

type Receipt struct {
    DeliveryID, CauseID, ToChatID, State string // pending|accepted|recorded|processed
}

// JournalSeam is the recipient single-writer append the host binds.
// Recorded is mailbox hasRecorded: does this journal already hold deliveryID?
// Append writes the line under that id. The router never bypasses this seam.
type JournalSeam interface {
    Recorded(ctx context.Context, deliveryID string) (bool, error)
    Append(ctx context.Context, env Envelope) error
}

// HostLocator finds the engine host that owns a conversation.
// Live=false means the host is retired or unreachable: the envelope stays pending.
// Citing a chat as evidence must not call Route.
type HostLocator interface {
    Route(ctx context.Context, conversationID string) (seam JournalSeam, live bool, err error)
}

type Store interface {
    Put(ctx context.Context, env Envelope) (Envelope, error)
    Get(ctx context.Context, deliveryID string) (Envelope, state string, err error)
    Ack(ctx context.Context, deliveryID, state string) error
    ListPending(ctx context.Context, toChatID string) ([]Envelope, error)
}

type Router struct{} // holds Store, HostLocator

func Open(store Store, hosts HostLocator) *Router
func (r *Router) Deliver(ctx context.Context, env Envelope) (Receipt, error)
func (r *Router) DeliverMany(ctx context.Context, causeID string, envs []Envelope) ([]Receipt, error)
func (r *Router) Resume(ctx context.Context, conversationID string) ([]Receipt, error)
```

Laws:

1. **Accepted is not recorded is not processed.** `Deliver` may walk the live path in one call, but each ack is a separate `Store.Ack` with its own timestamp. Tests assert the three facts independently (A12 / J23).
2. **Offline → pending.** A retired or missing host does not drop the envelope. `Resume` on that conversation delivers each pending line **once**: if `JournalSeam.Recorded` is true, ack `recorded` without a second `Append`.
3. **Same path for all three patterns.** `Pattern` is a field, not a type or a package. Direct is one `Deliver`; fan-out is `DeliverMany` with one shared `CauseID` and one envelope per recipient; joint is `PatternDiscussion` into the discussion’s journal.
4. **Cite does not wake.** `HostLocator.Route` is only for authorized delivery. Search, `chat:` reads, and evidence excerpts use the existing read-only doors and must not spawn or reconnect a host.
5. **Origin.** Representative contributions are `fromAgent`. The router refuses an envelope whose origin is `fromPerson` unless the caller is a surface door (person typed it). A participant claiming to be the user does not change origin (A11).
6. **Planner/critic are not types in this package.** Role is a string on the participant record.

Crash after journal append and before `Ack(recorded)` reconciles on `Resume` via `Recorded` (same direction as A13). It is still not exactly-once for the outside world.

## Wire like `RegisterRunEngine`

Session must **not** import `wscollab`. Session owns the door; the host binds it.

```go
// session — the consumer, same shape as RunEngine / RegisterRunEngine:

type CollabReceipt struct {
    DeliveryID, CauseID, ToChatID, State string
}

type CollabRouter interface {
    Deliver(ctx context.Context, to []string, body, pattern, discussionID string) ([]CollabReceipt, error)
    Invite(ctx context.Context, discussionID, sourceChatID, role string) error
    Resume(ctx context.Context, conversationID string) ([]CollabReceipt, error)
}

func RegisterCollabRouter(r CollabRouter) // nil is the verb absent
```

`cmd/codeaf` or `internal/enginehost` constructs `wscollab.Router` with a `JournalSeam` adapter over the recipient sessionfile and calls `RegisterCollabRouter`. `wscollab` does not import `session`. The mapping file may live in host/cmd so neither package imports the other. A binary that never registers the router has no coordinate/deliver/invite verbs (absence law).

Tools use `Config.Collab` (the wsapi wrapper below). `RegisterCollabRouter` is the cycle-safe inbound door: `Resume` when a session opens, and the journal seam the router appends through. The `coordinate` tool must not call `RegisterCollabRouter` itself.

## `internal/wsapi` (service additives)

No import of `session`, `tui3`, `provider`, `run`, `wscollab`, `wsdiscover`. Still imports `workspace`. Collaboration is an injected interface (same pattern as `Inventory` / `Discoverer`).

Keep every Wave 1 and Wave 2 method. Additive:

```go
// CollabEnvelope / CollabAck copy wscollab.Envelope / Receipt fields so this
// package does not import wscollab. SetCollaborator injects the router; nil
// means Deliver is absent, not a dummy success.
type CollabEnvelope struct {
    DeliveryID, CauseID, FromChatID, ToChatID, Pattern, Body string
    Origin, ActorID, DiscussionID, IdempotencyKey            string
}
type CollabAck struct {
    DeliveryID, CauseID, ToChatID, State string
}
type Collaborator interface {
    Deliver(ctx context.Context, env CollabEnvelope) (CollabAck, error)
    DeliverMany(ctx context.Context, causeID string, envs []CollabEnvelope) ([]CollabAck, error)
    Resume(ctx context.Context, conversationID string) ([]CollabAck, error)
}

type CoordinateRequest struct {
    CoordinatorID string
    ChatIDs       []string // marked IDs; order preserved
}

type ManageFolderRequest struct {
    CoordinatorID, FolderID string
}

type ScopeView struct {
    ID, Kind, CoordinatorID, FolderID string
    ChatIDs                           []string // resolved members
    Revision                          int
}

type DeliverRequest struct {
    FromChatID, Body, Pattern, CauseID, IdempotencyKey, DiscussionID string
    ToChatIDs []string // one = direct; many = fan-out
}

type DeliverReceipt struct {
    DeliveryID, CauseID, ToChatID, State string
}

type InviteRequest struct {
    DiscussionID, SourceChatID, Role string
    // Role is a free label. ActorID is minted by the service, never taken from this request.
}

type ParticipantView struct {
    ActorID, DiscussionID, Kind, Role, SourceChatID, Status string
}

type CreateDiscussionRequest struct {
    ChatID, CoordinatorID, Title, IdempotencyKey string
    FolderIDs []string
}

type DiscussionView struct {
    ChatID, Title string
    FolderIDs     []string
}

type ConflictDiscussionView struct {
    ChatID       string
    FolderIDs    []string
    Participants []ParticipantView
    Reused       bool
    Exhausted    bool
}

func (s *Service) SetCollaborator(Collaborator)
func (s *Service) CoordinateSelected(ctx context.Context, req CoordinateRequest) (ScopeView, error)
func (s *Service) ManageFolder(ctx context.Context, req ManageFolderRequest) (ScopeView, error)
func (s *Service) InspectScope(ctx context.Context, coordinatorID string) (ScopeView, error)
func (s *Service) Deliver(ctx context.Context, req DeliverRequest) ([]DeliverReceipt, error)
func (s *Service) InviteToDiscussion(ctx context.Context, req InviteRequest) (ParticipantView, error)
func (s *Service) OpenConflictDiscussion(ctx context.Context, conversationID string) (ConflictDiscussionView, error)
func (s *Service) CreateDiscussion(ctx context.Context, req CreateDiscussionRequest) (DiscussionView, error)
func (s *Service) ListParticipants(ctx context.Context, discussionID string) ([]ParticipantView, error)
func (s *Service) PauseCoordination(ctx context.Context, coordinatorID string) error
```

Scope (A16 / J20):

- `CoordinateSelected` writes `ScopeKind=selected` and `SnapshotJSON` of the given IDs on the coordinator’s participant row. Adding a sibling elsewhere does **not** enlarge `ChatIDs`. `InspectScope` returns that snapshot.
- `ManageFolder` writes `ScopeKind=folder-dynamic` and `FolderID`. `InspectScope` resolves **current** descendants, including future members, shared objects **deduped** once.
- A fifth chat filed in Billing after a selected-four stays out of selected scope and **does** appear once `ManageFolder` is the live responsibility.

`CreateDiscussion` does **not** mint a transcript. Session mints the conversation id (16 hex) and journal first, then calls with that `ChatID` and optional `FolderIDs` via existing `AddPlacement`. Shared placement does not merge the rest of either folder (P8 / J22). Failure after mint leaves the discussion unfiled under Root; retry is idempotent on `IdempotencyKey`.

`InviteToDiscussion` mints `ActorID` in software. Inviting parent representatives into **one** conflict discussion dedupes by `SourceChatID` so there is one participant per distinct ancestor, including at most one Root (A17 / J24). Two turns may be a per-level starting budget, not a prohibition on parent join. Root cannot exceed user delegation. A grant cannot expand itself (Wave 4).

`OpenConflictDiscussion` is the production door for that law. When `EffectiveGuidance` is `Conflict`, software opens or reuses **one** discussion (stable id from the disagreeing folder set), files it in those folders, and invites the triggering chat plus one participant per distinct ancestor including one Root. The first parent to answer is not the boss. Finite rounds and time stop escalation loops; an exhausted room returns `Exhausted` and further Deliver/Invite refuse with `missing authority reaches the person` / unresolved conflict reaching the person. IssueGrant with `CoordinatorID=root` and no issuer is that same checkpoint; Root citing a person grant still cannot expand classes or scope. This is automatic parent escalation, not “always ask the user after two turns.”

`Deliver` with one `ToChatID` is direct; several is fan-out (`PatternFanout`, one receipt each, shared `CauseID`). Joint contributions set `DiscussionID` and `PatternDiscussion`. Nil collaborator: the method is absent (do not return a dummy delivered receipt).

`PauseCoordination` sets the coordinator participant `paused`. It stops **new** Deliver, Invite, and LaunchOrJoin from that coordinator. It does not stop existing work — that is `StopWork` (Wave 4) — and does not refuse `Resume` of already-pending lines. Closing a TUI view must not call it. Archive uses `ParticipantArchived` and suppresses automatic wake-ups.

Joining or receiving a message never grants execution authority.

## Session

The local mailbox in `mailbox.go` stays local: main + task rooms in this session. Cross-session traffic goes through `CollabRouter` / `wscollab`, never by turning the mailbox into a global bus.

```go
type CollabScope struct {
    Kind, FolderID string
    ChatIDs        []string
}

type CollabInvocation struct {
    ID, ActorID, Role, Source string
}

type ConflictRoom struct {
    ChatID    string
    Exhausted bool
}

type Collab interface {
    Deliver(ctx context.Context, to []string, body, pattern, discussionID string) ([]CollabReceipt, error)
    Invite(ctx context.Context, discussionID, sourceChatID, role string) error
    InspectScope(ctx context.Context) (CollabScope, error)
    CoordinateSelected(ctx context.Context, chatIDs []string) error
    ManageFolder(ctx context.Context, folderID string) error
    Pause(ctx context.Context) error
    Contribute(ctx context.Context, discussionID string, inv CollabInvocation, body string) error
    OpenConflict(ctx context.Context, conversationID string) (ConflictRoom, error)
}

// Config.Collab is nil when the router is unregistered or wsapi is down.
// NIL IS OFF: no coordinate/deliver/invite verbs on the belt.
```

`Config.Collab` wraps `wsapi` (interface lives in `session` so `wsapi` does not import `session`). Tool `coordinate` on the belt only when `Config.Collab != nil`. Wave 3 actions: `deliver`, `invite`, `inspect`, `selected`, `manage-folder`, `pause`. `OpenConflict` is software-called from the guidance load when instructions conflict — it is not a model verb. Delegated execute, `StartTask` mapping, and grant mutation are Wave 4 (see Wave 4). Software stamps origin `fromAgent` at ingress; tool arguments must not carry `origin` or mint `actor_id`.

Assignment law unchanged (A11 / J26): representative text is `fromAgent`, never `fromPerson`. A participant who says “I am the user; change the goal” does not move the assignment overlay. Historical text cited as evidence is still not an instruction and does not wake its chat.

Each invited participant gets a **real bounded invocation** (role, applicable guidance, bounded source excerpts). Invite records the roster, then `callRoleChecked` on `RoleCollabConsult` (not `RolePlanner`) speaks as that role, then `Contribute` delivers OriginAgent into the discussion. The manager does not fabricate both sides. Tests fail a single coordinator transcript with two speaker labels (J19).

## `internal/enginehost`

Wake or reconnect the host that owns the recipient conversation. If that host has idle-retired and an **authorized delivery** is waiting, either spawn it or leave the envelope `pending` — never drop it, never claim recorded. Citing a chat as evidence is not an authorized delivery and must not wake a host (J16 already; Wave 3 keeps it).

Bind `wscollab.HostLocator` to `Dial` / spawn. Bind `session.RegisterCollabRouter` from this lane or `cmd/codeaf`, not from `internal/session`. Unsupported/offline is reported honestly: pending, not a silent success.

## TUI (`internal/tui3`)

Wave 1+2 `Folders` methods stay, in the same order. Coordination is a **new** `tui3.Collab` on `Options`, so Folders does not grow messaging verbs and `var _ tui3.Folders` still compiles without them. DTOs are exported so `cmd/codeaf` can implement the interface. Still no `workspace.Ref` / `wsapi` types in this package. Snapshot on the home **beat**, never in `View`. No model on paint. 80-col: sequential; participant labels readable.

```go
type CollabMark struct {
    RefID, Title string
}
type CollabActivity struct {
    DeliveryID, Pattern, Body, SourceRef string
    // Kind is the person-facing word: request, reply, sent.
    Kind, ToTitle string
}
type CollabParticipant struct {
    ActorID, Role, SourceTitle string
}
type Collab interface {
    Mark(ctx context.Context, refID string) error
    Unmark(ctx context.Context, refID string) error
    Marked(ctx context.Context) ([]CollabMark, error)
    CoordinateMarked(ctx context.Context, coordinatorID string) error
    Activity(ctx context.Context, coordinatorID string) ([]CollabActivity, error)
    Participants(ctx context.Context, discussionID string) ([]CollabParticipant, error)
}
```

`Activity` reads `ListChatTraffic` (from or to the coordinator, any state). It does not use `ListPendingDeliveries`. Kind is `request` for a direct outbound, `sent` for fan-out, `reply` for inbound or a joint contribution into this discussion. Store words are never painted.

Nil `Options.Collab`: no mark/coordinate chrome; natural-language coordination still works if `session.Config.Collab` is wired. Marking is never required to coordinate. Joint discussion looks like a normal chat with participant labels. Deliveries arriving must not jump selection or composer (P12 / J06). Preview still launches **no** AI.

No new slash command. `/folders` unchanged. `/folder` stays filesystem.

## Tick / wiring (`cmd/codeaf`)

- Construct `wscollab.Router` with the real workspace store and enginehost locator once those doors exist; register it (`session.RegisterCollabRouter`).
- `foldersAdapter` still implements Wave 1+2 Folders (`var _ tui3.Folders`). A separate adapter implements `tui3.Collab` (`var _ tui3.Collab`).
- After journal append of an authorized inbound delivery, `Resume` that conversation so pending lines appear once.
- Do not change `standing.Interval`. Do not add a second daemon.

## Tests the lanes owe before handoff

- schema: v1 list without migrate; v1→v4 and v3→v4 on first write; actor IDs software-minted (a supplied model-like `actor_id` is not stored as the actor); `AckDelivery` refuses skips; grant/execution_bindings wait for Wave 4 v5; complexity ≤ 15.
- collab: accept/record/process are distinct; dedupe by delivery ID; offline resume once (A12); direct, fan-out, and joint on the same router path; `Route` is not called from an evidence citation; origin `fromPerson` refused on a representative envelope.
- wsapi: selected snapshot does not grow when a sibling is filed elsewhere; `ManageFolder` includes a later descendant once (A16); `CreateDiscussion` placement does not merge folders; Invite dedupes ancestors including one Root (A17); `OpenConflictDiscussion` reuses one room and invites ancestors+Root on Conflict; Root IssueGrant without issuer reaches the person; nil collaborator does not return a dummy receipt; Pause does not stop existing work.
- session: mailbox still local (no bus); `coordinate` absent when `Config.Collab` nil; representative text `fromAgent` cannot move assignment (A11); Wave 3 belt has no execute — delegated execute is Wave 4; per-participant invocation evidence, not one transcript faking two speakers; guidance Conflict calls `OpenConflict` rather than asking after two turns.
- host: retired host → pending, not recorded; authorized delivery may wake/reconnect; evidence citation does not.
- tui: coordinate from an existing ordinary chat; mark is optional; request/reply/sent copy with source links; participant labels; 80-col sequential; P12 selection stability while deliveries arrive; Wave 1 verbs intact.
- proof: delete the inter-chat communication denial; state: ordinary chats coordinate; group chat optional; three patterns; selected snapshot vs whole-folder; Root escalation is hierarchical, not “always ask after two turns”; TRY.md Wave 3 includes a **direct** message, not only a group demo; tuiwords needles. Live tmux is not this lane’s pass.

## Isolation

Same as Wave 1. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Do not prove only one group-chat demo and infer direct/fan-out.

# Wave 4 contracts

Coordination freeze 19 September 2026. Wave 1–3 types, methods, iota values, origins, and person-facing spellings stay. Amend only through a PlanDB note and a CONTRACTS.md patch. Do not invent a third execution store, a manager subclass, or a second daemon. A Folders tab-bar place is **Folders-entry** (below), not this freeze. Coordinators may **execute** when an authentic grant says so.

Wave 4 work package: `docs/design/collaborative-workspace/issue-4.md` (branch-local). PUBLICATION-POLICY.md: no GitHub issues, comments, or PRs before owner verification. Journeys J27–J35 plus affected earlier journeys. Packages are not implemented in this freeze.

This is the last slice. Coordinators in Wave 3 had read/discuss/organize; this freeze adds **explicitly delegated execution**.

## Lane ownership (disjoint)

| Lane | PlanDB | Owns (create/edit) | Must not edit |
|---|---|---|---|
| schema | `t-w4-schema` | `internal/workspace` v4→v5 migration and tables `grants`, `execution_bindings`; actor mint; request-key bind | `internal/wsexec`, `internal/wsapi`, `internal/tui3`, `internal/session`, `cmd/codeaf`, `internal/run`, `internal/plandb` |
| exec | `t-w4-exec` | **New** `internal/wsexec/*`. Launch-or-join, inspect, steer-with-authority, pause work, stop work, observe. `Store` / `Runtime` interfaces + fake runtime in tests | workspace schema files, wsapi, tui3, session, cmd; do not put folder membership in `plandb.ParentID` |
| wsapi | no dedicated Wave 4 task at freeze; fill `internal/wsapi` against these signatures (coordinator may split one) | `IssueGrant`, `RevokeGrant`, `LaunchOrJoin`, `InspectWork`, `SteerWork`, `PauseWork`, `StopWork`, `ObserveWork`. Inject executor like Inventory — **do not import** `wsexec` | `internal/workspace` internals beyond calling new Store methods; tui3; session; cmd |
| session | no dedicated Wave 4 task at freeze; fill against these signatures | assignment law for delegated revisions on **both** `CODEAF_TASK_BELT` roads; `Config.Exec`; coordinate execute actions | `internal/workspace` schema, `internal/wsexec` files, tui3 panels |
| tui | no dedicated Wave 4 task at freeze; fill against these signatures | discuss launch state (software-derived); `pause coordination` vs `stop work` as two verbs; new `tui3.Exec` on `Options` | `internal/workspace`, `internal/wsapi`, `internal/session`, `cmd/codeaf` |
| tick / wiring | session + `cmd/codeaf` | Unattended jobs reuse `codeaf tick` + in-window pass every `standing.Interval` (5m). Bind executor. Compile-time `var _ tui3.Folders` and `var _ tui3.Collab` still hold; add `var _ tui3.Exec` | do not change `standing.Interval`; do not add a second daemon; posture from the **home profile**, never the repo, never `--yolo` |
| proof | `t-w4-proof` | `internal/manual/chat/` denials of delegated execution; probes; `internal/e2e/tuiwords_test.go` needles; TRY.md Wave 4 | product logic; `internal/tui3/*_test.go` |

Integration onto `feat/collaborative-workspace-0918` in order: schema → exec → wsapi → session → tui → tick/wiring → proof. Schema and exec may land in parallel against this freeze (exec tests inject a fake `Store` and a fake `Runtime`). Wsapi waits on both doors. Session waits on wsapi+exec. Wiring’s adapters must implement every TUI Folders and Collab method (Wave 1–3 still compile) **and** `var _ tui3.Exec`.

New functions in every Wave 4 package stay at cyclomatic complexity ≤ 15. No dummy production fallbacks: a missing executor, a nil `Config.Exec`, or a down host is a labelled pending/absence, never a fabricated completed launch, never a second action for the same request key, never `fromPerson` minted from model text.

## Product names (person-facing)

Wave 1–3 names stay. Additive:

| Surface | Spelling |
|---|---|
| Launch | `launch-or-join` — if equivalent work already exists, follow it; otherwise start **one** owned run/task |
| Same issue | two discussions of the same issue are allowed; two unnoticed implementations are not |
| Pause | `pause coordination` — stops **new** deliver/invite/launch. Closing a view does not pause. Closing the TUI does not stop authorized work |
| Stop | `stop work` — explicit separate action on existing work. History remains |
| Remove placement | unfiles the chat; does **not** cancel authorized work and does not delete history |
| Launch state | software-derived run state on the discussion/folder preview; never a model call on paint |
| Offline / unsupported | reported honestly; never a silent success or a fake `100%` |
| Empty work roll-up | emptiness law: nothing, never `0 runs` |

No new slash command. `/folders` unchanged. `/folder` stays filesystem. Icons through `tokens` only.

## `internal/workspace` (schema v5)

Keep every Wave 1–3 method and type. Listing remains read-only and does **not** migrate. Writes call `ensureSchema`, which migrates v1→v2→v3→v4→v5 or any prefix of that, or creates v5.

`PRAGMA user_version` after a successful Wave 4 write-path migration is **5**. `application_id` stays `0x4146434c`. Foreign/future/corrupt still refuse. Test v1-to-v5 and v4-to-v5.

Only two new tables: `grants`, `execution_bindings`. Launch intent is **not** a third table: it is the reserved row on `execution_bindings`, written **before** runtime admission, keyed by `RequestKey`.

Actor IDs are minted by software (`mintID`, 16-byte hex, same as Wave 2/3), **never** by the model. `PutGrant` / `PutBinding` mint when `ID` is empty. A tool argument named `actor_id` or `grant_id` is ignored on write. The model cannot supply `from_person`.

```go
const (
    GrantActive     = "active"
    GrantRevoked    = "revoked"
    GrantSuperseded = "superseded"
    ClassRead       = "read"
    ClassDiscuss    = "discuss"
    ClassOrganize   = "organize"
    ClassExecute    = "execute"
    ClassSteer      = "steer"
    ClassStop       = "stop"
    BindReserved    = "reserved"  // intent + request key written; runtime not called
    BindAdmitted    = "admitted"  // runtime accepted; run-instance id not yet saved
    BindBound       = "bound"
    BindPaused      = "paused"
    BindStopped     = "stopped"
    BindCompleted   = "completed"
    BindFailed      = "failed"
    RoadSessionTask = "session-task" // CODEAF_TASK_BELT unset
    RoadBashRun     = "bash-run"     // CODEAF_TASK_BELT=bash
)

// ActionJSON is a canonical JSON array of action-class strings.
// SnapshotJSON is a canonical JSON array of conversation IDs for ScopeSelected.
// Empty SnapshotJSON + ScopeFolderDynamic resolves descendants at read time.

type Grant struct {
    ID, Goal, CoordinatorID, ScopeKind, FolderID, SnapshotJSON string
    ActionJSON, Issuer, Origin, Actor, Status                    string
    BudgetUSD                                                    float64 // 0 ⇒ daily rail only
    Revision, RevocationRevision                                 int
    CreatedAt, UpdatedAt                                         string // RFC3339
}

type ExecutionBinding struct {
    ID, RequestKey, EquivalenceKey, WorkID, RunInstanceID, Road string
    OwnerChatID, GrantID, CoordinatorID, RuntimeRef             string
    AssignmentRev, GrantRev, State, Fence, Owner                string
    LeaseUntil, CreatedAt, UpdatedAt, BoundAt, AdmittedAt       string
}

func (s *Store) PutGrant(ctx context.Context, g Grant) (Grant, error)
func (s *Store) GetGrant(ctx context.Context, id string) (Grant, error)
func (s *Store) ListGrants(ctx context.Context, coordinatorID string) ([]Grant, error)
func (s *Store) RevokeGrant(ctx context.Context, id string, expectedRevision int) (Grant, error)
func (s *Store) PutBinding(ctx context.Context, b ExecutionBinding) (ExecutionBinding, error)
func (s *Store) GetBinding(ctx context.Context, id string) (ExecutionBinding, error)
func (s *Store) BindingByRequestKey(ctx context.Context, requestKey string) (ExecutionBinding, error)
func (s *Store) BindingByEquivalence(ctx context.Context, equivalenceKey string) (ExecutionBinding, error)
func (s *Store) BindRuntime(ctx context.Context, requestKey, runInstanceID, runtimeRef string) (ExecutionBinding, error)
```

`PutGrant` mints `ID` when empty. `Status` defaults to `active`. A second `PutGrant` that would add action classes or enlarge scope beyond the issuer’s own grant refuses (`ErrInvalid`, word `expand`). Revocation writes `revoked`, stores `RevocationRevision`, and increments `Revision` in the same writer transaction. A revoked grant cannot launch, steer, or stop; already-bound work stays until an explicit `StopWork`.

`PutBinding` with a non-empty `RequestKey` is unique. A second insert with the same key is a no-op success that returns the existing row. The reserved row **must** exist before any runtime `Admit`. `BindRuntime` moves `admitted` → `bound`, writes `RunInstanceID` + `RuntimeRef` + `BoundAt`, and refuses a second distinct `RunInstanceID` for that key (`ErrConflict`, word `binding`).

`RunInstanceID` is immutable once bound. A reused `plandb.db` path is not a run identity; the adapter allocates and persists this id. Folder membership is never written to `plandb.ParentID`.

A grant/binding change this slice owns still commits in **one** writer transaction. No model or network I/O inside that transaction. `root_state.revision` still increments on mutating writes that affect Root-scoped grants.

## Launch intent, request key, recovery

Workspace DB and task/run stores cannot atomically launch together. The order is software, not the model:

1. Authenticate the grant (`active`, `ClassExecute` present, scope includes the target).
2. Write the reserved `execution_bindings` row (`RequestKey`, `EquivalenceKey`, grant/assignment revisions) **before** runtime admission.
3. Runtime admits with that request key (`StartTask` on the session-task road; run engine / PlanDB when `CODEAF_TASK_BELT=bash`).
4. `BindRuntime` saves the returned run-instance id on the same row.

Crash after runtime accepted and before `BindRuntime`: recovery **finds** the request-key execution and binds; it does not start a second external action (A14 / J31). If the runtime cannot support idempotent admission by request key, add that seam before enabling automatic launch.

Lease expiry ≠ worker dead. An expired job lease returns to `pending` the way Wave 2 already does; it does not mark the binding stopped and does not justify a replacement launch. Fence commitment and reconcile actual runtime ownership before replacement. Retries are at-least-once; there is no exactly-once promise for the outside world.

## `internal/wsexec` (execution adapter)

New package. One adapter over **both** existing roads. Does **not** import `session`, `tui3`, `wsapi`, `provider`, `run`, `plandb`. May import `workspace` for `Grant` / `ExecutionBinding`. Defines the interfaces it needs; tests inject a fake `Store` and a fake `Runtime`. Production wire maps session `StartTask` and the registered `session.RunEngine` onto `Runtime`.

```go
type LaunchRequest struct {
    RequestKey, EquivalenceKey, GrantID, CoordinatorID, OwnerChatID, Brief string
    GrantRev, AssignmentRev string
}

type SteerRevision struct {
    WorkID, GrantID, Text, PersonRequestID string
    GrantRev int
}

type WorkView struct {
    WorkID, RequestKey, RunInstanceID, Road, State, OwnerChatID, GrantID string
    Joined bool // true when LaunchOrJoin followed existing work
}

type ResultView struct {
    WorkID, RunInstanceID, State, Detail string
}

type AdmitRequest struct {
    RequestKey, Brief, Road, OwnerChatID string
}

type AdmitResult struct {
    RunInstanceID, RuntimeRef, Road string
    Already bool // runtime already had this request key
}

// Runtime is the existing task/run door. Session-task maps to StartTask.
// bash-run maps to the registered RunEngine / PlanDB store.
type Runtime interface {
    Admit(ctx context.Context, req AdmitRequest) (AdmitResult, error)
    Inspect(ctx context.Context, runInstanceID string) (WorkView, error)
    Steer(ctx context.Context, runInstanceID string, rev SteerRevision) error
    Pause(ctx context.Context, runInstanceID string) error
    Stop(ctx context.Context, runInstanceID string) error
    Observe(ctx context.Context, runInstanceID string) (ResultView, error)
    FindByRequestKey(ctx context.Context, requestKey string) (AdmitResult, bool, error)
}

type Store interface {
    PutBinding(ctx context.Context, b ExecutionBinding) (ExecutionBinding, error)
    BindingByRequestKey(ctx context.Context, requestKey string) (ExecutionBinding, error)
    BindingByEquivalence(ctx context.Context, equivalenceKey string) (ExecutionBinding, error)
    BindRuntime(ctx context.Context, requestKey, runInstanceID, runtimeRef string) (ExecutionBinding, error)
    GetGrant(ctx context.Context, id string) (Grant, error)
}

type Adapter struct{} // holds Store, Runtime

func Open(store Store, runtime Runtime) *Adapter
func (a *Adapter) LaunchOrJoin(ctx context.Context, req LaunchRequest) (WorkView, error)
func (a *Adapter) Inspect(ctx context.Context, workID string) (WorkView, error)
func (a *Adapter) Steer(ctx context.Context, rev SteerRevision) error
func (a *Adapter) PauseWork(ctx context.Context, workID string) error
func (a *Adapter) StopWork(ctx context.Context, workID string) error
func (a *Adapter) Observe(ctx context.Context, workID string) (ResultView, error)
func (a *Adapter) Recover(ctx context.Context, requestKey string) (WorkView, error)
```

Laws:

1. **Launch-or-join.** `LaunchOrJoin` looks up `EquivalenceKey` first. A reserved/admitted/bound row for that key is joined (`Joined=true`); a second `Admit` is refused. Two discussions of the same issue may share one binding. Critique-only (grant lacks `ClassExecute`) is not a launch and is not blocked (A10 / J28).
2. **Request key before admission.** `LaunchOrJoin` writes the reserved row, then calls `Runtime.Admit`. It never admits first.
3. **Recover finds, it does not relaunch.** `Recover` calls `FindByRequestKey` and `BindRuntime`. It must not call `Admit` when the runtime already has that key.
4. **Immutable run-instance id.** Once bound, `RunInstanceID` does not change. A reused plan-database path cannot alias a different run.
5. **Folder is not ParentID.** The adapter never writes folder membership, collection id, or coordinator id into `plandb.ParentID`.
6. **Pause work ≠ pause coordination.** `PauseWork` / `StopWork` act on an existing binding. They are not `PauseCoordination`.
7. **Steer-with-authority.** `Steer` requires an authentic grant with `ClassSteer` and a citation of the original person request (`PersonRequestID`). Model-supplied person origin is refused on both roads (A11 / J30).
8. **Dual roads.** `Road` is `session-task` when `CODEAF_TASK_BELT` is unset, `bash-run` when it is `bash`. Tests are table-driven across both. Numeric session task ids and string plan ids stay distinct; plan ids are qualified by `RunInstanceID`.

## Wire like `RegisterRunEngine`

Session must **not** import `wsexec`. Session owns the door; cmd/host binds it.

```go
// session — the consumer, same shape as RunEngine / RegisterRunEngine:

type ExecView struct {
    WorkID, RequestKey, RunInstanceID, Road, State string
    Joined bool
}

type ExecResult struct {
    WorkID, RunInstanceID, State, Detail string
}

type Executor interface {
    LaunchOrJoin(ctx context.Context, grantID, brief, equivalenceKey string) (ExecView, error)
    Inspect(ctx context.Context, workID string) (ExecView, error)
    Steer(ctx context.Context, workID, text, personRequestID string) error
    PauseWork(ctx context.Context, workID string) error
    StopWork(ctx context.Context, workID string) error
    Observe(ctx context.Context, workID string) (ExecResult, error)
}

func RegisterExecutor(e Executor) // nil is the verb absent
```

`cmd/codeaf` constructs `wsexec.Adapter` with a `Runtime` over `StartTask` / the registered `RunEngine` and calls `RegisterExecutor`. `wsexec` does not import `session`. The mapping file may live in host/cmd so neither package imports the other. A binary that never registers the executor has no launch-or-join/steer/stop-work verbs (absence law).

## `internal/wsapi` (service additives)

No import of `session`, `tui3`, `provider`, `run`, `wsexec`, `wscollab`, `wsdiscover`. Still imports `workspace`. Execution is an injected interface (same pattern as `Inventory` / `Discoverer` / `Collaborator`).

Keep every Wave 1–3 method. Additive:

```go
// LaunchRequest / SteerRevision / WorkView / ResultView copy wsexec fields
// so this package does not import wsexec. SetExecutor injects the adapter; nil
// means LaunchOrJoin is absent, not a dummy completed view.
type LaunchRequest struct {
    RequestKey, EquivalenceKey, GrantID, CoordinatorID, OwnerChatID, Brief string
    GrantRev, AssignmentRev string
}
type SteerRevision struct {
    WorkID, GrantID, Text, PersonRequestID string
    GrantRev int
}
type WorkView struct {
    WorkID, RequestKey, RunInstanceID, Road, State, OwnerChatID, GrantID string
    Joined bool
}
type ResultView struct {
    WorkID, RunInstanceID, State, Detail string
}
type Executor interface {
    LaunchOrJoin(ctx context.Context, req LaunchRequest) (WorkView, error)
    Inspect(ctx context.Context, workID string) (WorkView, error)
    Steer(ctx context.Context, rev SteerRevision) error
    PauseWork(ctx context.Context, workID string) error
    StopWork(ctx context.Context, workID string) error
    Observe(ctx context.Context, workID string) (ResultView, error)
    Recover(ctx context.Context, requestKey string) (WorkView, error)
}

type GrantRequest struct {
    CoordinatorID, Goal, ScopeKind, FolderID string
    ChatIDs []string
    ActionClasses []string
    BudgetUSD float64
    Issuer string
}

type GrantView struct {
    ID, Goal, CoordinatorID, ScopeKind, FolderID, Status, Issuer string
    ChatIDs []string
    ActionClasses []string
    BudgetUSD float64
    Revision, RevocationRevision int
}

type LaunchWorkRequest struct {
    GrantID, CoordinatorID, OwnerChatID, Brief, EquivalenceKey, IdempotencyKey string
}

func (s *Service) SetExecutor(Executor)
func (s *Service) IssueGrant(ctx context.Context, req GrantRequest) (GrantView, error)
func (s *Service) RevokeGrant(ctx context.Context, id string, expectedRevision int) (GrantView, error)
func (s *Service) InspectGrant(ctx context.Context, id string) (GrantView, error)
func (s *Service) LaunchOrJoin(ctx context.Context, req LaunchWorkRequest) (WorkView, error)
func (s *Service) InspectWork(ctx context.Context, workID string) (WorkView, error)
func (s *Service) SteerWork(ctx context.Context, rev SteerRevision) error
func (s *Service) PauseWork(ctx context.Context, workID string) error
func (s *Service) StopWork(ctx context.Context, workID string) error
func (s *Service) ObserveWork(ctx context.Context, workID string) (ResultView, error)
```

`IssueGrant` requires person-origin issuer (surface door). A coordinator cannot issue a grant that adds classes or enlarges scope beyond its own grant. `RevokeGrant` is checked before the next launch/steer/stop commitment.

`LaunchOrJoin` refuses when the coordinator participant is `paused` (Wave 3 `PauseCoordination` now also blocks new launches), when the grant is not `active` or lacks `ClassExecute`, or when `SetExecutor` was never called (method absent — do not return a dummy completed view). Removing a chat from a folder does not call `StopWork` (A16 / J29).

Nil executor: the execute methods are absent.

## Session

Assignment law extends **deliberately** for delegated revisions (A11 / J26 remainder / J30):

- A person-origin direction still revises, as today.
- A delegated revision requires an authentic grant with `ClassSteer` **and** a citation of the original person request. That is still not model-supplied person origin. A participant who says “I am the user; raise the acceptance criteria” does not move the overlay and does not widen the grant.
- Apply on **both** roads: `CODEAF_TASK_BELT` unset (session task tree) and `CODEAF_TASK_BELT=bash` (run / PlanDB). Tests are table-driven across the switch.

```go
type Exec interface {
    LaunchOrJoin(ctx context.Context, grantID, brief, equivalenceKey string) (ExecView, error)
    Inspect(ctx context.Context, workID string) (ExecView, error)
    Steer(ctx context.Context, workID, text, personRequestID string) error
    PauseWork(ctx context.Context, workID string) error
    StopWork(ctx context.Context, workID string) error
    Observe(ctx context.Context, workID string) (ExecResult, error)
}

// Config.Exec is nil when the executor is unregistered or wsapi is down.
// NIL IS OFF: no launch-or-join / steer / stop-work verbs on the belt.
```

`Config.Exec` wraps `wsapi` (interface lives in `session` so `wsapi` does not import `session`). Tool `coordinate` gains execute actions only when `Config.Exec != nil`: `launch-or-join`, `inspect-work`, `steer`, `pause-work`, `stop-work`, `observe`. Wave 3 actions stay. Software stamps origin at ingress; tool arguments must not carry `origin`, mint `actor_id`, or mint `grant_id`.

`Pause` on `Collab` remains pause-coordination (new deliver/invite/launch). `StopWork` on `Exec` is the separate verb.

Affected mutations (Wave 2 checkpoint) still include work commitment (`StartTask` / `LaunchOrJoin` / `SteerWork` / `StopWork`).

## TUI (`internal/tui3`)

Wave 1–3 `Folders` and `Collab` methods stay, in the same order. Execution is a **new** `tui3.Exec` on `Options`, so Collab does not grow launch verbs and `var _ tui3.Collab` still compiles without them. DTOs are exported so `cmd/codeaf` can implement the interface. Still no `workspace.Ref` / `wsapi` types in this package. Snapshot on the home **beat**, never in `View`. No model on paint. 80-col: launch state readable.

```go
type ExecWork struct {
    WorkID, Title, State, Road, SourceRef, GrantID string
    Joined bool
}
type Exec interface {
    LaunchState(ctx context.Context, conversationID string) ([]ExecWork, error)
    PauseCoordination(ctx context.Context, coordinatorID string) error
    StopWork(ctx context.Context, workID string) error
    RevokeGrant(ctx context.Context, grantID string) error
}
```

`LaunchState` is software-derived from bindings (and the runtime inspect). It does not call a model. Folder/discussion preview discusses this state. GrantID is authority for revoke; it is not painted.

`PauseCoordination` is the Wave 3 verb (new decisions/launches). `StopWork` is the separate explicit action. `RevokeGrant` is a third verb (`v` revoke grant). They must not share a chord. Closing a view does not pause, stop, or revoke. Existing tab-close `stop work` spelling is this stop action, not pause.

Nil `Options.Exec`: no launch-state chrome; natural-language launch still works if `session.Config.Exec` is wired. Preview still launches **no** AI. Deliveries and binding updates must not jump selection or composer (P12).

## Tick / wiring / spend (`cmd/codeaf`)

- Construct `wsexec.Adapter` with the real workspace store and a `Runtime` over `StartTask` / `RegisterRunEngine` once those doors exist; register it (`session.RegisterExecutor`).
- `foldersAdapter` still implements Wave 1+2 Folders. Collab adapter still implements `tui3.Collab`. A separate adapter implements `tui3.Exec` (`var _ tui3.Exec`).
- After a reserved row whose runtime admitted but did not bind, `Recover` that request key so a crash does not launch twice. Recover finds; it must not Admit (A14 / J31).
- `codeaf tick` and the in-window standing pass `LaunchOrJoin` reserved execute grants so authorized work continues after TUI close. Do not change `standing.Interval`. Do not add a second daemon.
- Posture (unattended permissions, daily rail) comes from the **home profile**, never the repository, never `--yolo`. A folder instruction cannot grant itself unattended permissions (A20).
- Same daily spend rail. Job-category reservation so parallel jobs cannot all spend the last dollar. Exhaustion defers (`pending` / `deferred`) and stays visible; never a fabricated completed launch (A15 / J33).
- Closing the TUI does not stop authorized work (J32). If the host cannot run unattended, the UI says so.

## Tests the lanes owe before handoff

- schema: v1 list without migrate; v1→v5 and v4→v5 on first write; actor IDs software-minted; grant cannot self-expand; `RequestKey` unique; `BindRuntime` refuses a second run-instance id; Wave 4 schema creates `grants` and `execution_bindings` only; complexity ≤ 15.
- exec: launch-or-join; duplicate request key does not `Admit` twice; A14 crash (runtime accepted, binding missing) recovers by key and does not start a second action; lease expiry ≠ worker dead; `ParentID` never receives a folder id; fake runtime only in tests.
- wsapi: `PauseCoordination` blocks new launch and does not stop existing work; `StopWork` is the separate door; placement remove does not cancel; nil executor does not return a dummy completed view; revoke is checked before commitment.
- session: assignment law on both `CODEAF_TASK_BELT` roads (table-driven); delegated revision needs authentic grant + original person request; model-supplied person origin refused; execute actions absent when `Config.Exec` nil.
- tui: launch state software-derived; `pause coordination` and `stop work` are two verbs; Wave 1–3 verbs intact; P12 selection stability.
- tick/wiring: recover-by-key; compile-time Exec; history/collab seams unchanged; spend reservation / exhaustion visible.
- proof: delete denials of delegated execution; state: launch-or-join; two discussions allowed / two unnoticed implementations not; pause vs stop; unattended; both task roads; what closing the terminal does; TRY.md Wave 4; tuiwords needles. Live tmux is `t-w4-live`, not this lane’s pass.

## Isolation

Same as Wave 1. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Live journeys must exercise **both** roads. Do not prove only one road and infer the other.

# Folders-entry contracts

Coordination freeze 19 September 2026. This is a **product refinement named Folders entry**, not a fifth GitHub issue. Internal release ordinal is 5 only for `ready.json` later; do not overwrite `control/releases/wave-{1,2,3,4}/`. Wave 1–4 types, methods, iota values, origins, and person-facing spellings stay except where this section names a supersession. Amend only through a PlanDB note and a CONTRACTS.md patch. No GitHub issues, comments, or PRs. Prefer no speculative rewrite.

**Owner supersedes the old no-eighth-tab-bar-place law.** Folders is now a **dedicated registered place** on Home top navigation, not only a home panel.

Inspected live code at freeze (`feat/cw0918-fe-contracts`): `placeOrder` is `home tasks spend settings standing memory search`, `placeBarPlaces=4`, bar words `home tasks spend settings`. `/folders` is already not an alias of `/folder` (`commands.go` `checkCommands`); bare `/folders` currently focuses the home folders panel (`runFoldersCommand` → `showFoldersPanel`). `workspace.organize` unset is **on** (`OrganizeEnabledAt`). Organize jobs are `observe_and_organize` processed by `session.ProcessOrganizeJobs` on the standing pass / `codeaf tick`. Wiring is `cmd/codeaf/folders_adapter.go` (`var _ tui3.Folders`).

## Lane ownership (disjoint)

| Lane | PlanDB | Owns | Must not edit |
|---|---|---|---|
| ui | `t-fe-ui` | `internal/tui3` Folders **place** (`place_folders.go` new), `pages.go` `placeOrder` / `placeBarPlaces` / bar tests, `/folders` routing into the place, visible actions, Root/empty, 80-col. **Keep** the home `folders` panel as enter-from (heading still `folders`; enter on the heading opens the place). | workspace internals, wsdiscover, organize job implementation |
| organize | `t-fe-organize` | `internal/wsapi` organize door, `internal/wsdiscover` / `internal/workspace` job reuse, `cmd/codeaf` + `internal/session` binding of the explicit action. Default: fresh profile does not invent folders. | `internal/tui3` except filling Options |
| proof | `t-fe-proof` | `internal/e2e` harness + `internal/manual/chat` + TRY/amendment. Journeys J36–J43. **Not** other packages' unit tests. | product logic |
| gaps | `t-fe-gaps` | read-only audit + PlanDB fix tasks | product files |

Integration (`t-fe-integrate`) applies real lane commits onto `feat/collaborative-workspace-0918`. No temporary production fallback adapters: a missing organize door is a labelled refusal, never a fake `done`, never an empty successful RootView that invented folders.

## Product names (person-facing)

Wave 1–4 names stay (`folders`, `/folders`, `n f e m w x i`, emptiness whisper, `instructions`, `discovery delayed`, `degraded`). Additive and superseded:

| Surface | Spelling |
|---|---|
| Tab-bar / place word | `folders` |
| Bar, left to right | `home` `tasks` `spend` `settings` `folders` |
| Digit | `alt+5` is Folders (was standing). Standing / memory / search stay off-bar at `alt+6` `alt+7` `alt+8` |
| Slash | `/folders` **enters this logical Folders place**. Not an alias of `/folder` |
| Filesystem | `/folder` `/place` `/dir` stay physical directories |
| Visible actions (not slash-only) | `New folder` · `New chat` · `Organize existing chats` |
| Optional slash for the survey | `/folders organize` — same door as **Organize existing chats**, never the only door |
| Empty folder *list* | heading `folders` plus `logical groups of chats · /folders create Billing` — never “no folders yet” |
| Root unfiled chats | drawn even when the folder list is empty; they are a second truth |
| Job progress on the Folders place | `queued` · `running` · `delayed` · `done` · `cancel` — never store words `pending` `leased` `completed` `deferred` `cancelled`, never `checked` |
| Root | virtual; never a `collections` row; never a CLI list entry |

New chat may start at Root (no folder selected) or in the selected folder (pending membership on first message, Esc creates nothing). If a visible-action chord collides, rename the chord, not the action. Record the landed chords in the manual and TRY.md.

## TUI (`internal/tui3`) — ui lane

- Append `pageFolders` as the **last** `page` iota value (do **not** insert it between existing IDs). New file `internal/tui3/place_folders.go` registers the place (`id`, `word` = `folders`) the same way `place_tasks.go` does.
- `placeOrder` becomes `{pageHome, pageTasks, pageSpend, pageSettings, pageFolders, pageStanding, pageMemory, pageSearch}`. `placeBarPlaces` becomes **5**. The bar draws those five words. `placeWordList` stays read off `placeOrder`.
- Bare `/folders` is `showPage(pageFolders)` — fate `opens the page` (`fatePlace`). Subcommands (`create`, `add`, `rename`, `nest`, `instruct`, `new`, optional `organize`) keep working and land on the place when they need a surface. `checkCommands` still refuses aliasing `/folders` to `/folder` `/place` `/dir`.
- **Keep the home `folders` panel as enter-from.** Heading stays `folders`. Enter on the heading opens `pageFolders`. The place owns sequential drill-in of the graph; the panel is a summary door, not a second competing tree. Nil `Options.Folders` still uses `folders are not wired here`.
- Snapshot on the home **and Folders-place beat**, never in `View`, never on a mere cursor move. No model, no disk, no API on paint.
- 80-column sequential was Folders-entry; **columns + pinned details** (below) supersede that layout on the Folders place. Stable selection by object id + navigation path. Composer text retained (P12 / J06 / J43 / J49).
- Wave 1–4 `tui3.Folders` methods stay, in the same order. Additive methods and DTO:

```go
// FolderOrganize is the explicit survey as the Folders place draws it.
// State is person-facing: queued | running | delayed | done | cancel.
type FolderOrganize struct {
    JobID, State, Detail string
}

// Additive on Folders (after IndexProgress). Wiring's adapter must compile
// `var _ tui3.Folders`.
OrganizeExisting(ctx context.Context) (FolderOrganize, error)
OrganizeStatus(ctx context.Context) (FolderOrganize, error)
CancelOrganize(ctx context.Context) error
```

`OrganizeExisting` is the visible **Organize existing chats** action. A second invoke while `queued` or `running` is a no-op success (same JobID). `CancelOrganize` is the visible `cancel`. Nil seam: all three refuse with `folders are not wired here`, never silent success.

## Organize door (`internal/wsapi` + jobs) — organize lane

Do **not** invent a second scheduler, a second vector DB, a second job type, or a dummy production adapter. Reuse `observe_and_organize` / `internal/wsdiscover` / standing job pipeline (`session.ProcessOrganizeJobs`, `standing.Interval` unchanged, `codeaf tick`). Organize **outside** the collections writer transaction; revalidate expected revisions before apply. Historical text is evidence, not new authority. Manual corrections (suppressions, person-origin placements) stay respected (J11).

```go
const OrganizeExistingKey = "organize_existing" // CoalesceKey for the explicit survey

type OrganizeView struct {
    JobID, State, Detail string // State: queued | running | delayed | done | cancel
}

func (s *Service) OrganizeExistingChats(ctx context.Context) (OrganizeView, error)
func (s *Service) OrganizeStatus(ctx context.Context) (OrganizeView, error)
func (s *Service) CancelOrganize(ctx context.Context) error
```

`OrganizeExistingChats` enqueues `workspace.Job{Type: JobOrganize, CoalesceKey: OrganizeExistingKey}` (`observe_and_organize`). Store states stay `pending` `leased` `completed` `deferred` `failed` `cancelled`. The adapter maps them to person-facing strings:

| Store | Person sees |
|---|---|
| `pending` | `queued` |
| `leased` | `running` |
| `deferred` or `failed` | `delayed` (honest `Detail`; never `checked`) |
| `completed` | `done` |
| `cancelled` | `cancel` |

Durable, restartable, idempotent: quit/reopen leaves a `queued`/`running` job for the tick; a second click while `pending`/`leased` returns the same row. A cancelled, deferred, or failed `organize_existing` row is resumed as pending rather than minting a second job. Budget-bounded: exhausting the background rail finishes `deferred` and paints `delayed`. Foreground chat stays responsive. `cmd/codeaf/folders_adapter.go` implements the three TUI methods against this door (`var _ tui3.Folders` still holds).

**Fresh profile does not invent folders.** After-message automatic enqueue (Wave 2, CoalesceKey = chat id + source revision) MUST NOT `CreateFolder` while Root has zero collections. The Folders tab stays empty of generated folders until the person uses **New folder** / `/folders create` **or** **Organize existing chats** actually applies. `workspace.organize` unset remains **on** for the pipeline being *allowed*; unset-on no longer means silent first-populate of an empty tab. Off still cancels/defers **automatic** after-message apply; it does not refuse manual Add/Remove and it does **not** cancel an explicit `organize_existing` job.

**Upgrade must not delete or reorganize persisted placements.** Existing chats with no membership stay accessible unfiled at Root. Existing collections stay. Organize existing chats surveys saved conversations and may create useful folders from evidence; it does not wipe the graph first.

No model in render. Actual job launch, not a fake countdown or a synchronous LLM on the UI thread.

## Session / CLI (organize + ui binding)

- `session.ProcessOrganizeJobs` stays the runner. Distinguish explicit (`CoalesceKey == OrganizeExistingKey`) from automatic (chat:rev) when `workspace.organize` is off.
- Conversation id for membership is still `session.Place.ID()` (16 hex).
- First-message order unchanged: mint transcript, then `AddPlacement`. Failure leaves the chat unfiled under Root.
- Trusted origin at ingress unchanged: TUI verbs stamp `OriginPerson`; the folders tool stamps `OriginOrganizer`.

## Tests the lanes owe before handoff

- ui: `pageFolders` registered; bar words `home tasks spend settings folders`; `placeBarPlaces==5`; bare `/folders` enters the place; `/folder` `/place` `/dir` still open the filesystem sheet; empty folder list whisper and unfiled Root chats both draw; `New folder` `New chat` `Organize existing chats` visible without requiring a slash; 80-col sequential; selection/composer stable. Package tests live here, not in the proof lane.
- organize: `OrganizeExistingChats` enqueues `observe_and_organize` with `OrganizeExistingKey`; repeated click coalesces; cancel; restart resumes; fresh store with chats does not CreateFolder until the explicit door or a person-created folder; upgrade fixture keeps placements; compile-time `var _ tui3.Folders`; no second daemon; no FakeEmbedder in production.
- proof: e2e/manual/probes/TRY only. **Not** `internal/tui3/*_test.go`. Committed harness covering J36–J43 by frozen names. Live tmux is later acceptance (`t-fe-validate`), not this lane’s pass.

## Journeys this freeze names (proof owns the harness)

| ID | Observable |
|---|---|
| J36 | Fresh empty logical Folders tab: heading `folders`, whisper, no generated folders |
| J37 | Real `/folder` unaffected and never mirrored |
| J38 | Visible `New folder` / `New chat` start at Root or the selected folder |
| J39 | Existing chats visible unfiled at Root before organizing |
| J40 | Visible `Organize existing chats` launches an actual asynchronous job while chatting |
| J41 | Resulting shared/nested folders can browse/rename/move/why via TUI |
| J42 | Repeated click + restart + `queued`/`running`/`delayed`/`done`/`cancel` correct |
| J43 | 80-col navigation; selection and composer stable |

## Isolation

Same as Wave 1. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Do not start `codeaf` against the owner's HOME.

# Folders columns + reactive contracts

Coordination freeze 19 September 2026 on Folders-entry SHA `7fb6a803edd9c29a10872ce90d87310728812641` (J36–J43 pass). Do **not** start from `06643b17`. This freeze names **Finder-style columns + pinned details** on the logical Folders place and **reactive organization** after explicit opt-in. Wave 1–4 and Folders-entry types, methods, iota values, origins, and person-facing spellings stay except where this section names a supersession. Amend only through a PlanDB note and a CONTRACTS.md patch. No GitHub issues, comments, or PRs. Do not write `releases/folders-entry/ready.json`.

Inspected live code at this freeze (`feat/cw0918-rx-contracts` at `7fb6a803`): Folders place is sequential (`place_folders.go`); visible actions `New folder` `New chat` `Organize existing chats`; strip `c` is New folder and swallows `coordinate these`; `CreateFolder(name)` is always Root; Right on a place opens the verb strip (`placekeys.go`); organize runs on `standing.Interval` (5m) / `codeaf tick` with no enqueue wakeup; `organize_bind.go` `surveyExisting` recaps `Unfiled[0..]` at `organizeSurveyCap=8`; organize does not consult `v3StandingDailyRail`. Audit overlay: [`COMPLETE-UX-AUDIT.md`](COMPLETE-UX-AUDIT.md). Product-choice source: `FOLDER-FIRST-JOURNEY-AUDIT.md` §choices.

**No second daemon. No filesystem mirroring.** `/folder` `/place` `/dir` stay physical. Logical graph is a DAG of stable IDs, not a cwd/tree. `observe_and_organize` stays the only organize job type. Navigation never calls the organizer.

## Lane ownership (disjoint)

Parallel after this freeze. One serial integrate. Same-SHA live TUI. Do not start a second Folders-place UI worker.

| Lane | PlanDB | Owns | Must not edit |
|---|---|---|---|
| runtime | `t-rx-runtime` | `cmd/codeaf/organize_bind.go` (survey cursor >8, greeting/no-action refuse); `cmd/codeaf/chatv3_folders.go` enqueue + wakeup; `cmd/codeaf/chatv3_standing.go` organize DailyRail; `internal/session/organize_jobs.go` kick/coalesce/timings; `internal/wsapi/organize.go` `OrganizeThisChat` + enablement; `internal/wsapi` `CreateFolderIn`; `internal/workspace` job cursor/timing columns on the **existing** jobs table; `internal/config` `workspace.reactive`; RoleEmbed DailyRail reserve. Additive `cmd/codeaf/folders_adapter.go` methods that fill the new `tui3.Folders` doors. | `internal/tui3/**`, `folderadd.go`, `collabview.go` |
| ui | `t-rx-ui` | `internal/tui3/place_folders.go`; **new** `internal/tui3/foldercolumns.go` + `foldercolumns_test.go`; **new** `internal/tui3/folderdetails.go` + `folderdetails_test.go`; `internal/tui3/folders.go` additive Folders methods + person-facing words; `internal/tui3/collabact.go` chord `g` / `Coordinate selected` (not `collabview.go`); F03 parent New folder; F05/J44–J48 columns+details; F08 Why+Undo compact line; F11/F14/F21 details; F12 `Manage this folder`; F13 chord; `Organize this chat` visible; Right vs strip; J49 stale-detail guard; place/home beat still snapshots, never View. | `cmd/codeaf/organize_bind.go`, `internal/tui3/folderadd.go`, `internal/tui3/collabview.go`, wsexec |
| proof | `t-rx-proof` | `internal/e2e` harness + `internal/manual/chat` + TRY/amendment for **J44–J49** and F09/F10 first-user timings. Visible actions, not slash-typed as the pass. Measured enqueue/start/commit/visible. **Not** other packages' unit tests. | product logic |
| add-old | `t-ux-add-old` | **new** `internal/tui3/folderadd.go` + `folderadd_test.go`. May add **one** Folders-place restable hook named below. | column/details rewrite of `place_folders.go`; `organize_bind.go`; `collabview.go` |
| collab chrome | `t-ux-collab-chrome` | `internal/tui3/collabview.go` (F15 request/reply/sent paint) | `organize_bind.go`, column files |
| exec | `t-ux-exec` | wsexec / tick LaunchOrJoin / `execact.go` revoke (F18/F19/F20) | organize jobs, `place_folders.go` |
| integrate | `t-rx-integrate` | apply runtime+ui+proof onto `feat/collaborative-workspace-0918`; must ancestor `t-fe-live-tui3` | feature invention |
| ux-integrate | `t-ux-integrate` | merge add-old + collab-chrome + exec onto the rx SHA | feature invention |
| rx-validate | `t-rx-validate` | live columns + reactive on the rx SHA | F24 / whole F01–F24 |
| ux-validate | `t-ux-validate` | live F01–F24 + affected J01–J49 on **one** SHA; blocks `t-fe-ready` | production except harness |

`t-ux-add-old` waits on this freeze only for the named hook. `t-fe-ready` waits on `t-ux-validate`, not on this document.

## Product choices (explicit defaults)

Settled here. Lanes do not re-decide them.

1. **Startup.** First visit of a fresh profile lands on **Folders Root** (not Home, not last-chat). Thereafter restore the last workspace view: last place, and if that place is Folders, the last navigation path (ancestor ids + selected id + selected path). Home overview stays reachable (`home`, `alt+1`).
2. **Logical vs physical.** Show the actual repository / working directory when starting work, via existing project selection. Never map a folder name to a disk path. `/folder` unchanged.
3. **Starting management.** `Manage this folder` from details opens an **ordinary** scoped chat with a goal composer and visible folder scope (current + future membership rule, allowed actions). If the person is already in a coordinating chat for that folder, reuse it. No planner/critic template. No manager entity.
4. **Details priority** (draw in this order; omit a block that is empty — emptiness law; never a sentence saying it is empty): folder = name/purpose, current activity / needs-you, attached child folders, attached chats, coordinating chats, instructions (own, then inherited), Why/Undo/source **only** with real history; chat = title/excerpt, current status/work, folder placements + `also in `, Why/Undo/source, `Open chat`. No invented manager/decision types, fake counts, or model-derived details on selection. Loading/unavailable/offline are honest.
5. **Archive / remove / stop.** `remove this placement` is one edge. Archive of a chat/folder is the existing put-away path, not remove. `pause coordination` and `stop work` stay two verbs. Confirm destructive archive/delete. No silent cascade onto the other kinds.
6. **Shared ordering.** Stable per-container order (the parent). Same node identity across parents. Automatic additions **append** and must not reshuffle the selected row (restore by object id + navigation path).
7. **Right vs verb-strip (one behavior per context).** On the Folders place, `→` **drills** and never opens the strip; `shift+→` opens the strip. Other places keep today's `→` strip (and home's column walk). Help names both. Details actions are also restable rows, so the strip is not required.

## Product names (person-facing)

Folders-entry names stay. Additive and superseded:

| Surface | Spelling |
|---|---|
| Visible actions (not slash-only) | `New folder` · `New chat` · `Organize existing chats` · `Add existing chats` · `Organize this chat` · `Manage this folder` · `Coordinate selected` · `Open chat` |
| Compact activity | `Added to Billing · Why · Undo` (folder name is the real collection name; omit the line when there is no committed change) |
| Coordinate chord | **`g`** `Coordinate selected`. Supersedes Folders-place `c`/`coordinate these` (that `c` stays **New folder**). Same `g` on the home folders panel. |
| New folder chord | **`c`** `New folder` (unchanged) |
| Add existing chord | **`b`** `Add existing chats` |
| Manage chord | **`d`** `Manage this folder` |
| Organize this chat chord | **`t`** `Organize this chat` |
| Undo chord | **`u`** `Undo` |
| Why | **`w`** stays `why here` / compact `Why` |
| Folders place `→` | drill to the next child column, or focus details for a leaf |
| Folders place `←` | parent column; from details, back to the leaf's column |
| Folders place `shift+→` | verb strip. Hint includes `shift+→ actions` |
| Folders place `↑` `↓` | choose rows in the focused column |
| Folders place Enter | folder: already-drilled focus stays; chat: open the existing full conversation. Esc returns without dropping composer |
| Job progress | `queued` · `running` · `delayed` · `done` · `cancel` — never `checked`, never guessed percents, no toast/unread flood |
| Opt-in setting | `workspace.reactive` — unset is **off** |

If a visible-action chord collides, rename the chord, not the action. Record landed chords in the manual and TRY.md.

## TUI (`internal/tui3`) — ui lane

Miller columns over the **logical** membership graph, Finder screenshot as layout reference only.

- **Wide:** Root column → selected folder's children → deeper children as space permits → **pinned** details. Selecting a folder reveals its children in the next column. Selecting a chat previews it in details; Enter opens the existing full chat. Keep the selected ancestor path visible/highlighted. If depth exceeds width, horizontally window older ancestor columns and show a breadcrumb. Never squash unlimited columns into one list.
- **Narrow / 80-col:** one readable navigation column, breadcrumb, switchable details view. Preserve selected object/path when resizing back to wide (J48).
- Shared folder/chat is one stable ID at multiple placements, not duplicated history. Navigation path is separate from identity. `also in ` links; selecting the same shared node from a different parent preserves **that** path (J45). Deduplicate rollups.
- Snapshot on the Folders-place beat (and after a mutation), never in View, never on a cursor move. No model, no disk, no API on paint. Optional empty panels disappear.
- **J49:** every async detail request carries `selectedID` + `selectedPath` + generation. A result that does not match the current selection is dropped. Graph updates rewrite affected columns/details in place; do not teleport selection, do not auto-open a generated folder, do not drop composer.
- **F03:** visible `New folder` is one action. Parent is the folder the person is standing in (open / selected folder row). At Root with no folder selected, parent is empty. Cancel the name box = no mutation. Implementation calls `CreateFolderIn`, not Root `CreateFolder` plus a later nest the person never asked for.
- **F12:** details restable `Manage this folder` (`d`) — ordinary chat, visible scope, no manager row type.
- **F13:** marked chats + `Coordinate selected` (`g`). Fifth unmarked chat stays excluded. Existing-chat reuse; no history merge.
- **Organize this chat** (`t`) is visible on the current chat's details (and the strip). Repeated click coalesces.
- **Add existing chats** hook (exactly one, for `t-ux-add-old`):

```go
const folderAddExistingWord = "Add existing chats"

// beginFolderAdd is the one Folders-place door t-ux-add-old fills.
// Defined in folderadd.go. Absent file ⇒ the restable row is absent
// (capability absent, not a broken button).
func (a *app) beginFolderAdd() tea.Cmd
```

Restable stop word is `Add existing chats`. `t-rx-ui` preserves this stop when columns land; `t-ux-add-old` must not rewrite column/details beyond that single call.

- Home `folders` panel stays enter-from. It does not grow a second column browser.
- Mouse where the existing TUI already supports it. Global tab-bar (`alt+1`…`alt+8`) unchanged. Details actions have an accessible focus route (restable rows; Tab into the details pane).

Additive on `tui3.Folders` **after** `CancelOrganize`, same order. Wiring's adapter must compile `var _ tui3.Folders`:

```go
// CreateFolderIn is visible New folder. Empty parentID is Root.
// Non-empty creates and nests in one store transaction (no Root orphan).
CreateFolderIn(ctx context.Context, name, parentID string) (FolderView, error)

// OrganizeThisChat is visible Organize this chat. CoalesceKey is
// session.OrganizeCoalesceKey(conversationID, latest source revision).
OrganizeThisChat(ctx context.Context, conversationID string) (FolderOrganize, error)

// FolderChange is the compact activity line. Empty means draw nothing.
type FolderChange struct {
    Action, FolderName, CollectionID, RefID string
}
```

`CreateFolder(name)` stays Root-only for `/folders create` without a parent. Nil seam: new methods refuse `folders are not wired here`, never silent success.

Undo is **not** a new store verb: details `Undo` calls existing `RemovePlacement` with `OriginPerson` so J11 suppression holds.

## Organize door + jobs — runtime lane

Do **not** invent a second scheduler, vector DB, job type, or dummy production adapter. Reuse `observe_and_organize` / `internal/wsdiscover` / `session.ProcessOrganizeJobs` / `v3OrganizePass` / `codeaf tick`. `standing.Interval` (5m) remains crash/restart fallback, not the happy-path wake.

```go
const KeyWorkspaceReactive = "workspace.reactive" // unset = off

func (s *Service) CreateFolderIn(ctx context.Context, name, parentID string) (Folder, error)
func (s *Service) OrganizeThisChat(ctx context.Context, conversationID string) (OrganizeView, error)
```

**Enablement.** `workspace.organize` unset remains **on** (pipeline allowed) as Folders-entry. `workspace.reactive` unset is **off**. Visible **Organize existing chats** that successfully enqueues writes `workspace.reactive=on` (opt-in). Do not infer consent from collection count. Manual `New folder` does not opt in. `Organize this chat` is targeted and does **not** by itself opt the workspace in. Off does not delete placements. Automatic after-message graph writes run only when **both** organize and reactive are on. Related-work discovery stays read-only when reactive is off. Fresh Root still shows a sent conversation with **no** generated folders for a greeting.

**Wakeup.** On `EnqueueJob` success and after a `FinishJob` that committed a membership change, kick **one** `v3OrganizePass` through the existing standing pass lock (same constructor as the Interval ticker). Coalesce: at most one in-flight pass plus one dirty follow-up. No goroutine per keystroke, no unbounded workers. Explicit click is urgent in that kick but **cannot** bypass DailyRail, pause, cancellation, or authority. `workspace.organize` off still skips automatic chat:rev jobs; explicit `organize_existing` and `OrganizeThisChat` still run (same Folders-entry explicit law).

**Cross-process UI.** Same-process: the kick may bump the existing Folders-place / home beat. Other windows: existing beat + `RootSnapshot.Revision` short poll. No new fsnotify daemon. No model/disk on render.

**Survey checkpoint (F09).** `organizeSurveyCap=8` is a **per-lease slice**, not a wall. Persist a cursor on the existing jobs row (`workspace.Job.Cursor` additive column, not a new table). Next lease continues after that cursor, including no-action pages. Restart resumes. Never rescan `Unfiled[0..]` forever while eight no-action chats sit in front.

**Greeting / no-action.** Tiny, empty, greeting, and `PlanNoAction` must not `CreateFolder`. That is success without a graph write.

**Coalesce.** Message bursts: latest source revision supersedes stale pending work for that chat (`OrganizeCoalesceKey`). Stale plan must not apply (source revision check stays). Archive/pause semantics unchanged.

**DailyRail + cost.** Organize RoleOrganize **and** RoleEmbed reserve on the same daily rail as standing (`v3StandingDailyRail`). If that rail is exhausted, finish `deferred` and paint `delayed` — organize must not spend after standing is blocked. Fold RoleEmbed into session Account usage when the seam exists; otherwise still reserve the rail and do not imply embeddings are unmetered. Instrument:

| Instant | Who writes | What it is |
|---|---|---|
| enqueue | runtime, on `EnqueueJob` | `EnqueuedAt` |
| start | runtime, on successful lease | `StartedAt` |
| commit | runtime, on `FinishJob` after apply | `CommittedAt` |
| visible | ui proof, when the beat draws the new membership | not stored |

Scheduler delay = start − enqueue. Model latency = commit − start. No unsupported wall-clock promise in the product or the manual.

**First-user sequence the runtime+ui together owe (proof measures):** clean logical Root → first sent chat visible, no generated folders → **Organize existing chats** starts promptly while typing/reply stay responsive → useful first folders/placements appear without waiting five minutes → a later meaningful chat links the same way → Why/Undo durable, chat identity intact → **Organize this chat** targeted rerun → bursts/stale/cancel/archive/budget/offline/restart/cross-window do not duplicate or interrupt. Include a no-op greeting and a >8 unfiled/no-action backlog.

## Tests the lanes owe before handoff

- ui: J44 wide Root>folder>subfolder + pinned details; J45 two paths one identity + Also in; J46 preview then full open/return; J47 truthful actionable details; J48 80/wide resize + windowing + help names `→` drill and `shift+→` strip; J49 selection/path/composer + stale detail dropped; F03 New folder nests in one action; `c` New folder and `g` Coordinate selected both reachable; `Add existing chats` hook preserved; no model on paint; package tests here, not in proof.
- runtime: wakeup on enqueue (not 5m-only); survey cursor past 8; greeting creates no folder; OrganizeThisChat coalesces; reactive unset does not auto-CreateFolder; Organize existing chats sets reactive on; DailyRail blocks organize; timings enqueue/start/commit present; compile-time `var _ tui3.Folders`; no second daemon; no FakeEmbedder in production.
- proof: executable J44–J49 + F09/F10; measured scheduler vs model; actual tmux wide+80col pane evidence (filesystem `/folder` shots do not count); TRY/manual. Live F24 stays `t-ux-validate`.

## Journeys this freeze names (proof owns the harness)

| ID | Observable |
|---|---|
| J44 | Wide Root → folder → subfolder columns + pinned details |
| J45 | Shared object via two paths, one identity, Also in; path preserved |
| J46 | Chat preview in details, Enter opens full existing chat, return keeps path |
| J47 | Folder instructions/activity/attachments truthful and actionable |
| J48 | Narrow/wide resize, deep-path windowing, keyboard/help (`→` drill, `shift+→` actions) |
| J49 | Async graph updates preserve selection/path/composer; stale detail cannot overwrite |

J36–J43 remain required. F01–F24 remain required; this freeze does not weaken them.

## Isolation

Same as Folders-entry. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Do not start `codeaf` against the owner's HOME. Do not write `ready.json` from this lane.
