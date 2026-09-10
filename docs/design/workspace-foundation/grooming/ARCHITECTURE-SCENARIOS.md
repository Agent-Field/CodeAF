# Architecture scenario review and missing-piece audit

2026-09-10. Written after [ARCHITECTURE.md](ARCHITECTURE.md), then used to review
and tighten it. This is a separate logical walkthrough of the proposed model,
not execution of the product or competitive benchmarks. S1–S7 refer to the
architecture's sequence diagrams; G1–G12 identify its gap ledger.

**Assessment**

The selected journeys fit the same folder/chat/work/file model, with Context and
Run records. They do not require departments, a permanent agent per folder,
another scheduler, a separate memory truth store, or a new folder for each
correlation. Several flows are conditional on unconfirmed product choices and
unimplemented operational contracts. “Fits the design” is not “works in #662.”

The review specifically tests the five previously agreed domain journeys and
the later Slack, production, organization, workflow-learning and inspection cases.
The comparator scope is useful documented capability families, not every feature
of every reference product. A model that can describe an external action cannot
perform it without the corresponding tools, access and host.

**A. Detailed walkthroughs**

**J01 — direct conversation without setup**

Input: “Help me understand this code; let's discuss options.” S1 retains a message,
assembles current direction and retrieves source evidence. Direct reading and
discussion stay in this chat; no Work item is necessary. With no sensible folder,
the chat remains unfiled. Opening it tomorrow restores history and live owner
state without starting a new paid investigation merely because it was viewed.

Counterexample: a model's interpretation of “we might rebuild this” cannot become
an accepted implementation task. A discovered related folder is a reference,
not permission to inherit its unrelated instructions. Check the absence of
unrequested tasks, background commitments and governing-placement changes.

Result: **fits S1**. Ordinary chat exists; automatic organization/scope safeguards
are part of G1/G2. No obligatory task primitive is added to the simple path.

**J02 — daily Slack review, edit it later, stop one review**

Input: “Weekdays at 9, review #product and #launch for decisions I owe. Read and
report; don't post.” S1 creates ongoing Work R with source message, accepted
scope, time/timezone and permission. The setup chat and R can both be referenced
from Work/Launch. S2 admits occurrence O1; Run U1 records R's current revision,
reads source messages and produces a result in U1's conversation.

The person opens U1 and asks why a message matters; the same run conversation
continues. “Also include #sales from tomorrow” revises R, not just U1's prompt.
U2 consumes the new revision while U1 retains its historical inputs. “Stop today's
review” cancels U2; R can remain active for tomorrow. “Stop checking Slack” stops R
and prevents future admission. Ambiguous “stop it” with multiple consequential
targets uses the existing question path.

Negative cases: repeated occurrence O1 does not create U1 twice; a quiet monitor
does not alert; an explicitly requested daily digest still delivers; missing Slack
credentials never produce a false “reviewed” claim; input cursor advancement
does not lose an unprocessed page after a crash.

Result: **fits S1/S2/S3/S7 conditionally**. G3–G6/G9 remain real implementation
work. The draft's standing mechanism is a useful base, not proof of this journey.

**J03 — two software bugs share a cause**

Inputs: two authorized fixes in two chats, each with local task number 1. The task
references include their owning sessions. Issue A discovers evidence of a common
authentication bug. S4 retrieves Issue B as a candidate and tests the connection;
a third task containing the word “auth” is rejected as unrelated.

S5 delivers a peer-origin question to B. They retain one shared exchange. A
confirmed shared investigation gets one claim; both issue outcomes depend on it.
If A and B ask simultaneously, the owner returns the same shared-work identity.
Its result is delivered to both, but each bug still needs its own acceptance,
actual patch/delivery and checks. An answer from B cannot authorize merging A.

Negative cases: offline B leaves a pending/timeout state rather than lost delivery;
duplicate replies cannot start another investigation; peers cannot deadlock by
holding resource claims while waiting for each other. Partial common progress is
not completion of both issues. Similar wording alone is insufficient for dedupe.

