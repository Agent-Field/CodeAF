# T03b build — one record for direction, lanes 1–3

2026-09-10. Lane `codex/personal-t03b` (Claude Code Opus on Spark), a child of
`codex/personal-ai-backend` at `caa0c5bb7`. The design is
`pai-wave03-refs/design-t03b/DESIGN.md` §6 lanes 1, 2 and 3; the review bar is
SCALE-AUDIT laws L1–L10. Every build and test below ran on Spark. No tui3 suite and
no broad suite ran (C21, C25).

**No lane here changes anything a person sees.** Lane 1 changes how the
collections database is opened. Lanes 2 and 3 add a library with no product
caller. So there is no manual change and no new change entry. The invalidations
at the end of this file are for the coordinator to fold into
`docs/changes/unreleased/662-personal-ai-backend.md` when the feature lands.

---

## Lane 1 — store compat, WAL, optimize

### Change → effect → evidence

| Change | Effect | Evidence |
|---|---|---|
| `initialize` accepts a store stamped newer than this build when its `store_meta` row `min_reader_version` is at most this build's version. It verifies the tables this build knows, in one read snapshot, and touches nothing else (`workspace/store.go` `acceptNewer`, `verifyNewer`) | A later schema bump no longer strands this build. Before, any unknown `user_version` was refused (`store.go:240-241` at `caa0c5bb7`) | `TestAnOlderBuildOpensANewerStoreThatDeclaresItReadable`: a synthetic v4 store with an extra table and `min_reader_version=3`. `Open` and `OpenExisting` both succeed, and every v3 door runs on it (create, rename, add, remove, members, parents, place, unplace, governing walk, context create, revise, withdraw, page, applies, history). Afterwards the stamp is still 4, and the newer table's row and the declaration are unchanged |
| A newer store that needs a newer reader, says nothing about readers, or declares nonsense is refused as `ErrNewerStore`, with the file left byte for byte | A caller can name the cause ("written by a newer aforge"), and a refused store is not rewritten (the WAL switch runs only after acceptance) | `TestANewerStoreThisBuildMayNotReadIsRefusedByNameAndLeftAlone`: three subtests plus a declared-readable store with `placements` dropped. The existing `TestOpenRefusesFutureForeignAndDamagedDatabases` (`user_version=99`, no declaration) still passes |
| `PRAGMA journal_mode=WAL` once, after a successful open. It is read first, so later opens take no lock for it. A switch SQLite declines leaves the old, correct mode, and the next open retries | Per-turn readers no longer lock a committing writer out under the one-second busy bound | `TestTheStoreIsInWriteAheadLoggingAndStaysThere`: a plain `sql.Open` handle afterwards finds `wal`. `TestAHeldReadSnapshotDoesNotLockAWriterOut`: a reader holds its snapshot for 1.5 s while another handle commits. Subtest 1 is the old rollback journal and the writer fails `database is locked`. Subtest 2 is WAL, where the writer commits and the reader's snapshot does not move. `TestConcurrentReadersAndWritersSeeNoLockRefusal`: 4 writer and 4 reader handles, 60 rounds each, zero refusals |
| `Close` runs `PRAGMA busy_timeout=0; PRAGMA analysis_limit=400; PRAGMA optimize` before closing | The planner statistics (`sqlite_stat1`) are kept up to date (L10). A close never waits for a peer; a busy close skips the refresh and loses no data | `TestClosingAHandleRefreshesThePlannersStatistics`: `sqlite_stat1` has rows for `placements` after a handle that used the index closes |

**The new tests fail on the old code.** Each was checked by removing the change and
running the test again:
- with the `optimize` line removed: `close left no statistics for placements: 0, … no such table: sqlite_stat1`;
- with `useWAL` removed: `journal mode "delete"` and `a writer was locked out by a reader's snapshot: database is locked (5) (SQLITE_BUSY)`;
- the old version switch refused every `user_version` above 3, so the newer-store test fails at its first `Open`.

### Validation (Spark)

```
go vet ./internal/workspace/                                   ok
go test -count=1 ./internal/workspace/                         ok  9.5s (whole package, old and new tests)
go test -count=1 ./internal/workspaceview/                     ok
go test -count=1 -run 'Collection|Organization|Placement' ./cmd/aforge/   ok
go test -count=1 -run '^(<every test in organization_test, governing_test, context_trace_test>)$' ./internal/session/   ok
make build                                                     ok  (bin/aforge)
go test -tags e2e -count=1 -run '^TestLocalWorkJourney$' ./internal/e2e/   PASS 3.0s (scripted model, real binary)
./scripts/laws.sh                                              ok
```

