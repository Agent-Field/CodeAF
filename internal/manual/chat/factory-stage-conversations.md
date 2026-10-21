# factory stage conversations

## a stage is a conversation — stage_result and plan_edit, and a stage never posts to github

In the factory, a step like plan, write or review runs as one ordinary conversation,
made in the item's own team, where it joins as the item and the stage: `#12 · review`, or
`#12 · review 1/2` when the stage may run more than one round (like any conversation it then
titles itself from its work, and that title is what its tab shows). Its handle in the team is
the step's own name, `@review`, or `@review2` for a second round's conversation, and it is never
renamed. It opens with a brief: the
stage's ask, one line saying it is that step of a run on the issue, the item's
title and body (the first 6,000 characters), who opened it and its labels; the last
comments; where its worktree stands and the diff command that shows the change;
the run's stages in order
with this one named (`stages: plan › write › … · this is plan: do this stage's part, and leave
the rest to the stages after it`), your notes, what the steps before it reported (the plan
whole, a line and the words of each step, every claim so far: see what a step is handed by the
steps before it), the stage's settings in words
(`until clean · max 3`), for a stage that runs until clean the sentence
that findings count only what is still wrong once its own fixes are in (until proven: that every
claim needs its evidence), a line saying a question it cannot settle goes to
the item's manager through `ask` and it waits for the answer, and the sentence
`End by calling stage_result with your final result.`