Result: **fits S4/S5/S6**, requiring addressed consultation, confirmed shared claims
and resource coordination. The draft proves qualified references and informational
context, not the complete collaborative journey. G3/G4/G9 are relevant.

**J04 — API contract changes across backend, frontend and documentation**

Initial state: three authorized efforts use Context C revision 1. A discussion
suggests a rename; it remains proposed and does not govern any consumer. The
person later accepts the new contract, producing revision 2 with an exact source.
S4 finds consumers through explicit applicability and retained material dependency
references. Each active effort receives a sourced update at a safe boundary.

Before consequential operations, current revision checking prevents a slow
consumer from proceeding as if revision 1 still governs. A short coordination
exchange can settle compatibility. S6 checks the actual integrated frontend ↔
backend flow and documentation against revision 2; three individually plausible
reports are not proof of integration.

Negative cases: quoted suggestions cannot become accepted changes; unrelated
consumers remain unaffected; completed published docs are flagged or updated only
under the relevant assignment/maintenance authority. A failed event consumer after
the context commit must receive the change after restart, without another user
message. A cap on snapshot size cannot drop the accepted contract.

Result: **fits S1/S4/S5/S6 with G2/G3/G4/G8**. The draft's explicitly resumed
context-revision cases cover only the first portion. A durable semantic revision
plus a wake path is required for the requested autonomous behavior.

**J05 — ongoing repository maintenance / software factory**

Input: “Investigate eligible issues and dependency failures, prepare fixes and PRs,
follow this upgrade policy, and leave merging to me.” Ongoing Work R references
the current policy and required method/tool capabilities. GitHub events and a
scheduled check are alternative triggers into R, not different maintenance owners.
An eligible issue produces finite Work F with a run and explicit acceptance.

F uses a suitable workspace/host, consults overlapping feature work if useful,
runs required checks on Spark, opens a PR through the actual authorized connector,
and retains its result. Delayed CI becomes a durable wait. A result event resumes
F; it does not start a second fix. R remains active after F finishes. Stopping R
affects future intake according to S7 while ongoing fix cancellation is explicit.

Negative cases: the same issue seen through polling and webhook should coalesce
using stable issue/version/work identity; two different issues must not collapse
because their titles are similar. A reopened issue or new head revision may require
new work even if an earlier occurrence was handled. Pending/skipped tests do not
count as green. PR-write access does not imply merge authorization.

Result: **fits S2/S3/S5/S6/S7**. No Factory object is required. G3/G4/G5/G9 must be
implemented and tested. The finite issue work and ongoing intake remain distinct.

**J06 — research invalidates a marketing claim**

Initial state: an authorized campaign draft used a sourced competitor finding C1.
Research finds contradictory evidence; it creates a qualified revision/withdrawal,
preserving both sources. S4 uses the draft's material dependency and selective
search to identify likely affected work. A contradiction need not immediately
become a confidently accepted replacement fact.

The authorized draft work removes or qualifies the unsupported claim, retaining
the prior artifact version. S6 inspects the resulting draft. If the campaign was
already published and no maintenance assignment covers it, surface the impact and
the needed decision; do not silently assume perpetual permission to alter it.

Negative cases: stale transcript text cannot override current withdrawn state;
unrelated similar claims remain unaffected; a missing source is shown as missing;
the new evidence cannot enlarge the campaign's goal or publish authority.

Result: **fits S4/S6**. Revision/withdrawal has draft evidence; autonomous impact
processing and completed-output treatment remain G3/G7/G8/G10.

**J07 — conference email affects travel and calendar**

Input: “Watch conference email and tell me about changes affecting my itinerary.”
S3 persists email E and filters it against the current itinerary. A relevant
schedule change produces one impact check, source links and an attention item.
The run reads the calendar if available and permitted. Under report-only authority,
it neither moves the meeting nor sends a message on the person's behalf.

