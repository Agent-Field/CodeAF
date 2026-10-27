# Native knowledge architecture

This contributor reference describes the chat application's profile knowledge
module. Product behavior belongs in the [Memory manual](../internal/manual/chat/places.md#knowledge-from-saved-chats-inside-memory).

## Ownership and entry points

| Responsibility | Implementation owner |
| --- | --- |
| Start, resume, and stop background work | `cmd/codeaf/chatv3_knowledge.go`, `internal/knowledge/build.go` |
| Discover and read saved history | `internal/session/knowledge_history.go` |
| Admit immutable sources and publish readable pages | `internal/knowledge/store.go` |
| Compile evidence into maintained concept guides | `internal/knowledge/compiler.go` |
| Reuse validated evidence checks across updates | `internal/knowledge/compiler_questions.go`, `internal/knowledge/compiler_provider.go` |
| Integrate reviewed additions into maintained guides | `internal/knowledge/compiler_answer_integration.go`, `internal/knowledge/compiler_integration_merge.go` |
| Preserve edits and deletion intent | `internal/knowledge/edit.go` |
| Agent knowledge tools and local recall | `internal/session/knowledge_tools.go`, `internal/knowledge/compiler_recall.go` |
| Memory rendering, navigation, and editor | `internal/tui3/knowledge*.go` |

`knowledge.Open(profile)` returns a lightweight `Vault` handle. The vault owns
`knowledge/knowledge.sqlite` beneath the resolved profile. Callers use vault
operations rather than reading compiler state or writing database tables directly.
The chat and resident surfaces have separate lifecycles.

## Data flow

```text
saved history and new conversation events
  → immutable source admission
  → evidence extraction and concept organization
  → reviewed guides
  → atomic publication
  → Memory, indexed search, and agent recall
```

The initial build organizes a finite admitted history snapshot. Later evidence
updates affected guides or creates distinct concepts. Source eligibility and
concept organization are separate responsibilities. Saved task reports can supply
evidence; internal control traffic and deleted history remain excluded.

## Storage authority

| Stored data | Authority |
| --- | --- |
| Source events and import cursors | Admitted text, provenance, and resumable intake |
| Compiler checkpoints and validated receipts | Recoverable background compilation |
| Effective pages and full-text index | One published view for readers and search |
| Human overrides | Revision-checked edits and durable deletion intent |
| Build state | Background intent, stage progress, and recorded usage |

Generated prose does not replace source evidence. Page identity is independent of
its current title. Concept guides, detailed evidence notes, and topic maps have
different reader roles. A visible guide count and the total stored page count need
not be equal.

## Public module contracts

`Capture` admits local source text without a model call. `StartBuild` records
background intent. The process owns and joins `Run` through its context. `Flush`
runs compilation outside the UI loop. `Snapshot`, `Read`, and `Search` expose
published content. `EditPage` and `DeletePage` require the expected page revision.
`Revisit` operates on explicitly named immutable sources. Use the owning APIs for
optional intake rather than bypassing their concurrency checks.

Memory and agent recall consume the same effective pages. An edit must remain
human-authored across future builds. A deletion must remain suppressed. An older
editor revision must not overwrite a newer change. Generated additions to an
edited guide remain reviewable through its source evidence.

## Concurrency and failure boundaries

The worker and compiler have separate per-profile ownership locks. Provider work
runs outside SQLite write transactions. Independent guide work can run
concurrently; shared extraction decisions and visible publication remain ordered.
A publication transaction replaces pages, search entries, and the visible revision
together, after overlaying the latest human changes.

Interrupted work retains admitted sources and completed checkpoints. Invalid
provider output cannot become a valid cached result or a partial visible
publication. Every role uses the selected knowledge model; a fallback is refused.
Keep credentials and source content out of visible errors.

Incremental checks reuse validated results only for a matching complete request.
Changed evidence still reaches the compiler. Background provider requests retain
cache affinity within a profile, model, and compiler role. These optimizations
remain inside the vault; callers do not manage cache identity or invalidate
compiler checkpoints themselves.

Reviewed additions are organized against the existing guide before publication.
A size-limited update merges overlapping explanations with separate support and
meaning-coverage checks. Still-supported paragraphs from the previous publication
remain mandatory coverage inputs; refreshed evidence metadata preserves current
conflicts. Unchanged core context reuses maintained prose only while all original
support remains valid. Changed context is reviewed again. Useful additions are woven into their existing sections with support and meaning
review, including when they already fit the reading target. Only affected
sections are rewritten when the remaining guide fits.
The word limit is a target for maintenance: after a bounded repair, a reviewed
merge may retain up to its input word count to preserve useful meaning. Section
and paragraph limits still apply. Oversized integration inputs stop before integration requests; bounded selection
may already have incurred model charges. Failed updates keep the previous publication visible.
Projection policy upgrades can repair existing guides without importing history
again; the background worker owns that work through the normal vault lifecycle.

## Reader responsiveness

The UI performs database work through asynchronous commands. Search uses the
published full-text index. Late filter results are discarded. Unchanged publication
revisions reuse reader content. Rendering, browsing, and local search do not call a
model. Import and source-processing progress describe their own completed items,
not an overall completion estimate.

## Change and verify the owning boundary

Keep database representation, compiler checkpoints, and prompt composition inside
`internal/knowledge`. History eligibility changes belong with the session history
reader. User-facing changes also update the shipped Memory manual. Test through
capture, compilation, published pages, edit conflicts, and the actual terminal UI.

Ordinary tests use a provider fixture and require no credential. The opt-in
`TestKnowledgeLiveNativeCompiler` checks the actual recorded model through the live
provider. Run the repository's required checks before merging. Recheck cancellation,
source citations, human edits, deletion intent, and search consistency when changing
publication or concurrency.
