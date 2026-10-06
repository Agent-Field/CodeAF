# Memory architecture

Status: substrate shipped (Phase 0 plus the listed parts of Phase 1); the
deferred parts are named plainly in §8. Supersedes the ad-hoc design discussion
of 2026-10-03, and corrects several of its own early claims — the corrections
are marked **Corrected** where they land.
Audience: anyone implementing or reviewing memory work in this repository.

## 0. Why this exists

An audit of the current memory system (`internal/store/memory.go`,
`internal/session/memory.go`, `internal/session/memory_consolidate.go`,
`internal/reflex`) found the pipeline wired end to end — startup injection,
per-turn retrieval, automatic extraction, consolidation — but with four
structural defects:

1. **Scope leak.** The `scope` column (`user`/`project`/`env`) is a label, not
   an owner. `MemoryCandidates` filters on status only; dedup search is
   unfiltered. A memory from one project can surface — and be updated — in an
   unrelated project.
2. **Split write doors.** `/remember` and the model's `remember` tool dedup
   through `applyCandidate`; natural-language "remember…" routing calls
   `AddMemory` directly with no dedup. Duplicates accumulate and are never
   reconciled.
3. **Silent failure.** Retrieval and extraction errors are discarded; memory
   can quietly stop learning or recalling with no surface.
4. **Lossy import.** `importMemoryFile` renames the legacy file even when row
   writes fail, and ignores the scanner error. Partial imports are never
   retried.

Three product constraints shape the redesign:

- **Just works, on every install.** Any provider, including providers with no
  embeddings endpoint. No model downloads, no new heavyweight dependencies.
- **Accumulates for years.** Paging, archiving, and budgets are designed in,
  not retrofitted.
- **Relay-ready.** PR #1738 (pair two computers, sealed object sync, device
  identity) does not carry memory. Memory must be shaped so sync attaches
  later as a thin adapter, without a schema migration.

## 1. Principles

1. **The durable record is tiny; everything else is derived.** Text plus
   provenance is the only source of truth. FTS indexes, edges, vectors, and
   rankings are rebuildable.
2. **Permission is structural, relevance is computed.** A memory has exactly
   one owner. Retrieval filters on owner equality — there is no filter to
   forget. Sharing is an explicit copy with a new owner, never an audience
   list.
3. **LLM on write and maintenance; never required on the read path.** Reads
   are deterministic and fast. Semantic help comes from relations computed
   when a model was already in hand.
4. **Memory rows are immutable.** An update is a new row plus a `supersedes`
   edge. This makes replication convergent and the history complete.
5. **Every behavior-changing injection carries its why.** Trust is a schema
   property.
6. **Memory, skills, and standing orders stay separate.** Memory is what is
   true; a skill is how to do something; a standing order is a rule that
   fires. Memory may reference either; it never becomes one.
7. **Failures are journaled and visible.** "Just works" includes "tells you
   when it didn't."

## 2. Data model

### 2.1 Owner

```go
type Owner string
// "user"              the person, every project, every machine that shares the store
// "machine"           this computer only; never syncs
// "project:<key>"     a repository or working area
```

**Corrected.** The owner kinds shipped today are three, not four. `machine`
has no device id because the device identity is PR #1738's and #1738 has not
merged — a store keyed on an unmerged relay's ids would be a store that cannot
open on a machine that has never paired. `team:<teamID>` is deliberately
ABSENT: no door mints one yet, and a capability that cannot work is absent
rather than broken. Team sharing arrives with the sync work as an explicit
copy with a new owner.

`project:<key>` survives folder moves and cross-machine clones because the
key is hashed from the git origin URL **normalized** — scheme, credentials,
`.git` suffix and port folded away, host lower-cased — so an SSH clone and an
HTTPS clone of one repository are one project. The canonical path is the
fallback only when there is no remote to name. What it does not survive YET:
renaming a remote-less repository (its only identity is its path); the alias
table is open decision 4 and is not shipped.

### 2.2 Schema

