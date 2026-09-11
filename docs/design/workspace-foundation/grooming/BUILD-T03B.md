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
