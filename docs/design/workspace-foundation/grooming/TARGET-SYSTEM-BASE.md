# Target system first: records support behavior

2026-09-10. The user explicitly corrected implementation-led reasoning. The task
is to derive the foundation from the personal AI goal we have discussed, then
compare draft #662 against it. Existing names and code are not the product spec.
This note supersedes implementation-first framing in the preceding review, not
the code findings. It records proposed mechanisms separately from agreed intent.

## Goal to preserve

A personal AI environment for development, business and personal work: start
naturally anywhere; find things in stable familiar folders; retain understood
direction without a separate save gesture; discover relevant information beyond
links; work and coordinate in parallel; continue authorized responsibilities when
chats close; inspect sources, current work and outcomes; redirect or stop it.
Reduce repeated explanation, conflicting assumptions and manual routing.
The ambition is broad capability beyond the reference products, not a claim
that either the diagram or current implementation proves parity.

Folders are navigation, not employees or mandatory project roots. Unfiled chats
are valid. A chat may do work directly; routine checking need not create a visible
task. A folder does not become autonomous merely because it contains something.
Do not introduce mandatory roles, supervisors or one agent per instruction.

## The target behavior around the record model

1. Understand an incoming request or event: source, intended outcome, applicability
   and existing authority. Resolve uncertain placement without blocking ordinary
   interaction on organizational bookkeeping.
2. Assemble current direction and state; retrieve source-linked evidence locally
   and globally within access. Know what is required versus merely informative.
3. Execute using tools and tasks, consult other work when useful, and coordinate
   shared outcomes without impersonating the person or expanding the assignment.
4. Check whether the requested condition actually holds. Gather objective evidence
   where possible; use tool-using AI investigation and judgment where needed.
   Missing evidence is unknown, not success or proof of failure. A failed or pending
   condition leads to correction, waiting or an explicit user decision.
5. Retain sources, accepted changes, findings, execution results and evidence,
   with current revisions distinct from superseded history.
6. Continue, finish or wait according to the request. External changes, schedules
   or findings can reactivate ongoing work with bounded, deduplicated delivery.

These are responsibilities of the software, not six new user primitives or
mandatory serial model calls. The UI reads the resulting records: familiar folder
navigation, current activity and drill-down into sources/evidence. A complete graph
may be an inspection view, but need not be the primary way someone works.

## 'Make sure' is an outcome contract

The desired instruction is not satisfied merely by putting its words in a prompt.
For 'run tests before calling this work done', resolve applicability, attach the
requirement to the work, execute/inspect the relevant tests against the relevant
revision, and use the evidence to decide whether to finish or continue. An AI may
plan the checks and investigate failure; the test result is not replaced by its
opinion. For 'keep production healthy', retain ongoing work and activation rather
than pretending the condition can be permanently completed once.

Not every instruction needs an independent AI or periodic monitor. A response
style preference can shape generation; a completion requirement needs outcome
checking; a changing external condition needs ongoing observation. These are
proposed execution behaviors derived from the request, not separate object types.
The prior discussions support the active outcome-oriented goal; the exact
checking, escalation and continuation contracts still need grooming.

## What the SVG leaves unspecified

The SVG supplies record containers and legal reference shapes. It does not yet
settle interpretation of accepted direction, folder/subtree applicability and
conflicts, precise source anchors and artifact versions, context supplied to an
execution, evidence establishing an outcome, durable activation/continuation,
or delivery and deduplication of cross-chat consultation. Existing task acceptance,
context revisions and ongoing-work fields are places to refine these semantics;
do not create a generic node for every new fact needed by the implementation.

## Retrieval is a supporting pipeline, not the product architecture

Keep original exchanges/tool evidence and current structured state. Optional
extraction creates source-linked summaries/findings; it cannot be the only path
to original information or acknowledged instructions. Select applicable direction
structurally. Retrieve other evidence via exact references, lexical and potentially
embedding search, with bounded model exploration of sources and related records.
Use current revisions and original evidence to resolve stale or conflicting hits.

The current title shortlist, per-exchange compression, lexical neighbour selection
and ambiguous scope are plausible recall failure points. The user's poor results
do not establish that small models alone are the cause. Diagnose whether a fact
was retained, retrieved, interpreted or applied incorrectly before changing the
model. Compare source recall, stale/wrong-scope answers, final task success, latency
and cost; do not select an architecture from a model's self-rating of usefulness.

## Build toward the complete base

Refine the existing records and finish one vertical work loop: understand → retain
direction → apply in later/delegated work → act → check evidence → report and allow
inspection/correction. Then extend the same loop across time and related work:
wakes, offline continuation, consultation, conflicting changes, pause/cancel and
duplicate observations. Include thin usable navigation/inspection in each slice.
Quality improvements to retrieval and models should fit behind these stable
contracts, not require redoing folder identity or task lifecycle.

Next grooming artifact: a small contract table for each existing record covering
what it means, who may change it, when it is read, and what evidence supports its
state. Then map exact schema/runtime deltas onto #662 and define full product
acceptance. Full suites and acceptance run on Spark against the exact candidate;
no implementation, deployment or merge is authorized by this proposal alone.