```sql
CREATE TABLE memories (
  id           TEXT PRIMARY KEY,        -- ULID, minted by the store
  owner        TEXT NOT NULL,
  kind         TEXT NOT NULL,           -- preference|decision|fact|state|procedure_ref
  title        TEXT NOT NULL,
  text         TEXT NOT NULL,           -- immutable
  tags         TEXT NOT NULL DEFAULT '[]',
  status       TEXT NOT NULL DEFAULT 'active',  -- active|superseded|deleted
  supersedes   TEXT,                    -- prior version id; the version chain
  confidence   REAL NOT NULL DEFAULT 0.6,
  source_json  TEXT NOT NULL,           -- session, message ref, actor, explicit|extracted
  created_at   TEXT NOT NULL,
  confirmed_at TEXT NOT NULL,           -- last time evidence agreed
  review_after TEXT,                    -- NULL = durable
  origin       TEXT NOT NULL,           -- device id that wrote it
  lamport      INTEGER NOT NULL
);
CREATE INDEX idx_memories_active ON memories(owner) WHERE status='active';

CREATE VIRTUAL TABLE memory_fts USING fts5(
  title, text, tags,
  content='memories', content_rowid='rowid',
  tokenize='porter unicode61'
);

-- Semantic relations computed at write/maintenance time. Zero read cost.
CREATE TABLE memory_edges (
  src TEXT NOT NULL, dst TEXT NOT NULL,
  rel TEXT NOT NULL,                    -- relates_to|contradicts|supersedes|co_used
  weight REAL NOT NULL DEFAULT 1.0,
  origin TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (src, dst, rel)
);

-- Append-only journal. Observability, ranking signal, and (later) the
-- replication log.
CREATE TABLE memory_events (
  seq         INTEGER PRIMARY KEY AUTOINCREMENT,
  id          TEXT NOT NULL,            -- event ULID
  origin      TEXT NOT NULL,            -- device id
  origin_seq  INTEGER NOT NULL,         -- monotone per origin
  lamport     INTEGER NOT NULL,
  kind        TEXT NOT NULL,            -- written|superseded|deleted|used|
                                        -- contradicted|write_failed|tidy
  memory_id   TEXT,
  payload_json TEXT,
  created_at  TEXT NOT NULL,
  UNIQUE(origin, origin_seq)
);

-- High-volume suggestion events folded into counters, not raw rows.
CREATE TABLE memory_stats (
  memory_id TEXT NOT NULL,
  day       TEXT NOT NULL,
  suggested INTEGER NOT NULL DEFAULT 0,
  used      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (memory_id, day)
);

-- Embedding plugin. Created only when a provider with an embeddings API is
-- configured; dropping the table retires the tier.
CREATE TABLE memory_vectors (
  memory_id TEXT PRIMARY KEY,
  model     TEXT NOT NULL,
  dim       INTEGER NOT NULL,
  vec       BLOB NOT NULL
);
```

Notes:

- The store mints all IDs (ULIDs). Today the extraction model invents IDs;
  two machines will collide. ULIDs are coordination-free and time-sortable.
- `status='active'` partial index means superseded and deleted rows are never
  scanned on the read path, however large the archive grows.
- `UNIQUE(origin, origin_seq)` makes event application idempotent — safe under
  the relay's resubmission behavior.

### 2.3 Store contract

```go
// Shipped in this form. The names are the store's, the types are concrete:
type WriteRequest struct{ Owner, Type, Title, Text, Tags, SourceSession string }
type WriteResult struct{ Memory Memory; Outcome string; Why string }
func (s *Store) Write(WriteRequest) (WriteResult, error)

// The read funnel is the store's existing RRF candidates read, now
// owner-scoped; the session's router model still rejects from the shortlist.
func (s *Store) MemoryCandidates(owners []string, terms string, limit int) ([]MemoryStub, error)
func (s *Store) SearchMemories(owners []string, query string, limit int) ([]Memory, error)
func (s *Store) GetMemories(owners []string, ids []string) ([]Memory, error)
func (s *Store) MemoryPage(owners []string, cursor MemoryCursor, limit int) ([]Memory, MemoryCursor, error)

// Sync seams. Wired to PR #1738's transport later; inert until then, and
// proven only by tests until then.
func (s *Store) ExportMemoryEvents(after int64, limit int) ([]MemoryEvent, int64, bool, error)
func (s *Store) ApplyMemoryEvents(events []MemoryEvent, allow func(owner string) bool) (AppliedMemoryEvents, error)
```

