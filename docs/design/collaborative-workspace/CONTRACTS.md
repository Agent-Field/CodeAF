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

`PRAGMA user_version` after a successful write-path migration is **2**. `application_id` stays `0x4146434c`. No guidance/grant/delivery/execution tables.

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
