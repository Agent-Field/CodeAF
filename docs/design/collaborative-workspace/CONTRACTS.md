# Wave 1 contracts

Coordination baseline 18 September 2026, **reconciled 19 September 2026** (PlanDB notes n-91ee n-uijj n-ludz n-q2mx n-5r0l n-5sbo n-cw71 n-ux5g). The freeze is not an excuse to ship missing invariants: every finding below must have a test, not only a comment. Amend only through a PlanDB note and a CONTRACTS.md patch. Do not invent extra tables, tools, or an eighth tab-bar place.

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

`PRAGMA user_version` after a successful Wave 1 write-path migration is **2**. Wave 2 write-path is **3** (see Wave 2). `application_id` stays `0x4146434c`. Wave 1 created no guidance/grant/delivery/execution tables; Wave 2 adds only the five named v3 tables below. Grant/delivery/execution stay absent.

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

- Append `panelFolders` as the last `homePanelID` iota value (do **not** insert it between existing IDs). Insert the slot after `panelRecent` in `homePanelOrder`. `keep: 2`, `least: 3`, `rest: 4`, `most: 8`, not pinned. This is an eighth **home panel**, not a tab-bar place.
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

Coordination freeze 19 September 2026. Wave 1 types, methods, iota values, origins, and person-facing Wave 1 spellings stay. Amend only through a PlanDB note and a CONTRACTS.md patch. Do not invent grant/delivery/execution tables, an eighth tab-bar place, a keyword-only filer, or a private HTTP client.

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

`PRAGMA user_version` after a successful Wave 2 write-path migration is **3**. `application_id` stays `0x4146434c`. Foreign/future/corrupt still refuse. Test v1-to-v3 and v2-to-v3. No participants/deliveries/grants/execution_bindings.

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

- schema: v1 list without migrate; v1→v3 and v2→v3 on first write; lease/fence mismatch refuses; expired lease returns to pending; coalesce; suppress key; no Wave 3/4 tables; complexity ≤ 15.
- discover: cursor identity is not a byte offset; crash/replay (A13); rewind/delete invalidates derived rows (A22); version/dimension mismatch does not compare vectors; fake Embedder in tests only.
- embed: `/embeddings` adapter; availability inspect without printing keys; spend tagged; pin not a text-tier Register; degraded path labelled, not a silent lexical filer.
- wsapi: `no-action` writes no membership; validator rules 1–10; InstructFolder person-only; EffectiveGuidance Root-once + conflict bit; SuppressPlacement blocks identical evidence; ApplyActionPlan expected-revision atomic.
- session: ConversationHistory set when Memory is nil; `search_conversations` present with memory off; hybrid adds candidates beside BM25; tool sentence updated; checkpoint pauses only affected mutations; RoleOrganize through callRoleChecked, never RoleAuditor.
- tui: instructions section + empty whisper; indexing not fake 100%; organizer why-here; delayed/degraded copy; no organize on hover; Wave 1 verbs intact.
- tick/wiring: enqueue after journal; coalesce; compile-time Folders; history seam split from memory.
- proof: delete denials of automatic organization and inherited instructions; state limits (no keyword-only file; suppressions; memory-off; delayed vs checked); TRY.md Wave 2; tuiwords needles. Live tmux is not this lane’s pass.

## Isolation

Same as Wave 1. `CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only. Measure scale in passages/vectors/memory/latency, not chat count alone (J18).
