# One conversation, several execution scopes

Working plan, 2026-09-05. This is a proposal and implementation sequence, not a
description of capabilities already shipped. The draft PR carries the earlier
conversation-runtime work as a baseline; its presence is not evidence that every
choice in that baseline is correct.

## Decision

Keep one owner for each requested outcome. Let that owner choose direct work,
short parallel lanes, or a durable task according to the independence and lifetime
of the work. Do not infer a new outcome owner from the number of files already
edited. Do not create a task merely to wait for work an existing owner started.

The main conversation remains useful for ideation, decisions and concise results.
Substantial execution belongs in a work conversation that the person can enter and
steer. A short fork is an execution detail of its caller, not another assignment
that needs a name, plan, admission debate, independent reviewer and landing.

This is not a new scheduler alongside the current ones. `fork` already exists;
tasks, job receipts, the mailbox and result projection already exist. The work is
to make their boundaries coherent and remove redundant decisions.

## What the evidence establishes

The frozen calibration in `CALIBRATION-20260905.md` contains 36 attempts and only
two repetitions per cell. Aforge's four small standalone cases all passed, but
their mean billed cost was approximately 3.4–7 times Pi's. Interactive follow-up
and revision each passed once in two attempts. That is diagnostic evidence, not a
frontier result.

In the revision timeout, the requested CSV already existed, the old Markdown was
gone and the original command ran once. Automatic handoff then created work mainly
to wait/check, and review continued past the deadline. Correct files alone do not
turn that timeout into success. The defect spans ownership, observation and
completion; changing the file-count threshold cannot establish a sound contract.

Existing fork code checks declared scopes within a batch and runs hands in the
caller's directory. It delivers results asynchronously through job notifications.
The comments alternate between a join and continued builds while siblings write.
A shared workspace needs an explicit consistency rule; describing disjoint writers
does not prove that a parent build reads a stable tree. Shared prompt prefixes also
do not guarantee provider cache hits, especially when tools or system tails differ.

## The three execution choices

| Choice | Ownership and context | Completion and interaction |
| --- | --- | --- |
| Direct | Current owner, current context; small actions and synthesis | Return the answer or retain an explicit outstanding operation |
| Short fork | Same owner; immutable inherited history plus a narrow lane instruction and actual capabilities | Bounded attributed results return to caller; caller integrates and checks the combined output |
| Durable task | New responsible owner; current user contract, selected evidence and retrievable sources | Independent work conversation, revisions, restart/reconnect and nested delegation |

The number of lanes is a ceiling, not a target. Serial execution is preferable
when setup, repeated context or integration costs exceed independent useful work.
Two simultaneous tool calls need no new model lanes. Four independent edits may
benefit from a fork; a coupled migration may benefit from one sustained worker.

A fork is unsuitable when it needs a separate lifecycle, broad shell access,
conflicting writes or lengthy investigation. A task is unsuitable when all that
remains is observing an operation whose receipt belongs to the caller. Promotion
must transfer a concrete remainder and accessible dependencies, rather than
rephrasing the entire original request after part of it is already done.

## Six boundaries, with one writer for each fact

1. **Assignment:** the person's request, effective revisions, acceptance and owner.
   Agent coordination cannot rewrite user authority. An answer to a question is
   not automatically a revision to every active task.
2. **Execution:** attempts, processes, fork groups, cancellation and limits. These
   records establish what ran; they do not judge whether an artifact is useful.
3. **Communication:** addressed events with source, request/revision correlation,
   receipt and consumption. Tasks and sessions use the same local delivery seam;
   future cross-session transport can resolve addresses without redefining content.
4. **Context:** compile the provider view from the current contract, role/tool
   capabilities, relevant evidence and bounded working history. Preserve source
   handles and call/result pairing; never mutate the authoritative journal to save
   prompt bytes.
5. **Outcome:** the owner accepts or reports a result against its current revision.
   A process exit is execution evidence; an explicit test result is check evidence;
   neither is a universal proof of quality. No recursive review merely to validate
   another review's wording.
6. **Presentation:** project these facts into running, finishing, done, incomplete
   and needs your look. Opening a work conversation does not start another writer.
   Routine activity updates its work item; meaningful changes and results appear
   in conversation without stealing focus or overwriting a draft.

Start with cohesive files and narrow interfaces inside the existing session
package. Extract packages only when dependencies form a real boundary. A generic
bus, universal graph or parallel replacement engine would increase the migration
surface before proving the product improvement.

## Fork semantics to settle before expanding its use

- A fork gets one immutable context snapshot. Each lane receives its difference,
  sibling responsibilities, current assignment revision and its own tool belt.
  Inherited task promises must not contradict the narrower belt. Full-history and
  selected-history variants are measured; cached input is never assumed free.
- Read-only lanes must not invent writable files merely to satisfy the schema.
  A read-only lane has no write capability. Editing lanes declare disjoint scopes.
- Scope admission must account for other live batches and the caller, not merely
  sibling declarations in one invocation. Canonical path aliases and symlinks need
  the same treatment at admission and execution. A prompt rule is not isolation.
- Results can stream individually, but combined builds and acceptance require a
  stable relevant workspace. Do useful independent reading while siblings write;
  do not present a build over a changing tree as final evidence.