`WriteResult` reports added / skipped-duplicate, plus a human-readable why.
Every outcome, including a failure, is journaled — a failed write is
`memory_write_failed` in the journal, not a vanishing error.

## 3. Write path — one door

1. Normalize text; classify kind and tags (extraction model, already running).
2. Resolve owner. `/remember` and the tool set it explicitly. Extracted owner
   is a proposal: promoting anything to `team:` requires user confirmation.
3. Same-owner duplicate and conflict search (FTS5 + edges; vectors when the
   plugin exists). Never cross-owner — two projects may hold genuinely
   different truths about the same words.
4. Decide: add | new-version (supersede) | skip-duplicate.
5. Emit edges proposed by the extraction model (`relates_to`, `contradicts`,
   `supersedes`).
6. Journal the outcome.

Consequences by construction — ALL FOUR SHIPPED:

- The audit's split-door dedup bug disappears: there is one door.
- Cross-project updates become impossible: conflict search never leaves the
  writer's owner.
- Import correctness: the legacy importer checks `scanner.Err` and every row
  result; it renames the source file only when every row landed, and the
  retry is idempotent because kept lines skip at the same door.
- Extraction failures journal `memory_write_failed` with the error instead of
  vanishing, are said once per five-minute window as a dim line, and fall
  back to `v3/memory-failures.log` when the store itself is what failed.

**Corrected — the decider is never a permission.** The early draft let a
failed model settle refuse an explicit save. Shipped: the decider's failure is
journaled and the words still land through the store's own dedup door. Model
enrichment improves writes; it is never the difference between a save
happening and not happening.

**Corrected — the dedup door is deliberately small.** Exact text (case and
whitespace folded) or exact title, within the same owner, is a duplicate;
everything subtler is the decider's job. A similarity threshold would be a
guess wearing a rule's clothes, and two lines that are not the same words are
not the same memory.

## 4. Read path — the funnel

Deterministic, no LLM required, every tier optional except the first two.
Each tier emits a ranked ID list; reciprocal-rank fusion (k=60) merges
however many contributed. A missing tier is a no-op, not an error.

**Corrected — nothing truncates the corpus before lexical.** The early draft
capped the structural tier at 200 candidates and implied the funnel ran
through that cap. Shipped: the lexical tier runs over the WHOLE owner's
active set (it is an indexed read — truncating it before the query would make
memory two hundred and one unreachable forever, the defect the store already
removed once), and the k of the final budget is the only cut that happens
after ranking.

```
1. Structural   WHERE owner IN visible(ctx) AND status='active'
                AND (review_after IS NULL OR review_after > now)
                → ranked, no cap (the whole owner's active set)
2. Lexical      FTS5 BM25 against the turn text → top 50
3. Edges        1-hop relates_to/contradicts/co_used neighbors of the
                current top 20 → joined at their edge weight
4. Vectors      brute-force cosine over memory_vectors, if the table
                exists → top 50          (optional plugin)
5. JIT rerank   one aux-model call ranking the fused top 30, ONLY when
                the fused pool above the cutoff exceeds 2×k and an aux
                model is configured       (optional booster)
6. Budget       top k (≈8) → inject, each with a why-line naming the
                tiers that nominated it ("same repo", "used 12×",
                "you corrected this")
7. Journal      fold suggestions into memory_stats; record used/contradicted
                as raw events
```

Why brute force is fine for tier 4: 10k memories × 1536 dims ≈ 15M
multiply-adds — milliseconds. A vector index is a 100k-row problem; revisit
only there.

Cold start (no edges, no usage, no vectors) rides tiers 1–2, which is already
stricter than today's unfiltered LIKE search.

### Semantics without embeddings

Embeddings are one of four sources of relatedness; the other three need no
provider capability at all:

| Source | Computed | Cost at read |
|---|---|---|
| Structural + lexical | always, in SQL | none |
| Write-time edges (`relates_to`/`contradicts`) | extraction model, once per write | one SQL join |
| Behavioral edges (`co_used`) | tidy, mining the journal | one SQL join |
| Vectors | optional provider plugin | milliseconds |

