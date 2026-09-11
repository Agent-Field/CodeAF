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

The independent review of `caa0c5bb7..dc9251859` found lanes 2–3 not mergeable,
with seven confirmed blockers. The fixes are in two commits after `dc9251859`,
and the [review pass](#review-pass--seven-blockers) section below records them.
Where a lane section below said something the review showed wrong, it is
corrected in place and marked *(review)*.

---

## Lane 1 — store compat, WAL, optimize

### Change → effect → evidence

| Change | Effect | Evidence |
|---|---|---|
| `initialize` accepts a store stamped newer than this build when its `store_meta` row `min_reader_version` is at most this build's version. It verifies the tables this build knows, in one read snapshot, and touches nothing else (`workspace/store.go` `acceptNewer`, `verifyNewer`) | A later schema bump no longer strands this build. Before, any unknown `user_version` was refused (`store.go:240-241` at `caa0c5bb7`) | `TestAnOlderBuildOpensANewerStoreThatDeclaresItReadable`: a synthetic v4 store with an extra table and `min_reader_version=3`. `Open` and `OpenExisting` both succeed, and every v3 door runs on it (create, rename, add, remove, members, parents, place, unplace, governing walk, context create, revise, withdraw, page, applies, history). Afterwards the stamp is still 4, and the newer table's row and the declaration are unchanged |
| A newer store that needs a newer reader, says nothing about readers, or declares nonsense is refused as `ErrNewerStore`, with the file left byte for byte | A caller can name the cause ("written by a newer aforge"), and a refused store is not rewritten (the WAL switch runs only after acceptance) | `TestANewerStoreThisBuildMayNotReadIsRefusedByNameAndLeftAlone`: three subtests plus a declared-readable store with `placements` dropped. The existing `TestOpenRefusesFutureForeignAndDamagedDatabases` (`user_version=99`, no declaration) still passes |
| `PRAGMA journal_mode=WAL` once, after a successful open. It is read first, so later opens take no lock for it. **WAL is best-effort** *(review)*: a failed read of the mode, or a switch SQLite declines (another handle holding the rollback journal, a filesystem without shared memory), is ignored, the store stays in its old and correct mode, and the next open retries. Nothing here guarantees WAL; the behaviour below holds once it is set | Per-turn readers no longer lock a committing writer out under the one-second busy bound | `TestTheStoreIsInWriteAheadLoggingAndStaysThere`: a plain `sql.Open` handle afterwards finds `wal`. `TestAHeldReadSnapshotDoesNotLockAWriterOut`: a reader holds its snapshot for 1.5 s while another handle commits. Subtest 1 is the old rollback journal and the writer fails `database is locked`. Subtest 2 is WAL, where the writer commits and the reader's snapshot does not move. `TestConcurrentReadersBesideAWriterSeeNoLockRefusal` *(review: narrowed in lane 2, see below)*: one writer and four readers, zero refusals. Writer-against-writer contention is not claimed |
| `Close` runs `PRAGMA busy_timeout=0; PRAGMA analysis_limit=400; PRAGMA optimize` before closing. **It never fails a close** *(review)*: its error is discarded | The planner statistics (`sqlite_stat1`) are refreshed when a close finds the database free (L10). A close never waits for a peer; a busy close skips the refresh and loses no data, so statistics can lag on a store whose every close is contended | `TestClosingAHandleRefreshesThePlannersStatistics`: `sqlite_stat1` has rows for `placements` after a handle that used the index closes |

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
| `internal/direction`: vocabularies validated in Go; `lane()` is the one governing predicate; `Propose`, `Note`, `Revise`, `Accept`, `Reject`, `Withdraw`, `Supersede`, `Link`, `Unlink`, `Import`, `FinishImportRun`, `Rebuild`, `Verify`, `Current`, `At` | Every write goes through `Store.write`, one `BEGIN IMMEDIATE` (L1, L4). Inside it: `writeTx.fenced` checks the pointer, `permit` holds the revision to the §4.1 table *(review: was `checkAuthority`, which was broader than §4.1)*, and `put` writes the revision and its rows, maintains `direction_live` and moves the pointer | See the tests below |
| `PersonReceipt` has unexported fields and exactly four constructors (`FromCardAnswer`, `FromTerminal`, `FromPage`, `FromVerifiedStatement`). `As(class, ref)` names a non-person writer. `AsPerson(receipt)` is the only way to author as the person | Each writer creates and moves exactly what its §4.1 row says *(review: "can only propose or note" was wider than §4.1)* | See the review pass: `TestEachWriterCreatesExactlyWhatSection41Allows`, `TestEveryWriterAndTransitionIsAllowedOrRefusedAsDesigned` |
| `receipt_law_test.go` (on the laws gate) | The constructors are called only at allow-listed sites (none yet), no `internal/session/tools_*` file names the receipt or `AsPerson`, only the four constructors return a `PersonReceipt`, and `ReadSnapshot`/`WriteImmediate` are used only by this package | *(review)* The original laws matched `pkg.Name` spellings and were walked around by a dot import and by `s.Workspace().WriteImmediate`. They now type-check; see the review pass |
| Fences (L1) | A stale writer gets `ErrConflict` and nothing changes | `TestAStaleFenceIsRefusedAndTheWinnersRevisionStays` covers accept, reject, withdraw, revise, link and supersede. `TestRacingAnswersToOneCardProduceExactlyOneAcceptance`: two handles, one wins, one `ErrConflict`. `TestASupersedeWithOneMovedRecordWritesNothing`: all or nothing. `TestAcceptingAReplacementProposalSupersedesWhatItNamed` |
| `direction_live` derived by one function (`liveRows`) for both writes and `Rebuild` | Rebuild ≡ incremental | `TestRebuildingTheLiveIndexEqualsWhatTheWritesMaintained`: 24 seeds × 70 random operations, refusals included, with `Verify` after every step, then a rebuild compared row for row. At least a third of the steps must have applied. **Proven discriminating:** with the per-record `DELETE` in `refreshLive` removed, it fails at step 2. `TestTheLiveIndexHoldsExactlyTheLiveLanes` |
| Non-restoration (C16) | Wording the person rejected cannot be proposed again, as a new record or a revision, by anyone but the person | `TestRejectedWordingIsNotProposedAgainByAnyoneButThePerson` |
| Bounds | Every bound refuses the whole record, and a record exactly at every bound is accepted | `TestBoundsRefuseTheWholeRecord` (20 cases, plus a missing folder and a link to a missing record) |
| Statement receipt (§1.4) | A wake note, a peer line, a non-input line or an absent quote yields no receipt. A statement authorizes only the quoted words, cited by the line's hash | `TestAStatementReceiptComesOnlyFromThePersonsOwnWords` |
| Import primitive | Idempotent by `(store, id, sha256)`. It keeps a context's id and revision numbers, copies `legacy_delegated` without promoting it, and appends when the source changed. Only it may create a `legacy_workspace` place, which a person's later revision may keep but not add. *(review: it accepted a caller-supplied person receipt, appended stale versions over newer ones and over a person's change, and renumbered a context's later revisions; all four are fixed, see the review pass)* | `TestImportIsIdempotentByContentAndNeverMintsAPerson` |
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
- **D-3: state-machine readings.** *(review: not approved as design compliance and
  replaced by the §4.1 table in `authority.go`; the list below is what the first
  build did, kept for the record.)*
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
  *(review: approved.)* Precisely: `at` is read inside the write transaction once
  it holds the writer lock (`Store.write`), so receipts on one store order as their
  commits do; it precedes the commit by the transaction's own few statements, and it
  is not the instant the person answered, which the receipt's `ref` names.

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
| Bounds: `ErrGoverningTooLarge` (a `*GoverningTooLargeError` naming the heaviest target) above 64 records or 64 KiB; `ErrClosureTooLarge` above 256 edges | Admitted whole or stopped, never truncated. *(review: "heaviest" counted paths; it now counts each record once per target — by records over the record bound, by bytes over the byte bound)* | `TestResolveStopsAtItsBoundsAndNamesTheHeaviestTarget`: 65 records name the folder; 2 × 40 KiB records; a 257-edge chain. `TestTheHeaviestTargetCountsRecordsNotPaths` |

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

**Latency, first build, no import history** *(review: re-measured with 20 re-imports
per live rule in the review pass)* (`AFORGE_DIRECTION_MEASURE=1 go test -run
TestMeasureResolveLatency -v ./internal/direction/`; load average about 3 on 20 cores). Each subject pays
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

*(review: superseded by "Plans now checked in" in the review pass. The goldens are
now captured from the statements a resolve sends, and `bodies` and `legacy`
changed.)*

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
- **`Via` holds representative paths, not every path** *(review: labelled so in the
  code too)*: one shortest surviving chain per subject ref for a folder target, and
  `Blocked` keeps one chain per excluded folder on a way to the target. §2.1 says
  "every surviving path", but the number of paths in a two-parent DAG grows
  exponentially, so the provenance is bounded by refs and exclusions instead. The
  reach *decision* is exact: a BFS that avoids the excluded folders.
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
  records in one direction.** This is the reading of "unresolved" in R8. *(review:
  mutual overrides — A over B and B over A — are an unresolved conflict, annotate
  neither record, and never hide a `conflicts_with`.)* Links are read from the
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

## Review pass — seven blockers

The review of `caa0c5bb7..dc9251859` confirmed seven blockers in lanes 2–3 and
approved D-1, D-2's purpose and D-4; D-3 was not accepted as design compliance.
Every blocker below got a regression first, run against `dc9251859` and shown
failing (`/tmp/t03b-review-head.log` on Spark), then the fix.

| Commit | Contents |
|---|---|
| `8ebfffc88` | Write path: blockers 1–5, `Verify`'s dangling pointer, `FinishImportRun` bounds, D-4 wording |
| `97a3023df` | Resolver: blockers 6–7, mutual overrides, delivered-record drift check, informational paging in SQL, executed-statement plan goldens, fixed-clock load model, `Via` wording |

### Per blocker: change → regression → what it said on `dc9251859`

| # | Change | Regression (failing on `dc9251859`) |
|---|---|---|
| 1 | An import never spells the person. `ImportRevision.Receipt` is legacy evidence: `legacy_person` (new: the person adopted it through the old store's own door), `legacy_delegated` or `legacy_unknown`. An item that claims `ActorPerson` is refused whole in `ImportItem.validate`, and `checkReceipt` refuses it again at `put` for any migration revision. The importer is on its own allow-list, `importSites` (empty until lane 4), under a type-checked law | `TestAnImportCannotCarryAPersonReceipt`: `an import through the card door wrote a person receipt: {Outcome:imported …}` for all four doors, and `refused imports left 4 records`. `TestTheImporterIsCalledOnlyAtAllowListedSites` did not exist; a probe calling `s.Import` passed every law on `dc9251859` |
| 2 | Import is fenced on the source version. `Legacy.Version` is ordered counters (`3/1`, `2`); a version no newer than the newest imported is `Stale`, a no-op with a report line (`ImportResult.Reason`). A record whose current revision is not the one the last import left, or not written by migration, is `Refused` with `ErrConflict` and the reason; the run row is still kept. The §4.1 migration row cannot follow another writer's revision (`followsOthers`), so `put` refuses it too | `TestAStaleImportNeverAppendsOverANewerOne`: `importer A's stale version: {Outcome:appended … Revision:3}; the record is now revision 3 "at most $250 a night"`. `TestAnImportNeverOverwritesWhatThePersonChanged`: `{Outcome:appended … Revision:3}; the record is now revision 3 accepted` |
| 3 | A kept identity keeps its revision numbers. A numbered history (`ImportItem.ID` set, each revision with its `WrittenAt`) is compared revision by revision with what is stored (`sameContent`); the first difference is `Refused`, and otherwise every missing revision is appended under its own number | `TestAContextImportedInStepsKeepsEveryRevisionNumber`: `(cccccc, 2) reads "the venue holds 50"; the old store's revision 2 said "the venue holds 45"`. `TestADivergentContextHistoryIsRefusedNotOverwritten`: `{Outcome:appended … Revision:3}, <nil>` |
| 4 | `Store.Workspace()` is gone; nothing exported hands out the workspace store, a transaction or a database. The receipt, importer and raw-door laws type-check every file that spells a watched name (`go list -export` supplies dependencies' export data; no new module) and judge each identifier by the object it resolves to. A method *declared* with a raw-door name (the interface route) is refused; a file outside the default build that spells a watched name is refused as unresolvable | On `dc9251859`, a probe with a dot-imported `FromCardAnswer`, `s.Workspace().WriteImmediate` and `s.Import` passed all three old laws (`/tmp/t03b-review-head-laws.log`). The new `TestTheDirectionAPIHandsOutNoRawStore`, run in a temporary worktree at `dc9251859`: `direction.Store.Workspace exposes func() *…/workspace.Store`. After the fix the same probe fails three laws at `probe.go:19`, `:23` and `:15`. `TestTheLawsCatchEveryRouteAroundThem` keeps four such probes under `testdata/lawprobe` |
| 5 | §4.1 is one table (`section41` in `authority.go`) and `permit` consults it for every revision: what each writer creates (kinds, state, required source class or quote origin, untargeted), which moves it makes, whether only on its own record, which link kinds, and whether it may write exclusions. The doors no longer decide | `TestEachWriterCreatesExactlyWhatSection41Allows` (27 cells) and `TestEveryWriterAndTransitionIsAllowedOrRefusedAsDesigned` (49 rows), written from the design's table: 12 create cells and 8 transition rows failed on `dc9251859` — the extractor creating targeted, rule, adopted-wording or finding records, a run proposing from a conversation, a voice proposing a rule, the Steward noting a finding or writing an exclusion, a model writing exclusions or precedence links, non-person writers revising their own findings or the Steward and extractor revising their own proposals, and the person superseding a proposal or withdrawing or superseding a finding |
| 6 | `direction_legacy_record` is on `(record_id, revision)`. The resolver's legacy read is a backwards covering seek for the newest mapping's rowid, then a rowid seek: two seeks per delivered record, whatever the import history. The import path's own lookup is the same shape. The load model now carries 65,000 mappings (20 per live governing rule) | `TestTheNewestLegacyNameIsReadWithOneSeekPerRecord`: `the legacy read returned 20 rows for one record`. **Plan gate proven:** with the old single-column index put back, `legacy's plan changed … USE TEMP B-TREE FOR ORDER BY` (and the same for `legacy-newest`) |
| 7 | The heaviest target counts each record once per target. Over the record bound it ranks by records (bodies are not read); over the byte bound, by bytes. The error carries `HeaviestRecords` and `HeaviestBytes` | `TestTheHeaviestTargetCountsRecordsNotPaths`: `collection f334f7bc… contributes 50, want folder B (aafe7303…) with 40 records` |

### The non-blocking items

| Item | Done | Evidence |
|---|---|---|
| Integrity before resolution | **Partly, and differently from the review's first suggestion.** Every resolve checks what it delivers against the records: the body read joins the record's pointer, so a live row naming a non-current revision, or a lane its revision is not in, is `ErrDrift`. `Verify` and `Rebuild` now refuse a record whose pointer names no revision (an outer join) | `TestAResolveRefusesALiveRowThatIsNotTheCurrentRevision` (failed on `dc9251859`: it delivered the drifted rule). `TestVerifyCatchesADanglingCurrentPointer` (failed: `passed Verify: <nil>`). **Not at every open:** a full `Verify` of the load model takes 1.10–1.13 s on Spark, and every turn opens the store; §2.5's background check at open belongs with the product lane that owns a process |
| Mutual overrides | A over B and B over A is an unresolved `Conflict`; neither is annotated; a `conflicts_with` stays. Longer cycles need no rule, because overrides annotate pairs and delivery is a union | `TestMutualOverridesAreAnUnresolvedConflict` (failed: `conflicts []`) |
| `Via` wording | "Representative paths" in `Applied`'s doc and above | — |
| Informational paging | The page is cut in SQL: `GROUP BY record_id ORDER BY written_at DESC, record_id LIMIT ? OFFSET ?`, with each finding's matched places as one JSON array. Only the page crosses into Go. SQLite still sorts the rows matched at the subject's own places in a temp B-tree | `TestAnInformationalPageIsCutInTheStore`: 60 findings, a page of 10 reads 11 rows; the `informational` golden |
| Plan coverage from executed statements | Every resolver statement carries a `/* name */` prefix; the plan test records what a real resolve sent and plans exactly that, with its arguments. The write path's lookups are planned from the variables it uses. A golden no statement sends fails | `TestTheResolversPlansSeekAndNeverScanAGrowingTable` |
| Time-stable fixture | Every fixture date is drawn back from `loadNow`, and every load-model resolve runs at `loadNow` | — |
| WAL best-effort, optimize non-failing | Stated in lane 1 above | — |
| `FinishImportRun` | Counts are a JSON object of at most 4 KiB; runs are kept forever with their reports (§1.7) | `TestAFinishedImportRunKeepsABoundedCount` |

### Readings the fixes needed, for review

- **`legacy_person` is a new receipt actor.** §3.2 maps `Adoption{Actor: person}` to
  `receipt_actor=person`, and §4.1 forbids migration to "mint `receipt_actor=person`
  for anything that lacked it". The review ruled that no import carries `person`, so
  a hold the person adopted through the old store's door imports as `legacy_person`:
  evidence of an act this runtime did not see. It governs, labelled like the other
  two legacy actors, and lane 7's one-tap confirmation turns it into a person
  receipt. **Lane 4 maps person adoptions to it.** The column is open text; nothing
  in the DDL changes.
- **Where §4.1 is silent, the table refuses.** Two cells come from elsewhere in the
  design: the person revises a live record in place (§1.1), which is how exclusions
  and links are written, and only an import moves a finding out of `informational`
  (§3.4). **So no writer today may withdraw or supersede a finding** — including the
  person. Today's shared context lets it be withdrawn. **Owner or design: should the
  person's §4.1 row gain informational → withdrawn?** It is one line in `section41`.
- **Link kinds per writer.** §4.1 gives overrides and conflicts_with to the person
  alone. The model may also write `supersedes` (its `propose_change`: a replacement
  applied only if the person accepts) and `derived_from`; every other writer only
  `derived_from` (provenance, which the voice row's "citations in links" needs).
- **The voice row is Q7's yes.** If Q7 is no, only that row of `section41` changes.
- **Import outcomes.** `Stale` (nothing written, no error) and `Refused` (nothing
  written, `ErrConflict`) join `Imported`, `Unchanged` and `Appended`, each with a
  report line in `ImportResult.Reason`. §3.7's "non-zero exit on any skipped source"
  is lane 4's to apply to them.
- **The v4 DDL changed in place.** One index gained a column. No build ships v4 yet;
  the only v4 stores were test stores. A store made by `dc9251859`'s code keeps the
  one-column index and is still correct, only slower on provenance.

### Updated load model and latency (Spark, 2026-09-10)

50,000 records with **177,000 revisions**, 353,232 target rows and **65,000 legacy
mappings**; built in about 11 s. The walks are unchanged (closure p50 49 / max 125;
governing p50 30 / max 63, and every governing record now carries a legacy name);
statements per resolve p50 9, max 10.

| run | open+resolve+close p50 | p90 | **p99** | max | resolve only p99 |
|---|---|---|---|---|---|
| 1 | 5.18 ms | 8.04 ms | **10.12 ms** | 36.1 ms | 8.30 ms |
| 2 | 5.35 ms | 8.30 ms | **10.37 ms** | 35.1 ms | 8.47 ms |
| 3 | 5.29 ms | 7.84 ms | **9.80 ms** | 42.2 ms | 7.98 ms |

**p99 ≈ 10 ms against the 50 ms budget**, up about 2 ms from the first build. The
difference is a legacy name for every governing record (none were imported before)
and the pointer join on every body.

### Plans now checked in

```
closure       SEARCH p USING INDEX placements_reference (kind=? AND ref_id=? AND session_id=?)   ×2
candidates    SEARCH l USING PRIMARY KEY (target_kind=? AND ref_id=? AND session_id=? AND lane=?)
exclusions    SEARCH e USING COVERING INDEX sqlite_autoindex_direction_exclusions_1 (record_id=? AND revision=?)
links         SEARCH k USING COVERING INDEX sqlite_autoindex_direction_links_1 (…)
bodies        SEARCH d USING INDEX sqlite_autoindex_direction_records_1 (id=?)
              SEARCH r USING INDEX sqlite_autoindex_direction_revisions_1 (record_id=? AND revision=?)
legacy        SEARCH g USING INTEGER PRIMARY KEY (rowid=?)
              CORRELATED SCALAR SUBQUERY: SEARCH n USING COVERING INDEX direction_legacy_record (record_id=?)
informational SEARCH m USING INDEX memberships_reference (…); SEARCH l USING PRIMARY KEY (… lane=?);
              USE TEMP B-TREE FOR GROUP BY / ORDER BY (over the matched rows only)
snapshot      SEARCH direction_revisions
live-refresh  SEARCH direction_live USING COVERING INDEX direction_live_record (record_id=?)
rejected-text SEARCH direction_revisions USING COVERING INDEX direction_revisions_rejected (text_sha256=?)
legacy-key    SEARCH direction_legacy USING INDEX sqlite_autoindex_direction_legacy_1 (…)
legacy-newest SEARCH g USING INDEX direction_legacy_record (record_id=?); SCALAR SUBQUERY: SEARCH direction_legacy USING INDEX sqlite_autoindex_direction_legacy_1 (source_store=? AND source_id=?)
```

### Validation (Spark)

```
go vet ./internal/direction/ ./internal/workspace/ ./internal/workspaceview/   ok
go test -count=1 ./internal/direction/ ./internal/workspace/ ./internal/workspaceview/
                                                    ok  28.0s / 11.5s / 0.5s (load model included)
go test -count=1 -short ./internal/direction/        ok  14.8s
go test -count=1 -run 'Collection|Organization|Placement' ./cmd/aforge/   ok
go test -count=1 -run '^(<30 tests of organization, governing, context_trace>)$' ./internal/session/   ok
make build                                           ok
make changelog-check                                 ok
./scripts/laws.sh                                    ok (66 files, 20 packages; internal/direction included)
go test -tags e2e -count=1 -run '^TestLocalWorkJourney$' ./internal/e2e/   ok 3.0s
AFORGE_DIRECTION_MEASURE=1 go test -run TestMeasureResolveLatency -v ./internal/direction/   3 runs, table above
```

The first scripted run of the full suites ended with exit 143, a SIGTERM from
outside the script with no test output; every other step in that run passed. The
full suites were rerun in the foreground and passed, as above.

---

## Invalidations for the feature's change entry

These are for the coordinator to add to `662-personal-ai-backend.md` when the
feature lands on `dev`:

- "`collections.db` refused any `user_version` it did not know, so an upgrade stranded every older build. A build now opens a newer store that declares it a permitted reader (`store_meta.min_reader_version`), and refuses any other newer store with `ErrNewerStore`, leaving it untouched."
- "`collections.db` used the rollback journal with a 1 s lock bound. It is now in WAL, set once by the first open, and statistics are refreshed at close (`PRAGMA optimize`)."
- "Organization schema 3 was the newest. Schema 4 adds the direction record tables, but only `direction.Open` creates them; an ordinary open leaves a store at 3."
- "Direction was held in three stores (holds, shared context, memory decisions). `internal/direction` now holds the one record and its resolver, with no product caller yet; the old stores still govern."
- "An import could be read as able to carry a person's acceptance. It cannot: an imported acceptance is `legacy_person`, `legacy_delegated` or `legacy_unknown`, and only the four `PersonReceipt` constructors make a person receipt."
