# Confirmed slices become isolated build tasks

This is the user's working agreement for this effort, not a new product feature.
The ordinary repository rules still apply except where the user specifically
requires draft integration without merging into dev.

## Ownership

The current discussion task owns the grooming record and product confirmations.
The current implementation task owns first-wave code and integration branch
`codex/personal-ai-backend` / draft PR #662. A future confirmed slice gets its
own new Codex task. That task coordinates Claude Code with Opus on Spark, owns
its isolated worktree/branch, tests and review, and reports results. Do not
duplicate the active first-wave implementation or silently broaden its scope.

No new product slice is approved merely by saving this plan. The user explicitly
requested new build tasks as behavior is confirmed. Do not launch speculative
code tasks for open choices. Read-only design/testability checks may run in
parallel when useful, but must not be presented as accepted implementation.

## Before dispatch

1. Record confirmed behavior, its source, example and counterexample, conceptual
   or sequence diagram, the real user entry point and observable acceptance.
2. Inspect existing implementation. Name what is reused and the smallest missing
   behavior. If no new code is needed, prove it instead of refactoring for symmetry.
3. Settle shared interfaces and file ownership with the integration owner. State
   the behavior that is explicitly deferred; do not invent another status owner,
   instruction resolver, scheduler, universal graph or permanent manager.
4. Resolve the latest reviewed, committed integration base and record its exact
   SHA. An uncommitted worktree or stale documentation is not a reproducible base.
   Feature work here intentionally branches from the current integration work,
   rather than older dev, under the user's instruction.
5. Create the requested Codex task in an isolated project worktree. Use the
   existing integration branch as the requested starting branch and verify the
   resulting SHA; if it advances during setup, reconcile before coding. Keep the
   user's configured Codex model; the requested Opus applies to Claude Code work
   on Spark. Read the fleet skill before operating the cluster.

## Parallel work without duplicated design

Keep product discussion ahead of the next confirmed slice while the current
slice is built. Do not block discussion on a long test run. Change the contract
only through a recorded decision, with notice to affected builders.

Inside a slice, useful independent lanes include a bounded implementation area,
an acceptance fixture built against an agreed interface, and read-only review.
The Codex owner handles integration and shared runtime wiring. Use Claude Code
Opus on Spark as requested; each mutable lane has its own worktree and explicit
paths. Read-only review can run beside tests. Avoid multiple lanes changing the
same session/config/admission/scheduler files or repeating the same recon pass.

Across slices, parallelize one-responsibility activation and addressed
consultation only when they have separate entry points and a settled shared
wake/authority contract. Otherwise serialize their shared changes and parallelize
evidence work. Limit live-model concurrency to avoid fixture contamination and
misattributing spend. A second independent lane must have a real useful job.

## Build-task brief

Every dispatch should state in ordinary prose:

- Goal, confirmed decision IDs and links to the relevant journey/diagram.
- Exact base SHA, integration destination, branch/worktree and owned files.
- Existing mechanisms to reuse, interface agreements and deferred capabilities.
- User-visible actions, authority and current-state semantics.
- Concrete positive and negative E2E assertions, production entry points,
  requested model, time/spend ceilings and receipt locations.
- Claude Code Opus on Spark instruction, bounded lane ownership and Codex
  integration/review ownership.
- No integration until evidence and review pass; no dev/staging/main merge,
  promotion or release. Keep the parent PR draft and do not clean another task's tree.

Do not copy the whole discussion into every lane or inject an extra paraphrased
state card into task briefs. Reference the confirmed contract and person's
scenario; keep one source of truth for the intended behavior.

## Completion and integration

1. Build only with `make build`. Run relevant focused/structural/manual checks and
   real DeepSeek V4 Flash product journeys, plus actual TUI tests for UI promises.
   Check recovery, negative authority/scope behavior and unavailable state as
   appropriate. Do not silently skip explicit live verification.
2. Review the final integrated slice for correctness and unnecessary complexity.
   Record the exact tested SHA and receipts. If a product ambiguity prevents
   acceptance, return the reproducing example to this discussion; do not weaken
   the assertion or choose new behavior silently.
3. Push the isolated branch and open/update a PR into
   `codex/personal-ai-backend`, not parked `main`. A PR that fails the intended
   behavior stays draft. The user's staged-integration instruction overrides the
   default standalone-PR destination of dev for these child slices.
4. After required checks, review and agreed E2E pass, coordinate with the parent
   owner to integrate. The user has authorized reintegration after completion;
   do not repeatedly ask for permission for that same step. Reconcile newer base
   changes, rerun the affected acceptance on the combined commit and rebuild.
   If integration changes behavior, the previous receipts alone are insufficient.
5. Update the ledger, actual feature manual, parent handoff and evidence links.
   The PR into dev eventually carries its change entry; child slices may add
   entries where useful under repository rules. Keep proposals out of the
   feature manual until implemented. Mark integrated only after verification.
6. Remove only owned temporary lane worktrees/branches after their evidence and
   results are retained. Keep the main integration worktree and review binary
   available. Never overwrite a running binary by copying over it.

No wave is complete merely because it compiles, has a plan, has a test file,
passes a store unit test or has a green job that skipped its live prerequisites.
Conversely, a deliberately bounded passed slice need not solve every future
journey; its limits must be explicit before anyone calls it done.
