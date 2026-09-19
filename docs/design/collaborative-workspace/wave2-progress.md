# Wave 2 progress — remediations (not verified)

**Integrate SHA:** `f2de6878de6ae54d60ff4f98d97cb9b8f766e416`  
**Branch:** `feat/collaborative-workspace-0918` (no merge to `dev`)  
**Updated:** 2026-09-19T04:45Z

Wave 1 remains the only owner-verified binary. Do not treat any later SHA as `ready.json`. Automatic continuation into Wave 3 is authorized only after `releases/wave-2/ready.json`.

## Wave 1 owner launch (copy-paste)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
```

Do not pause the next wave for that try.

## Reviews and affected on the integrate SHA

Independent reviews: **ok: false**. Organizer unbound (`v3Organizer` nil), journals never ingested, J11 TUI hashed the stored digest a second time, `ApplyActionPlan` was per-action txns, empty 0/0 index looked caught-up, manual overclaimed automatic filing.

`t-w2-affected` **failed** `internal/guard` lock-defer on `SetEmbedder`. Live waits on `t-w2-affected2`. Do not claim `t-wave2`.

## Coordinator remediations (`t-w2-fix-laws`)

Lock-defer `SetEmbedder`, embed `Client.Embed` ≤15 plus package complexity test, `workspace.ApplyBatch` one writer txn, `RemoveAndSuppress` one txn, TUI suppress uses stored evidence hash, empty index is `discovery delayed`, manual says enqueue until the organizer is bound.

## Parallel (`t-w2-fix-organize`)

Bind production `v3Organizer` and ingest journals into `discovery.db`. Then reintegrate, affected2, re-reviews, live J09–J18, immutable `releases/wave-2/<sha>/codeaf`, `ready.json`.
