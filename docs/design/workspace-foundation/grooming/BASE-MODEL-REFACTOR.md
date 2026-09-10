# Proposed base-model refactor

2026-09-10. Proposal following the memory review, not approval to implement or
merge. The user clarified that draft PR #662 is the working baseline. Its live
GitHub head and local checkout both resolve to `c63e03b7f`. Dev `893d66067` was
also reviewed to identify available upstream changes, not to define what this
project already has.
The goal remains a personal environment for varied work, global discovery and
bounded autonomous collaboration, with familiar organization and no mandatory
roles, departments or new user-managed machinery.

## Correct implementation baseline

Our working draft already has:
- Collection records, typed references, ordered memberships and cycle prevention.
- The production collections tool to find, create, group and inspect work.
- Owner resolution for chats, tasks, ongoing items and files, including unavailable
  state, without copying execution ownership into the organization database.
- Shared-context records with immutable revisions, explicit targets, withdrawal,
  concurrent revision checks and bounded revision-pinned reads.
- Current-context refresh in chats and ordinary task workers, independent of learned
  memory. Readability is distinguished from current applicability, including after
  removal, retargeting, withdrawal and reopening a chat.
- Deterministic and recorded real-model journeys, plus known behavioral audit
  failures. This review does not claim fresh acceptance or all findings fixed.

These are implemented foundations, not items to build again. The gaps are their
semantics and reach: informational records are not accepted instructions, scope
is direct membership rather than descendants, organization is absent from some
execution paths, discovery/consultation/activation integration is unfinished,
and there is no finished folder dashboard. Read the draft's BACKEND, HANDOFF,
DESIGN-STATUS, change entry and actual tools/resolver when defining the delta.

## Target shape, using the existing vocabulary

Folder = name + ordered references to folders, chats, tasks, ongoing work or files.
Chat = ordered original conversation + current state + references to its tasks.
Context = current revision + retained revisions of sourced information/direction.
Task = requested outcome + acceptance + state + execution history/results.
Ongoing work = continuing request + activation conditions + authorized action,
limits, state and execution history. Files remain separately owned payloads.

These are refinements of the existing diagram, not six newly introduced objects.
Runs remain existing sessions/task executions; triggers and schedules are fields
on ongoing work, not independently owned product entities. Agents are executors,
not mandatory permanent owners of folders. Context remains attached by target
references rather than silently becoming a new allowed folder-member kind.

Folder membership remains a DAG, with shared references and valid unfiled chats.
It expresses navigation. Other typed relations express source, applicability,
task ownership, dependency, delegation and activation. An instruction explicitly
targeted at a folder may govern work according to the scope policy; merely filing
a chat containing an instruction does not adopt it everywhere that chat appears.

The existing artifact path reference cannot support exact historical file audit:
when that is needed, the source locator must include a content revision/hash or
retained snapshot, and still distinguish unavailable content from absent evidence.
Likewise, conversation provenance needs message/turn anchors, not just a chat ID.

## Refactors required, not just renames

1. Separate logical organization from execution location and instruction scope.
   world.Project currently groups by physical workspace, Config.Workspace is a
   tool root, and standing.Item.Workspace participates in grouping, applicability
   and execution. A folder should replace the organizing bucket, not those
   physical execution facts. A launch folder can include work on different hosts
   and paths. Tool roots stay explicit on execution configuration. Opening or
   moving a folder must not silently change paths, permissions or running work.

2. Converge persistent information/direction on a revisioned context contract.
   Extend the draft's context representation with a distinction between learned
   information and explicit accepted instruction, exact source anchors, applicable
   targets and scope policy, and correction/withdrawal history. Whether this is
   one table or several internal tables is secondary to one authoritative meaning.
   Memory becomes the learning/retrieval behavior over those records, not a second
   competing truth store. Preserve the difference between model inference, quoted
   historical speech and the person's current instruction. Explicit instructions
   need a durable acknowledged write path; optional inferred learning can remain
   asynchronous. This does not require another 'save' or confirmation gesture.

3. Remove the conceptual overload in standing. A held instruction becomes an
   instruction in that context contract. An item that actually wakes and performs
   work stays ongoing work. Reuse suitable scheduling/execution code underneath,
   while replacing physical-project applicability and folder-blind delivery.
   Do not implement a second scheduler just to change product vocabulary.