No install downloads anything. The fallback for a provider without
embeddings is not a degraded system — it is the designed baseline.

## 5. Scale plan

Assume heavy use: 5–20 durable memories/day → 2–7k/year. **These are
estimates, not benchmarks** — nothing in this repository has measured them
yet, and a number quoted from this section is a design assumption, never a
measurement.

- **Archive never scanned**: partial index keeps active-set queries O(active),
  not O(history).
- **The journal is the flood, not the memories.** `suggested` fires every
  turn, so it is aggregated into `memory_stats` counters at write time. Raw
  events are kept only for `used`, `contradicted`, `written`, `superseded`,
  `write_failed`, `tidy` — low volume, high value.
- **Keyset pagination everywhere**: `/memories` and the activity feed page by
  `(created_at, id)` cursors, 50 per page. No OFFSET. `ChangedSince` already
  sequences the home screen and keeps its contract.
- **Budgets are hard caps**: k≈8 injected, tidy archives the long-unused.
  Nothing truncates the candidate corpus before ranking: the structural tier
  ranks the whole owner's active set and only the final k is cut.
- **Vectors isolated**: 6 KB/row at 1536 dims lives only in `memory_vectors`;
  non-embedding installs never pay it.

## 6. Relay readiness (PR #1738)

Pay the schema cost now; pay the transport cost later. Four decisions already
in the schema do the work:

