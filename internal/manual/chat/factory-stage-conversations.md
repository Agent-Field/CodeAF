# factory stage conversations

## a stage is a conversation — stage_result and plan_edit, and a stage never posts to github

On the factory floor, a stage like plan, write or review runs as one ordinary conversation,
made in the item's own team and named by the item and the stage: `#12 · review`, or
`#12 · review 1/2` when the stage may run more than one round. It opens with a brief: the
stage's ask, the item's title and body (the first 2,000 characters), your notes, one line for
each stage that ran before it (`write: done · 3 claims`), the stage's settings in words
(`until clean · max 2 · fanout per-finding`), and the sentence `End by calling stage_result once.`

Inside, it works like any conversation: it reads, changes, runs, and can hand parts that do
not need each other to tasks. Your words while it runs reach it, and what it is doing shows in
the item's stream.

It is given two tools that no other conversation has:

- `stage_result` ends the stage's work: done or not, how many findings are still open, its
  claims with their evidence, notes for the stages after it, and a few lines on what it did.
  It answers `reported · the stage ends when you stop`. Once only: a second report is refused.
- `plan_edit` proposes adding, skipping or switching on stages. It answers
  `proposed · the runner applies it within the recipe's bounds`; nothing changes from the
  call itself, and proof and your gates are never skipped.

**What ends a stage** is code, not the conversation's say-so. `until clean` means it reported
zero findings; `until done` means it reported at all. A conversation that stops without
calling `stage_result` counts as `the stage ended without reporting`, which meets nothing,
so the round is counted and the stage stops at its max and asks you.

**Nothing posts.** Neither tool writes to GitHub or anywhere else, and neither raises a card.

**Where it opens.** It is made when the stage starts, in the folder of the repository's checkout,
and joins the item's team (`#12 · <title>`) under the one `factory` team in the team menu, where
you can open it while it runs or after. Nobody is at its keyboard: it runs unattended for at most
two hours, and its approvals are the ones `codeaf chat --once` has: a tool that would ask you is
refused with `needs approval but no resolver is attached: <rule>` unless your tool approval mode
or rules let it run (`stage_result` and `plan_edit` never ask). It has no `factory_add`, `factory_recipe` or
`factory_item`, because those wait on a card. The round is over when it has been idle, with no
task it started still running.
