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
round ran for two hours (or an idle Plan could not finish reporting within its two-minute recovery window), does the job wait for you, in plain words:

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
When the job ends (merged, closed, answered, dropped or closed here) its team closes quietly,
with no report and no line in its traffic; its conversations still open from the item's rows.
When the job comes back (you reopen it or run it again, or its pull request is reopened) its team
opens again and its next round joins it. A team you closed or archived yourself stays as you left it.

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
- `changes requested: <words>`;
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
skipped neaten · why: touches the call row`. It also removes a step from the job (`removed
pretest`; skipping only switches a step off) and moves one (`moved docs after write`, `moved
docs first`) when you ask; proof, a fixed step, a step a rule names, a step that has run,
your approvals and your posts stay where they are. A change the bounds refuse is answered
`nothing changed: <why>`, for instance `a stage is one word · "do through" is two` or
`the run has nine stages already`, and the steps stay as they were.

Nothing else asks the manager to reshape the steps: there is no separate "shape steps" action,
and a run never gives the manager a turn of its own first. The steps change only where a person
changes them, on the job's page or in words to its manager here.

## talk to the manager — what you type is the brief before a run and the steer during it

What you type into the item's chat goes to the run; nothing else is needed.

- **Before a run**, your instructions since the last run become the item's notes when
  you press `r`, and every stage's brief opens with them (`keep the old API`).
- **During a run**, instructions reach the active round and the next stage's brief. The
  item's internal log keeps `steer: <words>`; the chat does not repeat your message as an
  acknowledgement. Pure status questions never reach a worker or a later stage's notes,
  including check-ins typed before launch. An instruction is the same as `S` in the factory.
- **While the run waits on a question**, a line that starts with `yes` (`yes, go`) answers yes
  and a plain `no` answers no; for a plan, a gate or a stage that fell short, any other words
  (`no, use the other file`) answer it in words, as `a` does. The chat then says `answered: yes`. Words that do not answer a budget question are
  taken as a steer instead. Status-only questions such as `Where do we stand here? What's next?` and `Are we stuck?` never answer the pending decision.

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

- **Default Budget** is up to $5 per factory job when the repository has no explicit
  `default_budget`. Existing saved job budgets and configured repository defaults stay
  unchanged; the default is not a migration or a budget increase for existing work.
- **Before Start** the job's cost is what its chat has spent, and Start carries that into the
  run, so a step's budget question counts it.
- **During a run** the manager, stages and their delegated workers record priced calls
  immediately against the same job. Child totals folded back into a parent are not charged again.
- **Counted once.** The spend page still lists every call once. The job's figure reads the
  same dollars; it is not an additional charge.
- **At the budget** new model calls and tools are refused. The job's owned agent families
  stop promptly, including delegated tasks, background commands and adaptive runs. This
  applies while the manager is idle too. Calls already sent can still report charges afterward;
  stopping work cannot reverse a provider charge already incurred.
- **Change Budget** by clicking its sidebar value or through Actions. Both menus offer $5,
  $10, $20 and $30 and share the confirmed value. Decreasing $10 back to $5 is allowed when
  recorded spend is at most $5. The server rechecks spend while writing the new cap. At exact
  equality the cap is valid but no more paid work may begin. Choose a higher cap to continue;
  a budget question never exempts the manager from the limit. Start is a single-word button;
  the Budget field shows its ceiling.

## Which model and branch does a factory step use?

A step opens on the model selected for its job. A finished step's chat shows
the model recorded for that step when it is reopened, rather than the current
profile default. New branches made by the app use the job person's GitHub
login as their namespace; existing jobs keep their recorded branches.

## What happens when fd is missing?

The find tool searches with its built-in file walker. It downloads nothing.
Without fd it skips .git, node_modules and vendor, does not follow directory
symlinks, and does not apply .gitignore. Its description states those limits.


## Auto and Manual: automatic approvals, retries and publishing

New issue jobs default to **Auto**. The job header's Auto / Manual switch is next to
Actions. Auto uses the shared yellow switch; Manual is grey. The saved mode follows
the job across views and restarts. Older jobs with no saved mode stay Manual until
you explicitly enable Auto; upgrading cannot publish an already waiting job.

