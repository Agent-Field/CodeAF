# Capability review and the issue-to-completion example

2026-09-09. Follow-up to the workspace model lab. The user asks which relationships
can actually be made, where intelligence enters, whether the model needs refactoring,
and whether the system can express the work done by Claude Code, OpenClaw and Grok Bot.
This records findings and proposed next grooming, not approval of new behavior.

Evidence: draft PR #662 remains open and draft at c63e03b7f. Current local dev is
0bb39a7ce. Organization-runtime claims below refer to the draft, not dev or a release.
The existing standing engine was inspected in both checkouts. No test suite was run
for this read-only review. All future full suites and acceptance runs must use the
Spark fleet workflow from the repository root, retaining exact snapshot/revision,
job ID and results. Read the fleet skill before submission; no laptop fallback.

## What relations really exist

The draft's collections tool can list/show/find/create collections and add/remove
membership references. Collections can reference collections, chats, tasks, standing
items and artifact paths. Membership neither starts work nor grants permission.
Task workers can read organization but cannot mutate it.

The shared_context tool can create/read/revise/withdraw sourced informational records
with explicit targets. Its source is assigned by the runtime to the saved writer
conversation. Although the store's source union permits chat/task/artifact, the normal
chat tool does not let the model select an arbitrary task or artifact as source.
The mock's examples of those store-level shapes must not be mistaken for available
chat authoring actions.

Other edges have specific owners: chat owns tasks; task dependencies and delegation
parent belong to the task graph; standing origin and latest run belong to standing;
context source and targets belong to each context revision. There is no general
user-editable 'relate any two things with any meaning' tool. The mock is read-only.

## Three different meanings of 'make sure'

1. 'Fix this issue and ensure tests and style pass' is a task with acceptance
   criteria. It can be satisfied once; do not manufacture a recurring watch.
2. 'For future work, always run tests before claiming done' is an existing held
   standing rule. It is carried into applicable work; hold never wakes or runs
   an independent check by itself.
3. 'Keep watching CI; investigate when it fails' is an ongoing item with wake
   condition, probe, action, grant and limits. It has an explicit setup card today.

For a probe, the engine executes one configured command or tool (a command can
collect several facts), bounds its output and asks a model whether the evidence
calls for action. That judgment does not itself have an iterative tool-use loop.
A task action does: it creates a fresh session/journal under the standing item's
run directory, submits the action brief plus evidence and acceptance, and records
its outcome. When division is enabled and the text explicitly enumerates width,
the firing can arm the existing task graph, delegate parts and fold their results.
An old comment saying every firing is only one sequential turn is stale relative
to standingWideWork and the runner's tail loop.

Outcome delivery tries the open origin chat, then another open chat in the same
physical workspace. Otherwise it retains a note for the origin, or the project
inbox for a home exchange. Logical collection membership is not the delivery route.
The OS timer, when installed, can run checks with the terminal closed; this does
not imply hosted availability when the execution machine is offline.

## What is recorded, and what the mock hides

Standing origin retains the conversation/transcript or saved home exchange,
optional originating task, and relevant turn IDs. The original words remain.
Run journals, last-check fields, recent judgments, logs and ledger entries provide
some further evidence. They do not form a lossless causal graph of every decision,
check, permission change or context revision actually consumed by a run.

The prototype omits or compresses important existing standing properties: reach
(conversation, physical project/workspace, machine), explicit exceptions and the
compiled brief. These scopes are not the logical folder DAG. The latest-run file
is also too thin a representation of unattended sessions and any child task graph.
A future visual revision should expose these existing facts without inventing
new agent/job/role primitives. Do not say the mock contains every serialized field.

## Intelligence versus system guarantees

Model judgments interpret the request, decide whether it is one-off or lasting,
prepare tool calls/briefs, assess ambiguous probe evidence, perform delegated work
and synthesize results. Automatic organization and discovery beyond explicit links
are not a completed product loop in this draft. Automatic retention of explicit
accepted personal direction remains agreed product intent with scope/authority open.

Identity, allowed edge kinds, revision checks, membership cycles, task state,
dependency scheduling and action limits are software responsibilities. For objective
CI facts, require evidence rather than a model saying 'looks green'. The handoff
records independent behavioral failures, including a refused add reported as success
and unreliable completion judgment. Existing mechanisms are not an E2E guarantee.

## Next grooming: finish one issue reliably, including delayed CI

Use existing task, ongoing item, session and artifact structures to answer:

- Which exact repository/PR/head commit is the check about? What invalidates a
  green result when a later commit arrives?
- Which checks are required, and how are pending, skipped, unavailable and failed
  results distinguished? Full suites run on Spark; short probes inspect job status
  instead of restarting an expensive suite every tick.
- Who owns continuation while CI runs: the existing task or an explicitly requested
  ongoing watch? Where can the person find that work after closing its chat?
- How are repeated observations deduplicated, failures retried and overlapping fixes
  avoided? Current tick locking/history are not a general external-event contract.
- What happens after pause, cancellation, changed direction, changed grant or merge?
  What exact evidence lets the owner call the work done?

This transition tests information, execution and control together. Keep SQLite and
existing record owners for now. A read adapter/index may be needed for reverse links
and run navigation; a graph database cannot supply missing evidence or delivery
semantics. Consider schema changes only after these concrete missing facts are pinned.

## External capability baseline, not a claim of parity

Official docs reviewed in this discussion:

- Claude Code agent teams support independent sessions, direct communication and
  shared task coordination: https://code.claude.com/docs/en/agent-teams . Aforge's
  task graph is not evidence that the draft's cross-collection consultation works.
- OpenClaw documents recurring and webhook-triggered work, a background-task ledger,
  delivery routes and durable flows: https://docs.openclaw.ai/automation . Existing
  standing checks do not establish equivalent connector/event/recovery behavior.
- Grok documents scheduled/email-triggered automations with inspectable conversations
  per run: https://x.ai/news/grok-automations . Its official overview describes Grok
  Bot's persistent cloud computer: https://docs.x.ai/grok/overview . Aforge's logical
  graph alone supplies neither that hosted environment nor its connected services.

The record vocabulary appears reusable across these task families, but complete
functional parity, ease of asking, integration breadth and reliability have not
been established. Retain the superset as the product ambition, not a completion claim.