Inside, it works like any conversation: it reads, changes, runs, and can hand parts that do
not need each other to tasks, but never to a quick task. It runs on your default model, or on the job's own model when one
was chosen for the job (the desktop app's model chip on the job); a model chosen while a step
runs is picked up at that step's next turn. Your words while it runs reach it, and what it is doing shows in
the item's stream.

It is given two tools that no other conversation has:

- `stage_result` ends the stage's work: done or not, how many findings are still open, its
  claims with their evidence, notes for the stages after it, and what it did and found (a plan
  step writes its whole plan there, for the steps after it).
  It answers `reported`. A successful done report ends the step immediately, without another model reply.
  Use `final: false` for an intermediate report that can change (see a step that reports again).
- `plan_edit` proposes adding, skipping or switching on stages. It answers
  `proposed · the runner applies it within the recipe's bounds`; nothing changes from the
  call itself, and proof and your gates are never skipped.

**Nothing posts.** Neither tool writes to GitHub or anywhere else, and neither raises a card.

## how a step is judged — until done, clean, proven, green, what findings count, claims need evidence

Whether a factory step is finished is decided by code, not by the step's conversation, from
the `stage_result` it reported, and only a report that says done can pass:

- `until done`: the report says done (see a step stopped without reporting).
- `until clean`: done with zero findings. Findings count only what is still wrong after the
  round's own fixes, so a review that found three problems and fixed them reports zero.
- `until proven`: done, at least one claim, and every claim `ok` with its evidence filled in.
  Every claim needs `evidence` (what shows it: the command and its result, the file, the
  screenshot); a claim whose evidence is blank is not proven.
- `until green`: done, and a check step's command exited 0. A conversation step has no exit
  code of its own, so for it green is the same as done: it reports done once the tests pass.

A step that falls short of its until goes round again while it has rounds left (see a second
round).

A report that contradicts its own step's rule is refused the moment it is made and the report
before it stands, so the step fixes it in the same turn: a review that says done with findings
open is answered
`not reported: you reported 2 findings still open: fix them and report again, or report done: false`,
a claim marked ok with blank evidence, done or not,
`not reported: claim 2 has no evidence: add what shows it, or set ok: false`,
and a proof that says done with no claims
`not reported: you reported done with no claims: add each claim with what shows it, or report done: false`;
an honest shortfall, `done: false` or a claim marked not ok, is never refused.

## a second round — the review found the same thing again, fix then check, how many rounds

A step that falls short goes round again, in a fresh conversation, until its rounds run out.
The shipped steps have room for that: plan, write and the other steps that do the work get 2
rounds, and test, review, proof and security, which are judged on what they leave, get 3 (see
the default steps). A repository's `.codeaf/factory.yaml` sets its own with `max:`.

Every round after the first opens with what the round before reported: `round 2 of review.
The last round reported:` followed by what it said, `1 finding still open`, and each claim it
could not show as `not shown: <claim> (<its evidence>)`, the first 4,000 characters of it.
Its notes are already among the brief's notes. A stage that runs until clean, green or proven
is then told `Fix what it left in the checkout first, run the tests, then review again and
report only what is still wrong.`; one that runs until done is told `It did not finish. Carry
on from what it left in the checkout, and report done once the ask is carried out.` So a round
never just looks at the same unchanged checkout again. A check just runs its command again.

Only when the last round falls short does it ask you, for instance
`review is not clean after 3 rounds: 1 finding · try again, send it back, or stop the job?`.

## a step stopped without reporting — what ends a step, the out of rounds state, my job stopped and waits, try again

A step's conversation ends its work by calling `stage_result`; nothing inside it decides the
step is finished. A step never hands work to a quick task (it has no `quick_task`, its reads
are never handed off) and its reply is never moved to a task. When it stops without
reporting, codeaf gets the report, as codeaf and never as you, each way only if the one before
brought nothing:

1. A turn that ended on an error (the provider refused, a timeout) is tried once more:
   `the turn stopped on an error · trying it again: <why>`.
2. Three reminders, `stopped without reporting · reminded, 1 of 3` to `3 of 3`. The second
   repeats the step's ask; the third, `3 of 3, on a turn that must report`, requires
   `stage_result` through the provider's `tool_choice`, so a model that honours it cannot
   answer in words.
3. codeaf makes the report: one short call reads the step's brief, its last message and its
   checkout's change (`git diff --stat` and the diff, the first 4,000 characters) and must call
   `stage_result`. The log says `reported for the step · from its brief, its last message and
   its change`, the round's Timeline line starts `reported for the step ·`, and it is judged
   like any other report.

A spending limit is a stop, not a missing report: a turn today's limit refused stops the job
with `today's spend limit stopped this step` (another limit: `a spend limit stopped this step:
<its words>`), with no reminder and nothing asked. Only when all three came back empty, or a
round ran for two hours, does the job wait for you, in plain words:

- `plan stopped without reporting · try again, or stop the job?`, with why after a colon when
  codeaf could not make the report;
- `write ran out of time without reporting · …`
- `plan did not finish: <what it said> · …` when it reported that it could not;
- `test is not proven: 1 of 2 claims shown · …` for a step that runs once;
- `review is not clean after 3 rounds: 1 finding · …` only for a step that loops.

Rounds are named only for a step that loops. `send it back` is offered only when a finished
step before this one can take the work back; plan, the first step, never offers it. Try again
runs the step once more with the words you add; stop the job is the job's own Stop.

## a step that reports again — the latest report stands, updated, not cut off after reporting

`stage_result` may be called more than once in a round. Intermediate reports use `final: false`;
Each call replaces the one before until the final report. `done: true` is final by default and ends the model turn
immediately after the tool receipt is saved. `final: true` can also finish an incomplete step.
An intermediate report is never cut off merely because it reported. The runner judges a final
report against the step's rule. It never needs an extra model reply to
accept a final report. The two-hour wall still bounds a step that never finishes.

The Timeline shows the final report only, with `· updated 1 time` or `· updated 2 times` when
the step changed it, so you can see it corrected itself.

## what a step is handed by the steps before it — does write see the plan, the whole plan, earlier claims, planned files

Every factory step's brief carries what the steps before it reported, so write works from the
plan and not from the issue alone:

- **The plan, whole.** Every step after plan has a paragraph `the plan (from the plan step):`
  with the plan step's output, up to 6,000 characters; a longer plan is cut there and its
  heading says so. The plan step itself is told `Write the plan in output: what you'll change,
  in which files, and how you'll know it works.`
- **The files the plan names.** Once the plan finished, write's brief lists
  `files the plan names:`, the same list the job shows as the plan's files (up to 20).
- **Each earlier step.** `before this stage:` has a line per step, `- write: done · 3 claims`,
  with up to 1,000 characters of what it said under it; plan's line says `its plan is above`.
- **Every claim so far.** `claims made so far:` lists each earlier claim with its step, `ok` or
  `not ok`, and its evidence. Proof's brief asks it to confirm or correct each in its own
  claims rather than invent new ones.
- **Notes** stay in `notes:`, whole.
- **Where the changes are.** Once a step has run, the work-tree line adds that `git log` and
  `git diff` on the branch show what earlier steps changed: the work tree, not the brief, is
  the source of truth.

All of it is held to about 12,000 characters. Over that, the oldest steps' words go first
(their line says `what it said is cut to fit this brief`), then the oldest claims; the plan is
never cut.

A job that moves to another machine, because its worker went away, keeps all of it: the
runner there is handed each earlier step's whole report (done, findings, claims, notes), not
only what it said.

## where a stage's conversation opens — watch a running stage, open it afterwards

A stage's conversation is made when the stage starts, in the item's own worktree (a folder
of its own on its own branch, never your checkout), and joins the item's team (`#12 · <title>`) under the one `factory` team in the team
menu. Nobody is at its keyboard: it runs unattended for at most two hours, and a card it raises
takes its default. It has no
`factory_add`, `factory_recipe` or `factory_item`, because those wait on a card. The round is
over when it has been idle, with no task it started still running.

**While it runs you watch it in the item's log**, one line per step (`read ledger.go`,
`bash go test ./...`, `stage_result …`); the process running the factory holds the conversation
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