Auto passes optional routine plan gates, keeps bounded stage retries, and publishes
passing proof as a draft PR through the same recorded, exact-commit approval door.
It repairs and updates that same PR within the job's budget and retry limits. The
service records each approved publication; a worker cannot manufacture consent.
Neither explicit Auto nor Manual grants automatic merging, even when a recipe permits it. **Merge remains
your decision.** Manual asks at enabled plan gates and before publication or repair
pushes. Optional per-step skips remain available; neither mode resets retry limits.

## Retry a failed step or a failed message

A failed pipeline step has **Try again** in the job's current conversation status.
It reruns that step and its unfinished tail, keeping earlier completed steps,
the working branch, linked PR, retry history and total spend. **Take over** remains
available through its normal ownership and budget gates. A cap must be raised before
another attempt can spend. Completed publication status does not disable step retry.

In Auto, a lost runner or a recognised transient model connection failure gets at
most three automatic retries, after 2, 5 and 15 seconds. They are counted in
attempts, not minutes; the only deadline is for the runner, which has 15 minutes
to take a queued retry. The same 15 minutes, counted again from each new failure
and from a raised budget, bound how long a worker that still holds the job may
keep it before a retry can start. The service persists the attempt count and that deadline
and resumes them after a restart without an open job tab. The job shows
the reason, next retry and limit; **Try again now** can skip the wait. A still-held
worker is never silently replaced. After the limit, **Try again** makes a new manual
attempt. Manual, Take over, repository-off, spending limits and terminal jobs stop
automatic admission. A successful step ends that recovery.

A message has **Retry message** only when the backend retained the failed message.
An old error in a cold or factory-owned conversation does not imply that capability.
Message retry and step retry are different actions. Unknown failures, authentication
and permission denials require attention. Interrupted publication and merge are not
blindly replayed: their recorded outcomes and commit authority must be reconciled.

## Auto exceptions and critical approvals

The conversation shows the engine's concrete Auto exception immediately, including
the needed budget, proof or repository approval. It does not promise publication
while those checks are blocking it.

Critical unanswered decisions, fixed repository approval gates, missing access,
failed publication and budget/attempt exhaustion still stop for a concrete next
step. Auto lets the manager decide ordinary engineering choices within the issue;
it does not supply credentials, expand money limits or override explicit tool
denials, shell permission rules or external account permissions. Only named
internal factory controls and reads inherit its routine permission grant.

The mode switch is separate from **Take over**. Enabling Auto never resumes a
manually paused working session. **Resume pipeline** remains explicit. Switching
Manual applies at the next approval boundary and cannot undo an already started
push. A failed automatic publication stays visible instead of retrying forever.

## command receipts, live progress and GitHub checks after publishing

A running step publishes its chat identity immediately, so the desktop can show actions and
open that conversation while it works. Provider turn errors are recovery events; only the
runner announces the final stage outcome. Progress uses paths relative to the checkout.
Completed stage results carry command receipts in their notes: the command, starting folder,
outcome and explicit test outcomes when the output has them. A final PASS summary is not a test.
Later steps receive these receipts instead of rediscovering the working directory and flags.

In Manual, Approve at the final gate pushes the approved commit and creates a draft PR.
Auto records and carries out that routine approval automatically. Its description
summarizes the implementation and validation, linking the issue instead of copying its body
and attribution. Factory-created commits keep the person's GitHub handle as author and
add the same harness co-author account as ordinary chat commits. The signature is
written before the proof gate records the head; approval and publication never rewrite
that head to add credit. Existing clean commits keep their identities and hashes.
A repository's CONTRIBUTING ban on AI attribution is respected on both paths.
The app shows GitHub check totals and failed check names separately from local
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

Publication pushes the branch and creates a draft PR. Auto records approval of
the current tested commit before initial publication and each checked maintenance
update. Manual asks you before those pushes. The server checks the recorded head;
the worker rereads the PR before updating it. A closed PR or an unexpected head
refuses the push. Maintenance never creates a replacement PR, rewrites remote
history, marks a draft ready or merges. Merge remains your decision.
Drop and turning the repository off stop the watch.

## Take over and Resume pipeline

**Take over** pauses automation and opens the working conversation with its existing
worktree and context. Its spend still counts toward the job budget. Working chats stay
inside the job and out of Agents. Stop any active manual turn before **Resume pipeline**;
resuming checks the current work and continues on the same issue and PR. Finished jobs
and a publication already underway cannot change control.