### Reversal

Revert the commit. To leave WAL, run `PRAGMA journal_mode=DELETE` with no other
handle open. Builds older than this lane handle a WAL store correctly: WAL is
SQLite's own format, not this store's.

---

## Lane 2 — direction schema v4 and write API (no product caller)

### Change → effect → evidence

| Change | Effect | Evidence |
|---|---|---|
| Schema v4 = DDL §1.5 verbatim (`workspace/direction_schema.go`), plus `store_meta` declaring `min_reader_version=3`. It is built by one table of per-version additions (`additions[v]`), so a fresh store and an upgraded one come from the same text. No CHECK on any vocabulary (L5) | The direction tables exist and are stamped. A v3 build that has lane 1 keeps using the store | `TestEnsureDirectionUpgradesAVersionThreeStoreAndKeepsItReadableByVersionThree`: v3 rows survive, and the v4 store passes exactly the check a v3 build makes (declaration, then `verifyVersion(3)`). **Cross-binary:** a real v4 store made by this lane was opened with `go run ./cmd/aforge` at the lane-1 commit `ea44755b3`. `collections list` answered and `collections create` wrote; afterwards `user_version=4`, the governing live row was intact and the mode was `wal`. The pre-lane-1 build `caa0c5bb7` refused it with `unsupported collections database (application 1095123788, version 4)`, which is F11, confirmed |
| The upgrade to v4 runs in one `BEGIN IMMEDIATE` that re-reads the stamp | A failure part-way leaves v3 intact | `TestAFailedDirectionUpgradeLeavesAWorkingVersionThreeStore`: a foreign `store_meta`, the last object v4 creates, fails the upgrade after every direction table was created inside it. Afterwards the stamp is 3, there are zero direction objects, the v3 rows are present, and the v3 doors work. `TestConcurrentDirectionUpgradesFromVersionOneHappenExactlyOnce`: 8 handles go from v1 to v4, with one `store_meta` row. `TestAVersionFourStoreMissingADirectionTableIsRefused`: refused byte for byte |
| **An ordinary `Open` does not upgrade to v4** (`ordinaryVersion = 3`). Only `workspace.Store.EnsureDirection`, called by `direction.Open`, creates the tables | Nothing that runs today stamps the owner's store v4, so a rollback to a pre-lane-1 binary (every `dev` build) stays safe until lane 1 is on `dev`. See the decision below | `TestAnOrdinaryOpenLeavesTheStoreAtVersionThree`. The existing upgrade tests now assert `ordinaryVersion` |
| `internal/direction`: vocabularies validated in Go; `lane()` is the one governing predicate; `Propose`, `Note`, `Revise`, `Accept`, `Reject`, `Withdraw`, `Supersede`, `Link`, `Unlink`, `Import`, `FinishImportRun`, `Rebuild`, `Verify`, `Current`, `At` | Every write goes through `Store.write`, one `BEGIN IMMEDIATE` (L1, L4). Inside it: `writeTx.fenced` checks the pointer, `checkAuthority` checks who may write what, and `put` writes the revision and its rows, maintains `direction_live` and moves the pointer | See the tests below |
| `PersonReceipt` has unexported fields and exactly four constructors (`FromCardAnswer`, `FromTerminal`, `FromPage`, `FromVerifiedStatement`). `As(class, ref)` names a non-person writer. `AsPerson(receipt)` is the only way to author as the person | A model, the extractor, a run, a voice or the Steward can only propose or note | `TestEveryWriterAndTransitionIsAllowedOrRefusedAsDesigned`: 43 rows of writer × transition × state, each allowed or refused (`ErrTransition`, `ErrNoReceipt`). A refusal leaves the record unmoved, and an allowed change carries a receipt exactly when §1.2 requires one. `TestOnlyReceiptsSpellThePersonAndOnlyImportSpellsMigration` |
| `receipt_law_test.go` (`go/ast`, on the laws gate) | The constructors are called only at allow-listed sites (none yet), no `internal/session/tools_*` file names the receipt or `AsPerson`, only the four constructors return a `PersonReceipt`, and `ReadSnapshot`/`WriteImmediate` are used only by this package | `TestPersonReceiptsAreMintedOnlyAtAllowListedSites`, `TestOnlyTheFourConstructorsSpellAPersonReceipt`, `TestOnlyTheDirectionPackageUsesTheStoresRawTransactions`. **Proven discriminating:** probe files `internal/session/tools_zz_lawprobe.go` (calls `FromCardAnswer`) and `cmd/aforge/zz_lawprobe.go` (calls `FromTerminal`) each produced a failure naming file:line. The probes were removed |
| Fences (L1) | A stale writer gets `ErrConflict` and nothing changes | `TestAStaleFenceIsRefusedAndTheWinnersRevisionStays` covers accept, reject, withdraw, revise, link and supersede. `TestRacingAnswersToOneCardProduceExactlyOneAcceptance`: two handles, one wins, one `ErrConflict`. `TestASupersedeWithOneMovedRecordWritesNothing`: all or nothing. `TestAcceptingAReplacementProposalSupersedesWhatItNamed` |
| `direction_live` derived by one function (`liveRows`) for both writes and `Rebuild` | Rebuild ≡ incremental | `TestRebuildingTheLiveIndexEqualsWhatTheWritesMaintained`: 24 seeds × 70 random operations, refusals included, with `Verify` after every step, then a rebuild compared row for row. At least a third of the steps must have applied. **Proven discriminating:** with the per-record `DELETE` in `refreshLive` removed, it fails at step 2. `TestTheLiveIndexHoldsExactlyTheLiveLanes` |
| Non-restoration (C16) | Wording the person rejected cannot be proposed again, as a new record or a revision, by anyone but the person | `TestRejectedWordingIsNotProposedAgainByAnyoneButThePerson` |
| Bounds | Every bound refuses the whole record, and a record exactly at every bound is accepted | `TestBoundsRefuseTheWholeRecord` (20 cases, plus a missing folder and a link to a missing record) |
| Statement receipt (§1.4) | A wake note, a peer line, a non-input line or an absent quote yields no receipt. A statement authorizes only the quoted words, cited by the line's hash | `TestAStatementReceiptComesOnlyFromThePersonsOwnWords` |
| Import primitive | Idempotent by `(store, id, sha256)`. It keeps a context's id and revision numbers, copies `legacy_delegated` without promoting it, and appends when the source changed. Only it may create a `legacy_workspace` place, which a person's later revision may keep but not add | `TestImportIsIdempotentByContentAndNeverMintsAPerson` |
| §5 guards | The column set is checked in, and the package imports only the organization store | `TestTheDirectionTablesHoldOnlyTheDesignedColumns`, `TestTheDirectionPackageImportsOnlyTheOrganizationStore` |

