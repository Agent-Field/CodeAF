# Task-space comparison: aforge, Claude Code, OpenClaw and Grok Bot

Research checkpoint: 2026-09-09. This is product analysis, not accepted design,
a benchmark, or authorization for new implementation. “ClawCode” is interpreted
as Claude Code pending correction. A dedicated Grok Bot product was found and
used for this comparison; Grok Build and Grok chat automations are separate
adjacent products, not silently interchangeable with Bot.

## How to compare fairly

These are programmable agent environments with substantial overlap. Theoretical
possibility, support through custom scripts/integrations, and native behavior
demonstrated reliably are different. A strict set containment claim would need
evidence about tools/data access, execution environment, model competence,
budgets, recovery, operating duration and authority, not just an attractive
conceptual diagram. Documentation confirms offered mechanisms; it does not
benchmark success at arbitrary complex work.

Useful work patterns are analytical descriptions, not new aforge primitives:

| Work pattern | Nontrivial outcome | What makes it hard |
| --- | --- | --- |
| Divide a bounded problem | Audit many routes or reconcile hundreds of source documents into supported conclusions. | Independent evidence gathering, verification and synthesis; aggregation must not conceal errors. |
| Jointly solve a coupled problem | Migrate a system while API, storage, UI and operations constraints evolve. | Findings alter other workers' plans; messaging alone does not reconcile incompatible assumptions. |
| Maintain an ongoing responsibility | Process a changing support queue or monitor a repository, acting on material exceptions. | Intake, retained state, waiting, retries, stop, permissions and avoiding repeated action. |
| Keep several changing efforts coherent | A feature delay affects a launch, customer promises, campaign drafts and meeting preparation over weeks. | Discover affected work, preserve current decisions and authority, reopen the right outcomes, and keep the person informed. |
| Reuse and improve a way of working | A successful investigation or reporting method is reused with new inputs and revised after feedback. | Separate reusable procedure from private context and measure improvement rather than equating repetition with quality. |

The common intersection includes investigation, creation, transformation,
tool-mediated action and iterative checking, subject to actual tool access and
configuration. Parallelism broadens investigation; communication lets intermediate
findings change another plan; persistence carries intent across inactive periods.
They combine, but none implies the other.

## What the official documentation establishes

**Claude Code.** Distinguish caller-owned subagents from agent teams: teams permit
direct teammate exchanges and shared task coordination, whereas ordinary
subagents return to a caller. Current docs also describe cross-session discovery
and messaging, reusable scripted dynamic workflows, and cloud/desktop/session
scheduling. It cannot fairly be categorized as purely short-lived coding chat.
Sources: [agent teams](https://code.claude.com/docs/en/agent-teams),
[cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging),
[workflows](https://code.claude.com/docs/en/workflows),
[scheduling](https://code.claude.com/docs/en/scheduled-tasks).

**OpenClaw.** A Gateway supports persistent agent/session routing and channels;
background children and experimental scripted Swarm support parallel execution.
Heartbeats run periodic turns rather than implying nonstop model inference.
Standing-order guidance separates authorization from automation timing. Thus
ongoing personal assistance, parallel research and multi-stage operations can
overlap here; it is not simply a timer attached to a chatbot.
Sources: [multi-agent routing](https://docs.openclaw.ai/concepts/multi-agent),
[subagents](https://docs.openclaw.ai/tools/subagents),
[Swarm](https://docs.openclaw.ai/tools/swarm),
[heartbeat](https://docs.openclaw.ai/heartbeat),
[standing orders](https://docs.openclaw.ai/automation/standing-orders).

**Grok Bot.** The vendor describes persistent bots with computers and signed-in
app access, concurrent work, collaboration in shared threads, retained context,
and learning a routine from demonstration. This supports a product model of
entrusting recurring or extended work across existing apps. These are vendor
claims, not independently measured reliability or proof that every demonstrated
routine generalizes.
Source: [Grok Bot](https://x.ai/bot).

**Adjacent Grok offerings.** Grok Build documents large parallel workflow runs
with saved progress and reusable programs. Grok automations document scheduled
and email-triggered conversations. Those reinforce ecosystem overlap, but do
not prove that each capability is exposed identically inside Grok Bot.
Sources: [Build workflows](https://x.ai/news/workflows),
[Grok automations](https://x.ai/news/grok-automations).

## Where aforge sits

The conceptual model can express bounded work, ongoing responsibilities, shared
context, coordinated reasoning and retained methods without requiring permanent
worker identities. Its proposed strength is one continuous, person-readable
account of related work: folders provide stable navigation, decisions retain
source and applicability, and executions are attempts at work rather than the
identity of the entire environment.

That is a product/semantic emphasis, not evidence of a strict task superset or
exclusive technical ability. Other programmable environments can implement
similar patterns. Aforge must demonstrate less manual context transfer, fewer
inconsistent decisions, understandable recovery and control, or another concrete
improvement on equivalent journeys. Folder organization alone establishes none
of those outcomes.

The first backend slice has evidence for real owner lookup, ordinary workers and
current explicit sourced information, including revision/withdrawal/reopen.
Its broader task and standing machinery predates this slice. The new store's
source attribution is not accepted-decision authority. It currently refreshes
explicitly resumed consumers; it does not prove live cross-conversation
consultation, unlinked discovery, semantic activation from shared changes,
context in scheduled firings, or learning/adoption.

The source checkpoint is production commit `a216cdcf5` plus test clarification
`6ce8be8bd`, recorded by the implementation owner in
`docs/design/workspace-foundation/TEST-RESULTS.md` on draft #662. Nine scenarios
have evidence across two runs, not one all-green full-suite run. The calendar
case is shared-information consumption, not a complete calendar connector test.

Additional practical gaps to assess rather than assume away: usable external
system access, hosting while the user's machine is off, large workload
performance/cost, visible controls for many ongoing efforts, and complete
cross-work recovery. General shell access is not equivalent to a tested,
maintained connector or deployment environment.

## Implication for our grooming

Keep the current primitives. Compare complete operations under the same tool and
authority assumptions. The interesting target combines discovering a change,
understanding its consequences, acting within existing responsibilities,
coordinating when needed, and retaining the outcome so the next cycle begins
with current understanding. No permanent roster or broad platform rewrite is
required merely to express that.

The five journeys are probes of this larger task space. They should test the
transitions between work patterns, not become five separate domain engines.
Modularity comes from reusable behavior and clear ownership. Our next design
choices remain applicability, accepted meaning, reaction, coordination and
visibility; competitor terminology does not add requirements by itself.
