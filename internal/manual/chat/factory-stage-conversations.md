# factory stage conversations

## a stage is a conversation — stage_result and plan_edit, and a stage never posts to github

On the factory floor, a stage like plan, write or review runs as one ordinary conversation,
made in the item's own team, where it joins as the item and the stage: `#12 · review`, or
`#12 · review 1/2` when the stage may run more than one round (like any conversation it then
titles itself from its work, and that title is what its tab shows). It opens with a brief: the
stage's ask, the item's title and body (the first 2,000 characters), the run's stages in order
with this one named (`stages: plan › write › … · this is plan: do this stage's part, and leave
the rest to the stages after it`), your notes, one line for
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

## where a stage's conversation opens — watch a running stage, open it afterwards

A stage's conversation is made when the stage starts, in the folder of the repository's
checkout, and joins the item's team (`#12 · <title>`) under the one `factory` team in the team
menu. Nobody is at its keyboard: it runs unattended for at most two hours. It has no
`factory_add`, `factory_recipe` or `factory_item`, because those wait on a card. The round is
over when it has been idle, with no task it started still running.

**While it runs you watch it in the item's log**, one line per step (`read ledger.go`,
`bash go test ./...`, `stage_result …`); the process running the floor holds the conversation
itself, so it cannot be opened yet, and `enter` on its stage row says
`plan has no conversation yet · it opens when a round ends`. Once the round has ended, `enter`
on the row, or the team menu, opens it like any other conversation.

## what a stage may do — edit files and run commands, the allow posture

A stage conversation runs with the allow posture, the same open gate `--yolo` gives, inside the
folder of the repository's checkout. So it edits and writes files and runs commands (builds,
tests, `git`) there without asking anyone. Your own approval settings
(the tool approval mode, the tool rules, the command rules) do not narrow it: a rule that says
"ask me" would only ever be refused there, because nobody is at its keyboard to answer.

Two floors still hold under it, as they do under `--yolo`: a critical command such as `rm -rf /`,
`mkfs` or `shutdown`, and a call that acts in your name such as `gmail_send`, would ask, and in a
stage that ask is refused with `needs approval but no resolver is attached: <rule>`. Nothing in a
stage ever waits on you.

To run stages narrower, set `CODEAF_FACTORY_POSTURE` to another posture (`ask`, `guardian`,
`deny`, or `auto` for your own settings rows) in the environment codeaf's host starts in; under
any of them a call that would ask is refused the same way.
