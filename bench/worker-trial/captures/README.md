# Captures

Evidence from the aforge-as-worker trial, kept because it is written at runtime and
exists nowhere else in the tree.

## `510-run1-stream.log`
The full person-facing stream of the first `aforge do` hand-off of issue #510
(2026-09-03T08:24:30Z, 2700.05s, $1.9783, exit not captured). Line 105 is the refusal
whose evidence is invented — see below. Line 117 names the store the run kept.

## `510-run1-leaf-task-2.txt`, `510-run1-leaf-task-2-x1.txt`
`aforge why task-2` and `aforge why task-2-x1` against
`~/.aforge/runs/aforge-do-1923110067/graph.db`. Between them: 19 + 104 turns, 156 `sh`
calls, one failed `read`, and **zero** `write` or `edit` calls. Grepping every `cmd` in
both for `cat >`, `tee`, `>>`, `EOF`, `sed -i` or `patch` returns nothing. Nothing on the
tree was changed by this run, by any mechanism.

## The refusal with invented evidence

`510-run1-stream.log:105`, verbatim:

> gate: refused — CLAUDE.md — The only file this run changed is CLAUDE.md; no leaf-harness
> change was implemented. There is no per-round counter of identical timed-out tool calls,
> no new stop-reason kind distinct from the wall and wire failure carrying command and
> count, no exported threshold constant, no scripted-brain acceptance test, and no
> docs/changes/unreleased/ entry for PR 510. The worker's own message says it stopped
> 'before implementing'. The file of record, CLAUDE.md, performs none of the required
> behaviour. — no more work could be started on it

Everything after the first clause is true. The first clause — and the "file of record"
sentence that leans on it — is not: no file was changed. `git status` in the working
directory was empty when the run ended.

## The leaf contract, written at runtime

`510-run1-leaf-task-2.txt`, turn 0, opens: *"Read CLAUDE.md first; then locate the leaf
harness round loop under internal/ …"*. Turn 1 is therefore
`read {"path": "CLAUDE.md"}`, which returns:

> no tool named "read". Available: sh, job, write, edit, web, recall, capabilities (loads: media, documents)

The contract is composed by the planner at runtime and is not in the tree; this is the
only record of it.

## Do not delete

`/home/santosh/.aforge/runs/aforge-do-1923110067` is **pinned** at the request of
aforge-v2-10, which is verifying the refusal defect against it before filing. It stays
until that session says the issues are filed. The three files in this directory are the
durable copies, so the store can be reaped afterwards without losing the evidence.
