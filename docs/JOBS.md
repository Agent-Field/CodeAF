# Background jobs — async work inside an atomic leaf

The leaf executor is deliberately linear: model turn → tool calls →
results → next turn. That shape is why it benchmarks and why it stays
debuggable — and it must not change. But a strictly blocking `sh` makes
three honest kinds of work impossible: servers ("start it, then test
against it"), long builds ("compile for six minutes, do other things
meanwhile"), and watch loops ("tell me when the port answers"). This
document fixes that with the least primitive set that could work.

## Decision 1 — Two primitives, no new concepts

- **`sh` gains `bg: true`** — same command, same process-group
  hardening, but returns immediately with a job handle. Output streams
  to a plain workspace file (`.aforge/jobs/<n>.log`), so the model can
  tail, grep, and diff it with tools it already has. The `t` field
  becomes the job's hard lifetime cap (default 15m); foreground `sh`
  is byte-identical to before.
- **One `job` tool** — peek (`{id}` → status + only-new output), wait
  (`{id, wait: s}` → block briefly for exit; one call replaces a poll
  loop), kill (`{id, kill: true}`), list (`{}`). States are exact:
  `running · exited 0 · exited N · timed out · killed`.

**Rejected:** a monitor primitive (compose it: background a
`until curl -s :8080/health; do sleep 1; done` loop, then wait on it),
a scheduler (that is charter machinery), push notifications into the
model (nothing can interrupt a linear turn — see Decision 2 for what
replaces them), and any per-feature tool the two primitives can compose.

## Decision 2 — Reporting rides the turns it already has

"Report every n seconds" cannot mean interrupts in a linear system. It
means this: whenever *any* tool result returns while jobs exist, the
harness appends one bracketed status line per running job and one per
terminal transition since the last report —

    [job 1 · running 2m10s · last: Compiling ffmpeg...]
    [job 2 · exited 1 after 40s · last: FAIL: TestRouterCascade]

Every turn carries fresh status for free; a failure surfaces exactly
once with its exit code and last line even if the model never asks; and
when no jobs exist the appendix is zero bytes — the common path is
untouched. The model is prompted to *work between waits*: start the
build, write the docs, then collect.

## Decision 3 — Nothing outlives its leaf

At leaf end — success, failure, deadline, or scheduler abandonment —
every surviving job group is killed (TERM, grace, KILL) and the summary
says so. A leaf is atomic; a process meant to live past it is a
standing responsibility wearing the wrong clothes, and the right door
is a charter. Log files remain as recorded workspace artifacts, so
provenance and recall see what ran even after the process is gone.

## Decision 4 — Universal by construction

The job registry lives in the toolbox every leaf already gets: chat
reflexes, resident tasks, and headless `aforge run` gain identical
behavior from one implementation. The tool descriptions carry the
strategy tersely (bg for servers/long builds/watch loops; one wait
beats many peeks; kill your servers when done testing) — the knowledge
ships in the contract, not in a wiki.