### Decisions taken inside the design, for review

- **D-1: the v4 upgrade is explicit, not at every open.** §6 says only "Upgrade v3 → v4
  in one IMMEDIATE transaction". The lane-1 fix is not on `dev`, so an upgrade at open
  would stamp the owner's store v4 the first time any feature-branch binary opened it.
  Every `dev` binary would then refuse it, and the governing read would stop execution.
  The cross-binary check above shows exactly that refusal. So `Open` stops at v3 and
  `direction.Open` upgrades. **Switching to upgrade-at-open is one line**
  (`ordinaryVersion = schemaVersion`) once lane 1 is on every binary the owner might
  roll back to.
- **D-2: additions to the §6 verb list**, each required elsewhere in the design:
  - `Revise`, for §1.1 revise and the §4.1 "revise its own proposal" row;
  - `Unlink`, so a person can take back a `conflicts_with`;
  - `Current` and `At`, which are the §2.1 exposure-receipt reads;
  - `Verify`, the §2.5 integrity check;
  - `FinishImportRun`, for the `direction_import_runs.finished_at` column.
- **D-3: state-machine readings.**
  - A proposal ends in accepted or rejected. Withdraw applies to accepted records and
    to findings.
  - Withdrawn → accepted is the person taking it back.
  - Supersede applies only to live records (proposed, accepted, informational).
  - A proposal carrying `supersedes` links replaces those records only when the
    person accepts it, fenced at the named revisions. Adding a `supersedes` link to a
    non-proposal is refused.
  - `overrides` and `conflicts_with` only go from a rule or decision.
  - A writer may revise its own proposal or finding, where "own" means the same author
    class and ref as the current revision.
  - Only a person may revise an accepted record, and that revision carries their
    receipt.
  - Rejected wording blocks proposals by every writer except the person.
