---
kind: added
title: An organization workstream's parts live on its server, each with its own rev
pr: 2639
surface: [factory]
invalidates:
  - "An organization's workstreams are one JSON file, factory-workstreams.json, rewritten whole on every save."
  - "Two people editing two different pages of one organization workstream refuse each other with 412."
  - "An organization workstream's plan, wakes, suggestions, jobs, unarchive and spend exist only on the laptop."
  - "Dropping a decision from an organization workstream deletes it."
---

The organization server keeps its workstreams in SQLite, `factory-workstreams.db`, and copies the old `factory-workstreams.json` in once on first use, renaming it `factory-workstreams.json.migrated`. A save writes only the rows it changed.

Each part has its own route and its own rev: pages are added, renamed, moved and deleted one at a time under the page's rev (their words are a live document, not these routes); versions of a page are saved by clients and restored as a new version by whoever restored it; the plan, wakes and suggestions keep the desk's objects with the server's authors; a factory job belongs to at most one workstream by membership, which now decides who can see and change it; unarchive works; a workstream carries a budget and the spend posted against it. A decision a change leaves out is kept as superseded. `workstream.changed` says which part changed, which one, who changed it and its rev. The engine forwards each route as a `factory.org_workstream_<part>` method and says `org_workstream_parts` in hello.
