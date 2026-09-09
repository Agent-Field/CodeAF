# Common model and practical task coverage

Discussion proposal, 2026-09-09. This extends [LANDSCAPE.md](LANDSCAPE.md), not
the accepted product contract. It adds no primitives or implementation mandate.

## What the earlier mathematics established

The local study at `/Users/santoshkumar/Documents/org-theory-study/study.md`
explored finite transition systems, refinement, workflow soundness and
decentralized control under partial information. Five small naive models had
counterexamples involving review identity, evidence versions, revocation,
capacity reservation and interacting changes. Repairs established narrow safety
properties, not a universal minimal ontology, general usefulness or guaranteed
progress. No worked business-organization theory was found in those results.

Its primary foundations were [refinement mappings](https://www.microsoft.com/en-us/research/publication/the-existence-of-refinement-mappings/),
[workflow-net soundness](https://www.vdaalst.com/publications/p628.pdf) and
[partial-history-sharing control](https://arxiv.org/abs/1209.1695).
Our application to AI collaboration is a synthesis, not a theorem from them.

The consequences remain useful: a task tree does not resolve shared-resource
conflicts; several agreeing agents may share one mistaken source; a timeout
does not establish that an external action failed; relevant information is not
delegated authority; refusing every risky action does not complete useful work.

## A common language without another object model

Describe a run using the existing work, context and execution records:

    intent + observed information + applicable decisions + allowed actions
        -> reasoning, tools and optional collaboration
        -> outcome + evidence + effects + retained updates

This is a feedback loop: further observations, user corrections and outcomes
change what should happen next. Retained context carries information between
runs; activation determines when another run is due. An ordinary chat can do
direct work without creating a standing responsibility.

Parallel workers are multiple concurrent runs. Coordination changes what each
run knows or plans. An ongoing responsibility survives its individual runs.
A reusable method describes how runs proceed. Folders provide navigation and
scope relationships where agreed. These are interpretations of existing
concepts, not proposed services, universal nodes or permanent agent roles.

The semantic distinctions need enforceable rules:

- A retained assertion keeps its origin; inference does not silently become a
  person's accepted instruction.
- Runs and evidence can identify the relevant revisions they used. A changed
  premise can require reconsideration; it does not rewrite earlier history.
- Permission is checked when an effect is attempted. Being informed of a
  decision or being related to a folder does not itself grant permission.
- Retrying after an uncertain external result requires establishing what
  happened. Duplicate-free behavior needs support at the effect boundary.
- Stop, correction and recovery are part of the work's behavior, including a
  path to completion or an intelligible request for human input.

Exact applicability, reaction and correction behavior remains in grooming.
This document does not settle recursive folder inheritance or cancellation of
in-flight work after a revision.

## Why human–AI collaboration changes the design

Our design hypothesis is that human attention and correction are scarce, while
AI reasoning can be replicated and replaced but remains context-limited and
fallible. Additional agents can repeat a shared error. The person supplies and
revises goals, taste and delegated authority; AI can help clarify those goals,
investigate uncertainty, propose methods and perform authorized work.

That favors durable work and sourced understanding over requiring a permanent
employee roster. It also favors selective context over sending everyone all
history. Replication should follow measured needs for independence, knowledge
or throughput; it is not inherently a quality improvement. Persistent named
helpers may still be useful presentations over the same mechanisms.

## What a meaningful superset would mean

Let C(S; E, B, q) denote journeys system S can complete in environment E, within
budget B, at required reliability q, while respecting the person's authority.
E includes actual tools, data and hosting. B includes setup and repair effort,
human attention, money and elapsed time. q is a requirement to evaluate, not an
estimated probability supplied by this document.

Calling aforge a practical superset of another system would require containing
its task set under comparable conditions, not merely being able to describe
the same workflow or write an arbitrarily large custom program. We have no such
proof or benchmark. A common formal model can describe all four without one
implementation containing the others.

Optimize the tradeoffs among outcome quality, human effort, inconsistent/stale
assumptions, time and money, subject to authority and acceptance constraints.
Do not allow a weighted score to compensate for an unauthorized action.

## Five requests across four systems

Cells describe documented mechanisms and our inference about the remaining
work. They are not measured ease-of-use rankings or exhaustive impossibility
claims. Grok Bot means the dedicated product, not Grok Build or the X bot.

| Request | Claude Code | OpenClaw | Grok Bot | Aforge checkpoint and intended improvement |
| --- | --- | --- | --- | --- |
| Find several bugs, recognize the common cause, fix and test it. | Workflows can fan out investigation and cross-check results; synthesis still must establish the shared cause. | Background agents or scripted Swarm can divide investigation; repository and test setup must be available. | Parallel bots and app access provide a route; repository-level repair and test guarantees are not established by its product page. | Existing workers plus explicit shared context support a starting point. The first-wave case demonstrates shared information, not autonomous discovery of the common cause. |
| Change an API while backend, UI and docs work in parallel. | Teams and independent sessions exchange findings; a message alone does not reconcile every outdated decision. | Sessions exchange messages and can watch subsequent human/goal changes; dependency-specific reconciliation still needs behavior. | Shared-thread bots can collaborate; precise decision applicability and revision rules are not established by the page. | Sourced revisions reach explicitly resumed consumers. Live consultation and the accepted-decision contract remain unfinished. |
| Keep a repository healthy every night, preparing fixes within my rules. | Scheduled cloud/desktop work and reusable workflows provide ingredients; each environment needs appropriate access. | Persistent Gateway, activation and workers provide ingredients; maintenance policy, effects and recovery need configuration. | Persistent bots and scheduled routines fit the request; repository-specific execution must be demonstrated. | Existing ongoing-work machinery exists. Scheduled consumers do not yet receive the new context seam; this complete loop is unproved. |
| Correct research and update every affected campaign draft without publishing. | Research and editing can be coordinated through workflows/messages; an affected-output inventory and publication boundary must be maintained. | Search, messaging and change watching support coordination; evidence-to-output dependencies need a method. | Research/comms bots can pass work in a shared thread; complete propagation of a source withdrawal is not established. | Intended source/applicability/dependency model fits. First-wave evidence covers explicit consumers, not finding every affected output automatically. |
| Watch a travel change, revise calendar plans, and retain my correction for next time. | Tools and scheduled work can implement this with calendar/travel access and a retained procedure. | Ongoing assistance and session context fit; integrations and action permissions determine completion. | App access, retained context and demonstrated routines directly fit its positioning; reliability remains unmeasured here. | Current travel case proves shared-information consumption only. Real calendar effects, activation, scoped correction and later reuse remain to prove. |

Claude sources: [workflows](https://code.claude.com/docs/en/workflows),
[teams](https://code.claude.com/docs/en/agent-teams),
[messaging](https://code.claude.com/docs/en/cross-session-messaging),
[scheduling](https://code.claude.com/docs/en/scheduled-tasks).
OpenClaw sources: [session tools](https://docs.openclaw.ai/concepts/session-tool),
[Swarm](https://docs.openclaw.ai/tools/swarm),
[standing orders](https://docs.openclaw.ai/automation/standing-orders).
Grok source: [Bot product description](https://x.ai/bot), vendor claims only.
Aforge evidence: integration worktree `docs/design/workspace-foundation/TEST-RESULTS.md`,
production `a216cdcf5`, test clarification `6ce8be8bd`, draft #662.

## What this changes in our next discussion

The architecture appears compatible with the common model; completeness and
usefulness remain to demonstrate. A software or marketing factory is a configured
combination of responsibilities, methods, context, tools and runs. No factory
primitive is implied. A custom dashboard would be a view and control surface
over that work, not an independent second authority or state store.

Next groom one general transition: a person corrects a decision while related
work exists. Specify where the correction applies, how affected work finds it,
what happens to work already underway, what may happen automatically, and what
the person sees. Measure repeated explanation, stale outputs, recovery effort,
human interventions and completion. Use domain examples as counterexamples to
the rule rather than special-purpose features. This preserves the existing
slice plan and does not authorize a new implementation task.
