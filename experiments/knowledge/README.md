# Knowledge lab

An experiment on the `knowledge-base` branch of codeaf, starting from `dev`.
It reads an existing message database, extracts sourced claims, compares direct
topic notes with reconciled notes, and exports a local wiki for human inspection.
It does not change the installed binary, desktop UI, live memory store or schedules.

## Run a private replay

Use Python 3.12 or newer on Linux or macOS with the standard library. The offline
viewer works in a modern browser on any platform. Set `OPENROUTER_API_KEY`
through your shell's credential configuration. Every model role is pinned to
`deepseek/deepseek-v4.1-flash`; returned model IDs are checked before accepting
responses. OpenRouter may route between providers of that exact model. There
is no alternate model and no OpenRouter Batch API.

```sh
python experiments/knowledge/lab.py replay \
  --database /absolute/path/to/graph.db \
  --out /absolute/private/path/to/knowledge-lab \
  --sessions 8 --audit --hierarchy --articles
python -m http.server 8765 --bind 127.0.0.1 \
  --directory /absolute/private/path/to/knowledge-lab/viewer
```

Open `http://127.0.0.1:8765`. The viewer also opens directly as an HTML file.
Use `--sessions 0` for all eligible sessions in that database. Outputs contain
personal conversation material: keep the output folder outside the repository.
Only generated viewer files belong in its served directory; do not serve the
whole output folder. Sources are read through a read-only SQLite snapshot.

## Ask the knowledge base

```sh
python experiments/knowledge/lab.py ask \
  --out /absolute/private/path/to/knowledge-lab \
  --scope codeaf --retrieval coverage \
  'What have we decided about storage?'
```

Retrieval variants are `lexical`, `graph` (model-planned search plus bounded
one-hop note traversal), and `coverage` (adds a diversity/coverage heuristic).
They are experiment variants, not established improvements. Scope labels in
this importer are semantic groupings inferred from text, not security identities.
They must be replaced with codeaf's proven owner/project keys before integration.

## What is stored

- `direct/` and `reconciled/`: JSON snapshots, SQLite projections, an append-only
  hash-linked local event ledger, and Markdown notes with wiki links and citations.
- `calls/`: cached model outputs and measured successful-call usage, cost and
  latency. No credentials or request headers are written. Failed attempts may be
  billable and are explicitly excluded from the successful-call cost total.
- `audit.json`, `comparison.json`, `scheduling.json`: semantic verifier results,
  structure measurements and replayed scheduling policies. Same-model judgments
  are not human ground truth; quotation presence is not semantic entailment.
- `viewer/`: offline note browser, full-text search, backlinks, original source
  messages, history and experiment comparison. It makes no model calls.

## Try a shared-context update

```sh
python experiments/knowledge/lab.py update \
  --messages /absolute/private/path/to/new-messages.json \
  --out /absolute/private/path/to/knowledge-lab
```

The message array uses `id`, `session`, `role`, `ts` and `text`. One completion
receives new messages from multiple chats plus up to 48 existing candidate claims
and 12 notes. Temporary claim IDs are resolved and exact evidence is validated
locally. New message text is capped at 24,000 characters. Replaying known IDs
does not invoke the model. New assertions have not passed a separate semantic
audit, and candidate selection currently scans the in-memory claims. This is an
incremental experiment, not a live capture daemon or production permission layer.

## Verification

```sh
python -m unittest discover -s experiments/knowledge -v
```

The agreed interfaces are conversation input → sourced knowledge and question
→ cited recall. Tests exercise unsupported quotes, temporal correction, project
separation, duplicate cycles, assistant attribution and idempotent replay.
External model responses are substituted only at the provider seam. Live replay
uses the pinned model and records its returned identity.

## Limits of this experiment