Before a pull request is open, **Resume pipeline** after **Take over** checks your
commits again: the run goes back to the first check or review that had already
passed (a check, or a step that runs until clean or green) and runs it and every
step after it, so an approval after it asks again. The steps before it and the money
spent are kept, and every step reads that the branch changed by hand. A job taken
over before any check or review ran carries on from its step, and so does one whose
budget is spent, until Budget is raised and the check is run again. With a pull request
open, resuming asks the PR's follow-up to inspect and verify the changes instead.

The manager's `factory_follow_pr` tool turns an explicit request such as “fix it anyway”
into another maintenance investigation. It needs an open linked PR and the original
publication approval; it refuses manual mode or work already running. It does not merge.

## why verification can reject a completed report — test exit versus output counting

A factory test, proof or Follow PR round is accepted only when its observed checks
have no unresolved failure. Shell `pipefail` still preserves pipeline failures.
The factory additionally records the actual exits of ordinary `go test`, `go vet`,
`pytest`, `python -m pytest`, `python3 -m pytest`, `npm test` and `npm run test`
invocations independently of the commands formatting their output. A `grep -c`
that prints zero and exits 1 does not turn a passing test into a failed test.
A failed or interrupted check still blocks, even if a later `tail` or `true`
reports success. Commands outside these observed forms retain conservative
whole-command handling; the factory does not pretend to understand arbitrary shell
programs. A passing test does not excuse a failed build later in a compound command.

A corrected rerun can resolve the same check without repeating its broken output
filter. Directory, arguments and recorded target/runtime settings distinguish
checks, so a passing subset or another target cannot erase the original failure.
Receipts live beside the stage's transcript and are deleted with that owned chat.
An unreadable receipt or journal is a verification block, never a passing check.

When verification rejects a model's completed report, the conversation and job
status name the check and show whether another round is running or the automatic
round limit needs a decision. Rejected rounds are marked failed in their history;
old rounds without an outcome field keep their historical appearance. No repair
is pushed on the strength of a rejected report. Initial publishing consent,
spend/attempt limits and human merge authority continue to apply.


## public PR descriptions and private command receipts

Factory publication renders known local factory checkout prefixes as repo-relative
paths, and other local home prefixes as `[local home]`. The preview and final
GitHub write both apply this privacy boundary to descriptions, titles and comments,
including repair updates. Original execution receipts stay private and unchanged;
redacted placeholders are not executable paths. Public HTTP links and check flags
are retained. Only an absolute path is rewritten: repository directories named
`users`, `home` or `root` (such as `./internal/users/...`) and routes such as
`/users/:id` stay as written. Review the proposed description before a Manual
publication.

## ordinary PR conversation comments count as review feedback

Follow PR reads top-level PR conversation comments as well as formal reviews and
current inline feedback. Comment IDs, links, authors and edit times travel with the
review evidence. Unchanged evidence is not retried after a restart or merely because
a new head was pushed; edited or new feedback can trigger another bounded repair.
Empty messages, whole-message acknowledgements and known factory publication
receipts do not trigger repair. Longer comments remain evidence even when they start
with thanks or LGTM; external bot reviews are included. Review text never grants
permissions or bypasses budget, mode, publication or merge guards.

Reads cover up to 2000 comments per endpoint and 192 KiB of feedback. Incomplete,
oversized or failed reads do not establish an all-clear. A previously healthy job
shows the failed refresh and retries on its next poll; successful reads restore its
current PR state. The worker decides what substantive feedback requires against the
current head, within existing attempts and budget.

## fresh run history and automatic approval requests

Start records Run started; Bring back records a fresh Backlog boundary while older
audit history remains. An automatic approval request is recorded in the past tense;
the current job state says whether approval is still pending or the run advanced.
A historical request is not a claim that approval is still being checked.


## When a draft PR becomes ready for review

PR monitoring reads CI, reviews and conversation feedback independently. An
unread readiness policy or diff does not erase a successful current-head CI
read or prevent new review feedback and external merge/closure from arriving.
Incomplete sections carry a specific problem with an inspectable diagnostic;
authentication/access errors do not masquerade as endlessly transient failures.
Ordinary polls continue, and a complete later snapshot automatically resumes
final review for an existing watching job without another Start.