Redelivery of E and restart reuse the handled identity and pending notification
receipt. A correction to E is a new source version and must be assessed. After
“move that meeting,” the system resolves the precise event/current version and
the actual granted action before editing, then retains the external result.

Negative cases: a changed meeting with a similar subject is not necessarily the
same event; lack of a calendar-write connector in a fixture cannot prove authority
enforcement. Test a write-capable connector and assert zero unauthorized writes.
An ambiguous external write after a crash is reconciled rather than replayed blindly.

Result: **fits S2/S3/S6/S7**, requiring actual intake, connector boundaries, effect
receipts and delivery. The draft's generated impact artifact proves a narrower
context-use case. G3/G4/G5/G9 apply.

**J08 — ensure tests pass; then keep production healthy**

The first request is an outcome requirement, not a new daily job: a finite fix
must gather exact-revision test evidence and satisfy applicable conditions before
completion. An AI can investigate a failed check; it cannot declare unknown CI
results successful. A correct patch must not be damaged merely because a second
model made an unsupported objection.

“Keep production healthy” establishes continuing Work, with the intended health
signals and permitted interventions made concrete. Observations can produce finite
incident work; recovery evidence closes the incident run, not the responsibility.
The person can review the trigger, checks, action and result in its run history.

Negative cases: unavailable health signals mean unknown/unavailable, not healthy;
repeated unchanged incidents do not create endless repairs; a repair exceeds its
budget or needs access through a visible controlled state. The broad sentence
needs an operational outcome contract; the system must not pretend it already
knows every service or acceptable remediation.

Result: **fits S3/S6/S7**. G2/G4/G9 remain essential. Instruction injection alone
does not complete either the finite evidence loop or continuing health assurance.

**J09 — wrong starting folder, shared files, exceptions and moves**

A launch discussion develops into conference travel planning. The conversation
may gain a Travel reference without losing its Launch history or acquiring every
Travel rule silently. An explicit trip budget is Context targeted to that trip's
work; removing the discovered Launch–Travel association does not remove the budget.

The person corrects an inferred association. Record its rejected basis so rereading
unchanged history does not restore it. A later explicit reference can establish
new relevance while retaining that earlier correction. A true governing move
changes future folder-scoped guidance; a shortcut does not. Compatible instructions
survive a local exception. The same file reached from both places is the same
artifact, but a draft in an isolated execution may still be pending delivery.

Negative cases: automatic filing must not change authority; two governing
placements cannot silently pick whichever conflicting instruction is newest;
removing membership cannot delete the underlying chat/file. No new folder is
needed for every intersection or every unfiled chat.

Result: **conditional on P1/G1/G2**. The architecture identifies the missing
semantic distinction; it does not mark that proposed choice user-approved.

**J10 — learn a workflow, then repeat it**

After a successful briefing, retained evidence can support a reusable method File
with versioned steps, input requirements and appropriate tools. This is learning a
way to perform work, not permission to perform it every day. “Use this every Monday”
establishes ongoing Work referencing that method and its schedule.

Each run records the method version and actual inputs. A corrected method applies
to future runs; past runs remain explainable. Learning from a browser demonstration
also requires actual observation/computer-use support; the storage shape alone
does not provide that capability. External procedure text never supplies its own
authority to operate tools.

Result: **fits S1/S2/S6**, with method-learning quality and capability integration
still open. No Method service or Workflow primary object is required initially.

**J11 — unexpected global connection and proactive suggestion**

Marketing asserts fast setup while an unlinked coding investigation found a slow
setup path. A source query or incremental discovery finds the candidate, checks
its actual relevance and offers the finding with sources. If marketing is already
authorized to correct its draft, that can continue. If the consequence is a new
benchmark project, make a proposal rather than adopting an obligation silently.

Negative cases: near-neighbor similarity does not prove contradiction; global
readability does not grant scope; a retired finding must not be injected as current.
The system can discover an unlinked relationship without creating a folder or
spawning an inter-agent conversation automatically.

Result: **fits S4**, but G7 needs retrieval measurement. The model supports the
case without promising immediate discovery of every relevant fact.

