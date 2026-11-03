---
kind: added
title: Workstream pages support version reads, restorable deletion and title-based read defaults
pr: 2290
surface: [app]
invalidates:
  - page.delete used to destroy pages and their history; it now marks pages deleted and restorable while retaining saved versions.
  - page.create used to default read_by_jobs to false for every title; it now defaults to true for Spec and Decisions unless explicitly overridden.
---

`page.get` accepts a saved version and renders that version's document when
markdown is requested. Deleted pages remain readable, but leave the live tree
and refuse edits and history reads. `page.list` with `include_deleted: true`
adds deletion roots, newest first, including an empty list when none remain.

`page.restore` with only an id restores a deleted page and the descendants
removed by the same deletion, preserving saved versions, nesting and order.
It appends under its live old parent or at the root. Restoring an old version
of a live page still requires both version parameters and creates a new version.

Creation compares trimmed titles without regard to case when choosing Read by
jobs. Explicit choices win and renames keep the existing setting; the initial
Overview remains off. Deleted pages remain stored until their collection is
deleted.
