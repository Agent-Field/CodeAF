# Native knowledge architecture

This reference describes the chat application's native knowledge module. The
user workflow lives in the [Memory manual](../internal/manual/chat/places.md#knowledge-from-saved-chats-inside-memory).

## One profile database

`internal/knowledge.Vault` owns the database at `knowledge/knowledge.sqlite`
beneath the resolved profile. `Open(profile)` returns a lightweight handle;
operations open bounded SQLite connections. WAL permits readers while the
compiler makes short transactions. Write transactions begin immediately so
concurrent source admission cannot fail while upgrading a read lock.

| Stored data | Authority |
| --- | --- |
| Immutable source events and journal cursors | Original admitted text, provenance, pending work, and exactly where import resumes |
| Compiler publications and checkpoints | Extraction, learning, topic registry, validated model receipts, and recovery after interrupted composition |
| Effective pages and FTS | Reader, indexed search, citations, links, and one committed publication revision |
| Human overrides | Revision-checked Markdown edits and durable deletion tombstones, separate from generated prose |
| Build state and history errors | First-build intent, background progress, partial imports, retries, and recorded cost |

Compiler documents retain optional claim and learning metadata inside SQLite.
Per-page records and full-text indexes serve readers; a page lookup does not
load the compiler document. JSON files and experimental exports are not read
paths. Publication overlays the latest human changes, replaces effective pages
and search entries, and commits the visible revision in one transaction.

## Background ownership

`v3Process` starts and joins `Vault.Run` under its process context. A worker lock
admits one process per profile; another running process waits and takes ownership
when the OS releases that lock. A separate compiler lock prevents overlapping
manual flushes. These OS locks retain worker ownership during provider requests;
SQLite transactions end before network work begins.

A successful empty inventory arms future capture and shows the waiting state.
Existing history waits for **Build knowledge base**. The action records durable
intent and returns; it does not run models on the UI loop. Initial history drains
in bounded extraction batches to a durable source boundary before organization.
Newly arriving sources wait beyond that boundary, so ongoing chats cannot prevent
the initial collection from publishing. Future turns use the quiet/max-wait policy. Cancellation
leaves admitted sources and completed stages available for the next process.

`internal/session.KnowledgeHistory` owns saved-journal discovery and parsing.
It visits all project buckets without a recent-chat limit, includes archives and
legacy flat journals, resolves sealed layouts through `Place.Transcript`, and
includes saved task reports from conversation-owned, loose and adaptive journals,
and excludes deleted conversations/tasks and injected task briefs. Only complete records advance
cursors. Original messages use stable session/byte-offset IDs. Admission and
cursor advancement commit together; retries compare immutable source content.

## Compiler and evidence

The compiler preserves the original extraction, exact-quote validation,
claim deduplication, qualified assistant reports, human-only newer corrections,
contradictions, stable note identity, catalog-aware subject discovery, reviewed explanations,
question resolution, topic reconciliation, and canonical mention links.

Provider results enter through typed role-response checks. Claims and paragraphs
must reference admitted sources or known claims; quotes must occur in the supplied
context. Separate support and lasting-usefulness review gates generated explanations. Model receipts record the
actual returned model ID and reject substitutions. The configured knowledge model
applies to every role; credentials never enter receipts or visible errors.

The reader and editor share the same effective pages used by agent recall.
Edited prose is explicitly human-authored, with original source history kept
separate. Tombstones suppress generated pages across rebuilds. Stale editor
revisions cannot overwrite later edits. Search entries change in the same
transaction as visible page content.

## Responsiveness and bounds

Extraction admits at most 64 spans and 24,000 source characters per call. A
journal read handles at most 256 records or 64 eligible sources, with a 16 MiB
record bound. Discovery plans at most 32 evidence notes per request. All planning completes
before initial prose synthesis. Draft plans preserve evidence-note coverage; a
whole-tree review then consolidates new concepts without changing previously
published identities or protected human pages. Existing concepts are inspected
in windows of at most 128 before new pages are proposed, with exact IDs retained
for reuse. Concise concept headings and two-level folders come from the evidence,
not a fixed project taxonomy. Independent subject drafts and editorial refreshes
run with at most two provider jobs; claim updates and publication remain ordered.
Oversized
subjects retain a partition-needed state rather than truncating claims.

The UI performs database work through asynchronous commands. Search uses FTS,
late filter results are discarded, and unchanged publication revisions reuse
cached reader content. Rendering and navigation do not call the provider.
Provider responses and timeouts are bounded; retries use the same selected model.

## Verification

Tests cross saved-journal-to-readable/searchable-page, human-edit/rebuild, and
Knowledge-screen seams. They cover history from multiple projects, first-chat
population, notes/compaction exclusion, source support, correction authority,
review rejection, duplicate replay, and navigation while a build is requested.

`TestKnowledgeLiveNativeCompiler` is opt-in through `CODEAF_KNOWLEDGE_LIVE=1`.
It uses the existing OpenRouter credential, removes Python from the executable
search path, and checks the actual model recorded for every call. Ordinary tests
use a provider fixture and require no credential.
