// Package cellstore seals a cell: at every tool-call boundary it turns what the
// call did into a Receipt, snapshots the cell's folder through the embedded
// engine, and records the result as a Turn (docs/ARCHITECTURE.md 3.1, 4.3;
// docs/SCHEMAS.md 1, 5, 6).
//
// The seam is [Store]; [Engine] implements it. Engine composes the harness's
// files and hands the engine's verbs to a [Transport]: [Spawn] runs the engine
// once per verb, [Daemon] asks its long-lived process over a unix socket (the
// repository stays open, so a seal pays no store open), and [Fallback] uses the
// daemon and falls back to the spawn only when the daemon is absent
// (transportFor is the one place that chooses). Nothing above the interface
// knows which one answered.
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
//   - Engine (engine.go) is the Store over the engine, one data directory per
//     cell so writers never share a catalog lock. A seal is one turn-end verb
//     (`hook turn-end --turn <receipt id>` on the command line), which attaches
//     a folder the engine has not seen and snapshots it with the agent-run
//     trigger. The folder need not be a git repository, and none is ever
//     created in it.
//   - Transport (transport.go, daemon.go, ops.go) is how a verb reaches the
//     engine; an Op is one verb in both its spellings.
//
// DEATH CREATES A BRANCH (L12). Every seal appends a Turn to .cell/turns.jsonl
// naming its parent, and engine snapshots are immutable, so a chain that a dead
// device sealed and never uploaded is still a chain with a known parent: stage 1
// re-parents it as a child cell. Nothing here overwrites a turn.
//
// FAST PATH. The engine keeps a stat cache per store, so a file whose stat has
// not changed costs one statx and is neither opened nor read. A caller that
// knows what changed says so in [TurnInfo].Changed, and the engine then visits
// only those paths; without it the engine walks the folder through the cache.
// bench_test.go measures both on 10,000 files with one changed.
//
// DEFERRED (L10 layout). .cell/ still lives inside the folder tools run in,
// and the engine snapshots that folder as it stands. The seal therefore writes
// every harness-owned file under .cell/ (meta.json, receipts, blobs) itself,
// immediately before snapshotting, so what is sealed is what the harness
// wrote; the transcript is the session engine's file and is sealed as found.
// The engine can now do its half: `--cell-dir <dir>` seals a directory outside
// the folder as the tree's .cell/ entry, and `rewind --cell-dir <dir>` puts it
// back there. What is left is moving the cell's state directory out of the exec
// root and passing it, which belongs to the cell layout.
package cellstore
