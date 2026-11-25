---
kind: added
title: Fluid apps keep the person's records in one data store, read and written through data.* methods
pr: 2933
surface: [engine]
invalidates:
  - "Was true: a fluid app had files but nowhere to keep its records, and nothing could check an app's call against what it declared. Is true now: internal/apps/data keeps every record of every type in one SQLite file per person at <codeaf home>/data/data.db (WAL, one writer goroutine). Types merge from app.json (an app may use another app's type and add fields); a field an app stops declaring is hidden and its values kept; deleting an app keeps its records. Writes are validated per field type with defaults, required, unique, masked (shown as •••• until a person reveals it; every reveal logged), computed (cel-go), ai values that never overwrite a person's, and freeze rules. Every change is history under a batch, and data.undo reverts a batch unless its records changed since. QueryField tokens compile to parameterised SQL with keyset pages and StateMeter counts; data.subscribe sends data.changed row diffs; CSV import runs from the Data place only, its dry run being the import rolled back. Every call with an app is checked against that app's capabilities first: not_allowed with detail.need."
  - "Was true: a record type belonged to the app that made it, data_json was keyed by field name, \"me\" was a literal, and a live query that dropped had to refetch everything. Is true now: types and fields have stable ULIDs (data is stored by field id; a rename keeps every value and the old name keeps working), created_by_app is provenance only, the person is one ULID shared with drafts, each field remembers who wrote it (person, ai, flow, import) and an AI write leaves a person's value or clear alone until the person resets it, every record has a _rev that refuses stale edits, every write lands an outbox event in its own transaction (Outbox/Claim with unique receipts), and data.subscribe returns a cursor to resume from or answers data.reset."
  - "Was true: a record's stage was a field like any other. Is true now: a type with a lifecycle moves its state only by data.transition (state, CEL guard with signed() and open_work(), writer kinds, revision), recorded in history with its reason and its effects handed to the outbox; work items, timers (FireDue, recurring, catch-up), sign-offs bound to a version hash, and comments are tables with delta events and a per-record activity feed; unique fields are enforced (case-insensitive for text); sequences number records (INV-0042); a ledger type is append-only and changes only by data.reverse; data.query takes where/select with one-hop relate and data.aggregate computes count/sum/avg/min/max on the server, money in whole cents."
  - "Was true: SIZE-BUDGET was 79,191,048 bytes. Is true now: it is 84,226,904 bytes, raised by the 5,035,856 bytes cel-go adds on darwin/amd64 (PERF.md)."
---

The engine-side Go API other lanes call is `data.NewPlace(home, Options)` with
`Main`, `Store(draft)`, `SeedSample`, `CloseDraft` and `OnCommit`, and on a store
`RegisterApp`, `ForgetApp`, `TypeSet`, `Match`, the record methods, `Outbox`,
`Claim`, `Cursor`, `TrimOutbox`, `Transition`, `Transitions`, `Reverse`,
`FireDue`, `QueryRows` and `Aggregate`, and the work item, timer, sign-off and
comment methods.
