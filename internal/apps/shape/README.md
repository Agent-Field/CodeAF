# Private shape drafts

`New(cfg, all)` reloads every committed `<app>/drafts/<member>/draft.json`
under `cfg.Root`. Corrupt metadata remains an error; it is never treated as a
missing draft. With no configured root the skeleton doors still return
`not_built`. The Builder pipeline uses the persisted journal as its queue.

`shape.open` snapshots `Store.Definition` into `files/` and real records into
`overlay.db`. A second open preserves the request journal and refreshes the
records snapshot only before the first committed scratch write. SQLite's
online backup API copies committed WAL pages and supports generated columns;
no open database is copied as raw bytes. Three triggers per records table set
`_shape_scratch.dirty` in the row transaction. Reads and rolled-back writes do
not set it, and deleting a scratch row does not clear it.

`shape.get` returns `{draft: null}` before opening and after discarding.
`shape.discard` removes only the caller's draft folder. `shape.diff` returns
one stored change's full-text `FileDiff` list or the complete draft against its
saved base. `shape.change.cancel` removes the request and rebuilds files by
reapplying the remaining passed changes. Independent edits in the same file
are preserved; an edit that cannot be reapplied returns `conflict` and leaves
the journal unchanged.

## Pipeline helpers

These helpers stay inside package `shape`. Hold `s.mu`, call
`s.draftFor(scope)`, and use the returned `*draft`. Release the service lock
before invoking a model; reacquire it and look up the draft again afterwards.

- `AddChange(change bridge.Change, diffs []bridge.FileDiff) error` appends a
  request. Use `checking` and no diffs before writing/checking. Missing `N`
  and `At` are supplied; IDs must be unique.
- `SetChange(change bridge.Change, diffs []bridge.FileDiff) error` replaces the
  request/progress and diffs without reordering. Pass `d.Diffs[change.ID]`
  when changing only progress. Failed and question changes never join files.
- `ApplyFiles(changeID string, files spec.Files) error` accepts a complete
  checked definition, records additions/modifications/deletions relative to
  that change's preceding definition, marks it passed, and rebuilds files.
- `Files() (spec.Files, error)` returns detached definition bytes.
- `Rebase(newBase int, files spec.Files) error` saves the new base, retains
  requests, resets changes to checking with no old diffs, and refreshes a clean
  overlay. Replay uses `ApplyFiles` after checking each request again.

Each committed mutation publishes `ShapeEvent{kind: "draft", draft: ...}` on
`TopicShape(app, member)`, including discard with state `discarded`. Responses
and events are detached from the journal. `draft.json` embeds the wire `Draft`
and adds `base_files` and `diffs` indexed by change ID; the shared bridge types
are unchanged. Metadata uses a synced temporary file plus rename. It is the
commit point: restart rebuilds `files/` from the saved base and passed diffs.

Records and Shape share `records.DraftOverlayLock` while refreshing/removing
or using an overlay. Draft reads retain that lock until closing SQLite;
`mode=rw` prevents a racing discard from recreating a phantom scratch database.
Parsed record definitions without source files carry no alias annotations. The
connection supplies a private empty annotation document in that case; it does
not change the store's bundle or accept malformed source bytes.

# Shape Apply and version handoff

Apply reads the caller's committed draft journal and rebuilds its checked
files under the service lock. Only a ready draft
whose remaining changes passed Check can apply. The supplied base and stored
draft base must both match the current version.

The version store is the existing definition repository and its tags. The
small extension in `store/versions_shape.go` exports `CommitShape` and
`VersionRequests`; request boundaries live in commit metadata, including
multiline requests. There is no second versions registry.

The records hook in `records/migrate_shape.go` counts real SQLite values,
tests migration on a WAL-aware copy and holds the real writer lock through the
version commit and migration. Its connection views expose current fields and
aliases; their write triggers retain hidden values in backing tables.

The pipeline installs `Service.replayHook func(app string, newVersion int)`.
Apply calls it with the journal lock released after removing the applying
member's draft from disk and memory. Restore calls it
after publishing the new version. The hook is responsible for replaying other
members' requests.

Field renames use `alias` on the destination field, or explicit Builder
summaries such as `alias value → amount`. Until the shared spec schema exposes
that annotation, the pipeline must pass `store.ShapeValidationFiles(files)`
to Check, while persisting the original annotated files. This temporary
validation copy also declares old names for references in filters and runs.

Health uses Check with a real-record snapshot substituted for its sample
provider. The isolated checker has no Records, Runs or Hub, so it cannot
reseed content or start behaviour runs. Health is stored atomically in
`health.json` and published on the health topic. Records remain available;
the desk owns displaying the last good version read-only.

Persistent operations require a configured apps root. An unwired constructor
with an empty root retains the skeleton's unavailable response.

## Builder changes and replay

`shape.request` returns a checking change immediately. The ordered journal is
the queue: one worker per member draft sends definition files, the kit, synthetic
sample records, the request and the last Check report to the Builder. It streams
Plan, Write, Render and Behaviour steps, then passes the complete candidate to
Check. A failed report returns to the Builder for at most three rounds. The
last issue becomes `Check failed: …  round 3 of 3`; failed changes never join
files or reach Apply. `shape.change.fix` replaces the request with `Fix: <hint>`
and starts three fresh rounds. Plan edits become `<section> › <id>: <text>`.
Cancellation removes only that change and rejects an in-flight result.
The Shape prompt derives `agent.md` tools from the app's record capabilities:
readable types require `records.query` and `records.get`; writable types also
require `records.create`, `records.update` and `records.delete`. Missing tools
are repaired even when the requested change concerns another definition file.

Personal requests without migration lines apply as soon as Check passes. Queued
personal requests commit separately through the same migration/version door,
keeping unprocessed requests in the journal on the new base. A change with
migration lines stays ready for the preview and a person's Apply.

Apply calls the installed replay hook after releasing the draft lock. Every
other open draft rebases on the new files and re-runs its requests in order;
old model results cannot overwrite the new base. An all-pass replay publishes
`replayed` with the old/new versions, applying member and request count.
`shape.replay.dismiss` persists dismissal of that line. An incompatible request
becomes a question with concrete options and `Drop this request` last; its draft
blocks Apply and its member receives a `shape_question` approval titled
`CRM · Builder needs you` (using the app's name). `shape.answer` replaces the
request with the answer, rechecks it and replays later dependent requests; drop
removes only that change. Checking journals resume when the desk opens or reads
the draft after a restart.

`shape.ask` opens a `shape_ask` Inbox approval addressed to the Editor who applied
last. Its title is `@raj asks for a change to CRM “…”`. The approved payload has
`action: shape_this`, the app and the original request; the desk opens Shape with
those words pre-filled. The Editor sends the request from the composer.
