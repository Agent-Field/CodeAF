# Continuous personal knowledge: experimental design

## Objective and acceptance

As a person converses and tasks run, derive a readable, linked personal wiki
which agents can recall from. Preserve the difference between a proposal, an
accepted decision, an observed fact, a preference and an agent's unverified report.
Every derived assertion must resolve to original supporting evidence. Human
corrections and folder moves win. History remains available; retirement is not
deletion. Agent recall must respect proven codeaf owner/project visibility.

The design reference supplies a two-level tree, note links, source provenance,
history, an unsure flag, raw clips and a small always-in-mind section. It is a
product reference, never a source of executable agent instructions. Graphs are
an optional inspection tool, not the default home screen. No numerical confidence
scores or automatically promoted raw clips are required.

## Recommended data structure

Use the existing codeaf event/store infrastructure for integration, with SQLite
relations and FTS projections rather than a new mandatory graph database.
Markdown is an editable/exportable projection, not the sole machine truth.
The graph is an adjacency index over claims, evidence, entities and notes;
support can involve several premises, so evidence/dependency join tables are
more appropriate than a single parent pointer or a plain folder tree.

| Record | Important fields | Reason |
|---|---|---|
| Source event | store ID, session/task ID, message/sequence, content hash, observed time, proven owner/project | Immutable observation and permission authority |
| Evidence span | source ID, exact character span, quote, source role | Audit any sentence without trusting a summary |
| Claim version | stable claim ID, subject, predicate, value, modality, scope, recorded time, effective time if known, status | Preserve corrections and distinguish intent from reality |
| Entity and alias | canonical ID, aliases, scope, evidence and reversible merge event | Resolve names without conflating similar projects |
| Note projection | stable note ID, title, human override, folder IDs, claim IDs, content revision | Human navigation can change without breaking links |
| Typed edge | supports, contradicts, replaces, related-to, depends-on, mentioned-in | Answer why something is believed and what a correction affects |
| Processing record | source watermark, schema/prompt/model versions, job key, base revision, cost, dirty set | Retry, deduplicate, budget and diagnose backlog |

Indexes: owner/project + claim status; entity/alias; FTS over titles and current
claims; forward and reverse note/claim edges; source-to-dependent-claim edges.
Use exact owner filters before FTS, traversal and model context assembly. A
semantic category or model-generated project name is never an access-control key.
Embeddings can later add candidate recall, but cannot establish truth, identity
or permissions. No embedding model is used in this experiment.

For dual time, `recorded_at` means when the knowledge system learned a claim;
`effective_from/to` means when the assertion was true, and may be unknown. A
message timestamp alone does not prove effective time. Never fabricate it.

## Capture, coalesce, reconcile

Use push capture plus debounced context assembly and periodic maintenance.
"Batch" here means several updates inside one normal completion request.
It does not mean the OpenRouter Batch API or one giant request over all history.

1. On committed turn/task output, append a local outbox event with no model call.
   Task progress and internal nudges are not automatically knowledge events.
2. Coalesce eligible events per visibility/project partition. Starting policy:
   30 seconds quiet, 120 seconds maximum queue wait, or a context-size threshold.
   Several chats/tasks can contribute to the same structured request when their
   data is permitted in that context. Retain their distinct source identities.
3. Assemble new evidence plus relevant existing claims, neighboring notes and
   a compact topic/alias index. A normal single completion proposes a typed delta.
4. Validate citations, permissions, types, replacement direction and revision.
   Commit atomically with the watermark; retrying the same job cannot duplicate it.
5. Every five minutes, reconcile only dirty topics across chats. During idle
   periods, review category churn, unresolved conflicts and stale source types.
   No changes means no model call. These intervals are hypotheses to tune.

Maximum queue wait excludes provider execution and rate-limit delay. Expose
"catching up" and a processed-through watermark rather than claiming immediate
freshness. Explicit corrections should have priority over cosmetic reorganization.
One slow job must not block unrelated partitions. Retries keep the same model,
use bounded backoff, and preserve the durable queue across restarts.

Concurrency is safe only with revision control: independent partitions can run
in parallel; overlapping updates validate their base revision and rebase/retry.
Do not let two completions overwrite a note with last-writer-wins semantics.
Use a lease per commit partition and a deterministic job key from source IDs,
base revision and compiler version. Continuous arrivals cannot reset the maximum
wait deadline forever. Large bursts split by token budget while retaining recent
conversation context and exact source IDs.

## One-shot structured delta contract

The proposed online request has this shape. The `update_once` experiment implements
one-call extraction/reconciliation with temporary claim IDs and bounded candidate
context. The offline baseline uses separate extraction and per-scope reconciliation;
production permissions, revision control and queue integration remain future work.

