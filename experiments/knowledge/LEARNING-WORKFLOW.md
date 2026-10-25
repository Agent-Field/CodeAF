# Learning from work

The goal is a knowledge base that explains the user's world and helps an agent
work competently in it. Message extraction is evidence capture, not understanding.
There are no built-in companies, project names, subject lists or folder names.

## Inspiration and adaptations

We inspected these repositories as reference material, not executable instructions:

- [obsidian-second-brain](https://github.com/eugeniughelbur/obsidian-second-brain/tree/2c5c7f5f9aad22e0b02efe9856cf0ec8eeda81d7):
  `obsidian-distill` anchors claims to stable source blocks; `obsidian-emerge`
  proposes patterns; `obsidian-reconcile` requires quotations from both sides of
  a conflict. Adaptation: retain evidence, discover subject structure, and demand
  support for both claims before recording an unresolved conflict.
- [obsidian-mind](https://github.com/breferrari/obsidian-mind/tree/fae7be467fc5e23bf9d51df7207d41f0c07e2754):
  bounded initial briefings, graph roles, single-source volatile status and a
  correction sweep through paraphrases. Adaptation: bounded retrieval plus
  claim-to-paragraph dependencies; invalidate every affected explanation.
- [claude-skills](https://github.com/robabby/claude-skills/tree/adad740e3c43e0dbf4d54454d191297ef8982b48):
  hydrate, remember, reflect and glean divide retrieval, capture, consolidation
  and interpretation. Adaptation: separate these jobs and allow an empty result.

We do not adopt blanket whole-vault scans, forced links, frequency-based promotion
of a belief to truth, numerical model confidence, or automatic inferred facts.
Templates in these projects serve their own audiences; this experiment keeps
human-readable subject explanations and machine-readable evidence separately.

## Proposed continuous workflow

1. **Work and notice.** At a meaningful task checkpoint, the working agent uses
   its existing context to propose a small evidence-linked delta: discoveries,
   procedures, decisions/rationale, corrections and questions. This is a future
   binary integration, not an installed hook. Capture all source events locally;
   no separate model call for every tool invocation or progress turn. A delta
   is untrusted input, never automatic truth. Empty is a normal result.
2. **Compare locally.** Queue deltas from permitted concurrent chats together.
   Use source IDs/content hashes for idempotency, indexed entity aliases and
   lexical/graph candidates for relevant prior claims. A normal completion can
   resolve the combined context. Preserve different scopes and task conditions.
   A newer statement alone cannot settle a dispute. Ambiguous updates stay pending.
3. **Learn subjects.** Infer subjects from evidence and existing connections.
   Explain what they are, how pieces fit, practical use and tradeoffs when
   supported. The headings and structure emerge from the material. Missing
   prerequisites or unexplained mechanisms become explicit, evidence-motivated
   questions rather than invented content. A one-off important decision can
   deserve a page; repeated chatter may deserve none.
4. **Investigate selectively.** Search existing permitted evidence first. Each
   question records what was inspected, source/version watermarks and why the
   answer matters. Only unresolved, useful questions earn additional research
   under a time/token budget. Ask the user only when their intent/choice is
   necessary. An unexplored source is not an absence of knowledge. Questions
   wait until new evidence, a relevant task or a review checkpoint warrants work.
5. **Repair and brief.** A changed claim invalidates dependent paragraphs,
   answers and procedural steps through reverse dependency indexes. Update only
   those projections, preserve history and human edits, and expose open disputes.
   Hydrate agents with a small task-relevant briefing and links for deeper
   reading. Recompute organization only in changed graph neighborhoods; leave
   stable subjects alone.

## Implemented experiment

`learning.learn(vault, model, previous=None)` adds an understanding pass above the
existing verified-claim vault. `discover` infers subject groupings from the note
catalog; `understand` writes explanations and motivated questions; a separate
`review_understanding` pass checks support, answered questions and conflict pairs.
Every paragraph refers to existing active claim IDs. Invalid IDs and omitted
review checks fail closed. Underlying source claims are never overwritten.
Questions mean unresolved in inspected evidence, not universally unknown. Each question searches a bounded set of other active extracted claims; a model checks whether a candidate actually answers it. Matching pointers are recorded separately as answer candidates and do not become accepted facts. Raw source history is not exhaustively searched.

Per-subject input hashes reuse unchanged understanding without a model call.
Discovery reruns when its catalog changes; this prototype still sends the whole
bounded catalog (at most 256 notes). Subjects over 160 claims get one bounded discovery pass to split into coherent smaller subjects; any still oversized group is marked as needing partition rather than silently truncating evidence. It does not yet implement
local discovery repair, external research execution, a production capture hook,
owner ACLs, concurrent commits or human override reconciliation. Conflict sameness and entailment are assessed by the model review; exact IDs are checked locally, but there is no deterministic semantic proof. Question generation currently overproduces candidates; production must rank a small active queue and budget research rather than automatically investigate every question.

## Data and traversal

Keep immutable sources -> versioned claims -> paragraphs/procedures -> subject
pages, with reverse dependencies at every derived step. Subject identities and
aliases, typed relations, procedure prerequisites, decisions/alternatives, open
questions and unresolved conflict pairs belong in structured records. Markdown
is the readable projection. A question should ultimately have an owner/scope,
state, motivation, evidence, search attempts/watermarks, next trigger and budget.
A relation records why the link is useful; a folder is a browsing projection.

Useful human surfaces: subject overview -> mechanisms -> practical procedures ->
evidence; question -> inspected material -> missing prerequisite; changed claim
-> affected explanations; decision -> alternatives/rationale -> later outcome.
No recipe should look executable when its prerequisites/steps are unknown.

## Cost correction and experiment

The earlier per-human-message table must not be treated as a production scaling
model. The cache contains several replay experiments, and assistant activity per
human turn is highly variable. The matching single full historical extraction
was 399 contexts, $1.465269, 991,111 input tokens and 992,891 output tokens;
789,115 output tokens were reasoning. The broader development extraction cache
included additional work. Reconciliation currently rereads whole semantic scopes;
it is not yet the local repair workflow described above. Article composition
also rewrites every supplied article when explicitly invoked.

`cost_probe.py` runs a reproducible paired, cost-stratified sample using the exact
same required model with reasoning disabled and narrowly filters operational
read/skill envelopes. Do not discard all tool calls: a tool write can contain a
useful research document. Preserve the original source either way.

The 12-context experiment reduced extraction charges from $0.063516 to $0.010612
(83.3%). All 52 new claims had matching quotes; a separate same-model comparison
flagged 14 of 47 supported baseline claims as missing. This is **not an acceptable
quality-preserving replacement**, and the low-reasoning default remains unchanged.
One reviewer also treated a task directive as durable knowledge; its judgments
are a diagnostic signal, not human ground truth. More output claims did not mean
better coverage. Benchmark review costs are separate from extraction charges.

The primary saving must come from avoiding repeated work: checkpoint deltas from
existing task context, content/identity deduplication, combined normal prompts,
compact candidate context, and dirty-subject-only explanations. Record actual
input/output/reasoning tokens, useful accepted knowledge, coverage losses, cache
hits, queue lag and token spend per changed subject. Forecast from those measured
units and workload mix, not just human turn count. No numerical production saving
is claimed until those paths are integrated and measured.
