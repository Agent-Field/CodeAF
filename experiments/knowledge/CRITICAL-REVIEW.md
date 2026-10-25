# Critical review: useful pages are not yet a dependable knowledge base

The current experiment demonstrates sourced subject pages and bounded recall. It
has not demonstrated complete capture, primary-source accuracy, stable long-term
organization, or production cost at sustained load. Model acceptance and article
length are not quality metrics.

## Findings and changes

| Finding | Change in this iteration | Remaining limit |
|---|---|---|
| Every guide paragraph depended on its whole subject | Depend only on cited claims; keep exact raw-source hashes separately | Uncited semantic dependencies and later conflicting new evidence still need discovery |
| Source citations can lead back to an agent's own summary | Project source roles, claim kinds and unresolved conflict flags onto prose; expose qualification in the inspector | Role is coarse provenance, not factual verification; tool fetch versus tool write needs explicit host metadata. independently_verified stays false because this experiment performs no independent verification |
| Sourced prose can still be a temporary execution diary | Separate evidence support and lasting reference usefulness gates; both are required | Same-model judgements can miss noise; historical present-tense descriptions still need freshness checks |
| Review repeats quotes and includes uncited records | Send cited claims/blocks once and use block IDs in paragraphs | Same-model semantic review remains fallible |
| Conflict awareness arrived after writing | Supply known conflict candidates to both writer and reviewer | Candidate detection is incomplete; no automatic adjudication or external verification |
| Quantity can disguise weak organization | Audit question saturation, missing topic links and unsupported reading paths | Hierarchy/identity quality needs held-out and longitudinal evaluation |

Source role/date changes invalidate learner reuse. Conflict changes currently replan the whole guide; paragraph-level cached composition is not yet implemented. Retired-claim conflict pairs remain in history, while current prose badges only include pairs with active alternatives.

The guide dependency regression exercises the existing sourced-note seam: retire
an unrelated claim and the paragraph survives; retire its actual cited support and
it disappears. Raw-only paragraphs remain dependent on their source hashes; this
does not mean future contradictions can be detected without new evidence intake.

## Next experiments, in order

1. **Evidence first.** Host-recorded source types distinguish user intent,
   repository snapshots, primary document fetches, command results and agent
   synthesis. An agent-written dossier with URLs remains a synthesis until those
   sources are actually retrieved. Keep valid-at and recorded-at times distinct.
   Measure incorrect promotion and unresolved contradiction handling on held-out
   cases; do not infer independence from source IDs or repeated wording.
2. **Stable topic maps.** Separate durable concept IDs from titles/folders. Review
   split/merge suggestions as reversible changes with redirect/history records.
   Build maps of content from semantic memberships and evidence-linked relations;
   do not invent relationship facts from co-occurrence. Measure disconnected
   subjects, useful route length, duplicate coverage and churn after new chats.
3. **Useful curiosity.** Ask fewer questions. Keep them only when missing knowledge
   blocks understanding or a real task; retain why it matters, inspected evidence,
   last attempt and wake condition. Rank by task relevance and topic coverage,
   rather than number of claims or easily answered trivia. New evidence can wake a
   parked question; unchanged time alone should not restart a failed search.
4. **Push capture with selective repair.** Stage optional same-completion proposals,
   deduplicate locally, debounce dirty updates across already permitted chats into
   one ordinary bounded request. Use source->claim->paragraph reverse dependencies,
   owner-scoped publication and idempotent event receipts. Maintain indexed source
   postings to notice relevant unseen evidence without invalidating every subject
   on every event. These production hooks are not installed yet.
5. **Quality per token.** Freeze a held-out task set with expected original evidence,
   corrections, negations, copied reports, sparse subjects and topic renames.
   Compare capture omissions, retrieval support, answer correctness, changed-note
   churn and total charged tokens. Use random omission audits as well as optional
   proposals; neither self-selection nor a same-model judge proves recall.

## Data direction

SQLite remains sufficient for the experiment. Store immutable source events,
versioned assertions, assertion/evidence support edges, paragraph dependencies,
concept identities/redirects and dirty-work receipts. A subject page is a reading
projection; Markdown is its portable export. Keep containment, related navigation,
factual dependencies, contradictions and hypothesized connections as different
edge types. A filesystem folder is one view, not the identity of the knowledge.

Production decisions need measured throughput and concurrency first. No graph
server, vector service, autonomous web-research loop or nightly full rewrite is
justified solely by the existing page count. Designer-facing opportunities include
source-basis badges, competing views, why-linked explanations, reading paths and
staleness indicators; none should imply complete or independently verified truth.