- **D-4: receipts are stamped with the write's time.** The constructors take no time,
  so a receipt's `at` is when the store recorded the act, not a caller's clock.

### Adjustment to a lane-1 test

Under parallel package load, lane 1's `TestConcurrentReadersAndWritersSeeNoLockRefusal`
failed once with `database is locked`. The failure was between its four *writers*.
That case is SQLite's single-writer queue under the 1 s bound; it is outside the §6
acceptance ("concurrent reader and writer"), and WAL does not change it. The test
became `TestConcurrentReadersBesideAWriterSeeNoLockRefusal`: one writer, four readers,
120 rounds, with errors labelled by side. It passes three runs in a row and under
parallel load. The discriminating 1.5 s held-snapshot test is unchanged.

### Validation (Spark)

```
go vet ./internal/direction/ ./internal/workspace/            ok
go test -count=1 ./internal/direction/ ./internal/workspace/   ok  10.5s / 11.8s
go test -count=1 ./internal/workspaceview/                     ok
go test -count=1 -run 'Collection|Organization|Placement' ./cmd/aforge/   ok
go test -count=1 -run '^(<organization, governing, context_trace tests>)$' ./internal/session/   ok
make build                                                     ok
make changelog-check                                           412 entries, all well formed
go test -tags e2e -count=1 -run '^TestLocalWorkJourney$' ./internal/e2e/   PASS 6.5s
./scripts/laws.sh                                              ok (20 packages; internal/direction on the gate)
```

### Reversal

The tables are inert and not even created by ordinary opens. Revert the commit. A
store that `direction.Open` already stamped v4 stays readable by lane 1 and later
builds (`min_reader_version=3`).

---

## Lane 3 — the resolver (library)

### Change → effect → evidence

| Change | Effect | Evidence |
|---|---|---|
| `direction.Resolve(ctx, Subject) (Effective, error)` per §2. It runs in one read snapshot. Its steps are: the upward placement closure (UNION over edges, `CROSS JOIN` from the frontier, `LIMIT 257`); one probe of `direction_live` per place; reach, exclusions and links in Go; bodies only for what it returns | One call answers what governs, what is pending and what is informational for a piece of work, with provenance (`Via`, `Blocked`, `Overrides`, `OverriddenBy`, `Legacy`), a snapshot id and the governing ancestry (`Placements`) | The §2.3 tests below |
| `Informational(ctx, subj, offset, limit)`: direct targets plus **one** membership hop (and `everywhere` / the legacy workspace), paged at ≤ 50 with `More` | Membership supplies relevance only in this lane (C15, C23) | `TestR10FindingsAreInformationalOneMembershipHop`: direct plus one hop, never two; pages of 10 and the last page |
| Bounds: `ErrGoverningTooLarge` (a `*GoverningTooLargeError` naming the heaviest target) above 64 records or 64 KiB; `ErrClosureTooLarge` above 256 edges | Admitted whole or stopped, never truncated | `TestResolveStopsAtItsBoundsAndNamesTheHeaviestTarget`: 65 records name the folder; 2 × 40 KiB records; a 257-edge chain |

**Every §2.3 row and worked check is a test:**
- R1 `TestR1GoverningReachWalksPlacementsOnly`
- R2 and C23 `TestR2AndC23DescendantsOnlyWhenTheRuleSaysSubtree`
- R3 `TestR3DirectionIsAUnionAcrossParentsInDisplayOrder`
- R4 and the C16 conference exception `TestR4AndC16AnOverrideDeliversBothAndAnnotatesThem`
- R5 `TestR5ExcludingTheSubjectRemovesTheRecord`
- R6 `TestR6AnExcludedParentBlocksOnlyThePathsThroughIt`: Via `[Europe, Trips]`, Blocked `[Conference, Trips]`; work only under Conference is not governed
- R7 and C16 move vs reference `TestR7AndC16AMoveChangesFutureResolutionAndKeepsWhatWasSeen`
- R8 `TestR8AConflictWhereBothApplyIsReturnedNotDecided`
- R9 `TestR9ProposalsArePendingNeverGoverning`: page of 20 with `PendingMore`, and the 30-day window
- R10 as above
- R11 `TestR11ALegacyWorkspaceMatchesImportedRecordsByCleanedPath`
- R12 `TestR12ASupersededRecordNoLongerReaches`
- C15 trip unlink `TestC15ATripsOwnBudgetSurvivesUnlinkingAndReachesNoOtherTrip`
- E1 sentence `TestE1OneSentenceIsOneGoverningRecord`
- subject validation `TestResolveRefusesAMalformedSubject`
- §5: `TestWhatAResolveDeliversCarriesNoGrantOrAttention`

