# Engine memory reuse

## Plan, task index and journal reads

Unchanged plan reads reuse the loaded plan while SQLite's data version and the
database/WAL file stamps match. A write through this or another handle still
refreshes the next read. Conversation task listings share plan read handles
across their run and plan projections. Each trajectory file is read at most once
per listing, including missing files; its parsed projections are reused within
that listing. The next listing reads fresh trajectory data. Interrupted run rows
are inspected without copying the whole run history for each plan task.

Project task indexes reuse decoded rows for matching device, inode, size, mtime
and ctime stamps. Admission requires mtime and ctime to be at least two seconds
old; unsupported file identities bypass reuse. The cache keeps four paths, each
at most 4 MiB on disk, and clears evicted references. Callers get separate row
slices for ordering and deletion filtering, with deep copies of files, crew
slices and maps, route prices, and verdict evidence. The cached rows remain
immutable. Concurrent misses share one in-flight decoding per path and stamp;
a changed stamp starts its own read. Unsettled and oversized files can share an
in-flight read but are never admitted to the retained cache.

Journal record caches now keep at most eight records, down from 32. Unleased
records expire 30 seconds after their last read, with the timer targeting the
earliest deadline. A callback's active record remains accounted for until it
returns. Journal and task-index admission share the timestamp settling guard.

Idle member release and the app task-list disk cache remain follow-up work.
This branch changes neither member ownership nor persistence writes or GC policy.

## Reproduce the allocation and correctness gates

On Go 1.26.5, linux/arm64, the included fixtures gate 200 unchanged plan reads
below 3 MiB allocated, a listing of 150 runs below 16 MiB, and 40 readings of a
1,500-row index below 64 MiB. These are allocation budgets, not process RSS or
elapsed-time claims. Compare the same tests at the base and candidate revisions.

With these fixtures and ordinary `-count=1` tests, a source overlay of the
`a71555ec0` base versus this branch measured:

| Fixture | Base bytes allocated | Candidate bytes allocated |
| --- | ---: | ---: |
| 200 unchanged plan reads | 152,977,728 | 565,272 |
| Listing 150 runs | 503,060,592 | 14,893,176 |
| 40 readings of a 1,500-row index | 267,895,312 | 33,463,056 |

Review regression gates additionally assert that mutating a returned row's
nested fields leaves the next read unchanged, 16 concurrent readers of one stamp
decode once, and a listing opens its live plan once and reads each trajectory
once. A weak-reference gate checks that plan-only reads release each raw
trajectory payload before reading the next task; the memo keeps only plan step
counts and requested app facts. Before these corrections, the deterministic
fixtures observed 16 decodings, two plan opens and two reads per matching
trajectory. The allocation caps above remain unchanged.

The overlay substitutes base production files while keeping the allocation
fixtures; its index fixture predates the injected cache clock. Test logs report
allocations so the candidate measurement can be repeated directly:

```sh
make test-focus PKGS=./internal/plandb RUN='^Test(RepeatedReadsOfAnUnchangedPlanLoadItOnce|AReadSeesAnotherHandlesWriteAfterAnUnchangedRead)$$' TEST_FLAGS='-race -count=3'
make test-focus PKGS=./internal/session RUN='^Test(AppTasksOpensThePlanOnceForAllTheRunRows|AnUnchangedTaskIndexIsDecodedOnce|TaskIndexEvictionClearsRetainedRows|TaskIndexRejectsUnsettledChangeTime|TaskIndexReadOwnsNestedFields|ConcurrentTaskIndexReadersDecodeOneStampOnce|TaskIndexChangedStampDoesNotJoinOlderRead|AppTaskListingRereadsChangedTrajectories|PlanTaskListingDoesNotRetainTrajectoryPayloads|AKeptRecordNobodyAsksForIsDroppedWhenItIdles|RecordExpirySchedulesTheEarliestRemainingDeadline)$$' TEST_FLAGS='-race -count=3'
make test-focus PKGS=./internal/filememo RUN='^TestTimestampsSettled$$' TEST_FLAGS='-race -count=3'
```
