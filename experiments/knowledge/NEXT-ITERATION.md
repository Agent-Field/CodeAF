# Experimental knowledge workflow after the working baseline

The approved baseline is `a7f167c`, permanently tagged
`knowledge-base-working-2026-10-25`. This iteration implements the five follow-up
experiments in local Python modules. It does not install response hooks into the
binary, modify the desktop, start a daemon or deploy a service.

## What now runs

| Area | Implementation | Observable limit |
|---|---|---|
| Evidence and time | `evidence.record` takes authenticated host provenance: user statement, agent synthesis, repository snapshot, document fetch, command result or legacy unknown. Records content hash, locator/revision, recorded-at and valid-at separately. Projection accepts an explicit freshness horizon. | No URL fetching or independent factual verification is implied. Legacy logs retain coarse provenance. Age is a review signal, never an automatic falsehood. |
| Stable topics | `topics.reconcile` retains IDs independently of labels; authenticated host decisions rename, merge, split and undo. Redirects and history survive. `topics.publish` creates scope reading maps. | Decisions must be authenticated by the adapter. Split children require disjoint complete note membership; cross-child prose is withheld. Undo is last-in-first-out. No model can self-authorize a structural edit. A redo uses a fresh decision ID; undone IDs remain in the idempotency ledger. |
| Useful curiosity | `curiosity.curate` retains usefulness decisions, evaluates changed/new questions against retained selections, and parks the rest without deleting history. Closing a question needs no model call; larger catalogs use a diverse bounded shortlist with explicit uninspected counts. New-source vocabulary matches create wakeups; maintenance includes waking evidence even below normal top-k. | Lexical wakeups can miss paraphrases or wake irrelevant gaps. Selection/review is fallible. A parked question is not proven unimportant. |
| Push capture | `Workspace.push` persists source events and optional proposals in a host owner/project partition. `flush` combines ready chats into one normal `update_once` request. A process lock serializes compilers; SQLite publishes source status and vault atomically. | Exactly-once local publication does not guarantee exactly-once provider charging after a crash. This is a local filesystem/SQLite implementation. |
| Quality checks | Frozen synthetic source-support contracts cover corrections, proposals, copied reports, scoping, negation and abstention. Reproducible sampled omission audits stage candidates; `revisit` recompiles original immutable sources. | Six contracts are not a general accuracy benchmark. Omission candidates need review; partial source windows and bounded captured-claim context cannot measure full capture recall. |

Internal host partition keys are removed from model-visible source/proposal
context. They must not become semantic subject labels. They remain attached to
persisted source records for host isolation. Source IDs and semantic scopes do not
authenticate access.

## When model work happens

1. Each committed source push and optional-proposal validation costs **zero model
   calls**. Optional sidecars can come from the ordinary agent completion; the
   experiment does not install that binary prompt or claim their extra tokens are
   free. Source/proposal lookups use referenced IDs rather than rereading the whole
   archive on every push.
2. Capture waits for 30 seconds of quiet across pending chats in the same host
   partition, or 120 seconds maximum age, or the context cap. One request processes
   at most 24,000 source characters and 64 events. `--force` explicitly bypasses
   waiting. Remaining events stay pending. Unrelated partitions never share a call. Reconciliation sends at most 24 candidate claims (8,000 JSON characters), eight compact note records (4,000 characters) and 1,500 characters of relevant alias pairs. Selection combines same-chat antecedents with lexical rarity. Full memberships remain local and are patched by addition, never truncated to fit the prompt.
3. Discovery synthesizes newly uncovered notes. Known subjects gaining claims keep
   their prose and receive an editorial-update marker. Curation inspects at most 256 questions / 80,000 JSON characters in a request; normally only changed questions and retained choices are sent. Closed questions do not trigger global re-curation. Selected gap
   closure is separately bounded; wakeup ID lists in planning context are capped. Guide learners remain explicit upgrades, not
   per-turn full-vault rewrites.
4. The complete refinement runner fingerprints its inputs and topic registry.
   Unchanged runs make zero model calls, including after questions were answered.
   `--advance`, new source evidence, changed task context or an explicit revisit
   can request further work. Output directories are bound to a host partition and
   refinement/export is serialized.
5. Random omission audits inspect at most sixteen source windows and a bounded
   claim context. They do not publish facts. Audited source IDs can be explicitly
   revisited through the normal compiler, retaining original citations and history. Long sources can use an exact host-supplied window while the full original source remains archived. Revisit adds omitted knowledge; it does not automatically retract earlier captured claims.

The new CLI runners use an 8,000-token output cap with reasoning disabled for these bounded experiments. The provider includes nondefault output caps in cache identities and retains failed-response usage. Historical bulk extraction defaults are unchanged.

Default timing bounds are experiment controls, not tuned production guarantees.
No OpenRouter Batch API is used. All live model roles remain pinned to
`deepseek/deepseek-v4.1-flash`, with recorded model verification and usage receipts.

## Run it

Keep inputs and generated data outside the repository. The caller authenticates
the owner/project values; these CLI flags do not implement a login system.

```sh
python experiments/knowledge/workspace.py seed \
  --directory /private/workspace --owner OWNER --project COLLECTION \
  --vault /private/canonical/vault.json

python experiments/knowledge/workspace.py push \
  --directory /private/workspace --owner OWNER --project COLLECTION \
  --source /private/source.json --kind agent_synthesis \
  --recorded-at 2026-10-25T16:00:00Z

python experiments/knowledge/workflow.py \
  --workspace /private/workspace --owner OWNER --project COLLECTION \
  --learning /private/learning.json --out /private/refined --questions 4

python experiments/knowledge/evaluation.py suite --out /private/heldout
python experiments/knowledge/evaluation.py audit --out /private/audit \
  --vault /private/refined/knowledge/vault.json --sample 8
```

A source JSON has `id`, `session`, `role`, `ts` and `text`. Repository snapshots
also require a host-recorded locator and exact revision. Pushes are immutable:
retry the same event/provenance, not a changed payload under its old ID. Pending
failures remain retryable. `workspace.py revisit --source-id ID` explicitly queues
an omission repair. For a long original source, add `--window-start OFFSET --window-length LENGTH` (one source ID, at most 24,000 characters). On a scheduled invocation, `workflow.py` flushes only due
capture work; the host still owns the actual timer or event loop.

Topic review uses `topics.reconcile(learning, registry, decisions=[...])` and
`topics.publish`. A decision contains a unique ID, an authenticated `reviewed_by`,
an operation and targets. Supported operations are rename, merge, split and undo;
model suggestions must be reviewed before reaching this interface. The viewer
resolves merged IDs and presents choices for split IDs without guessing a target.

## Cost accounting and remaining work

Report actual usage by role: successful calls, input/output/reasoning tokens,
provider-reported cost, retries/failures and cache reuse. Separate development
experiments, failed comparisons, audits and recall from steady-state capture.
Do not extrapolate a development cache by human-turn count. A production estimate
requires observed update-context sizes, chats/tasks per owner, queue frequency,
proposal omissions, repair rates and guide-upgrade demand.

The workspace currently publishes a complete local JSON vault in one SQLite transaction. Model context stays bounded, but local serialization/I/O grows with vault size; `publication_bytes` exposes that cost. Normalized incremental publication and long-duration throughput remain production work.

Production adapters still need authenticated event provenance, lifecycle hooks,
streaming commit boundaries, retention policies, crash/load tests across the real
host environment, privacy controls and a larger human-scored longitudinal corpus.
No performance improvement in the running TUI is claimed by this experiment.