**J12 — one year of work and “why did you do that?”**

Folder/work projections show current state and progressively expose references.
An action can be traced to its Run, triggering event or message, Work revision,
applicable direction, source anchors, tool result and artifact version. A historical
question reads historical versions deliberately; a new action reads current ones.

Negative cases: moving or deleting a source cannot silently substitute another
file; a path without historical contents is not historical proof. Missing sources
remain unavailable. The graph is an inspection projection, not a second owner of
task state, nor a requirement to load every message into a global visual graph.

Result: **fits the reference model and S1/S6**, requiring G4/G10 retention and
causal receipts. The draft retains only part of this trace.

**J13 — 10–20 concurrent efforts, offline recipients and restart**

Work owners expose current status and questions. The scheduler/admission boundary
limits concurrency, prioritizes interaction and retains background queue progress.
Shared store writes are transactional; conflicting context updates use expected
revision checks. Consultations retain pending delivery without blocking other work.

On restart, S7 reconciles executor liveness and uncertain effects before replacing
a run. A laptop that is off requires an available remote coordinator/host to
continue; otherwise the item truthfully shows pending/unavailable. Reading the
dashboard does not launch all those efforts again. Counts and completion claims
come from owner state, not model summaries of conversation history.

Result: **fits S2/S5/S7**, requiring G3/G4/G5 and measured load/fairness. Recorded
twelve-chat context tests are not equivalent to this complete concurrency journey.

**J14 — delayed checks, cancellation and unknown external effects**

A work item opens a PR and waits for CI. Its continuation references that exact
head. A new head invalidates the old head's success as proof of the current one.
Stopping the work prevents the old CI event from restarting it. If publication
was in flight when the host failed, use the target's operation identity/result to
establish whether it happened before any retry.

Negative cases: cancellation is not rollback; expiration of a claim is not proof
that its executor stopped; no reliable idempotency/readback may mean a person must
resolve an unknown result. Do not call this exactly-once merely because intake
deduplicates events. Resume must use the current work revision and permission.

Result: **fits S6/S7 with explicit uncertainty**, and cannot be promised without
G4/G5 and connector-specific reconciliation.

**J15 — parallel specialists with different tools and context**

Input: “Have a security reviewer and a performance reviewer examine this change.”
S1 admits bounded child work; S5 gathers independent findings and S6 checks the
combined result. Each specialist receives the relevant revision, a profile and
source references, retains its own transcript, and returns inspectable evidence.
Read-only review remains read-only even if a reusable profile requests write tools.

Check conflicting findings, an unavailable preferred model, context exhaustion
and a malicious instruction in reviewed source. Disclose material substitutions;
peer content cannot widen authority. Existing child execution is a foundation,
not proof of per-profile context and tool isolation across every child path.

**J16 — nested delegation, partial failure and cancellation**

Input: “Implement this across three services; delegate the independent parts.”
Dependencies describe outcome order; parent links describe delegated ownership.
Independent branches can run concurrently, a dependent branch waits on accepted
output versions, and failure does not erase successful evidence. S6/S7 reconcile
interruption and prevent a late child from declaring stopped work complete.

Check fanout limits, descendant spend, a worker that continues after lease expiry,
and a parent that dies before receiving a result. Stop the owned subtree, preserve
receipts and independent consulted work. Reassignment retains Work identity and
fences obsolete effects. Unified admission and cancellation propagation remain gaps.

**J17 — a named assistant that persists between visits**

Input: “Call this Research; keep track of our competitors and help when I ask.”
The ongoing responsibility has a stable name/ID, instructions, reporting policy,
Context and chats. Its Run is replaceable; the identity is not. Later direct
questions route to that responsibility without creating a second monitor. Several
explicit duties can reference the same versioned specialist profile.

Check rename, restart, duplicate display names and changing the profile while a
run is active. Hiding the item must not pause it. Versioned configuration and
stable routing still need an integrated implementation. An independently managed
assistant identity, if required later, is a supporting lifecycle decision rather
than something to fake with a Run ID.