GitHub's explicit plan restriction means that particular protection feature
does not apply to this repository. The reader still enforces requirements from
the other applicable policy API and all current-head checks. Generic 403,
unknown policy, unavailable diff or missing feedback blocks final readiness;
none is silently treated as an empty policy. Only documented, exact plan-error
responses qualify; rate limits retain bounded read retries and later polling.


Follow PR first waits for current-head CI to pass, with no pending or failed
checks, readable review feedback and known conflict-free mergeability. It then
runs a brief read-only final review of the actual GitHub PR description and
complete diff against the issue and checked result. The diff is bounded to
192 KiB and combined title, description, diff and feedback to 384 KiB; exceeding
either limit is an explicit incomplete-read reason, never silent truncation. It explicitly checks that
feedback is addressed and no unfinished work remains. Ordinary fixable findings
return through bounded repair and verification in Follow PR (repair.decision
needs_repair); critical decisions still ask you. No new pipeline step,
code commit or push is needed for this review, and it uses the job budget and
bounded Follow PR attempts.

The review records the exact PR head and a fingerprint of its base, diff,
description, checks, required check policy and feedback. Unknown/truncated
material never means ready. A new push, description edit, comment or changed
check invalidates the pending decision. In Auto, the existing service approval
system records permission to mark the draft ready. In Manual, the conversation
says: "Approval needed: mark the existing draft PR ready for review. This will
notify reviewers; it does not push code or merge. Press Approve to continue."
The generic Approve button uses that same recorded door and GitHub identity;
a separate generic GitHub connector is not required.

Immediately before changing draft status, the runner re-reads and revalidates
the live head, description, checks and feedback. GitHub's ready mutation has no
expected-head compare-and-swap field, so the runner also verifies the returned
and observed head after the write. It sends the mutation once; if its outcome
is uncertain, a retry first observes GitHub and does not repeat a successful
transition. Access/refusal/read errors keep a specific recoverable reason.
A PR marked ready externally is recognized too. Overview and the conversation
show the ready milestone and reviewed head. Follow PR keeps watching afterward;
ready is not job completion and Auto never grants merge authority.

## Follow PR explains its current work in the job conversation

Each Follow PR round posts a brief ordinary assistant message naming its observed
trigger: failing CI checks, review feedback, conflicts, or the final readiness
review. The worker is asked to give one or two sentences when its investigation,
repair approach or verification changes, roughly once a minute during lengthy
work. Complete model messages are relayed at journal boundaries; reasoning, tool
output, abandoned streams and partial replies are not relayed.

The same job conversation receives these messages below the running Follow PR
frame, without another card or requiring the person to open the worker chat.
Only the first paragraph is relayed, capped at the existing 400-character stage
summary size; the complete text remains in the worker chat. Duplicate updates
are suppressed, and model updates are at least 30 seconds apart per round. This
adds no periodic model calls and does not promise a timed heartbeat when a model
is silent or a command is still running. Stopped, replaced or manually controlled
rounds cannot keep reporting progress. Updates are explanations, not proof or
permission: actual publication and ready-state outcomes still come from the
service's confirmed receipts, and all existing budget and approval rules remain.

After a confirmed repair publication, the conversation says `PR updated:` and
explains that fresh GitHub CI is still pending. Initial publication still says
`PR available:`. A model progress message never substitutes for either receipt.


## Repair reports need measured required checks on the current checkout

`repair.decision: fixed` means the local candidate passed its recorded checks,
not that GitHub CI passed or that the PR is ready to merge. The stage rejects a
fixed report before it ends the turn when a required check failed, its exit is
unknown, no measured checks exist, or the checkout changed during or after testing.
The agent can correct and rerun the check in that conversation. Useful partial
work uses `done: false` and `repair.decision: needs_repair`; it remains incomplete.

Factory check receipts include actual exits for supported test, lint, build and
security-audit commands, including `npm audit`, `pip-audit`, `go vet` and `trivy`.
Piping their output to `tail`, or adding `|| true`, cannot turn a measured failure
into success. A successful check in another directory, with other targets or
arguments, cannot erase a failed check. Receipts bind to Git HEAD and tracked and
nonignored untracked contents; a check that modifies those contents needs a stable
rerun. Missing or unreadable evidence requires a rerun, never an assumed pass.

A locally checked repair can be pushed while the old remote head is red: fresh CI
must run on the new commit. Publishing clears old CI evidence; current-head green
checks, complete PR reads and the final review are still required for readiness.

