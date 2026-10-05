# Contextual memory implementation and acceptance

The branch starts at PR #1751 (`8b6a94080`) and merges current dev (`7e3033564`).
It uses one canonical store event journal. Memory rows are bounded claims; evidence,
source suppression, dependency proofs and dismissals are journal projections, not a
second knowledge database or scheduler.

Access is owner-filtered. Correction authority is narrower than read access: settlement
uses only the candidate partition, and a supersession cannot change owners. Provenance
records session/turn, source words or independent tool receipts, observation time and
source revision. Applicability retains natural conditions separately from project and
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
Model-reported use does not train benefit rankings. Dirty/untracked revisions exclude
outcome evidence rather than pretending one ':dirty' marker identifies every snapshot.

Cross-project links require successful full read receipts and an exact resolved reference,
not shared names. A bounded consumer reread checks whether its recorded assumption is
unchanged after a producer content change. This offers a hypothesis to inspect rather
than asserting breakage or authorizing consumer edits. Dynamic dependencies, partial
reads, arbitrary shell programs and worker-only receipts are not handled. Dismissal is
content-based; changed receipts alone do not resurface a notice.

Forgetting suppresses all historical sources of a claim and derived use; searchable
conversation history remains. Completed/abandoned user-declared work can expire from
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