Two limits still apply, as they do under `--yolo`: a critical command such as `rm -rf /`,
`mkfs` or `shutdown`, and a call that acts in your name such as `gmail_send`, would ask, and in a
stage that ask is refused with `needs approval but no resolver is attached: <rule>`. Nothing in a
stage ever waits on you.

A stage cannot post to its team either: its brief says `You have no team_post tool; report
through stage_result only.`

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
  (the manager can explain what it waits on), and after a stage that used its rounds,
  `asking you: <question>`;
- `sent back to plan: <your words>` when you said no at an approve step;
- `budget of $5 reached · asking you`;
- `plan asks: <question>` when a stage asks something, then `manager answered plan: <answer>` or
  `plan asks you: <question> · <why>` (see a stage asks a question, on the factory page);
- `answered: yes`, `answered: no` or `answered: <your words>`, however you answered;
- `steer: <words>`, `changes requested: <words>`;
- the final approval message explains pushing the branch and creating a draft pull request;
- the publication receipt links the draft PR or issue comment; publication does not mean merged;
- `stopped`, `paused`, `resumed`.

A line about one step's round (`started`, `done`, an `asking you:` it waits on, the `stopped`
that cut it) also names that step and round beside its words, so a surface that draws each round
as a frame can fold the line into it; the words are the same either way.

A line is never written into the middle of the manager's own turn: while it is answering you,
or while another codeaf has the chat open, the line waits and is written afterwards, in order.

## pressing run goes straight to the first step — does the manager reshape the run, manager set, shape steps

Pressing `r`, or saying `start` in the item's chat, starts the run at its first step (usually
plan) with the steps the item has now: the repository's recipe, unless you or the manager
changed them first. The manager is given no turn of its own before the first step, so nothing
waits on it and no card stands between the press and the plan.