### The load model and its measured numbers (Spark, 2026-09-10)

The fixture (`load_test.go`) is built once per test binary, in about 7.5 s:
- 2,000 folders in 7 layers (8, 24, 64, 160, 400, 800, 544). Every folder below the
  roots has two adjacent parents, so the DAG has diamonds everywhere;
- **exactly 100,000 placements**: folder edges, 20,000 chats in 1–3 folders, tasks in
  1–2, standing items and artifacts;
- 20,000 reference memberships;
- **50,000 records** with 123,000 revisions and 246,060 target rows. 10,000 are live:
  3,000 governing, 2,000 pending, 5,000 informational. 40,000 are rejected, withdrawn
  or superseded;
- exclusions on 5% of live records (half chats, half folders) and links on 3% of
  governing ones;
- `direction_live` is derived by the real `Rebuild`. The store is then closed once,
  so `PRAGMA optimize` leaves real statistics.

The 1,000 random subjects are 70% a chat, 20% a task with its chat, and 10% a
standing item with a chat; half carry a physical root. Walked per subject:

| | p50 | p90 | p99 | max |
|---|---|---|---|---|
| closure edges | 49 | 80 | 109 | 125 |
| governing records | 30 | 43 | 54 | 63 |
| pending shown | 8 | 12 | 16 | 20 |
| informational page | 18 | 30 | 38 | 46 |
| statements sent | 9 | 10 | 10 | 10 |

Zero of 1,000 subjects crossed the governing bound.

**Latency** (`AFORGE_DIRECTION_MEASURE=1 go test -run TestMeasureResolveLatency -v
./internal/direction/`; load average about 3 on 20 cores). Each subject pays
`OpenExisting`, which includes schema verification, then `Resolve`, then `Close`,
which includes `PRAGMA optimize`:

| run | open+resolve+close p50 | p90 | **p99** | max | resolve only p99 |
|---|---|---|---|---|---|
| 1 | 4.28 ms | 6.49 ms | **8.11 ms** | 33.1 ms | 6.34 ms |
| 2 | 4.03 ms | 6.29 ms | **7.80 ms** | 32.3 ms | 6.18 ms |
| 3 | 4.19 ms | 6.31 ms | **8.07 ms** | 27.3 ms | 6.24 ms |

**p99 ≈ 8 ms against the 50 ms budget.** The max is the first, cold call. PERF.md
forbids wall-clock gates, so the suite gates on work instead:
- `maxResolveStatements = 10`;
- the plan goldens below.

The latency test runs only when asked for and asserts the 50 ms budget when it
does. PERF.md has a new section, "The direction resolver's cost".

### EXPLAIN goldens (L10) — no SCAN on any growing table

`TestTheResolversPlansSeekAndNeverScanAGrowingTable` covers every statement a
resolve sends, plus the write path's hot lookups. Each is planned against the load
model with its statistics, and compared with `internal/direction/testdata/explain/*.txt`.
The same statements are planned against an empty store with no statistics. In both
cases a `SCAN` of anything but the key lists (`q`, `c`, `x`, `keys`, the recursive
queue `u`/`up`, constant rows) fails. The checked-in plans:

