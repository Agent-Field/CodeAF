---
kind: fixed
title: the sweep tells furrow to forget a swept session's forks
pr: 197
surface: [engine]
invalidates:
  - "#183's own *Left out* said the sweep removes a swept session's forks as directories but does not tell furrow to drop their records. It does now: `reapSession` reads the session's checkpoint and asks `taskTree.dropUniverse` — the door a landing already used — to forget every universe it names, before the folder goes."
  - "`furrow.Workspace.DropFork` returned nothing and its comment argued that no caller could do anything useful with a failure. It returns an error: a landing still ignores one, because its work is already in and the leftover costs a line in a listing, while the sweep writes the miss into the sweep log, because there the record is the only thing left pointing at a directory that has gone."
  - "A drop furrow refused was invisible. A miss reads `sweep: could not tell furrow to forget the fork <name> of <ground>: <why>` in `~/.aforge/v3/sweep.log`, and a drop that happened reads `sweep: told furrow to forget the fork <name> of <ground>` — the last moment anything on the machine knows that name."
  - "The fake furrow in `internal/session/groundladder_test.go` answered `fork-rm` by printing a drop and kept no records at all, and had no `forks` at all. It keeps one file per universe under `.furrow/forks/`, which `forks` lists and `fork-rm` removes, so a test can assert that a record WENT rather than that a drop was asked for."
---

A landing drops its own fork record on the way past (`taskTree.releaseLanded`),
so only a session killed mid-run left one behind — a line in `furrow forks`, in
the person's own project, describing a directory the reaper had already removed.
That is precisely the litter `reapSession` exists to prevent, and it was in
somebody else's project instead of ours.
