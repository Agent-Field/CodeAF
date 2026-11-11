---
pr: 1899
kind: fixed
title: Reconcile factory verification for fresh and upgraded jobs
surface: [engine, chat]
invalidates:
  - "Was true: old unowned fixture and base-probe failures could become permanent obligations in later factory rounds. Is true: the job's canonical checkout boundary classifies these records as diagnostic at read time, including after their directories are deleted, without rewriting history. Unknown ownership stays conservative."
  - "Was true: passing Go tests with corrected build parallelism or broader package coverage left earlier failures unresolved. Is true: supported invocations record a supersession only when a later measured pass covers the same execution target and requested tests on current contents; subset, race, tags, stale and unknown evidence stay distinct."
---

The same reconciliation applies to resumed conversations and inherited finished
rounds. Diagnostic and superseded records remain auditable with their original
exits and provenance. A failed-only checkpoint now carries failures into the next
round rather than forgetting them because there was no passing check to reuse.
Historical passes invalidated by an edit retain their required scope as stale
obligations, so a new round cannot pass by forgetting all prior checks.
Ordinary Test/Proof completion reports use the same in-conversation correction
door as PR repairs, so a false completion can be corrected without consuming
another round. The guard explains that it reads evidence and does not run tests.

Acceptance drives the real factory stage maker, model/tool conversation,
receipt observer and completion guard with deterministic checks. It covers a
fresh failed Test followed by a covering pass, a sanitized 41-record replay of
an upgraded job (22 foreign fixture/base failures), rejection of a current real
failure, and a resumed history that recovers without replay and rejects stale
results after a source edit. No live job state is mutated by these fixtures.
