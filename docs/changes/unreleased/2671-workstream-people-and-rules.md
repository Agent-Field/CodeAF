---
kind: added
title: an organization workstream can sit inside another and has a steward, people and rules
pr: 2671
surface: [factory, engine, docs]
invalidates:
  - "Organization workstreams were flat. One can now sit inside one top-level workstream (parent on create and update); nesting is one level, and a workstream inside another always has its parent's people: owner and shares on a child are refused 422."
  - "Go-ahead allowances shared one cap across every job they started. An allowance may now carry per_job_usd, which also holds each job it starts to that amount; a workstream's start_without_asking_usd is written as one such allowance, id start-without-asking, and 0 removes it."
  - "Anyone who could govern a workstream's shares was the only one who could change its people. Its steward (its maker, until handed on) now governs them too, and is the only one who may change its rules."
  - "A standing order belonged to no workstream. It may now carry an optional workstream scope (orders.change {workstream}); nothing else about how it runs changed."
---

Workstreams v5 lane W5 (SPEC §2.1). The workstream answers people (owner and
shares as one list, teams with their members now) and rules (default owner,
fallback, start without asking, keeps running), with new methods
factory.org_workstream_archive_keep and factory.org_workstream_fold and hello
words org_workstream_nesting and org_workstream_rules.
