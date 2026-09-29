// Package cellstore seals a cell: at every tool-call boundary it turns what the
// call did into a Receipt, snapshots the cell's folder through the embedded
// engine, and records the result as a Turn (docs/ARCHITECTURE.md 3.1, 4.3;
// docs/SCHEMAS.md 1, 5, 6).
//
// The seam is [Store]. Stage 0 implements it by spawning the engine once per
// seal ([Engine]); stage 1 replaces it with a daemon behind the same interface,
// and nothing above the interface changes.
//
// THE PIECES, EACH ONE JOB:
//
//   - Turn and Receipt (types.go) are the persisted objects. Turn has exactly
//     one constructor, newTurn (turn.go), and this package is the only one that
//     calls it: law L2, enforced by law_test.go.
//   - The WAL (wal.go) is the device-local call-intent log. An intent is
//     written before a call, a completion after it. On reopen an intent with no
//     completion is INCOMPLETE and is only ever surfaced, never re-run (L9).
//   - Recorder (recorder.go) is the one hook: an executor.Executor decorator
//     that logs intent, runs the call, logs completion, and seals. It exists
//     only when CODEAF_CELLS is on ([Wrap]); off, the executor is returned
//     untouched.
//   - Engine (engine.go) is the Store that spawns the engine, one data
//     directory per cell so writers never share a catalog lock. A seal is one
//     spawn of `hook turn-end --turn <receipt id>`, which attaches a folder the
//     engine has not seen and snapshots it with the agent-run trigger. The
//     engine insists on a .git directory, so the first seal runs `git init`.
//
// DEATH CREATES A BRANCH (L12). Every seal appends a Turn to .cell/turns.jsonl
// naming its parent, and engine snapshots are immutable, so a chain that a dead
// device sealed and never uploaded is still a chain with a known parent: stage 1
// re-parents it as a child cell. Nothing here overwrites a turn.
//
// MEASURED (10,000 files, one changed, spark, load ~60): the engine CLI walks
// and stats every file on each snapshot (about 5 syscalls per file), so a seal
// costs hundreds of milliseconds, not the 15 ms the engine's library reaches
// with an explicit changed-path list (engine/docs/performance.md). The 50 ms
// target needs a CLI or daemon entry that takes the changed paths; see the
// lane report. The harness's own share (compose, WAL, turn log) is a few ms.
//
// DEFERRED (L10 layout). .cell/ still lives inside the folder tools run in,
// and the engine snapshots that folder as it stands. The seal therefore writes
// every harness-owned file under .cell/ (meta.json, receipts, blobs) itself,
// immediately before snapshotting, so what is sealed is what the harness
// wrote; the transcript is the session engine's file and is sealed as found.
// Moving .cell/ out of the exec root and composing it into the sealed tree
// needs the engine to accept an extra tree root, which is an engine change.
package cellstore
