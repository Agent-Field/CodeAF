# Subharnesses — many hands, one graph

## The idea

A leaf in the graph has always been executed by exactly one kind of worker:
the linear worker. This document introduces the second kind, and with it the
rule for every kind that follows: **a subharness is an alternative way to turn
the same Task into the same Outcome, chosen per node, and the graph never
learns what ran inside.** The `exec.Executor` interface was written for this
day (`internal/exec/executor.go:1-12`); this doc is the contract for actually
using it.

The name is deliberate: these sit *under* the main harness — the graph, the
store, the reconciler stay the one engine of record; a subharness is the
worker inside a single atomic leaf.

The first specialist is **`swe`** — a full software-engineering pipeline
(vendored from swe-pro-go: plan → parallel coder leaves in git worktrees →
per-leaf judge → merge → audit-fix loop → verified terminal status). It exists
because a coding issue that would decompose into eight linear leaves is better
taken *whole* by a subharness that owns worktrees, merges, and a verifier.
Later specialists (PR review, security analysis, …) follow the same
registration contract and inherit the same learning machinery for free.

## Vocabulary

This dimension is called **subharness**, everywhere: `Executor.Subharness()`,
`Node.Subharness`, `Provenance.Subharness`,
`profile-<model>-<subharness>.json`. The word "skill" is already taken — in
this codebase by the skill forge (learned executables in `~/.aforge/skills`),
and outside it by half the agent ecosystem — so the executor interface's
original `Skill()` method and the profile store's `skill` key are renamed as
part of this work. `"linear"` is the fallback subharness; the empty string
means linear. (`craft.Step.Skill` keeps its name for now: today it is brief
advice, not dispatch; it is revisited in the wave that promotes it.)

## The laws

1. **Additive specialization.** With only linear registered, every prompt is
   byte-identical to before this feature existed and every code path behaves
   identically. A menu with one entry is no menu.
2. **Degradation, never failure.** An unknown or unregistered subharness runs
   on linear (`Registry.For` already promises this). Mis-selection degrades
   to baseline behavior.
3. **The Outcome is the whole interface.** A subharness is first-class in
   chat, `do`, and headless `run` if and only if it returns a populated
   `exec.Outcome` (Text, Artifacts, Usage, Turns, Stop/Exhausted, Ran,
   Verdict), honors `Task.Control` (pause/cancel), drains `Task.Steer`, posts
   milestones through `Task.Share`, and writes the `.obs/<node>.trace.log`
   trace. Nothing downstream — narrator, delivery gate, distiller, folds,
   profiles, ledger, spend — may branch on the subharness name.
4. **A subharness that owns a verifier sets its own Verdict** (reserved by
   `linear.go:794-795`). The swe subharness's audit gate is exactly this.
5. **Two-surface covenant.** Every subharness is reachable from both dispatch
   paths: the resident `ExecuteFunc` (chat and `do`) and the headless
   scheduler (`plan`/`run`). A subharness that works in one and not the other
   is a bug in the seam.
6. **One mouth.** A subharness emits milestones via `Task.Share` and
   replaceable `store.MessageProgress` rows — never a stream of thread
   messages. The narrator speaks; the subharness reports.

## Registration — what a new subharness provides

One `exec.SubharnessInfo` registered at startup:

- **Name** — the subharness string.
- **Purpose** — one paragraph of "what this subharness is for," rendered into
  the compiler's subharness menu and the sizing prompt. This is how the
  choosing model learns the menu: adding a subharness automatically adds it
  to the choice context.
- **PriorAnchors** — the initial, prompt-level capacity ruler: three worked
  examples (comfortably atomic / borderline / oversized) in the style of
  `plan/size.go` sizeAnchors. This is the *initial* setting of the
  subharness's hardness; measurement replaces it (below).
