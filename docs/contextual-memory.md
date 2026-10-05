# Contextual memory implementation and acceptance

The branch starts at PR #1751 (`8b6a94080`) and merges current dev (`7e3033564`).
It uses one canonical store event journal. Memory rows are bounded claims; evidence,
source suppression, dependency proofs and dismissals are journal projections, not a
second knowledge database or scheduler.

Access is owner-filtered. Correction authority is narrower than read access: settlement
uses only the candidate partition, and a supersession cannot change owners. Provenance
records session/turn, source words or independent tool receipts, observation time and
source revision. Literal support is validated against the person's original span, but
every persisted or rendered human-readable field — claim body, conditions, rationale,
rejected alternatives, reconsideration and the observation — passes through the secret
redactor first, so a credential beside a real constraint never travels into the prompt
or memory_evidence. Applicability retains natural conditions separately from project and
revision eligibility. Authority distinguishes user-approved rules, decisions, observations
and proposals. Validity checks latest evidence, expiry, derivations and suppression.
Relevance uses bounded local approved context and lexical fallback plus optional router.

Exact duplicate writes are transactional and body-based; titles are not identity.
Sync records delivery identities and owner-proven tombstones even before an add.
Arrival-order conflicting updates can diverge between receivers: convergence is not
claimed. Legacy ownerless tombstones without a known row cannot be safely attributed.

Ordinary extraction preserves rationale, rejected alternatives and reconsideration
conditions. Supporting words are checked against the actual user turn. Conservative
wording gates reserve binding priority for explicit rules/choices; this remains a
heuristic, not semantic proof. Assistant claims do not corroborate themselves. Tool
claims show their actual receipt alongside their interpretation; they do not prove causes.
Tool receipts available for automatic extraction are turn-local; a later-turn
summary cannot acquire earlier receipts as independent corroboration.
Model-reported use does not train benefit rankings. Dirty/untracked revisions exclude
outcome evidence rather than pretending one ':dirty' marker identifies every snapshot.

Cross-project links require successful full read receipts and an exact resolved
reference, not shared names. A bounded consumer reread checks whether its recorded
assumption is unchanged after a producer content change, and offers a hypothesis to
inspect rather than asserting breakage or authorizing consumer edits. A PAIRED
re-read of a changed producer and an unchanged consumer does not reset the recorded
baseline: a successful full read alone is not demonstrated compatibility, so the
consumer's earlier producer circumstances stand until the consumer's own assumption
changes. Impact scanning is driven by observed state, not by a keyword list in the
turn, so ordinary wording is not missed. Dynamic dependencies, partial reads and
arbitrary shell programs remain unresolved.

Dismissal is precise. With several offers held, naming one dismisses only it, an
explicit plural dismisses the batch, and a bare dismiss that names nothing dismisses
nothing; "do not dismiss" and "don't dismiss" are negations and keep the offer.
Dismissal identity is content-based, persists as a canonical suppression across a
restart, and a later content change may be offered again. The held offer set is
bounded and EVICTABLE, so more than eight historical notices cannot permanently
block new material. A notice is model-facing context, not proof the person saw it.

Binding retrieval spends its window on authority: approved rules and confirmed
decisions are read through their own bounded projection, so a burst of newer
incidental observations cannot starve a live rare constraint, while latest
correction, expiry, conditions and suppression still win. Authority and scope
are judged against the claim's own supporting span, not the whole utterance, so a
true quote of ordinary talk cannot manufacture a rule. Scope is gated the same way:
a project span stays in its project even beside a global sentence, only an explicit
machine-wide span may take machine scope (which then applies across projects on the
same authorized machine and carries no project condition), "everywhere in this
project" is project scope, and no tool observation is inferred machine-wide. Receipts from conversation
history, this session's memory reader and task summaries are derived, not
independent observations: citing one back cannot corroborate a claim, which also
keeps suppressed history from re-entering as fresh proof. Genuine raw tool receipts
are unchanged. A standing run lent the canonical brain for binding is read-only: it
sees the owner's approved rules before its first action and extracts, observes,
imports and remembers nothing back.

Forgetting suppresses all historical sources of a claim and derived use; the raw
original conversation history remains on disk and can still be searched explicitly,
and that retention is not an automatic reuse path because a history receipt cannot
become independent observation. The notice/receipt attention state is bounded and
evictable; the immutable events_no_delete journal still grows, and that raw audit
growth is an explicit, honest limit rather than something pruned away. Completed/abandoned user-declared work can expire from
recall. Existing idle maintenance and standing orders remain the only maintenance and
scheduling machinery. Deferred intention retention alone does not guarantee a standing
order was proposed or ratified.

Full product expectations require final-head terminal evidence. Focused tests prove
contracts, not natural use or real task improvement. The baseline terminal run found a
premature assistant completion claim and lost release-only conditions, then completed a
ledger correction and a fresh-session JSON extension at about $0.078. That historical
binary is not final acceptance. The final report must explicitly list every required
case as pass, fail or not run and include source/binary identity, job IDs, terminal
recordings, actual files/commands/checks, spending and latency. No merge or release is
performed by this task.