- Preserve partial results, failures and cost on cancellation. A person's side
  question should not silently discard useful lanes. A change in scope must reach
  affected work or stop it, and results must carry the revision they used.
- Bound aggregate active lanes and reserve shared admission capacity. Per-lane
  round limits alone do not bound total cost. Initially describe money bounds as
  soft wherever in-flight provider billing can overshoot.
- The caller owns the final answer. A lane may finish while other lanes continue;
  that is progress, not completion of the requested outcome.

## Handoff and completion simplification

Replace overlapping policy decisions incrementally behind one factual snapshot:
current request/revision, outstanding owned operations, unread results, remaining
work and available budget. Start with a pure decision boundary that can be tested
without providers. Do not hold agent/graph locks while asking a provider or calling
registry callbacks.

The same decision must cover task children, fork hands and commands without
pretending they have identical lifetimes. An already running command retains its
execution owner and has an observable result. Waiting consumes no model turns.
New independent work can still be delegated while that command runs; the presence
of a job must not become a blanket exemption from delegation or finishing limits.

The capable model should normally request a task or fork directly with a concise
contract. A runtime governor may interrupt runaway work, but should not pay for
multiple readers to reconstruct the same decision. Remove old routing readers
only after scripted and live tests establish equivalent authorization, interruption
and completion behavior. Reduced calls are useful only if success does not fall.

## Evaluation that can support a Pareto claim

Use three suites: deterministic runtime contracts, controlled diagnostic tasks,
and frozen real GitHub issues. Keep coding, research, data and writing as separate
quality strata. Real coding issues need actual repository setup, investigation,
multi-file changes, dependency interaction and independent acceptance.

Each issue has immutable provenance, a pre-fix source SHA, environment/toolchain
definition, model-visible issue text and acceptance instructions, and private
evaluation material. Evaluate a clean base failure and reference success before
calling the case ready. Candidate workspaces must not contain solution commits,
future issue comments, gold patches or hidden tests. Known historical issues may
still be in model training; disclose that limitation and include held-out recent
issues. Never select only cases where our preferred decomposition wins.

Pair Aforge, Pi and OMP on the same base, model, reasoning setting, tool access,
provider-routing condition, deadline and task data. Pin every inference role to
`deepseek/deepseek-v4-flash-0731`. Use native Claude Code Opus for development only.
Freeze binaries, adapter versions and manifests. Preserve failures, unknown bills,
retries and incomplete outcomes. Do not replay a request to recover billing.

Run actual chat as well as headless controls. Chat variants must include a question
while work continues, a revised requirement, stop, entering/leaving a task and
reconnect. Measure correct-answer latency, time until direction is consumed, stale
work after revision, duplicate actions and usable-result time. A responsive
composer without a correct timely answer is not successful interaction.

Measure total billed cost including routing, naming, checking and recovery; wall
time to accepted usable output; independently checked quality; failure rate; and
cost per successful issue including failed attempts. Report distributions and
paired uncertainty, not only means. Provider cache and backend differences are
conditions, not hidden harness improvements. Judge costs and development expenses
are separately reported.

Use at least five paired repetitions for diagnostic estimates, then a broader
predeclared issue set with held-out cases and a precision-driven sample size.
Five repetitions alone are not strong evidence of quality equivalence. Compare
serial Aforge, existing fork and revised execution as ablations. Claim improvement
only within the measured task/model/door/conditions. There is no defensible global
Pareto claim over every possible task.

## Implementation order and stop conditions

1. Publish this plan and inherited work in a **draft** PR. Keep the shared checkout
   untouched. Inventory active PR overlap; do not silently absorb other lanes.
2. Repair the existing short-fork contract and establish real-issue preflight with
   offline proofs. No new scheduler, task state family or automatic planner.
3. Consolidate operation ownership and handoff decisions. Reproduce the revision
   failure with a scripted provider before changing policy, then run its frozen
   live diagnostic. Preserve substantial-work delegation and side-question control.
4. Reduce prompt/tool contract overhead and redundant readers one source at a time.
   Compare success and total billing with paired runs; revert losing variants.
5. Add global admission reservations and finish steering/cancellation/reconnect
   coverage. Exercise overlap and failure paths under the race detector.
6. Run curated real-issue and noncoding campaigns through chat and headless doors.
   Improve the dominant measured failure/cost next; repeat on held-out cases.

Keep the PR draft until architecture, manual, meaningful tests and comparative
evidence agree. Do not claim the whole plan is implemented when an individual
module lands. If an abstraction adds decisions or model calls without removing
existing ones, justify it against an observed failure or do not add it.

## Self-critique

“One owner” does not remove dependency complexity; it gives that complexity a
place to live. Copying context can be cheaper than writing briefs but expensive on
cache misses and misleading when it carries obsolete constraints. Shared files
save worktree overhead but require stronger consistency rules. A universal
completion state would erase distinctions between a stopped command, a failed
check and an unfinished artifact. Trusting a capable model reduces ceremony but
does not replace runtime receipts or cancellation. The acceptance of this design
is measured useful output with dependable steering, not fewer types or more lanes.
