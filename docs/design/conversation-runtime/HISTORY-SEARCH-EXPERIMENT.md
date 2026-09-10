# Conversation search and source reads

This change gives foreground chat, task workers, nested workers, forked hands and
task checkers the same read-only access to indexed conversation history. It fixes
an access gap as well as a retrieval problem: workers previously inherited no
writable memory store, so their toolbelt omitted conversation search altogether.

## Observed failure

A local user asked for a recent sandbox-browser task. The saved exchange showed
four task lookups, followed by two conversation searches, a transcript read and
two filesystem calls. The search ranked the requesting conversation's own fresh
tool-call text first. The model eventually found the conversation, but discovering
its location required knowledge of the on-disk layout. We did not locate the exact
reported worker refusal; the missing worker tool was established from its actual
constructor and toolbelt instead.

The experiment below uses synthetic fixtures, not the user's private history.

## Retrieval design

`search_conversations` has three operations in one schema:

- `query` searches message text across the store with SQLite FTS5 BM25; optional
  `session_id` restricts it to one conversation.
- `session_id` alone browses recent messages in that conversation.
- `ref` alone opens the indexed message and up to two actual neighbours on each
  side. References are returned for both matches and neighbouring messages.

The existing index avoids another service, embedding model, file crawler or
fuzzy-search executable. Query words are safely quoted Unicode tokens, combined
with OR for candidate recall. BM25 ranks candidates and journal order breaks
ties. Match-centred excerpts preserve the relevant passage in a long message.
Surrounding messages expose nearby corrections; source references allow a larger
read when the excerpt is insufficient. Neither ranking nor a saved memory is
treated as proof of the answer.

Broad search excludes the requesting agent's own thread to reduce self-matches.
Explicit scope can still open it, and workers can search their parent's history.
The inherited interface provides reads only; it does not enable memory writes,
background extraction or indexing of worker traffic. Missing readers leave the
tool absent. Database errors remain distinguishable from no matches.

Search defaults to eight hits, caps at twenty, and accepts at most thirty-two
query words. Excerpts and neighbouring messages cap at 400 bytes. An explicit
read preserves the full indexed anchor, including line breaks, up to the existing
16 KiB message limit. Results distinguish full text from excerpts, state the
indexed-history scope, and include conversation IDs, message IDs, dates and stored
roles. References are stable locators, not authorization tokens or transcript
line numbers. Historical text is identified as evidence, not instructions.

## Live experiment

Run the same tests from a clean checkout with `OPENROUTER_API_KEY` available:

```sh
make build
go test -tags e2e -count=1 -timeout 15m -v -run '^TestConversationSearch' ./internal/e2e
```

The harness creates a disposable home and pins every text-model role, tier and
fallback to `deepseek/deepseek-v4-flash`. Foreground cases use the full chat
toolbelt; the worker case enters through `Agent.StartTask`, runs its normal task
loop and enables the task checker. These are live engine tests, not a claim that
the tmux terminal suite was run.

An initial successful run on 2026-09-10 passed in 161 seconds:

| Case | Assertion | History calls |
| --- | --- | --- |
| Correction in a long message | MAPLE-92 supersedes CEDAR-81 | 1 |
| Another project | Retrieves violet-kestrel-47 across project folders | 1 |
| Ambiguous task-or-chat request | Finds original conversation after task lookup | 1 |
| Open a supplied source reference | Reads the exchange and retains the correction | 1 |
| Missing phrase | Reports no indexed match without inventing an answer | 1 |
| Actual checked task | Writes exact phrase, stored role, conversation/message IDs and source reference to decision.json; finishes done | 4 |

The five foreground cases rejected repeated identical searches and filesystem
history detours. They shared memory state deliberately: a remembered answer must
not replace locating the requested original conversation. The task artifact was
validated against the fixture's exact source record, and the model ledger was
checked to contain only the requested model. That run's recorded cost was
$0.012838 for chat and $0.005882 for the task. This excludes earlier iterations.

## What the iterations revealed