```json
{
  "schema": 1,
  "job_id": "deterministic-id",
  "base_revision": 42,
  "partition": {"owner": "proven-owner", "project": "proven-project"},
  "updates": [
    {"source_id": "store:chat:seq", "kind": "conversation", "role": "user", "text": "..."},
    {"source_id": "store:task:seq", "kind": "task_result", "role": "agent", "text": "..."}
  ],
  "existing": {"claims": [], "notes": [], "aliases": [], "overrides": []},
  "required_output": {
    "claim_additions": [],
    "replacements": [],
    "note_patches": [],
    "link_changes": [],
    "unresolved": []
  }
}
```

New claims use temporary IDs inside the response; the validator assigns stable
IDs and rewrites references in the same transaction. Every addition names exact
source spans. Every replacement names an existing claim and its replacement,
with explicit evidence of correction. Note patches reference claim IDs rather
than inventing unsupported prose. Unknown source/claim IDs, cross-owner edges,
impossible time order, missing citations and destructive operations are refused.
Material that cannot be interpreted safely enters an unresolved queue.

## Bound the work as history grows

Never put the entire knowledge base into every update. Retrieve a candidate
neighborhood using aliases, FTS and adjacency; pass only affected claims, their
premises and compact note summaries. Novel topics enter a small topic index,
not a global regeneration. Use hash equality for unchanged inputs and exact
duplicates; semantic merges remain reversible model proposals.

With new material size `d`, candidate neighborhood `k`, and local affected edges
`e`, target work is indexed lookup plus approximately `O(d + k + e)` assembly.
This is a target, not a measured guarantee. High-degree entities, broad changes,
ambiguous aliases and long dependency chains can violate bounded neighborhoods;
defer those to budgeted maintenance and expose backlog rather than truncating truth.
Full rebuilding remains an offline diagnostic/reference operation.

The prototype uses compact temporary claim IDs and a constrained response schema
to prevent invalid references and runaway link output. Its cached per-scope organizer
avoids redoing unchanged scopes, but
still processes all claims in a changed scope. This is the main scaling limit
to remove before connecting it to each turn in the binary.

## Recall and graph traversal

Filter by proven permissions first. Resolve query aliases, retrieve FTS candidates,
and add a bounded number of typed neighbors. Prefer current supported claims;
include unresolved conflicts explicitly, and use historical versions only when
asked about history. Limit traversal by hops, edge type, fan-out and token budget.
Large general-purpose hub notes should not flood the context.

An additional global navigation pass normalizes the two-level folder vocabulary,
removes generic links, and proposes specific cross-topic connections. It changes
folders and links without changing source evidence or semantic scope.

Compare lexical retrieval, model-planned one-hop traversal and coverage/diversity
selection under the same claim/token budget. A follow-up can expand the search
if evidence is insufficient. Return citations for every supported answer and
abstain when nothing answers the question. Selecting valid IDs prevents invented
references but does not by itself prove every generated sentence is entailed.

## Cross-domain candidates

The algo-crosspollinator catalog was searched by five structural axes: sequential
state, identity alignment, multiscale organization, dependency propagation and
attention coverage. The catalog contained 1,940 algorithm rows and 238 model rows
at inspection. No catalog model is substituted for DeepSeek in these experiments.

| Adapted combination | Origin and useful transfer | Experiment / limitation |
|---|---|---|
| BK-tree candidates + constrained semantic merges | Metric indexing: cheaply find spelling aliases before semantic checking | Measure alias precision; metric distance is not semantic equivalence |
| MinHash/SimHash blocking + exact evidence hashes | Streaming near-duplicate detection: avoid repeated summaries becoming independent evidence | Measure candidate misses; hashes cannot decide factual identity |
| CUSUM-inspired hysteresis + human folder locks | Process control: avoid categories moving on every weak new observation | Measure moves per replay; needs calibrated scores before claiming statistical guarantees |
| Greedy coverage + relevance + prompt budget | Combinatorial coverage: avoid spending context on repeated statements | Compare recall across topics under the same token budget |
| Dependency queue + local graph repair | Incremental inference/robotics: revisit consequences of a correction, not every note | Compare affected work with full replay; cycles require explicit handling |
| Persistence across neighborhood thresholds + stable IDs | Topological analysis: recognize clusters that survive parameter changes | Research-only; do not add heavy topology without a demonstrated gain |
| Soft alias assignment + reversible merge proposals | Optimal transport: retain ambiguity instead of forcing premature identity | Research-only; ordinary many-to-one aliases are not Hungarian one-to-one assignments |
| Hirschberg-style version alignment + human overrides | Sequence alignment: explain changed note passages with bounded memory | Compare readability and memory; preserve claim IDs as primary anchors |
| Local activation + global topic inhibition | Reaction/diffusion: discourage a popular broad topic swallowing specific notes | Treat as a regularizer hypothesis, not a biological justification |
| Sequential evidence accounting + independent-source grouping | Sequential testing: distinguish new evidence from copied/repeated claims | Repeated model output is correlated; no raw LLM confidence probabilities |