The offline compiler is deliberately separate from codeaf's production store.
It currently reconciles a whole semantic scope, not a bounded dirty neighborhood;
large-scope cost therefore grows with that scope. Its event ledger is a local
single-writer research artifact, not a distributed log or production synchronizer.
Historical source timestamps establish observation order, not the true effective
time of every fact. A corpus can contain synthetic tests and automated notices;
heuristic filtering and a model audit reduce this contamination without proving
complete removal. No claim is independently verified by repetition or by an
assistant saying it finished something.

See [the architecture and scheduling design](ARCHITECTURE.md) for the proposed
incremental system, cost model and designer opportunities.

## Understanding and cost experiments

See [the emergent learning workflow](LEARNING-WORKFLOW.md) for the repository
research, proposed checkpoint-based maintenance, generic understanding pass,
explicit unknowns and contradiction checks. Subjects are inferred from evidence;
no project or company name is hard-coded. `learning.learn` returns a separate
subject projection and reuses unchanged subject inputs.

A paired extraction experiment can be reproduced with an existing matching
historical corpus and provider cache. Keep its output private:

```sh
python experiments/knowledge/cost_probe.py \
  --corpus /absolute/private/path/corpus.json \
  --baseline-calls /absolute/private/path/calls \
  --out /absolute/private/path/cost-paired
```

The sample's cheaper extraction lost material coverage, so it has not replaced
the default. Cost reports no longer extrapolate development replay charges by
human-message count. Incremental updates send compact claim and note records,
without resending existing article bodies or original quote bodies. Narrowly
recognized operational read/skill envelopes remain archived without extraction;
tool writes, shell commands and unknown envelope shapes remain eligible.

```sh
python experiments/knowledge/learning.py \
  --vault /absolute/private/path/reconciled/vault.json \
  --out /absolute/private/path/understanding
```

This adds derived subject pages alongside the existing detailed notes and exports
an offline viewer. Repeat with the same input to reuse unchanged understanding. Discovery reruns when its bounded catalog changes; only unchanged per-subject explanations are reused.
Questions search other active extracted claims before remaining open; a candidate
answer is a source pointer awaiting review, not a newly accepted fact. This search
is bounded lexical retrieval, not proof that the full source history lacks an
answer. Discovery gives oversized groups one chance to split by their conceptual
structure; unsplit groups stay explicitly pending.

## Close knowledge gaps from existing logs

```sh
python experiments/knowledge/maintenance.py \
  --vault /absolute/private/path/reconciled/vault.json \
  --learning /absolute/private/path/understanding/learning.json \
  --out /absolute/private/path/maintenance \
  --questions 8 --context 'The current task or learning goal'
```

Repeated unchanged runs reuse attempts; `--advance` explicitly selects another
frontier. This searches existing sources and adds reviewed derived explanations,
with original citations, while preserving canonical claims. Partial answers stay
open. The model never runs external research or installs a schedule.

[The full architecture and inspiration ledger](FULL-KNOWLEDGE-BASE.md) distinguishes
implemented experiments from production work. [The optional working-agent delta
contract](WORKING-AGENT-PROMPT.md) explains how ordinary responses could propose
learning without a separate history-extraction completion. `proposals.collect`
validates and stages that envelope locally; it does not accept knowledge or alter
the installed binary's prompts/streaming.

`learner.learn_subject(vault, subject, model, previous=None, goal='',
max_steps=4, read_budget=18000)` offers a bounded read/search/write agent for a
selected subject. Pass the same pinned `provider.OpenRouter` adapter. Its reviewed
`sections` can replace that subject's sections before `learning.project`; retain
the returned receipt as `previous` for zero-call reuse. All input must already be
permitted by the host. Rich guides have section navigation in the inspector.
Neither source-count confidence nor longer articles are quality measures.

Bounded search can miss relevant lower-ranked evidence. `blocked_by_evidence`
means no accepted answer in the inspected attempt, including review rejection;
it is not proof the complete corpus lacks an answer. Guide caching rechecks read
sources, not newly available unread sources. Proposal collection currently stages
pending items only; production acceptance and bounded outbox retention are deferred.
