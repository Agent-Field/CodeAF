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

## The store is released

`/home/santosh/.aforge/runs/aforge-do-1923110067` was pinned while aforge-v2-10 verified
the refusal defect against it. That session filed on 2026-09-03 and released it, so the
store may be reaped by whoever cleans up; the three files here are the durable copies and
this branch is pushed, so nothing is lost when it goes. Nobody needs to delete it on
purpose.

The issues it produced: **#537** (the gate's file of record comes from the request text,
stat'd on disk, never from the diff — which is why a file the run only read was named as
one it changed), **#538** (the contract names a `read` tool the leaf does not have),
**#539** (a refused run does not say where the work went), **#540** (no pacing against the
wall), **#541** (budget overrun: billing before the check, plus unbounded landing turns),
**#542** (`--json` drops `exit_code`; the field is unexported), **#543** (the settle line
hides hedge waste).

## Two lines from this run that the polish wave is arguing from

`510-run1-stream.log` carries the house-rule argument for refusal wording inside one
stream, four minutes apart.

At 41m17s (line 105), ninety words opening on a claim about a file that is not true:

> gate: refused — CLAUDE.md — The only file this run changed is CLAUDE.md; …

At 45m0s (line 116), sixteen words opening on the verdict:

> partial — no relevant progress in 2 rounds; last change: nothing this job is about has changed

And at 26m1s (line 73), a third label for a body that the gate renders as a refusal
elsewhere:

> nothing is changing — carrying on has stopped changing anything — twice over now, nothing was written or altered — so this is handed over as it stands

That body appears in another (since-reaped) capture as `gate: refused — carrying on has
stopped changing anything — …`. Same words, two labels. The product already owns an
outcome-first label for this sentence and does not use it at the gate — which is the row
in `docs/design/polish/NEXT.md` on `ui/polish-v0`, and the reason line 73 needed a
permanent home.

## Evidence in a run store has a half-life

Three refusal specimens read off this box on 2026-09-03 could not be found thirty minutes
later — a run store reaped underneath them. A path into a run store is a citation with an
expiry date, and a description of a line is not the line. Everything in this directory is
copied out and pushed for that reason.