4. Make history and context access consistent across executors. Dev #769's new
   read-only history seam is an available upstream improvement, not already in
   draft #662. The draft still has the older conversation search. Root construction
   on newer dev also commonly gets history via Config.Memory. Turning off inferred
   learning should not disable original history,
   folder organization or explicit instructions. Chat, workers, checkers and
   unattended runs need the same applicability rules. Record which context revisions
   were supplied; do not claim this reveals a model's internal reasoning or proves
   that every supplied statement caused its action.

5. Connect work, not merely documents. Addressing another chat/task, delivering a
   finding, resuming work once, surviving offline recipients and avoiding message
   loops require explicit runtime delivery behavior. Search results and similarity
   edges alone do not provide these guarantees. Extend existing task/session
   delivery rather than inventing a social organization layer.

## Retrieval: indexed search plus bounded model exploration

RAG describes supplying retrieved evidence to generation; it is not an alternative
to agent tool use and does not itself require embeddings. Proposed read path:

    current request + live task state
        -> resolve applicable current instructions
        -> retrieve optional evidence using exact references, links and search
        -> model reads sources and follows useful leads within a budget
        -> answer or act with inspectable sources

Required instructions are selected by applicability, never by semantic similarity
or a small top-k shortlist. Optional evidence can be discovered globally within
the granted read access, with local relevance as a ranking signal, not a hard wall.
If the applicable instruction set is too large or contradictory, resolve that
explicitly rather than silently dropping requirements to fit a token budget.

Use the current lexical index as the baseline and add an embedding candidate
retriever in the retrieval slice for comparison on paraphrases and cross-folder
discovery. Merge/deduplicate candidates, then open original messages or current
record revisions. Let the model reformulate searches or expand neighbours when
needed; do not make each turn serially explore thousands of chats from scratch.
Embeddings should index source-linked exchanges/document sections and context
revisions, not only lossy memory summaries or one vector for an entire long chat.
Keep timestamps, speakers and source boundaries. Index updates/withdrawals must
follow source versions, with stale results revalidated on read.

Store indexes as rebuildable derivatives, carrying source/revision and embedding
model version. A failed or outdated index must not become the authoritative view
of active instructions or task status. No vector-server choice is needed yet.
SQLite and existing file/journal owners remain the proposed initial persistence;
no observed query or concurrency bottleneck here justifies a database migration.

Primary technical references, supporting the available mechanisms rather than a
claim that they have been evaluated on aforge:
- https://www.anthropic.com/engineering/contextual-retrieval explains complementary
  lexical/embedding retrieval, context-preserving chunks and reranking.
- https://www.sqlite.org/lang_with.html documents recursive hierarchy/graph queries.
- https://www.sqlite.org/fts5.html documents the existing lexical search mechanism.

## Transition and acceptance

Build in two coherent slices, each with a user-observable journey:

A. Refine the existing folder/context foundation: add the agreed context
   semantics and source references, separate execution location, and use one
   resolver in interactive and delegated work. Preserve originals and stable
   references. Migrate old memory as historical learned information; never infer
   a precise folder or user authorization from the old `project` label. Preserve
   old standing scope/grants through compatibility until an exact mapping exists.
   Avoid dual authoritative writes during transition.

B. Retrieval/continuation: compare lexical, hybrid and bounded model-search paths;
   connect ongoing work and consultation to the same context contract; retain
   activation identity, context revisions and results in existing execution records.
   Prove correction, withdrawal, delayed delivery, cancellation and duplicate-event
   handling as part of the real work loop, not merely a search benchmark.

Check source recall, mistaken cross-folder application, stale answers, instruction
loss, latency and cost on a large synthetic history with paraphrases, conflicting
facts, corrections and unfiled/shared chats. Full suites and acceptance run on
Spark against the exact candidate revision/snapshot, retaining job ID/results.
No such evaluation has run for this proposal, and no runtime change is made here.

The smallest remaining design choice is applicability: explicit folder/subtree
targets and exceptions when work appears in multiple folders. Decide this with
the distinction between where work is listed and what governs it intact. Then
write the concrete schema/API delta and A's end-to-end acceptance, before asking
an implementation task to build. Continue from #662; reconcile required newer
dev changes deliberately. Keep both existing PRs draft and unmerged.
