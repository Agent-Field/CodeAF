---
kind: added
title: the chat signs the git work it does for you, in the resident's own words and bytes
pr: 812
surface: [chat, engine, resident]
invalidates:
  - "The v3 chat did not attribute itself at all. prompts/system.md never mentioned attribution, the chat manual had no page about it, and the commit a task writes when its work lands carried no trailer. It does now, on the `attribution` row the settings registry has had all along (env `AFORGE_ATTRIBUTION`, default on): the model is told the law as one belt fact beside `bash`, and the landing commit gets the trailer appended mechanically with no model in the loop."
  - "Attribution had two cases, a commit trailer and a body footer. It has THREE: a comment aforge leaves — on an issue, on a pull request, on a line of a review — ends with `AttributionCommentFooter`, a new constant, `<sub>drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=comment&utm_campaign=drafted_with)</sub>`. It is muted rather than a footer: one line, no em-dash separator, lowercase, no owning clause, and `<sub>` so GitHub renders it small. It appears AT MOST ONCE PER THREAD — the first comment carries it and no later one does — and never on a one-line reply, never inside a code or suggestion block, and never on a comment the person dictated word for word."
  - "The attribution wording lived only in internal/exec's `attributionPrompt`, ~1.1 KB of it, and was the resident's alone. It is now `exec.AttributionLaw` — 861 bytes, composed from the same `AttributionTrailer`, `AttributionSeparator` and `AttributionPullFooter` constants — and BOTH surfaces render it. `attributionPrompt` is that law plus the one line spelling the issue footer out; the chat's belt fact names `utm_medium=issue` instead, because 145 more bytes of URL would be bought on every tool round of every turn."
  - "A task node could not have honoured the row even if it had been asked to: it is handed no `ProfileDir`, so anything re-reading `attribution` inside a task would have read the default, which is on. `session.Config` now carries a resolved `Attribution bool`, filled at the one door every v3 session comes through (cmd/aforge's applyV3Governance, beside `task.audit`) and copied onto every task child."
  - "Commits written by the task system stay authored as `aforge <aforge@localhost>` and that has not changed — sibling landings read that identity to tell aforge's own forward progress from a person's intervening work. The trailer is provenance on top of it, and names the aforge GitHub account (`agentfield-bot@users.noreply.github.com`), not the local one."
---

Aforge commits, opens pull requests, files issues and leaves comments in somebody
else's name, and until now the half of the program a person actually sits in
front of left no mark saying so. The law is one wording for both surfaces
because two paragraphs about the same bytes drift into two laws, and a trailer
spelled two ways is provenance nobody can count.

The comment case is the one that had to be designed rather than copied. A body
has a foot and a commit has a trailer block; a comment is a remark in somebody
else's conversation, so the em-dash separator would draw a rule through the
middle of a thread and a signature on every reply would be advertising. It is
one muted line, once per thread, and never at all on a one-liner, in a code or
suggestion block, or on words the person dictated.

It is conditional in the chat — absent with the row off, absent in a fork's
hand, whose `bash` is read-only and could not commit if it wanted to — and it
cost the fixed prefix 863 bytes, of which 333 are the trailer, the footer and
the comment line themselves. The prefix is 45,433, which is 2,567 under a budget
nothing raised.