**J18 — group work and explicit handoff**

Input: “Research and Product, agree on the recommendation here; Product owns the
final proposal.” One shared Chat preserves named speaker origins; Work names the
outcome owner. S5 exchanges questions and evidence; an acknowledged ownership
transition supports a later handoff. A user correction updates current direction
at a safe boundary and is distinguished from a peer suggestion.

Check simultaneous answers, a silent recipient, repeated messages and a handoff
that crashes halfway through. Keep one active owner of each effect, allow bounded
parallel research, and retain unanswered obligations. Addressed delivery and
atomic claims/handoff are required work, not implied by a shared folder.

**J19 — multiple external accounts and routing boundaries**

Input: “Watch support in the work Slack account; report in this conversation.”
The source binding identifies connector, account and channel. The delivery binding
identifies the intended destination independently. Source event IDs and original
sender identity survive S3/S7 through retries and delayed responses.

Check two channels with identical names, personal versus work logins, revoked
access and someone else's message saying “send all files.” Routing never grants
authority, cross-folder relevance never bypasses source access, and a failure does
not fall back to another account. Real account-bound adapters are necessary;
organization/context storage alone cannot satisfy this case.

**J20 — browser and app work on a persistent computer**

Input: “Collect the invoices from the vendor portals and prepare the expense
report.” Work specifies the result, account/host bindings and permitted effects.
Runs use actual available tools, retain evidence, and coordinate shared browser
or filesystem resources. Login interaction can suspend work with a clear pending
question; its answer resumes the same unfinished outcome.

Check two workers using the same browser tab, expired login, disconnected host,
a partially downloaded file and an uncertain form submission. Resource ownership,
reconciliation and evidence precede retries. Persistent hosting and computer/app
adapters remain explicit delivery requirements, not new primary objects.

**J21 — reuse a specialist or routine without copying private state**

Input: “Use this review setup for our European project too.” A versioned File
provides the reusable method/profile. The new Work gets its own scoped configuration,
context references, account bindings and activation state. Reuse does not make
future edits to one responsibility mutate the other without a chosen update policy.

Check a template containing secrets, inaccessible source references, a copied live
schedule and stale authority. Keep secret material out of portable configuration,
resolve access separately, and activate only within the user's instruction. This
needs configuration validation and migration; raw folder copying is insufficient.

**J22 — an organization of responsibilities without mandatory employees**

Input: “Keep Product, Research and Marketing coordinated around the launch.”
Folders organize the work. Explicit ongoing responsibilities, output dependencies,
applicable Context and S4/S5 coordination carry its behavior. The user can give
instructions through chat, inspect active responsibilities, change one outcome
and see which existing work is affected. Optional role names aid addressing.

Check circular dependencies, contradictory directions, duplicate ownership and a
request merely to describe an organization. Do not start responsibilities from a
descriptive diagram. Escalate irreconcilable outcome conflicts; suppress causal
feedback loops. The architecture fits a coordinated personal workspace. Enterprise
multi-tenant administration and organizational authorization need their own design
if requested; this is not a claim of full enterprise organization parity.

**J23 — add a capability without adding a primary object**

Input: “Only run the Slack review after the release finishes and the daily window
opens.” Work gains a typed activation configuration with explicit joined conditions,
correlation key/window and expiry; intake retains each source occurrence before
admission. A later calendar adapter can implement the same contract without changing
folder ownership or inventing CalendarWork as another main object.

Check duplicate events, the conditions arriving in reverse order, a schedule edit
between arrivals, timezone changes and an older runtime reading the new schema.
Unknown behavior remains inactive; old evidence cannot satisfy a new revision
unless its transition rule permits it. P3 keeps configuration separate from runtime
join/cursor state. Compound-trigger semantics are proposed work: merely storing
arbitrary properties would not provide correct execution.

**B. Reference-product coverage**

Official documentation was checked on 2026-09-10. These are documented capabilities,
not measured reliability claims. The links support each family; the mapping to our
architecture is this review's inference.