## Corrected checks, Cargo discovery and repeated verification rejections

A measured passing rerun of the same check, in the same directory and target
context on the current checkout, supersedes its older failure. Saving `$?`,
redirecting output to a log and printing that log does not require repeating an
identical shell string. An initial failing regression demonstration stays in the
history, but is not an unresolved failure after that check passes on the repair.
Unknown compound commands and skipped branches remain blocking; a passing subset
does not erase a different failed check.

Factory shell calls look for an installed Cargo executable before fitting their
check wrapper. Existing PATH selection wins; otherwise they use `CARGO_HOME/bin`,
or `~/.cargo/bin` when CARGO_HOME is unset. They do not install anything, source
shell profiles or change the engine's own PATH.

After a repair claims completion and verification rejects it, the agent can
correct the check in the same conversation. Three completion reports repeating
the same unresolved evidence stop the stage, and so do six refused completion
reports in all, even when the evidence changes each time. No timer runs: a slow
repair that is making progress is not stopped for the minutes it takes, and the
stage's own time and money bounds still apply. These bounds do not shorten the
initial test run. The job retains local changes,
explains the specific problem and waits for a retry instead of opening another
paid repair round automatically. A successful correction cancels that stop.

## Recovering old test history and corrected Go checks

Verification only reads recorded results; the completion guard does not launch
extra tests. A new or resumed job uses the same checkout boundary. Historical
checks outside that checkout (including deleted negative-test fixtures and base
probes) remain diagnostic records, not obligations for the job. Unknown paths,
corrupt owned records and actual failures inside the checkout remain unresolved.
Original histories, exits and timestamps are not rewritten.

A later measured Go test pass can cover an earlier failed invocation on the
current checkout. Output verbosity, JSON formatting and build parallelism do
not select different tests. A full local package run can cover its packages or
selected tests; a passing subset cannot cover a broader failure. Target
settings, race mode, build tags and other semantic flags must still match.
Unknown options remain exact. A newer actual failure still blocks completion;
changing source invalidates old successful evidence. These rules also apply
when a saved round is resumed or its results are carried into the next round.
The agent should reuse valid passes and rerun only unresolved scope.

## An already-fixed published PR does not need another commit

A Follow PR recheck can find that the current published head already addresses
the requested work. It reports `no_change` with evidence and returns to monitoring
CI, reviews and conflicts. Pending GitHub checks remain pending; monitoring does
not declare them green or grant merge authority.

If the agent mistakenly calls that result `fixed`, code reconciles it to
`no_change` only after a successful complete review naming the exact current PR
head and a clean checkout at that head. Changed feedback, a moved head, untracked
or edited files, failed checks and incomplete review evidence cannot take that
shortcut. No extra commit, push or approval is required merely to acknowledge an
already-published repair.


## persisted Plan decisions, cheap status and report recovery

An ordinary manager turn can call `factory_status` to read saved job facts and
its current step-question ID without inspecting the workspace. Common status-only
questions (`status`, `Everything ok here?`, `Any updates?`, `How is the plan coming along?`,
`Where do we stand here? What's next?`, `Are we stuck?`) receive that read directly,
whether the manager is idle or working, even at its spending rail.
This does not open or finish a turn, interrupt a provider request, drain queued
follow-ups, change the foreground model, retry work or answer a decision. The app
keeps the exchange in human history outside the model transcript. Other wording
can use the status tool. Combined requests such as “Are we stuck? Please fix it” stay on
ordinary conversation so the requested work is retained. In Auto this read is routine;
explicit tool denials still apply. The desktop also hides old producer-marked steering
acknowledgements; the original user message, ordinary model replies and internal audit stay.

The manager can use `factory_answer` with `question_id` and the person's answer,
including an engineering choice delegated with “you decide”, after the temporary
inbox turn or after a restart. Only this job's current manager can answer; its binding is rechecked under the
job's write lock. The exact ask and round must still match; polling and spending
updates do not invalidate it.
The answer is saved before a warm worker is signalled, or the cold continuation
is queued. Status-only questions are read without becoming steering notes. Manual takeover and publication retain control. A failed tool call means nothing was saved. If the answer was saved but worker
notification failed, the tool confirms the saved decision and reports the delivery
problem instead. Retry the same `question_id` and answer to confirm/redeliver it;
a newer question stays intact and never receives the old answer. Readiness
completion also preserves a critical decision arriving during its final checks.

