---
kind: fixed
title: Starting a factory job checks its issue is still open on GitHub
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: the poll lists open issues and pull requests only, so an issue closed after it arrived stayed in Backlog, and a person's start ran it. Is true now: start_now, run and run_again on a GitHub job read that one issue or pull request first, in one request. Closed or merged there, the job is done (`done_how` `closed` or `merged`), the record says so (kind `source_closed`, verb `closed` or `merged`, actor the factory), and the start is `409 #12 was closed on GitHub, so it wasn't started.` GitHub unreachable, or no token, starts the job anyway."
  - "Was true: the single-issue read (`forge.Issue`) dropped GitHub's `state`. Is true now: it carries it; the list rows still leave it empty."
---

The check is item by item, at the moment a person chooses the job: the poll's
own reads are unchanged, and a start never waits on GitHub being up.