| Reference family | Mapping to our design | Status of comparison |
| --- | --- | --- |
| Claude Code specialist contexts, reusable configurations and bounded tool access | J15/J16; S5/S6 | Requires consistent execution configuration and isolation across child paths. [Subagents](https://code.claude.com/docs/en/subagents). |
| OpenClaw isolated agent state and account-to-agent bindings | J17/J19 | Requires stable routing and actual access boundaries; a directory is not a sandbox. [Multi-agent routing](https://docs.openclaw.ai/concepts/multi-agent). |
| Grok Bot named persistent roles, reusable configuration and visible collaboration | J17/J18/J21/J22 | Composed responsibilities and profiles fit selected journeys; identity/handoff/configuration lifecycle remains real work. [Bots](https://docs.x.ai/grok-bot/bots), [Collaboration](https://docs.x.ai/grok-bot/chat-and-collaboration). |
| Claude Code direct work and team coordination: shared tasks and direct inter-agent messages | J01/J03/J04; S1/S5 | Representable; addressed consultation and shared claims still need implementation. [Agent teams](https://code.claude.com/docs/en/agent-teams). |
| Claude Code completion-driven continuation: a separate evaluator reads surfaced evidence and can request another turn | J08/J14; S6 | Existing checking foundation; exact evidence, waiting and reliable final-state reporting still matter. Our proposed tool-based evidence gathering is not a claim the competitor evaluator itself uses tools. [Goals](https://code.claude.com/docs/en/goal). |
| Claude Code cloud routines with schedule, GitHub and API triggers | J05/J13/J14; S2/S3/S7 | Requires available hosting and actual event adapters in addition to the logical model. [Routines](https://code.claude.com/docs/en/web-scheduled-tasks). |
| OpenClaw schedules, ambient checks, event intake, background history and durable multi-step flow | J02/J05/J07/J13; S2/S3/S6/S7 | Fits work/trigger/run/continuation records; no second scheduler needed. [Automation](https://docs.openclaw.ai/automation). |
| OpenClaw session search/history, cross-session sends and spawned subagent work | J03/J11/J12; S4/S5 | Source search foundations exist; automatic impact/consultation remains incomplete. [Session tools](https://docs.openclaw.ai/concepts/session-tool). |
| Grok scheduled/email-triggered automations with inspectable run conversations and reporting choices | J02/J07; S2/S3 | Our ongoing item plus run history fits; intake and outward delivery must be demonstrated. [Automations](https://x.ai/news/grok-automations). |
| Grok Bot persistent computer, app/browser use, independent collaboration and routines learned from demonstrations | J03/J10/J13; S2/S5/S7 | The model accommodates these; persistent hosting, computer-use adapters and learned-routine behavior are additional real product work. [Grok Bot](https://docs.x.ai/grok-bot/overview). |

No architecture forces us to adopt permanent employee/department concepts to
support these capabilities. However, not adopting them does not prove superior
capability: comparable end-to-end results and less routing/steering effort must
be measured. Product-specific media, device, enterprise, connector and packaging
features outside the discussed journeys have not been exhaustively audited here.

**C. Defects exposed by the walkthrough, and design corrections**

| Counterexample | Missing piece in a naive model | Resolution in the architecture |
| --- | --- | --- |
| A shared reference unexpectedly changes instructions | Membership conflates navigation and governance | P1 proposes placement/reference purpose; needs confirmation rather than implicit migration. |
| A saved contract never reaches work after a crash | Best-effort change notification | Durable owner outbox/intake, original source position and deduplication. |
| A pending signal is lost after advancing a Slack cursor | Source consumption conflates completed assessment | Persist intake before advancing cursor; track processing/disposition separately. |
| One event reappears through polling and webhook | Transport dedupe misses semantic duplicate work | Source-version identity and a confirmed work/problem claim, not text similarity. |
| An idle monitor never sends the daily report | “Quiet by default” overrides the request | Separate scheduled deliverable policy from meaningful-change-only alerts. |
| The next timer starts a second unfinished review | Recurrence conflates new occurrence and continuation | Explicit overlap policy; resume/coalesce where intended. |
| A Context summary and its reindexing repeatedly wake each other | Every background write is treated as new evidence | Emit impact only for meaningful source/semantic revisions; retain causal lineage and processed-version state; bound feedback chains. |
| Two tasks both claim a common investigation | Links alone provide no ownership | Atomic admission claim after confirming shared problem identity; keep original outcomes. |
| A stopped task restarts on a delayed CI result | Cancellation is only conversational text | Durable revision/fence checked on admission and action; existing actual stop controls. |
| A failed worker's lock expires while it still runs | Lease expiry mistaken for cancellation | Liveness reconciliation and fenced effects before replacement. |
| A model claims a file was delivered although landing is pending | Work success conflates external delivery | Owner-reported delivery state and acceptance conditioned on the requested outcome. |
| Source removal breaks historical explanation | Current path mistaken for immutable evidence | Versioned source anchors, retained content when promised, explicit unavailable state otherwise. |

These corrections refine behavior and infrastructure. They do not require another
primary product object. P1–P3, detailed scope conflict defaults and
missed-occurrence/in-flight policies remain proposed decisions.

**D. Evidence required before calling the implementation complete**

For each supported journey, retain a receipt naming the exact candidate revision,
production entry point, model, fixtures, assertions, actual outputs, time/spend,
job/session and limitations. Use real model-driven execution with deterministic
synthetic Slack/GitHub/email/calendar boundaries. Include write-capable fixtures
when asserting that an action was not authorized.

Check stored identities, revisions, action counts, source anchors, current owner
state and delivered artifacts. Convincing prose is not a receipt. Test repeated
events, wrong scope, conflicting revisions, rejected associations, unavailable
capabilities, process death at commit/delivery boundaries, cancellation, and a
new real event after a prior one was handled. Consumer refresh must be caused by
the event being tested, not a hidden manual resume from the harness.

The reusable acceptance groups are:

| Group | Journey coverage | Real boundary that must be exercised |
| --- | --- | --- |
| Conversation and direction | J01/J04/J09/J12 | Saved messages, current scope, actual source reads and correction persistence. |
| Recurrence and external intake | J02/J05/J07 | Real admission path, controllable clock, connector events, cursor and delivery state. |
| Coordination | J03/J04/J11 | Origin-aware addressed messages, shared claim, active and offline consumers. |
| Outcome and effects | J05/J06/J08/J14 | Artifact/version checks, external wait/result, permissions and delivery receipts. |
| Recovery and capacity | J02/J07/J13/J14 | Crash injection, stopped-state replay, overlap, liveness and bounded concurrency. |
| Specialists and addressing | J15/J16/J17/J18/J19 | Profile restrictions, source visibility, stable account routing, handoff and descendant cancellation. |
| Environments and reuse | J20/J21/J22 | Shared resource races, inaccessible credentials, copied configuration and bounded organizational authority. |
| Extension contracts | J23 | Compound conditions, version changes, unknown schemas and replay through the real admission owner. |
| Methods | J10 | Retained method version, independent later inputs, corrected method and real tool support. |

Full suites, full affected-package runs and acceptance run on Spark, never the
laptop. A missing key/tool/host or a skipped case is not a pass. This document
records logical review only; no product acceptance was run for this design change.

**Review conclusion**

All twenty-three selected journeys have an explicit route through the proposed
architecture. None requires expanding the working product vocabulary simply to explain its
behavior. This is not a cap on backend entities: independent lifecycle, addressing,
authorization and recovery state must be modeled explicitly. Final user-facing
names and projections remain open. The routes depend on the named scope, delivery, admission, evidence
and hosting contracts; some policy defaults still need confirmation. The draft
implements an organization/context foundation, not these complete routes.
Next design discussion should settle P1 and the change/stop boundaries, using
J09 and J14 as concrete consequences, before dispatching their implementation.
