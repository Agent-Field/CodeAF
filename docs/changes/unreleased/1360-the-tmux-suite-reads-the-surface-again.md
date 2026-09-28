---
kind: fixed
title: the tmux suite reads the surface where the waves that moved it put it
pr: 1360
surface: [chat, build]
invalidates:
  - "TestTUIE2E was reported as the one test that drives the real binary against a real model, and a green run of it as evidence that the surface still behaves. Six of its eighteen subtests had been red on the trunk for weeks, all of them waiting for a home or a tasks page that a named wave had deliberately changed, so the suite was reporting the drift of its own needles rather than anything about the product."
  - "The suite read a home panel by taking a fraction of the terminal's width — `panelColumn(screen, heading, tuiPlain/2)` for the left column. A panel's column stopped being a fact about the panel in #1046: one with rows in it stands in the field, an empty one stands in the rail, and `projects` and `spend` are pinned to the top of that rail whatever they hold. The helper is `panelBlock` now and takes no edge, finding both bounds from the heading's own position on its own row."
  - "`homeFootWord` was the whole of home's resting foot up to the key that leaves, and the suite joined it to `placeHintTail` to assert the sentence. #1046 put `ctrl+o open folder` between them for good, so the foot is three needles joined and `homeFootChordWord` is the new one."
  - "`homeHereWord` was a suffix of this window's own row on `where you were`, with the person's last words on the line under it. Both facts are one description clause beside that row since #1046 emptied the right margin of every field row for a time, so the suite reads `here · <what was last said>` as one string on one row."
  - "The `needs you` heading was matched as `needs you · `, leaning on the live count to tell the heading from the front of the gate's own `needs your ok …`. #1046 struck the count; the question is now asserted to stand inside that panel's own block."
  - "The refused-proposal subtest waited for `1 yes, set it up · 0 no · c change` to know a model turn had ended — the answer line a one-off reminder's card offers, copied out of `testAskHere` in #938 without its scenario. A proposal the tool refused has no answers to offer, so the wait could never be satisfied: it burned ninety seconds of every run and then failed in front of two assertions that were passing. It waits for the status line's `idle` instead."
  - "The task-room subtest pressed `Down` from a conversation group to reach the task under it. #905 made the tasks page a table with every family folded shut, so the press had nowhere to go and every assertion after it was reading the conversation's foot as though it were the task's. It presses `→`, which is the key that page's own foot names."
---

Nothing here is loosened to agree with the product. The foot is asserted as one
sentence with its three needles joined, so what is claimed is the order and that
nothing stands between them — which the untagged words gate cannot check, because
it only asks whether each clause is spelled somewhere in `internal/tui3`. The
`here` row is one clause carrying two facts, which a surface drawing either
without the other fails. The consent question has to stand in the rows of the
`needs you` panel's own columns, above the first blank row under it, rather than
merely somewhere later in the screen than the word.

`TaskOnTheRunEngine` is the seventh red subtest and is not read here.