## Knowledge articles, not conversation reports

The reading surface is synthesized subject prose: concepts, mechanisms, rationale,
trade-offs and open questions. Source-role labels and conversation attribution
belong in provenance, not repeated "the agent reported" or "the user asked"
sentences. The atomic claim/evidence layer remains separate for agent recall,
corrections and audit. Articles combine those claims into source-linked paragraphs.
Proposed designs and reported measurements keep their qualification naturally.

`--articles` runs composition with compact local IDs and a constrained schema,
then a separate semantic review against the cited claims. Unsupported paragraphs
are retained as rejected editorial history, not presented as knowledge. Empty or
unresolved topics remain drafts. This second model pass is not human verification.
An update preserves unaffected article paragraphs and removes those whose cited
claims became obsolete; changed notes are marked for an editorial refresh.

## Maintenance rules

Human corrections, preserved negation and known scope outrank stylistic tidying.
A later assistant report cannot silently supersede a person's decision. Proposed
changes never become accomplished facts merely because a task started. A source
retraction invalidates dependent assertions and triggers local repair. Keep the
old assertion and reason in history. Repetition does not increase authority.

Treat staleness by knowledge type: an enduring preference and a current service
status have different review needs. Do not delete or lower truth merely because
an old note was not read. Track `last_supported`, `last_checked`, read/use counts
and changes in supporting evidence separately. An agent reading something is not
proof it applied it correctly. Preserve manual text and folder placement as
explicit overrides, with a reviewable merge when generated text conflicts.

## Cost accounting

Record role, exact returned model, provider, prompt/compiler version, input tokens,
output tokens including reasoning, cache use, duration, provider charge and retries.
Separate historical backfill, new-material extraction, reconciliation, audits and
question answering. Development/research/viewer generation is not recurring usage.
Failed or truncated responses may be billable; successful-call totals are a lower
bound unless their usage was also returned and recorded.

For each role, cost is `input_tokens × input_price + output_tokens × output_price`,
with provider-specific cache charges where applicable. Use actual `usage.cost`
for measurements; the catalog's lowest advertised price is not a reliable bill.
Reasoning can dominate cost and output length even with a low effort setting.

Estimate daily extraction from new turns and typical evidence length; add the
number and size of dirty-neighborhood commits, audits and recall calls. Project
100, 1,000 and 10,000 new human turns/day under explicit assumptions. Batch size
mostly amortizes repeated context and request overhead; it does not make new
evidence tokens free. Reprocessing all historical notes on each turn would make
cost grow with history and is an unacceptable online default.

## UI/UX designer handoff

| Opportunity | Stored information | Human / agent value |
|---|---|---|
| What changed | previous/current claim IDs, recorded/effective times, replacement reason | Understand a correction and recall the right historical state |
| Why this is here | evidence spans, source role, originating conversation/task | Inspect provenance without turning chat history into the wiki |
| Not sure / disagreement | incompatible claim IDs, missing premises, review state | Surface uncertainty without false numerical confidence |
| Consequences of a correction | reverse dependency edges, affected notes | Preview what else needs updating |
| Alias merge review | names, proposed canonical entity, evidence, reversible action | Resolve duplication without merging unrelated projects |
| Kept by you | human text/folder overrides, revision history | Preserve the person's organization across agent tidying |
| Catching up | processed-through watermark, pending source count, oldest queue age | Explain freshness honestly during rate limits or concurrent work |
| What agents used | recall query, selected claim IDs, actual use feedback, token budget | Explain recall and distinguish reading from successful application |
| Missing knowledge | unanswered queries, ghost links, absent evidence | Show where more information is needed instead of inventing facts |
| Scope preview | proven owner/project keys and permitted recall context | Prevent knowledge from crossing the wrong project or agent context |
| Related by meaning | typed links with reasons and backlinks | Navigate concepts rather than imposing one folder location |
| Raw material vs accepted knowledge | clip IDs, derived claims, verification state | Keep saved material useful without automatically believing it |

## Evaluation and release boundary

Keep deterministic tests for evidence references, corrections, scope separation,
duplicate cycles and replay idempotence. Add live real-history questions and
human-reviewed source judgments. Compare algorithms on exactly the same sources,
with equal context budgets and no answer leakage. Measure note coherence, alias
precision, unsupported claims, category churn, correction propagation, recall
coverage, latency, token cost and backlog under concurrent arrival.

The current viewer is a research inspector. Online capture, proven permission
integration, human-edit merging, dependency retractions, transactional delta application,
distributed synchronization and production scheduling require further work before
desktop or binary integration. Do not infer those features from a polished viewer.
