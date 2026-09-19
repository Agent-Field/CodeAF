# Wave 1 contracts

Frozen 18 September 2026 for parallel lanes on `feat/collaborative-workspace-0918`. Amend only through a PlanDB note on `t-w1-contracts` (or the blocked lane) and a CONTRACTS.md patch at integrate. Do not invent extra tables, tools, or an eighth tab-bar place.

GitHub: #1216. Journeys: J01–J08. Pre-wave source: `610a32ba`. Design HEAD at freeze: recorded in PlanDB on `t-w1-contracts` done.

## Lane ownership (disjoint)

| Lane | PlanDB | Owns (create/edit) | Must not edit |
|---|---|---|---|
| storage | `t-w1-storage` | `internal/workspace/*` | anything else |
| service | `t-w1-service` | `internal/wsapi/*` (new package) | workspace internals, tui3, session, cmd |
| tui | `t-w1-tui` | `internal/tui3/homepanel_folders.go`, `internal/tui3/folders.go`, their tests; `internal/tui3/homegrid.go` (append `panelFolders` to the iota **last** so existing IDs do not shift; insert the slot in `homePanelOrder` after recent; whisper); `internal/tui3/commands.go` (`/folders` rows only); `internal/tui3/homeslash.go` (`homeFate` for `folders`); `internal/tui3/tui3.go` (`Options.Folders` field); `internal/tui3/place_home.go` (`homeRowVerbs` folder case); `internal/tui3/home.go` (folder enter + pending start); `internal/tui3/app.go` (`case "folders"` only) | `internal/workspace`, `internal/wsapi`, `internal/session`, `cmd/codeaf` |
| wiring | `t-w1-wiring` | `cmd/codeaf/collections.go` (additive `--reason`); `cmd/codeaf/chatv3.go` / `chatv3_local.go` (construct `wsapi.Service`, set `tui3.Options.Folders` and `session.Config.Folders`); `internal/session/tools_folders.go`; `internal/session/session.go` (`Config.Folders`); `internal/session/tools.go` and `internal/session/bashbelt.go` (append `foldersTools` the same way `memoryTools` is appended); `internal/session/prompts/system.md` (mention `folders` only if wired) | `internal/workspace` internals, `internal/tui3` except filling Options |
| proof | `t-w1-proof` | `internal/manual/chat/` pages that currently deny folder UI; `internal/manual/chat_test.go` probes; `internal/e2e/tuiwords_test.go` needles; `docs/design/collaborative-workspace/TRY.md`; optional untagged e2e helper. **Not** `internal/tui3/*_test.go` or workspace/wsapi tests unless a lane transfers them in a PlanDB note. | product logic; other packages' unit tests |

Integration (`t-w1-integrate`) applies lane branches onto `feat/collaborative-workspace-0918` in order: storage → service → wiring → tui → proof.

Storage draft lives in the storage worktree only (`origin.go`, `schema.go`, in-progress `store.go`). Coordinator checkout must not keep those uncommitted files after contracts land.

## Product names (person-facing)

| Surface | Spelling |
|---|---|
| Home panel heading | `folders` |
| Empty whisper | `logical groups of chats · /folders create Billing` (no ellipsis, never “no folders yet”) |
| Slash | `/folders` — **not** an alias of `/folder`. Optional later alias `/collections` is out of Wave 1. |
| `/folders create <name>` | make a logical folder |
| `/folders add <name-or-id>` | file the current chat here |
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

Mismatch of a non-zero expected revision returns `ErrConflict` (add this sentinel) wrapping `ErrInvalid` with the word `revision`. Zero expected revision keeps old CLI/add/remove unconditional.

`Add`/`Remove` keep their old signatures and mean person origin + empty reason (CLI compatibility). `Move` is add-destination + remove-source + two events in **one** writer transaction; other placements of the same ref stay. Cycle check remains in that transaction. `WhyHere` is the latest event for that active or last edge. Root is not stored as membership. `RootState` reads `root_state` (creating the v2 row only on a write path / ensureSchema).

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

Deduplicate conversation IDs in counts. Expected-revision conflicts return `workspace.ErrInvalid` wrapped with a stable `revision` mention until a dedicated error is added (`ErrConflict` allowed if storage introduces it).

## TUI (`internal/tui3`)

- Append `panelFolders` as the last `homePanelID` iota value (do **not** insert it between existing IDs). Insert the slot after `panelRecent` in `homePanelOrder`. `keep: 2`, `least: 3`, `rest: 4`, `most: 8`, not pinned. This is an eighth **home panel**, not a tab-bar place.
- Snapshot is a memo filled on the home **beat** (`readHomeFolders`), never in `View` or on cursor move.
- `homeFolderRow = 246`, `homeFolderBack = 247`. Member chats reuse `homeSession` with `cell.panel == panelFolders`.
- `app.pendingFolder` string: set by `n` / `/folders` new; consumed after first-message `renew`/`startChatEnter` via `AddPlacement`. Esc clears it and creates no transcript.
- `Options.Folders` is `tui3.Folders`, defined in `internal/tui3/folders.go` with **TUI DTOs only** (`folderView`, `folderPlacement`, `folderWhy`). It does **not** use `workspace.Ref` or `wsapi.Folder`. Wiring owns an adapter `tui3.Folders` ← `*wsapi.Service`. `*wsapi.Service` is **not** required to satisfy `tui3.Folders` directly.
- Nil `Options.Folders` (store unavailable): the panel heading still exists, but it is **not** an empty working workspace. Mutations (`n f m w x`, `/folders create|add`) must refuse with a visible failure, never silent success. Distinct from a working empty store, which uses the emptiness-law whisper.
- 80-col: sequential drill-in, `esc` back. No model on paint.

`wsapi.Service` should itself talk to a `store` interface matching the frozen `workspace.Store` methods so service tests can use a fake while storage is in another worktree. `Open` still calls `workspace.Open`.

## Session / CLI (wiring)

- `session.Config.Folders` is an interface with `List/File/Unfile/Move` methods wrapping `wsapi` (define the interface in `session` so `wsapi` does not import `session`).
- Tool `folders` on the belt only when `Config.Folders != nil`. Actions: `list`, `file`, `unfile`, `move`. Refuses cycles/unknown ids in result text.
- Conversation id for membership is `session.Place.ID()` (16 hex), never a UI path.
- CLI: `--reason` optional on `add`/`remove`; default output of old verbs unchanged.
- First-message order: mint transcript, then `AddPlacement`. Failure leaves the chat unfiled under Root; retry is idempotent.

## Tests the lanes owe before handoff

- storage: v1 list without migrate; first write → v2; cycle txn; events written; Root not stored; foreign/future/damaged refuse; complexity ≤ 15.
- service: add/move/remove/idempotency; unique counts; dual placement.
- tui: emptiness whisper; “also in”; selection/composer stability; 80-col sequential; no store read in View.
- wiring: tool absent when service nil; present when wired; `/folder` unchanged.
- proof: committed harness covering J01–J08 actions by frozen names; manual probes “group chats in folders”, “is /folder a logical folder?”, “also in two folders”. Live tmux is `t-w1-live`, not this lane’s pass.

## Isolation

`CODEAF_HOME` + private `CODEAF_PROFILE_DIR`; never `HOME`. `mktemp`. Run-unique tmux. Keys via `config.APIKeyAt` / e2e `liveKey`. Synthetic content only.