To change the steps, tell the manager in the item's chat. Its `factory_run` changes them at
once before a run, and only the steps not yet started during one (see change one job's steps,
on the factory page). What it set is one line, the same one the Adapted row of the item page
shows, one clause for each change and the reason last:
`manager set review: thorough on security, code and architecture · added arch after review ·
skipped neaten · why: touches the call row`. A change the bounds refuse is answered
`nothing changed: <why>`, for instance `a stage is one word · "do through" is two` or
`the run has nine stages already`, and the steps stay as they were.

Nothing else asks the manager to reshape the steps: there is no separate "shape steps" action,
and a run never gives the manager a turn of its own first. The steps change only where a person
changes them, on the job's page or in words to its manager here.

## talk to the manager — what you type is the brief before a run and the steer during it

What you type into the item's chat goes to the run; nothing else is needed.

- **Before a run**, everything you typed there since the last run becomes the item's notes when
  you press `r`, and every stage's brief opens with them (`keep the old API`).
- **During a run**, what you type is the steer: at the next stage (or round) it is folded into
  that stage's brief, a round already running is handed it, the item's log says
  `steer: <words>`, and the chat gets the same line. It is the same as `S` in the factory.
- **While the run waits on a question**, a line that starts with `yes` (`yes, go`) answers yes
  and a plain `no` answers no; for a plan, a gate or a stage that fell short, any other words
  (`no, use the other file`) answer it in words, as `a` does. The chat then says `answered: yes`. Words that do not answer a budget question are
  taken as a steer instead.

Each line is taken once. The chat's model still answers you; it shapes the run's stages with
`factory_run` and changes the budget, thinking and notes through `factory_item`'s card.
During a run the chat's own turn on each line may reshape the stages not yet started; the
runner gives it no second turn on the same words. Say `start` and the manager starts the run
with `factory_start`; it starts the job only when you ask. It never clones the repository
itself: the runner clones it when the job starts.

## the job's chat is its manager — where it works, and its spend counts toward the job's budget

A job's own chat (`T` on the floor, or the job's chat on its page) is the job's manager. What
you say there before Start is the brief, it starts the job when you ask, and its turns spend
from the job's budget like the job's steps: everything done for the job spends from one budget.

- **Before Start** the job's cost is what its chat has spent, and Start carries that into the
  run, so a step's budget question counts it.
- **During a run** each of the chat's calls adds to the run's spend as it is made, and the
  budget is checked after every round as before. A new run after one whose steps ran starts its
  spend from nothing, the chat's share of the old run included.
- **Counted once.** The spend page still lists every call once, under the chat. The job's
  figure is the same dollars read a second time, never added to the day's total.
- **At the budget** the chat refuses its next turn with
  `job budget reached · $5.02 spent of $5 · raise the job's budget to go on`, and your message
  stays where you typed it. A turn already running finishes. While the job waits on its budget
  question (`budget of $5 reached · asking you`), the chat still takes your `yes` or `no`.

A job on another computer's factory that is not running here has no record on this computer:
its chat's spend is on the spend page only.

**Where the chat works.** In the repository's checkout when one is known; else in the
factory's own clone, `~/.codeaf/v3/factory/repos/<owner>/<name>`, once the runner has made one;
else in an empty folder of the job's own, `~/.codeaf/v3/factory/chats/<id>`. Never in the
folder the window or the app was opened in.

## Which model and branch does a factory step use?

A step opens on the model selected for its job. A finished step's chat shows
the model recorded for that step when it is reopened, rather than the current
profile default. New branches made by the app use the job person's GitHub
login as their namespace; existing jobs keep their recorded branches.

## What happens when fd is missing?

The find tool searches with its built-in file walker. It downloads nothing.
Without fd it skips .git, node_modules and vendor, does not follow directory
symlinks, and does not apply .gitignore. Its description states those limits.


## automatic plan approvals and a separate publishing approval