Early reads clipped long anchors and caused repeated retrieval. Numeric read
arguments were mistaken for per-conversation positions; one sparse-history task
searched the filesystem and timed out. Opaque references, full indexed anchors,
explicit history boundaries and stored roles improved the contract. A shared-memory
case answered from memory without locating its source, prompting the source-first
tool guidance and task-miss hint. A checked task found the right evidence but
failed an ambiguous report-format requirement, so the final fixture uses a
precisely specified JSON artifact.

That task family still decoded and altered source references unnecessarily: after
searching and opening the real message, it tried earlier global IDs, including
zero. Those calls produced no fabricated evidence, and the final artifact was
correct, but this is a remaining tool-selection inefficiency. Opaque references
discourage arithmetic; they do not prevent a model from doing it. Task-family call
count was therefore reported rather than hidden behind an arbitrary three-call
correctness gate.

Repeating the experiment exposed an additional completion failure: a task wrote
the exact correct artifact, but its checker rejected the source metadata because
it could not establish which fields the person had requested. The captured
checker packet contained only the fallback acceptance, "Complete the brief and
report the result and checks run.", without the referenced brief. It tried to
reconstruct that missing request from the source conversation. Another run failed
the artifact assertion before these diagnostics were available, so its exact
cause remains unresolved. A subsequent diagnostic run passed with two history
calls; that variability is why the missing contract was fixed instead of simply
rerunning until green.

The checker now receives the admitted brief only when the current acceptance is
that exact fallback. Explicit acceptance criteria retain the existing contract-only
behavior. A regression test covers both cases. This fixes missing input to the
check rather than weakening artifact assertions or disabling the checker.

Two file-delivery repeats after that fix both recovered the exact source-backed
JSON. One passed; one was rejected because the checker confused the restored
workspace's temporary name with the worker's original root folder. Its own
reasoning explicitly confirmed all five source fields before rejecting the path
claim. That broader file-completion issue remains unresolved. The earlier
file-delivery scenario is reproducible from commit `ce81ac14f` using the live
command above; it predates the fallback-contract fix and can expose either failure.

The maintained task test now asks for the person's actual operation: return the
earlier approval phrase, stored role, conversation ID and source reference in the
task's final answer. It checks the worker's final message, not a tool result or
the checker's paraphrase, and still requires the task to finish done with checking
enabled. It does not claim coverage of JSON-file delivery or that the broader
completion issues have been solved.

A repeat of the direct-answer scenario also exposed a checker that confirmed the
answer but demanded a file despite the requested final response. The checker
packet now explicitly identifies the retained final response as a valid
deliverable when requested, while requiring evidence for its claims and retaining
file checks when a file is required or claimed. Worker and checker history calls
are logged separately: checker reads are useful independent checks, not worker
redundancy. Some checks rely on the supplied source receipts without issuing a
fresh lookup; the test requires worker retrieval, a grounded worker answer and
successful completion, and reports checker calls rather than mandating them.

The PR's first complete CI run also found a manual section-list failure on the
newer merge base. `TestEverySectionTheCutNamesComesBackWhole` fails identically
on clean `dev` commit `be1cc3749`: the tasks page's bounded heading list omits
"Back to main from a nested task — return to the conversation". This is separate
from conversation retrieval and was reproduced in a detached clean worktree.

## Limits and follow-up criteria

This is lexical message search. Titles, unindexed history, other machines' stores
and spilled-file contents outside the indexed stub are not newly searchable.
Semantic paraphrases with no shared words can miss. Search does not scan every
later message for a distant correction, so a model may need scoped follow-up
queries. The small live fixture set demonstrates the access path and selected
failure modes; it does not establish universal recall or non-regression in answer
quality.

Add fuzzy or semantic candidates only against a labelled retrieval benchmark
covering typos, paraphrases, false positives, distant corrections and large stores.
Compare recall, grounded-answer accuracy, latency, context size and total tool
calls. Regardless of candidate generation, answers should remain tied to original
messages through the same read contract.
