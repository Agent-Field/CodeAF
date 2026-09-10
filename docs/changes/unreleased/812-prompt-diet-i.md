---
kind: added
title: the chat signs the git work it does for you, in the resident's own words and bytes
pr: 812
surface: [chat, engine, resident]
invalidates:
  - "The v3 chat did not attribute itself at all. prompts/system.md never mentioned attribution, the chat manual had no page about it, and the commit a task writes when its work lands carried no trailer. It does now, on the `attribution` row the settings registry has had all along (env `AFORGE_ATTRIBUTION`, default on): the model is told the law as one belt fact beside `bash`, and the landing commit gets the trailer appended mechanically with no model in the loop."
  - "The attribution wording lived only in internal/exec's `attributionPrompt`, ~1.1 KB of it, and was the resident's alone. It is now `exec.AttributionLaw` — 571 bytes, composed from the same `AttributionTrailer`, `AttributionSeparator` and `AttributionPullFooter` constants — and BOTH surfaces render it. `attributionPrompt` is that law plus the one line spelling the issue footer out; the chat's belt fact names `utm_medium=issue` instead, because 145 more bytes of URL would be bought on every tool round of every turn."
  - "A task node could not have honoured the row even if it had been asked to: it is handed no `ProfileDir`, so anything re-reading `attribution` inside a task would have read the default, which is on. `session.Config` now carries a resolved `Attribution bool`, filled at the one door every v3 session comes through (cmd/aforge's applyV3Governance, beside `task.audit`) and copied onto every task child."
  - "Commits written by the task system stay authored as `aforge <aforge@localhost>` and that has not changed — sibling landings read that identity to tell aforge's own forward progress from a person's intervening work. The trailer is provenance on top of it, and names the aforge GitHub account (`agentfield-bot@users.noreply.github.com`), not the local one."
---

Aforge commits, opens pull requests and files issues in somebody else's name, and
until now the half of the program a person actually sits in front of left no mark
saying so. The law is one wording for both surfaces because two paragraphs about
the same four bytes drift into two laws, and a trailer spelled two ways is
provenance nobody can count. It is conditional in the chat — absent with the row
off, absent in a fork's hand, whose `bash` is read-only and could not commit if
it wanted to — and it cost the fixed prefix 574 bytes, of which 207 are the
trailer and the footer themselves. The prefix is 47,332, which is 668 under a
budget nothing raised.
