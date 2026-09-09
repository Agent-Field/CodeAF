# Personal AI: living design and delivery record

Updated 2026-09-09. Discussion owner: Codex task
`01a08653-1fcf-7880-b64f-dae46f29b86a`.

Start here when continuing this discussion. Read [decisions](DECISIONS.md) before
interpreting a proposal as accepted behavior. [Journeys](JOURNEYS.md) define the
product examples and evidence boundaries. [Delivery](DELIVERY.md) governs future
build tasks, branch ownership and integration.

[Task-space comparison](LANDSCAPE.md) places the aforge design alongside Claude
Code, OpenClaw and Grok Bot using official documentation. It distinguishes
expressible behavior, shipped mechanisms and verified outcomes; it does not
claim that aforge is a superset or add new primitives to the product.

[Common model and task coverage](FRAMEWORK.md) revisits the earlier mathematical
study, defines a practical comparison under resource and authority constraints,
and compares five requests across all four systems. It is a discussion proposal.

[System design diagram](SYSTEM.svg) is the whole-system logical map requested
after the user found the step-through presentation unclear. It separates durable
records from activation/execution, labels the main relationships and marks open
integration/design. It is not a UI mockup, a deployment topology, a new set of
services, or a claim that every shown path works today.

## The product we are preserving

Aforge is one personal AI work environment for development, research, marketing,
email, calendars and everyday work. The person can think in ordinary chat, make
things directly, delegate finite work, entrust ongoing responsibilities, and
return to inspect or steer them. Useful context and ways of working accumulate
through experience. Connections between efforts should reduce the person's need
to repeat information or route messages.

Preserve the [agreed model](../CONSTRAINTS.md): familiar folders organize
conversations, work and artifacts, with meaningful references, sources,
dependencies and explicit applicability. Decisions and instructions remain
identifiable. The organization of work does not dictate a hierarchy of permanent
agents, departments or managers. Organization, relevance and authority differ.

The user explicitly rejected adding primitives or architecture merely to fit a
new example. The terms below name delivery tracks and test slices ONLY. They are
not new product objects, modules, services or required UI labels. Customization
and specialized views are future boundary checks, not an add-on platform to
build now. Reuse existing methods/programs, task ownership, standing behavior,
memory and runtime boundaries where they suffice.

## Current work and integration destination

The main working branch for this effort is `codex/personal-ai-backend`, draft
PR #662, in `/Users/santoshkumar/af-personal-ai-backend`. It stays unmerged into
`dev`. “Bring it back to the main working branch” does not mean Git branch
`main`, `dev`, `staging`, or publishing a release.

The existing implementation task is **Ideate seamless home chat UX**,
`01a0839c-a6c1-7a93-94f2-be0e4c528f64`. Its recorded checkpoint covers production
commit `a216cdcf5` and test clarification `6ce8be8bd`: touched-package regression
passed and all nine live cases have evidence across two runs, not one entirely
green full-suite run. Paid CI reports NOT RUN because its key is absent. See the
integration branch's `TEST-RESULTS.md`; this is read evidence, not our rerun.

Its first wave owns collections, resolved owner state, explicit sourced context,
revision/withdrawal, ordinary chat/task context refresh and memory-off behavior.
It has acknowledged the five-domain acceptance in [JOURNEYS.md](JOURNEYS.md).
Scheduled application of that new context, autonomous triggers, live peer
consultation and global discovery are not yet proved by the first wave. Existing
docs may lag in-flight code; completion requires receipts on an identified commit.

This grooming record has its own documentation worktree and branch,
`/Users/santoshkumar/af-personal-ai-grooming`, `codex/personal-ai-grooming`, based
on pushed integration commit `81de2f213`. Keep proposal edits out of the active
implementation checkout. The discussion owner updates these files as the person
settles decisions; builders return evidence and proposed corrections rather than
silently changing the product contract.

## Active working method: one evolving demonstration

The user rejected the earlier multi-step roadmap as inefficient and accepted
working with one system drawing and one real demonstration together. Choose the
next experiment by the uncertainty it resolves. The five domains substitute into
the same model and reveal counterexamples; they do not each get a separate
architecture. See [DEMONSTRATION.md](DEMONSTRATION.md) for the current experiment.

Here we settle the next uncertain behavior and its visible consequences. The
implementation task makes the last agreed behavior work and returns real
receipts or concrete failures. Keep enough design ahead to unblock implementation;
do not groom a speculative platform first. UI observation, correction and stop
are considered with the behavior they control. The isolated branch, confirmed
contract and integration evidence rules in [DELIVERY.md](DELIVERY.md) remain.

The current experiment reuses the existing API-contract case: one sourced record,
three explicitly targeted collections and consumer chats, a revision, then
sequential explicit resumption. A fresh run passed at `12017ee2a`; its seven
artifact contents and full receipt are retained in the demonstration record.
The accompanying interactive drawing is an
illustration, not a connection to aforge or a new product E2E result.

The first missing transition is a person's ordinary explicit decision becoming
retained applicable direction. Automatic no-extra-save retention is confirmed
(C09); exact scope and authority remain open (D01-D04). Existing shared context
is information. Do not equate sourcing with person acceptance or implement new
semantics merely to make the demonstration appear complete.

## Folder inspector study

The user requested a Finder-like drawing to clarify a folder's contents versus
its properties. The exploratory interactive study is retained in the discussion's
artifact directory:

- Editable fragment: `/Users/santoshkumar/.codex/visualizations/2026/09/09/01a08653-1fcf-7880-b64f-dae46f29b86a/folder-inspector.html`.
- Standalone preview: `/Users/santoshkumar/.codex/visualizations/2026/09/09/01a08653-1fcf-7880-b64f-dae46f29b86a/folder-inspector-preview.html`.

It shows contents and a selected-item inspector. The folder's own proposed
properties stay small; applicable decisions, memory and connections remain
inspectable records. Selecting an ongoing responsibility reveals its activation
and permitted actions, rather than treating these as properties of the folder.
An alternative places decisions/memory in the contents list instead of the
inspector. The study is not a finalized TUI layout, implemented capability or
confirmation of scope inheritance. Selection, both placements, light/dark and
narrow/wide layouts were checked locally. No product E2E claim follows from that.
