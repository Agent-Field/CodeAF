# factory stage conversations

## a stage is a conversation — stage_result and plan_edit, and a stage never posts to github

On the factory floor, a stage like plan, write or review runs as one ordinary conversation,
made in the item's own team, where it joins as the item and the stage: `#12 · review`, or
`#12 · review 1/2` when the stage may run more than one round (like any conversation it then
titles itself from its work, and that title is what its tab shows). It opens with a brief: the
stage's ask, one line saying it is that step of a run on the pull request or issue, the item's
title and body (the first 6,000 characters), who opened it and its labels, and for a pull
request its base and head (`into main from alice:feature`), its line counts, each check with its
state, the issue it closes (`linked: #3`) and its changed files with their counts; the last
comments; where its work tree stands (a pull request's is at its head, and the brief names the
`git diff origin/<base>...HEAD` that shows the change, so the step never goes to the web for
it); the run's stages in order
with this one named (`stages: plan › write › … · this is plan: do this stage's part, and leave
the rest to the stages after it`), your notes, one line for
each stage that ran before it (`write: done · 3 claims`), the stage's settings in words
(`until clean · max 2 · fanout per-finding`), a line saying a question it cannot settle goes to
the item's manager through `ask` and it waits for the answer, and the sentence
`End by calling stage_result once.`

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

## a second round — the review found the same thing again, fix then check

A second round fixes what the first found, then checks again. When review (or anything that
runs until clean, green or proven) goes round again, round 2's brief adds `round 2 of review.
The last round found: … Fix those in the checkout first, run the tests, then review again and
report only what remains.`, carrying what the last round said and the claims it could not show
(the first 1,500 characters), so a round never just looks at the same unchanged checkout
again. Nothing is added on a run until done, and a check just runs its command again. When
the rounds run out, it asks you `one more round, or go on as is?`.

## where a stage's conversation opens — watch a running stage, open it afterwards

A stage's conversation is made when the stage starts, in the item's own worktree (a folder
of its own on its own branch, never your checkout), and joins the item's team (`#12 · <title>`) under the one `factory` team in the team
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
item's own worktree, `~/.codeaf/v3/factory/work/<repo>-<number>`, on its branch
`factory/<number>-<slug>`, never in your checkout. So it edits and writes files and runs commands (builds,
tests, `git`) there without asking anyone. Your own approval settings
(the tool approval mode, the tool rules, the command rules) do not narrow it: a rule that says
"ask me" would only ever be refused there, because nobody is at its keyboard to answer.

Two floors still hold under it, as they do under `--yolo`: a critical command such as `rm -rf /`,
`mkfs` or `shutdown`, and a call that acts in your name such as `gmail_send`, would ask, and in a
stage that ask is refused with `needs approval but no resolver is attached: <rule>`. Nothing in a
stage ever waits on you.

A stage cannot post to its team either: its brief says `You have no team_post tool; report
through stage_result only. Ignore team notices about renames.`, so a renamed teammate does not
send it hunting for a way to answer.

To run stages narrower, set `CODEAF_FACTORY_POSTURE` to another posture (`ask`, `guardian`,
`deny`, or `auto` for your own settings rows) in the environment codeaf's host starts in; under
any of them a call that would ask is refused the same way.

## what the manager hears — the progress lines in the item's chat

The run reports into the item's chat (its manager, opened with `T`) one line per event, written
as the manager's own message and marked as the run's (a surface may draw them as a timeline):

- `plan started` when a stage's first round starts;
- `plan done · 2m · $0.04 · <what it said>`: how long, what it cost, and the notes the stage left
  for the stages after it (else the last sentence its conversation said); a part with nothing in
  it is left out, so a free instant stage is `plan done · <what it said>`;
- `asking you: plan is ready · continue, or send it back?` when the run holds at an approve step
  (the manager can explain what it waits on), and after a stage that fell short,
  `test failed 1 of 2 · asking you: <question>`;
- `sent back to plan: <your words>` when you said no at an approve step;
- `budget of $5 reached · asking you`;
- `plan asks: <question>` when a stage asks something, then `manager answered plan: <answer>` or
  `plan asks you: <question> · <why>` (see a stage asks a question, on the factory page);
- `answered: yes`, `answered: no` or `answered: <your words>`, however you answered;
- `steer: <words>`, `changes requested: <words>`;
- `landed · proof sheet ready · your approval`, `shipped`, `stopped`, `paused`, `resumed`.

A line is never written into the middle of the manager's own turn: while it is answering you,
or while another codeaf has the chat open, the line waits and is written afterwards, in order.

## the manager did not answer — shaping a run, manager set, the recipe stands

Every run starts with the manager shaping it: one turn of the item's chat, before the first
stage (see the manager shapes the run, on the factory page). The item's stream and the chat both
get one line for what came of it:

- `manager set review: thorough on security, code and architecture · added arch after review ·
  skipped neaten · why: touches the call row`: the same line the Adapted row of the item page
  shows, one clause for each change and the reason last;
- `the recipe stands` when the manager changed nothing;
- `the manager did not answer · the recipe stands` when the turn failed, or did not end within a
  minute: the recipe runs as it is, with no question about it;
- `the manager was busy in the window · the recipe stands` when the chat open in your window was
  answering you, and was still answering when it was asked a second time;
- `the manager is open in another window · the recipe stands` when another codeaf has the chat
  open, so this one cannot give it a turn;
- `the manager's change was not applied: <why> · the recipe stands` when the bounds refused it,
  for instance `a stage is one word · "do through" is two` or
  `the run has nine stages already`; the stages stay as they were.

Shaping never stops the run; you read what it set at the first approve step. During a run, a change the manager makes on something you said is the same
`manager set …` line, or `the manager's change was not applied: <why>`.

## talk to the manager — what you type is the brief before a run and the steer during it

What you type into the item's chat goes to the run; nothing else is needed.

- **Before a run**, everything you typed there since the last run becomes the item's notes when
  you press `r`, and every stage's brief opens with them (`keep the old API`).
- **During a run**, what you type is the steer: at the next stage (or round) it is folded into
  that stage's brief, a round already running is handed it, the item's log says
  `steer: <words>`, and the chat gets the same line. It is the same as `S` on the floor.
- **While the run waits on a question**, a line that starts with `yes` (`yes, go`) answers yes
  and a plain `no` answers no; for a plan, a gate or a stage that fell short, any other words
  (`no, use the other file`) answer it in words, as `a` does. The chat then says `answered: yes`. Words that do not answer a budget question are
  taken as a steer instead.

Each line is taken once. The chat's model still answers you; it shapes the run's stages with
`factory_run` and changes the budget, thinking and notes through `factory_item`'s card.
During a run each line also gives the manager one turn to reshape the stages not yet started.