```
closure      SEARCH p USING INDEX placements_reference (kind=? AND ref_id=? AND session_id=?)   ×2 (setup, recursive step)
candidates   SEARCH l USING PRIMARY KEY (target_kind=? AND ref_id=? AND session_id=? AND lane=?)
exclusions   SEARCH e USING COVERING INDEX sqlite_autoindex_direction_exclusions_1 (record_id=? AND revision=?)
links        SEARCH k USING COVERING INDEX sqlite_autoindex_direction_links_1 (record_id=? AND revision=? AND link_kind=?)
bodies       SEARCH r USING INDEX sqlite_autoindex_direction_revisions_1 (record_id=? AND revision=?)
legacy       SEARCH g USING INDEX direction_legacy_record (record_id=?)
informational SEARCH m USING INDEX memberships_reference (...); SEARCH l USING PRIMARY KEY (... lane=?)
snapshot     SEARCH direction_revisions            (max(seq) on the rowid)
live-refresh SEARCH direction_live USING COVERING INDEX direction_live_record (record_id=?)
rejected-text SEARCH direction_revisions USING COVERING INDEX direction_revisions_rejected (text_sha256=?)
legacy-key   SEARCH direction_legacy USING INDEX sqlite_autoindex_direction_legacy_1 (...)
```

**Proven discriminating:**
- Joining exclusions on `revision` alone yields `exclusions plans a scan of a table
  that grows: "SCAN e USING COVERING INDEX …"`, on both the load model and the empty
  store.
- Defeating the closure's index yields `closure's plan changed`.

### Readings and open points for review

- **`Pending` is `[]Applied` plus `PendingMore`.** §2.1 caps pending at 20 but gives
  it no "more" flag. Without one, a 21st proposal would vanish silently, so the flag
  was added. `Effective` also echoes `Phase`, which §2.1 says is "recorded in
  provenance".
- **`Via` keeps one shortest surviving chain per subject ref**, and `Blocked` keeps
  one chain per excluded folder on a way to the target. §2.1 says "every surviving
  path", but the number of paths in a two-parent DAG grows exponentially, so the
  provenance is bounded by refs and exclusions instead. The reach *decision* is exact:
  a BFS that avoids the excluded folders.
- **A record fully removed by exclusions is not delivered, so its `Blocked` paths
  are not shown.** R5 says excluding the subject removes the record entirely. The
  design has no `Effective` field for excluded records. If the product wants them
  visible, that is an additive `Excluded []Applied`.
- **Open semantics question: a folder exclusion on an `everywhere` rule.** §2.2 step 4
  removes "only the paths through that collection". An `everywhere` target has no
  folder path, so as written such an exclusion has no effect. That is the literal
  reading, and it is what the code does. The alternative treats `everywhere` as a
  root above all folders, so that work reachable only through the excluded folder is
  exempt. No writer can create that combination until lane 7. **The owner or the
  design decides.**
- **A `conflicts_with` pair is resolved when an `overrides` link joins the two
  records.** This is the reading of "unresolved" in R8. Links are read from the
  side that carries them (`has_links`); the reverse `direction_links_to` read in
  step 5 is not needed, because R4 and R8 require both records to apply, so both are
  already candidates.
- **An override holds by record id.** `to_revision` records what the person saw, but
  a later revision of the overridden rule does not cancel the override.
- **The bound constants are here and in `internal/session/standing_world.go`**
  (`governingHoldLimit`, `governingPromptBytes`). Lane 6, which makes the governing
  block read `Effective`, should interpolate `direction.MaxGoverning` and
  `MaxGoverningBytes` and delete the session copies (the one-source-of-truth law).

### Validation (Spark)

```
go vet ./internal/direction/ ./internal/workspace/            ok
go test -count=1 ./internal/direction/                         ok  23.3s (load model included)
go test -count=1 -short ./internal/direction/                  ok  12.7s (load model skipped)
go test -count=1 ./internal/workspace/ ./internal/workspaceview/   ok
make build                                                      ok
make changelog-check                                           412 entries, all well formed
./scripts/laws.sh                                              ok (20 packages)
```

---

## Invalidations for the feature's change entry

These are for the coordinator to add to `662-personal-ai-backend.md` when the
feature lands on `dev`:

- "`collections.db` refused any `user_version` it did not know, so an upgrade stranded every older build. A build now opens a newer store that declares it a permitted reader (`store_meta.min_reader_version`), and refuses any other newer store with `ErrNewerStore`, leaving it untouched."
- "`collections.db` used the rollback journal with a 1 s lock bound. It is now in WAL, set once by the first open, and statistics are refreshed at close (`PRAGMA optimize`)."
- "Organization schema 3 was the newest. Schema 4 adds the direction record tables, but only `direction.Open` creates them; an ordinary open leaves a store at 3."
- "Direction was held in three stores (holds, shared context, memory decisions). `internal/direction` now holds the one record and its resolver, with no product caller yet; the old stores still govern."