- **Budget shape** — deadline floor and scaling, so `leafDeadline` and the
  watchdog fit the subharness (a swe run is minutes-to-an-hour, not a linear
  leaf's 15-minute floor).

## Selection — where the choice is made

The choice is made where sizing is already made, and it is journaled the way
`Provenance.WorkModel` and `Provenance.Craft` already are: once, at splice
time, durable across restarts.

- **Compiler (task scale).** `Brief` gains `subharness`; the compiler prompt
  gains a menu section (auto-built from registered SubharnessInfo + each
  subharness's measured self-knowledge) with the rule: *choose a specialist
  only when the job's essence matches its purpose; when in doubt, linear.*
  The choice rides `Compiled` → `Provenance.Subharness` → the leaf.
- **Planner (project scale).** The sizing pass sees every registered
  subharness's anchors side by side. Its verdict becomes *(subharness,
  size)*: a node that is oversized-for-linear may be atomic-for-swe, and
  decomposition stops there. This inverts the old behavior for coding
  subtrees: instead of eight linear leaves, one swe leaf.
- **Craft.** `craft.Step.Skill` is promoted from advice to dispatch when it
  names a registered subharness (later wave).
- **Escalation.** A leaf that fails or overruns on work matching a
  specialist's purpose escalates to that specialist as a second rung, the way
  model escalation already works. The *decision* is extended, not the
  machinery: the two judgements that already read a leaf that did not get
  there — the retry judge and the remainder judge — are handed the menu and
  may answer with a worker as well as with a remainder. The menu they are
  handed excludes the worker that just failed, so no worker is ever offered
  its own failure back; with one specialist registered, that means a failed
  specialist leaf sees an empty menu and follows the ordinary model-escalation
  path or fails honestly. A retry's new worker is journaled on the node
  (`EventNodeWorkerChanged`) and a continuation's rides its subtree
  provenance, so both survive a restart.

  The headless scheduler's escalation (`exec.Scheduler.Escalations`) stays
  mechanical, and that is not a breach of the two-surface covenant. The
  covenant is that every worker is *reachable* from both dispatch paths, which
  it is — the headless registry constructs every worker this build has, and
  the planner chooses one at sizing time. It is not that every judgement is
  made on both. There is no model in that retry loop to hand a menu to: a
  verdict puts the node back to pending and the ordinary launch path picks it
  up. Retries are judged on the surface that has a head to judge with.

## Learning the boundary — SWE hardness

The hardest requirement: the swe subharness's capacity envelope must start as
prompt text and then *calibrate itself*, in-session and across sessions. No
new machinery is invented; three existing loops are keyed by subharness:

1. **Profiles.** Every swe leaf lands a record in
   `profile-<model>-swe.json` (cost, tokens, elapsed, verdict, size bucket) —
   the profile store was explicitly designed for per-worker files
   (`profile.go:98-103`). In-session: records accumulate immediately and feed
   price consent (`medianProfileCost`) and self-knowledge. Cross-session: the
   file is loaded at launch.
2. **Anchors.** `plan.UseAnchors` is per-subharness
   (`UseAnchorsFor(subharness, anchors)` / `AnchorsFor(subharness)`). Each
   subharness ships PriorAnchors; `profile.NeedsRecalibration` fires per
   subharness on measured overrun/underrun, and `plan.Recalibrate` rewrites
   *that subharness's* anchors from *that subharness's* evidence —
   mid-session via the atomic value, durably via `Profile.Anchors`. The ruler
   doc's promise ("a different harness means rewriting these three examples
   and nothing else") is kept literally.
3. **Boundary evidence.** Two events are recorded as calibration evidence:
   a linear→swe escalation that then passed (the boundary was too high), and
   a swe run whose internal root-cut judged the goal trivial and finished
   under the linear median cost (the boundary was too low). Both land in the
   swe profile records and are rendered into the recalibration evidence, so
   the two rulers move toward the true seam between them.

   It is carried by one generic field at each layer and no branching anywhere:
   `exec.Outcome.Calibration` — free-text sentences a worker writes about its
   own fit, nil for linear — journaled as `profile.Record.Calibration`, beside
   `profile.Record.EscalatedFrom`, which names the worker that tried the task
   first and could not finish it. `plan.Recalibrate` renders both into the
   evidence the anchor-rewrite model reads. The swe executor writes its notes
   from what the engine already decided on the way past: the root-cut band, the
   intake classification, the audit's position against its own cycle ceiling,
   and the run's cost and wall clock against the ceilings it was given. The
   one comparison no worker can make about itself — this run against the
   *generalist's* median leaf cost — is made where the record is written, for
   any worker that is not the baseline, by asking the registry and never a
   name.

Self-knowledge (`selfknow.go`) renders per-subharness measured history into
every compile, so the compiler's menu choice is grounded in measurement
within the same session — this is the same "measured history as a prior,
never a hard rule" law the head already follows for reflex.

## The swe subharness runtime

swe-pro-go is vendored at `internal/swepro/` (provenance: commit af248e9,
org-internal copy; engine under `internal/swepro/internal/`, the codeaf
orchestrator exported as a callable package). It stays bug-for-bug; embedding
patches are minimal and marked `// aforge-embed:`.

- **One binary, isolated process.** The engine has process-global state (env
  knobs, plandb singleton), so each run executes as a re-exec of the aforge
  binary itself: main() checks an env sentinel (`AFORGE_SWEPRO=1`) first and
  dispatches to the vendored codeaf CLI. The engine's auto-resume supervisor
  re-execs `os.Executable()` with codeaf argv — the inherited sentinel makes
  that correct without modification.
- **Models and keys.** aforge and the engine are both OpenRouter-native. The
  executor passes aforge's API key, base URL, and the leaf's pinned model
  (work model / boost / escalation rung) as the engine's model pools. Spend
  from the terminal event lands in `Outcome.Usage`; aforge's ledger and
  profiles learn from it like any other leaf.
- **Workspace.** The engine requires a committed git repo. If the leaf's
  workspace is one, run in place; otherwise `git init` + initial commit
  first. Sidecars (`.plandb.db`, `.codeaf/`) are git-excluded by the engine.
- **Progress.** The executor consumes the engine's stdout NDJSON: stage
  events → `store.MessageProgress`; milestones → `Task.Share`; the full
  stream → the trace file; per-token deltas are dropped. `Task.Control` is
  polled between events; cancel signals the child process group.
- **Doneness.** Terminal `pass` → verdict pass (the subharness owns a real
  verifier — audit gate plus project verification); `fail`/`escalated` →
  failure with the engine's reason; `budget-exhausted` → `StopBudget` with
  `Exhausted` set, feeding the existing overrun-continuation replan.

## The engine copy is owned, not borrowed

`internal/swepro/` is a full local copy of swe-pro-go (initial import at
commit af248e9), and from the moment it lands it is **aforge code**: modified
freely, in place, whenever tighter integration serves the product — richer
events for the chat surface, steering injection, per-call data for the
ledger, performance work. Upstream swe-pro-go continues to evolve separately;
improvements worth having are harvested by occasional cherry-pick, not by
mechanical re-sync, and divergence is the expected steady state, not a debt.

Discipline that keeps this honest:

- `internal/swepro/UPSTREAM` records the import commit, and
  `internal/swepro/EMBEDDING.md` keeps a running divergence log — one line
  per intentional departure from upstream, so a future harvest knows what
  not to clobber.
- The initial import is scripted (`internal/swepro/revendor.sh`) so the
  starting point is reproducible; after import, direct edits are normal
  commits like anywhere else in aforge.
- Integration deepens in stages. v1 drives the engine through its process
  boundary (argv + env in, stdout NDJSON out, re-exec sentinel) because the
  engine's process-global state makes that the safe seam. As the copy is
  domesticated — globals threaded, hooks added — the boundary can tighten:
  in-process event callbacks instead of NDJSON parsing, mid-run steering,
  aforge's router behind the engine's backend interface. Each tightening is
  an ordinary aforge change now, not a fork-management problem.
- What stays fixed is the *outer* seam: the engine is reached only through
  the swe subharness's `Executor` implementation. However deep the
  integration goes inside, the graph still sees one Task in, one Outcome
  out — that is the modularity that lets the next subharness (PR review,
  security, …) arrive the same way.

## Canonical choice prompts for `swe`

The registered SubharnessInfo texts are written here, once, and the code
carries them verbatim. They are the *prior*; measurement rewrites the anchors.

**Purpose (the compiler-menu entry):**

> swe — an end-to-end software-engineering pipeline for changing code in a
> real repository. Give it a coding issue whole — a feature, a bug fix, a
> refactor with its tests — and it plans internally, edits in parallel git
> worktrees, judges each change before merging, and verifies the result
> against the repository's own build and tests before calling itself done.
> Choose it when the work must be discovered rather than merely made: a bug
> whose cause is not yet located, a refactor that crosses the codebase, an
> issue that demands substantial new test surface. Do not choose it when the
> change is already located and specified — a well-described edit in a
> handful of files is the default worker's job even when it is a whole
> feature — nor when the deliverable is prose or analysis about code rather
> than a change to it, nor when no repository's tests or build could say
> whether the job is done.

**PriorAnchors (the swe capacity ruler, in the linear anchors' voice):**

> Use these three reference tasks to judge scale against the swe worker. They
> are the ruler; place the node against them rather than estimating it on its
> own.
>
> TOO SMALL — a change already located and specified, however complete. Fix a
>   typo'd flag; correct one function when the failing test names it; add a
>   well-described option that touches a handful of files. The default worker
>   finishes this in minutes; this pipeline's planning and verification would
>   cost more than the change.
>
> RIGHT — one coding issue taken whole, however many files it touches.
>   Implement a described feature along with the tests that prove it; hunt
>   down and fix a bug whose cause is not yet located; carry a refactor
>   through an interface and every call site, keeping the suite green. One
>   repository, one coherent goal, verifiable by that repository's own tests
>   or build, finished in one run even if that run takes an hour.
>
> TOO BIG — more than one product-scale goal in one instruction. Build the
>   whole application from a spec; rewrite a codebase in another language;
>   "modernize" a repository with no stated end state. These decompose above
>   the leaf: several swe nodes with goals of their own, or a graph mixing
>   swe work with research and writing that are not code changes at all.
>
> Judge by the coherence of the goal, not the number of files. A change that
> touches forty files in service of one stated behavior is RIGHT. An
> instruction hiding three unrelated deliverables is TOO BIG even if each is
> small.

## A swe leaf is an ordinary node — the checklist

From the graph's perspective a swe run is one atomic node, and it meets every
requirement any node meets. Nothing about it is exempt:

- **Store truth.** It is spliced as a normal `NodeSpec` (with
  `Provenance.Subharness`), claimed through the same CAS claim, and moves
  through the same Claim/Start/Complete/Fail events the reconciler announces
  from. It appears as a job card in the TUI like any other node, is listed by
  the head's `board`/`result`/`read` tools, and is addressed by
  `deliverySessionID` like any other deliverable.
- **Inside-the-leaf visibility, not graph growth.** The engine's internal
  task DAG never becomes aforge nodes — that would un-atomize the leaf. It
  surfaces as within-node state: replaceable `store.MessageProgress` rows for
  stage progress, `Task.Share` lines for milestones (visible to siblings and
  to `do --json`'s `learned[]`), and the full event stream in
  `.obs/<node>.trace.log` for `aforge why`-style drill-down.
- **Control.** Pause/cancel via `Task.Control` between engine events; user
  messages anchored to the node land in the same steering mailbox (drained
  at cycle boundaries until the engine grows a mid-run intake).
- **Spend and learning.** Cost lands through `recordSpend` like any leaf;
  the run writes a `profile-<model>-swe.json` record; the verdict feeds the
  router ledger; the distiller reads its summary; folds, retrospectives, and
  the delivery gate treat it exactly as they treat linear leaves.
- **Failure and restart.** A crashed resident releases the claim and the
  node restarts like any other — with one earned advantage: the engine
  leaves a resume checkpoint in the workspace, so the restarted leaf resumes
  the engine run instead of starting over.
- **Escalation and continuation.** `budget-exhausted` maps to
  `StopBudget`/`Exhausted` and enters the same overrun-continuation replan;
  failure enters the same escalation machinery.

The test of the law: grep the reconciler, narrator, TUI, head, and store for
the string "swe" — none of them may contain it.

## Why this does not violate "no second engine"

Craft's law (`internal/craft/types.go`) is that learned *shapes* compile to
graph geometry rather than spawning a second execution engine. A subharness
is below that line: it is the executor of one leaf, behind the interface
built for plural executors. The graph, the store, the reconciler, the
journeys — the engine of record — remain singular. What varies is the same
thing that already varies per leaf: how one Task becomes one Outcome.
