// Package records stores app records in SQLite, validates app.json field
// types and relations, evaluates CEL computed fields, and publishes committed
// row diffs. Each app has separate sample.db and records.db files; real data
// starts empty, and Seed only replaces sample data. Batch changes are atomic,
// unique fields have SQLite indexes, and real deletes retain before/after
// history. Referenced records cannot be deleted until their relations are
// removed. Freeze and blind rules belong to wave 2.
package records
