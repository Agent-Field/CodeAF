---
kind: added
title: Fluid apps keep the person's records in one data store, read and written through data.* methods
pr: 2933
surface: [engine]
invalidates:
  - "Was true: a fluid app had files but nowhere to keep its records, and nothing could check an app's call against what it declared. Is true now: internal/apps/data keeps every record of every type in one SQLite file per person at <codeaf home>/data/data.db (WAL, one writer goroutine). Types merge from app.json (an app may use another app's type and add fields); a field an app stops declaring is hidden and its values kept; deleting an app keeps its records. Writes are validated per field type with defaults, required, unique, masked (shown as •••• until a person reveals it; every reveal logged), computed (cel-go), ai values that never overwrite a person's, and freeze rules. Every change is history under a batch, and data.undo reverts a batch unless its records changed since. QueryField tokens compile to parameterised SQL with keyset pages and StateMeter counts; data.subscribe sends data.changed row diffs; CSV import runs from the Data place only, its dry run being the import rolled back. Every call with an app is checked against that app's capabilities first: not_allowed with detail.need."
  - "Was true: SIZE-BUDGET was 79,191,048 bytes. Is true now: it is 84,226,904 bytes, raised by the 5,035,856 bytes cel-go adds on darwin/amd64 (PERF.md)."
---

The engine-side Go API other lanes call is `data.NewPlace(home, Options)` with
`Main`, `Store(draft)`, `SeedSample`, `CloseDraft` and `OnCommit`, and on a store
`RegisterApp`, `ForgetApp`, `TypeSet`, `Match` and the record methods.
