# Memory, context and ongoing work: current implementation and next design

2026-09-10. Read-only review of local dev at `893d66067` (#769), with the
organization draft at `c63e03b7f` inspected separately. Findings are source review,
not fresh behavioral test results. Recommendations below are proposals, not new
product decisions. No runtime changes or full test suites were run. Future full
acceptance remains on Spark under the owner's fleet instruction.

## What dev actually does

Memory is on by default and stored in the person's SQLite `graph.db`. It is not
the old flat memory.md. Before a substantive turn, SQL combines lexical matches,
past model-judged usefulness and recency to shortlist eight titles. A small model
chooses which memories to inject, within a 4,800-character budget. Recall failures
fail open; answering can proceed without selected memories.

After a turn, a background model reads the user/assistant exchange, proposes at
most one durable memory, and judges which injected lines helped. A second decision
compares that candidate with up to three lexical neighbours and adds, updates,
supersedes or skips it. This is model judgment, not measured proof of usefulness.
An idle tidy pass also revises/consolidates old memories with bounded operations.
Explicit remember paths exist, as do editing, forgetting and restoration.

The memory record contains type, scope label, title/text/tags, status, ranking
counters and write/source metadata. Types include fact, preference, decision,
correction and project_state. SourceSession and SourceSeq identify the writing
conversation and memory-write event; SourceSeq is not the source message anchor.
This is partial provenance, not an exact quotation-to-consumed-revision trace.

Critical mismatch: scope is only `user`, `project` or `env`, with no project or
folder reference on the memory. The default database is shared across projects.
MemoryCandidates filters active status, not an applicable project. Neighbour
search for deduplication is also not restricted to a project identity. The router
sees scope labels, but the injected body carries title/text/age, not those labels.
Thus 'project' does not implement the folder applicability contract we discussed;
cross-project recall or consolidation cannot be ruled out by these structures.

Task workers get a memory selection against their brief, without becoming memory
writers. Dev #769 separately gives workers, nested workers, forked hands and the
task checker a read-only conversation-history interface. search_conversations can
search across saved conversations, search/browse one, and open indexed messages
through opaque references with neighbours. It is lexical and index-bounded.
Reading another conversation is not active cross-conversation coordination.

Standing already implements at/every/file/idle/probe/hold. Hold carries an enduring
instruction and never wakes. Other kinds can check conditions and run actions;
a task action uses an agent session and can use existing task machinery. Original
words, origin, grants, limits and activity already have representations. Scope
is conversation/physical workspace/machine, not the logical folder membership DAG.

Code evidence: dev internal/session/memory.go, internal/store/memory.go,
internal/reflex/prompts.go, internal/session/memory_consolidate.go,
internal/session/tools_conversations.go, internal/session/task_run.go,
internal/standing/standing.go and internal/session/standing_run.go.

## How the organization draft differs

The unmerged draft adds explicit collection membership and sourced, revisioned
shared_context with explicit targets. The session integration reaches direct
members, not arbitrary descendants or semantically related work. These records
are informational, not automatic authority. It does not replace learned memory
or finish the accepted-direction contract. Do not describe the draft as dev.

## Proposed minimal engineering work

1. Retention and applicability together: define learned information versus
   accepted instruction, exact source anchors, explicit target identity, scope
   changes, correction and supersession. Avoid letting memory, held rules and
   shared context independently reinterpret the same accepted instruction. Whether
   storage converges is downstream of this contract; no new primitive is needed.
2. One context assembly policy across chat, task and unattended execution. First
   determine what applies; then rank optional relevant information and open source
   history on demand. Applicable requirements must not depend on a top-eight
   relevance shortlist. Record the revisions actually supplied where traceability
   is required. Folder inheritance and multiple-parent conflicts remain grooming.
3. Connect existing ongoing execution to that policy: where work lives, what wakes
   it, the authorized action, repeated-event handling, continuation, pause and
   completion evidence. Add missing event adapters when a real use case needs
   them; do not invent separate schedule, trigger or agent ownership objects.

The model can propose useful information, suggest scope, interpret language and
rank relevant sources. Software must preserve sources/revisions, enforce selected
scope and grants, and schedule/record execution. Reliable accepted instruction
retention cannot depend only on a silent best-effort post-turn extraction call.

Keep SQLite, journals, conversation search, task execution and the standing
scheduler. Refactor ambiguous scope and competing instruction write paths; do
not discard existing memories or standing items. No evidence here calls for a
graph database or a wholesale restart. Retrieval quality still needs behavioral
evaluation after applicability is correct; adding embeddings alone cannot fix it.

Next discussion: use one sentence, 'For this folder's work, run tests before
calling it done,' to settle its source, applicability, inheritance/exception and
enforcement across a chat, delegated task and later unattended run. This should
decide the shared contract before choosing a migration or implementation split.
