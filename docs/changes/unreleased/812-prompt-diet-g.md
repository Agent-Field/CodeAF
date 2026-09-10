---
kind: added
title: A small model gets a smaller prefix, derived from its own window and the crew's worker seat
pr: 812
surface: [chat, engine]
invalidates:
  - "The system page and the tool block used to be one shape for every model. There are now two, and which one a conversation sends is derived — never configured: under 32,000 tokens of window, or on the crew's `worker` model, it is lean. Nothing about a frontier conversation moved."
  - "`agentsFileLimit` was the one bound on the project's instruction file. It is still 8KiB on a full prefix and is 2KiB on a lean one, and a lean prefix quotes only the FIRST of AGENTS.md and CLAUDE.md that it finds."
  - "The capability shelf was one table, `capabilityGroups`. A lean belt shelves four more groups — `tasks` (propose_task, tasks), `watching` (watch), `memory` (track, commit, recall), `documents` (read_document) — and is HANDED the `questions` group at construction instead of being told to fetch it, so `ask` is carried and `load_capability` does not offer it."
  - "`Config.shelvesCapabilities` used to settle whether the page's shelved wording was rendered. That question is now asked per belt fact (`Config.shelvesFact`), because a lean shape shelves plenty and still carries `ask`."
  - "The memory reflex ran on every session with a store. It does not run on a lean one: `Config.Memory` is nil'd at construction, so there is no `<memory>` block, no `remember` verb and no reflex call, and the page says memory is off by itself."
---

Everything in front of a request is re-sent on every round of every turn. On a
128,000-token window the page and the tool block are a few percent of the room;
on a 16,000-token one they are most of it, and every instruction the model does
not need is one more thing for a small model to get wrong. So there are two
shapes of prefix now, and no new dial: the model's window (`ContextWindowFor`,
then `Config.ContextWindow`, then the default, with `TrustedWindowFor` over the
top) and the crew's open-weight `worker` row are the two facts that decide it.
`AFORGE_PROMPT_PROFILE=lean|full` pins it for a test or a bench cell, and an
unrecognised value is not a pin at all.

Lean drops two sections of the page by their `# ` heading — the standing section
and `# Interrupts and steering`, both of which are said again by the verb that
does the thing or the message that announces it — and keeps
`# How you spend the time` byte-identical with the worker's copy, because it is
the one section with ablation evidence behind it. `prefixbudget_test.go` now
weighs both arms: the full prefix is unchanged at 47,435 bytes and the lean one
is 34,343, against a diet target of 12,000 that the page and tool-block lanes of
this wave still have to reach. That number only ever ratchets down.