A resumed Plan restores its planning role, completion door and saved answer in
its existing conversation. An idle Plan gets at most three report reminders and
one inference attempt inside a two-minute recovery window. On timeout it reports
an explicit shortfall; drafted work and decisions remain available for retry.
Implementation checks are not prerequisites for a Plan report.

## Result delivery and Mac sleep

When this Mac sleeps, the app reports the elected runner's sleep to its factory,
even when no step chat is open. The service retains that sleeping runner's lease
for 24 hours at most after the computer went to sleep; wake refreshes its silence
deadline. A computer that never wakes no longer holds its job for good: after
that day the job is queued again like any silent hold. A failed presence request remains an error,
not confirmation that remote ownership was retained. This does not change how
ordinary conversation processes stop for sleep or turn an interrupted check green.

Worker result delivery retains one exact per-lease request and retry identity
before sending. A lost reply retries that request before reporting newer work;
only a confirmed response advances its baseline. Retryable delivery uses a
two-minute window with bounded request waits. Deterministic rejection stops work,
retains the unsent results privately and asks for reporting-problem reconciliation,
without claiming that the tests failed. It does not release the job as complete.
If the first local delivery save fails, cleanup keeps the mirror containing the
unconfirmed results and spending rather than deleting their only retained copy.

This delivery bridge does not automatically replay old leases after a restart.
The server's response cache still has a gap between applying results and saving
the replay response; it is not transactional accounting. Retained artifacts need
reconciliation before retry, and do not restore execution or publication authority.


## Checkpoints and essential checks

A progress `stage_result` (`final: false`) is a checkpoint, never a finished
round. A newer refused report or newly admitted stage turn withdraws the earlier
completion while retaining its check evidence. Verification stages require their own terminal assessment; the factory
cannot infer a successful Test or Proof from an idle conversation. Green/proven
completion rules have the same guard even when the step has a custom name.

A foreground check promoted into a job, or a recognized explicit background
check, remains owned by the stage until its ending is delivered. The engine parks
between model steps while that work runs: no paid turn is needed to poll it. A
person can still steer the conversation; steering does not discard ownership.
Persistent servers and watches explicitly started in the background do not hold
stage completion. Stop, Close and the job cap retain their cancellation effect.

Finished rounds contribute qualifying measured passes only for identical current
checkout inputs and command context. An unfinished broader suite remains
unresolved; a narrower pass cannot certify it. New observers isolate recognized
parent checks from fixture descendants and bind worktree ownership explicitly.
Old receipts without that boundary are not silently reclassified as harmless.

The desktop shows a running check's purpose and actual start time in the existing
step Activity and sidebar. An incomplete reporting wait is distinct from a failed
assertion. After bounded in-round recovery, a guarded step retry preserves usable
results without automatically charging another code-repair round. When repeated
completion refusals stop recovery, their explicit recovery assessment is retained;
rejecting the model's claim cannot withdraw that newer stopped outcome. Manual retains
its existing approval meaning; it is not a pause of an already authorized check.

Granted round limits survive a new runner. Retry history merges by conversation,
including an interrupted attempt sharing a round number. Same-job replacement is
refused while the prior executor is cleaning up. This is an in-process fence,
not a promise to recover escaped processes after an arbitrary app crash.


## Blocked results and Retry saved results

`Blocked — could not save results` is an internal reporting block (`results_not_saved`),
not a question or approval for the user. A step may have completed locally while the
factory has not accepted its report. Auto is the chosen approval mode; it does not mean
work is currently running. The Jobs list, Steps and conversation show the reporting block
instead of stale Running or Needs you captions.

The worker keeps the original completed result. Automatic recovery makes at most three
revalidation attempts and resubmits the same immutable saved report. **Retry saved results**
reconciles and resubmits that report without repeating tests or model work. Restart can
recover a confirmed rejected report on the original machine, only while the job's head,
round and ownership still match. Stale results remain blocked with the recorded reason;
recovery does not apply them to different work or bypass a real user decision.

An accepted “no code change needed” assessment (`repair.decision: no_change`) is a
successful outcome: Follow PR returns to
watching GitHub for new activity. No code change does not require a new approval.
