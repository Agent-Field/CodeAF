---
kind: added
title: Preview adding removing and reordering factory chat steps
pr: 1875
surface: [engine, remote]
invalidates:
  - "The source-preview change entry 0-factory-pipeline-edit-source-preview.md said there was no add, remove or reorder; factory.pipeline_edit now accepts add_step, remove_step and move_step for chat rows in an existing pipeline and reports order, counts and changed gate run-ups."
---

Structural previews lock fixed, non-chat and proof rows and refuse movable chat work after proof in the pipelines they structurally touch. A combined field edit cannot leave such a pipeline without its existing proof. This is preview policy: existing field-only edits and the separate pipeline-file pull-request route keep their behavior. Loop groups and the native structural editor are not included.