1. **Immutable rows + supersede chains.** Replication is set-union of
   immutable rows plus a fold of edges — convergent under any delivery order
   or duplication.

   **Corrected.** Text conflicts are not made impossible by immutability
   ALONE, because corrections in place exist: an explicit edit (the memory
   place's `e fix the wording`) rewrites a row's text under its own id, and a
   decider's "refine" does the same. Those are the mutable half, and the fold
   gives them deterministic — not conflict-free — semantics: first applied
   wins, the skipped half is counted, nothing resurrects a tombstone
   ([Store.ApplyMemoryEvents]'s rules). The seam's contract is "deterministic
   under any delivery order", which is the honest claim; "no conflicts" was
   the early draft overpromising.
2. **Store-minted ULIDs.** Unique across devices without coordination.
3. **The journal is the replication log.** `(origin, origin_seq)` cursors are
   exactly "what have you seen"; sync is exchanging `EventsSince` batches and
   applying them idempotently.
4. **Owner namespaces are the sync policy.** One function:

```go
func SyncPolicy(o Owner) Policy
// user:           replicate to the person's paired devices
// project:<key>:  replicate with the project (follows the folder)
// team:<id>:      replicate to team members
// machine:<id>:   never leaves the device
```

What Phase 4 adds when #1738 lands: an adapter moving `EventsSince` /
`ApplyRemote` batches over its sealed object transport, reusing its device
identity and pairing. Memory never learns what pairing is; the relay never
learns what a memory is. Ordering: Lamport stamps for merge order, physical
time for display; status folds last-writer-wins per memory id; supersede
edges reference ids causally, so skewed clocks cannot corrupt a chain.

Explicitly not built now: transport, pairing integration, encryption (the
relay's sealing carries events as opaque blobs later).

## 7. Migration from the current schema

Existing rows carry only `scope` (`user`/`project`/`env`) with no project
identity. **Shipped, and corrected where the early draft guessed:**

- `scope=user` → `owner=user` — unchanged, provable from the row.
- `scope=env` → `owner=machine` — the store-level machine, because the device
  identity is PR #1738's and it has not merged; a later relay migration can
  resolve `machine` against the pairing without touching rows.
- `scope=project` → **`owner=project:legacy`, tag `legacy-project`.** The
  early draft moved these to `owner=user`; that is the one move a migration
  must never make — attributing an unknown project's memory to the person at
  large WIDENS its visibility to every project. Quarantine never injects.
- Recovery: a door-side pass walks the state root's session folders, whose
  `meta.json` records each session's workspace as a fact; a quarantined row
  whose source session provably ran in a workspace is re-homed to that
  project's owner ([Store.RehomeMemories], journaled). Rows nothing proves
  stay quarantined, visible to the person and to nobody's model.
- Statuses map through (`active`/`superseded`/`forgotten` preserved).
- The migration is idempotent: a reopened store re-derives nothing.
- Every migrated row's history is already in the journal; no synthetic
  `written` events are minted, because the journal being truth means the
  migration may not add history that did not happen.

## 8. Phases — shipped, and deferred on purpose

### Shipped in this PR (substrate + trust)

- **Owner column + migration** with quarantine, not re-attribution; the
  door-side re-home pass recovers what the state root can prove.
- **Owner-keyed reads.** MemoryCandidates, SearchMemories, GetMemories and
  MemoryPage take the session's visible owners and REFUSE an empty list; the
  janitor's ListMemories keeps its unscoped read by contract.
- **One write door** across /remember, the remember tool, the routed
  command, extraction and import, with same-owner dedup and both outcomes
  journaled.
- **Visible failure:** journal events, a rate-limited dim line, a fallback
  file when the store itself fails.
- **Lossless, idempotent import** with the scanner's error read.
- **Memory/search decoupling:** the conversation index is its own handle; the
  memory row no longer takes the search verb down.
- **FTS5 proved at open**, with graceful degradation, no install step.
- **Keyset pagination** (`MemoryPage`), provenance and trust inspection as
  they already existed (the memory card's provenance read).
- **The sync seam** (export/apply with owner policy, idempotency, tombstone
  precedence) — proven by tests, wired to no transport.
- **Optional enrichment:** explicit saves never require the decider;
  embeddings stay absent-by-default.

### Deferred, and said so plainly

- **Embedding vectors / memory_vectors** — Phase 3; interface is
  provider-agnostic; no install downloads anything, nothing fails when the
  provider has no embeddings endpoint, and the index is rebuildable and
  model-separated when it arrives.
- **Edges, co-use mining, JIT rerank, review_after sweeps** — Phase 2. The
  early draft's `memory_edges` table is NOT in the shipped schema; edges ship
  when something computes them, not before.
- **Team owners (`team:<id>`)** — deferred with sync; sharing is an explicit
  copy with a new owner, and no door mints a team owner yet.
- **The sync transport** — Phase 4, on PR #1738's pairing; the seam above is
  the whole of what the store owes.
- **Project-key alias tables** for remote-less folder renames — open decision
  4, not shipped.
- **Surfaces on the new primitives:** the memory place still shows the whole
  store (the person's own overview); a per-project shelf view and a re-home
  verb are the natural Phase 1 follow-up, and the primitives are in place.

### The acceptance tests, and where each lives

- A memory written in project A cannot be retrieved or updated from project
  B — `internal/store/memory_owner_test.go` (store) and
  `internal/session/memory_owner_test.go` (through the session's own doors).
- Routed "remember…" dedups against existing rows —
  `TestEveryRememberDoorSettlesIntoOneRow`.
- A failed import leaves the legacy file un-renamed and retries —
  `TestAFailedImportLeavesTheFileAndRetries`, and the retry's idempotency in
  `TestAnImportSurvivesADeadDeciderWithoutDuplicates`.
- An induced write failure produces a visible `memory_write_failed` event —
  `TestAFailedExtractionIsJournaledAndSaidOnce`; the dead-store fallback in
  `TestAFailureWithADeadStoreReachesTheFallbackFile`.
- Memory disabled no longer disables conversation search —
  `TestMemoryOffWithAnIndexKeepsSearchAndLosesRemember`.
- The fold is idempotent, tombstones win, and a refused owner never widens —
  `internal/store/memory_sync_test.go`.

## 9. Code map — what actually changed

| Area | Shipped change |
|---|---|
| `internal/store/memory.go` | Owner column on the memories table; owner-keyed reads (`MemoryCandidates`, `SearchMemories`, `GetMemories`, `ListMemories`, `MemoryIndex`); FTS index created only when a probe proves it |
| `internal/store/memory_owner.go` (new) | The owner vocabulary, the scope derivation, the migration with quarantine, re-home |
| `internal/store/memory_write.go` (new) | The one write door (`Write`), same-owner dedup, the failure journal, keyset pagination |
| `internal/store/memory_sync.go` (new) | `ExportMemoryEvents` / `ApplyMemoryEvents` and the shared fold the replay uses |
| `internal/gitidentity/project.go` (new) | The project key: normalized origin remote, path fallback |
| `internal/session/memory.go` | `ownerForScope` / `memoryOwners` propagation; `writeRemembered` as the one settle-and-write path every mouth walks; the decider-failure fallback; import rewrite; failure journaling and the rate-limited notice |
| `internal/session/session.go` | `Config.MemoryProjectKey` (the door's project proof) and `Config.MemoryIndex` (the conversation index, not the memory) |
| `internal/session/chatlog.go`, `agent.go`, `tools_conversations.go` | The chat journal indexes into `MemoryIndex`; the search verb survives memory off |
| `cmd/codeaf/chatv3.go`, `chatv3_process.go`, `chatv3_layout.go` | `v3Process.Search` (the index handle with memory off), `v3ProjectKey` at every door a config changes conversations, the re-home pass at process open |
| `cmd/codeaf/engine.go` | The engine's search closure reads the search store, not the memory store |
| `internal/manual/chat/what-i-remember.md` | Owners, project isolation, quarantine, the failure line, the import retry, search-with-memory-off |

## 10. What this ran on — the verification record

- **Full suite** (every package, sharded, fresh): green on the spark, snapshot
  `3613b87` — `make build && make test`, 2026-10-03. Two reds on the first pass
  were the test copy's environment, not the code (a gitignore rsync filter
  dropped `.claude/`/`.codeaf/` testdata for internal/skills; the worktree's
  `.git` pointer file broke internal/ci's ignore check); both re-ran green on
  the repaired copy. One real red — `TestTheFixedPrefixStaysUnderItsBudget`,
  37 bytes over after the remember tool's schema description grew — was fixed
  in the same change and re-proven green.
- **Race** over the memory-touched packages (store, session, gitidentity,
  scoped to the memory tests): green on the spark.
- **Real-model E2E** (`go test -tags e2e -run TestTUIE2E`, the tmux suite
  driving the real binary): 18 of 19 subtests green first-run on
  `deepseek/deepseek-v4.1-flash`; `foreign_skills_reach_the_conversation`
  failed once on the shared box's load and passed its immediate re-run —
  reported flaky, per the touched-packages rule, not fixed.
- **The model**: requested DeepSeek v4.1 Flash; resolved from the machine's
  own catalog as `deepseek/deepseek-v4.1-flash`; the door opened on it and
  the surface carried it. OpenRouter's own response metadata named
  `deepseek/deepseek-v4-flash` as what its endpoint served on some calls — a
  serving fact of the provider's, recorded here rather than smoothed over.
- **The demo** (recorded against the rebuilt final tree, isolated
  `CODEAF_HOME`, two throwaway git workspaces): `/remember` lands one row;
  the model keeps a project-scoped memory through the remember tool; the
  store holds `project:<key>` for it; `/forget` drops the user row; the same
  words twice answer the one row's title both times (the second retype
  refined the first — one row, not two); a fresh process in a second project
  sees the user row and NOT the other project's memory; `/memories amber`
  and `/forget amber` in that second project answer "nothing matched" and
  touch nothing. Artifacts: `~/codeaf-memory-demo/` (raw pane stream,
  step captures, an asciinema cast built from the real screens).

## 11. Open decisions

1. Embedding models per provider (Phase 3; interface is provider-agnostic).
2. JIT rerank ambiguity threshold (start: fused pool above cutoff > 2×k).
3. Team copy-to-share UX (confirmation required; exact surface TBD).
4. Project-key aliasing for pre-relay folder renames (hash chain vs. manual
   alias).
5. A per-project memory-place shelf view and a re-home verb on the quarantined
   shelf (the primitives are shipped; the surface is not).
6. The memory place in a LONG-LIVED process showed a momentary stale read in
   the demo — a row the store held and a fresh process saw, which the open
   process's place found only after its refresh beat. The store's own reads
   are proven fresh ([TestProbeSnapshotSeesALaterWrite]'s shape lives in the
   owner tests as TestMemoryPageWalksAStableCursor and the isolation tests);
   the surface-level staleness is pre-existing behaviour this wave did not
   introduce and did not fix, and it is written here so it is nobody's
   surprise later.