Default approval steps are optional. Before Start, switch an optional approval step off to
continue automatically after its preceding step. Only you can choose that setting; a stage or
the manager cannot skip your approval. A repository can still fix a gate in its pipeline.
The step list shows the setting and the Timeline keeps the change. Skipping an internal gate
keeps the separate publishing approval: green proof does not silently publish your branch.
A recipe deliberately containing no approval stages retains its existing self-publish mode.

## command receipts, live progress and GitHub checks after publishing

A running step publishes its chat identity immediately, so the desktop can show actions and
open that conversation while it works. Provider turn errors are recovery events; only the
runner announces the final stage outcome. Progress uses paths relative to the checkout.
Completed stage results carry command receipts in their notes: the command, starting folder,
outcome and explicit test outcomes when the output has them. A final PASS summary is not a test.
Later steps receive these receipts instead of rediscovering the working directory and flags.

Approve at the final gate pushes the approved commit and creates a draft PR. Its description
summarizes the implementation and validation, linking the issue instead of copying its body
and attribution. The app shows GitHub check totals and failed check names separately from local
proof, records check changes in Timeline, and offers Open PR and View GitHub checks. A draft
needs to be marked ready on GitHub before merging; failing checks still block Merge.


## the factory manager does not poll or judge background completion

The factory runner owns the whole pipeline as well as its stages. A manager conversation
has no second goal owner or completion-check loop. It answers your words and stage questions,
while recorded runner progress and the next-action receipt explain what is happening.
A provider timeout inside a stage is not a terminal pipeline failure. Once a final report has
been accepted, another model reply is not required to finish the stage.


## factory chats belong to their job

Open the manager and step conversations from the job. They do not appear in Agents.
Every round keeps durable job ownership, including earlier rounds after a restart.
Drop deletes the job's local conversations, attachments and team records together with
its run data. Ordinary conversations in the same workspace are preserved. Generated
stage chats from older versions are recognized by their factory worktree and opening brief.
A step conversation has a visible Back to job conversation button. Completed results
retain their full Markdown; a waiting approval does not display a preparing animation.

## Follow PR — CI repair, requested changes and merge conflicts

After publication, one final **Follow PR** step watches the issue's open PR on the
factory's normal GitHub poll. Failed checks, requested review changes, current inline
review feedback and merge conflicts can queue another round in that same step.
Repeated observations do not start duplicate repairs. Healthy checks leave the job
watching until GitHub reports merge or closure. General PR discussion comments are
not a repair trigger. Closing the app pauses local polling and workers.

The original changed-file list is context, not a publish restriction. Necessary
dependency and test changes are allowed within the issue objective. The repair receives
check names and log evidence, with truncation called out when complete logs do not fit.
It reconciles remote changes, runs checks and reports its outcome. Failed checks block
completion; passing local checks do not claim that GitHub CI passed. Repairs have a
spending cap and at most three consecutive attempts before asking you; successful
checks reset the attempt count. Missing access or an unresolved decision can also pause.

## Approve publication and subsequent repair pushes

Initial publication approval explains that it pushes the branch, creates a draft PR and
authorizes checked maintenance pushes to that same PR within the job budget. The server
checks that consent against the original approval record. Before each push it rereads
the PR; a closed PR or an unexpected head refuses the push. Maintenance never creates
a replacement PR, rewrites remote history, marks a draft ready or merges. A job without recorded
publication consent still asks before pushing its repair. Merge remains your decision.
Drop and turning the repository off stop the watch.

## Take over and Resume pipeline

**Take over** pauses automation and opens the working conversation with its existing
worktree and context. Its spend still counts toward the job budget. Working chats stay
inside the job and out of Agents. Stop any active manual turn before **Resume pipeline**;
resuming checks the current work and continues on the same issue and PR. Finished jobs
and a publication already underway cannot change control.

The manager's `factory_follow_pr` tool turns an explicit request such as “fix it anyway”
into another maintenance investigation. It needs an open linked PR and the original
publication approval; it refuses manual mode or work already running. It does not merge.
