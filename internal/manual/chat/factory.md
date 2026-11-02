# The factory

## what is the factory — jobs, runners and a pull request at the end

The factory takes work that is already written down, such as a GitHub issue or a brief handed over from a chat, and runs it through the steps of its repository's pipeline on a runner. A person is asked at fixed points, and a job that changes code ends as a pull request, which its assignee can merge from the job itself. Nothing starts by surprise: a job a rule does not allow asks first, and a repository without `.codeaf/factory.yaml` runs on the defaults, under which nothing starts on its own.

Three things make one factory:

- the **service**, started with `codeaf factory serve`, which holds the jobs and the people;
- one or more **runners**, machines that pull a job and run it with their owner's keys;
- the **people**, each signed in with their own GitHub.

A machine with no factory set up has none of this: the chat's factory tools are absent from its belt rather than present and failing.

## codeaf factory serve — host a team factory, listen address and HTTPS

`codeaf factory` (the same as `codeaf factory serve`) runs the service and prints the address to give people, plus one operator access link. It listens on `127.0.0.1:7420` unless `--listen host:port` says otherwise, or the `listen` key of `~/.codeaf/factory/config.yaml` does (`--config path` picks another file). It runs no job itself: Run queues a job, and a runner that has joined pulls it. With no runner attached a job stays queued.

### Jobs at once and queued work

**Jobs at once** is the one number that limits how many jobs a computer runs
together. A newly attached computer defaults to **15 jobs at once**. Change it
in **Settings → Factory → Your computers**, with the stepper beside each of
your computers; it is owner-only. The supported range is 1–100. An existing
saved limit is kept on restart and upgrade, including a previously chosen limit
of two. A change applies within about five seconds without restarting.
Lowering the limit lets admitted jobs finish and withholds new jobs until there
is room. Raising it admits queued work up to the new limit. There are no CPU or
memory limits per job: the computer's own system shares them.

Jobs over the limit wait in the queue, oldest first. When the owner's eligible
computers are full, queued jobs say **Waiting for capacity** and show the
number of occupied execution slots and the configured limit. The notice points
to Settings → Factory → Your computers and offers **Change jobs at once**. An
offline or unknown runner is reported as waiting for a runner, without
inventing a full-capacity count. The manager calls these execution slots, never
benches, and gives the same Settings location.

A queued job whose work is already on one computer can only run there. While
that computer is offline or asleep, the job says **Waiting for <computer>
(offline for 3 h)** instead, and no free slot elsewhere or Jobs at once setting
starts it. Only that computer's own person sees its name; anyone else reads
**Waiting for its computer**.

Two overrides act on one queued job, and only its person may use them:

- **Start now** lets that job take one slot above Jobs at once, this one time,
  so it starts without waiting for another job to finish. It reads **Starting
  now** until a computer takes it. A job that goes back to the queue later
  waits like any other. Start on a Backlog job only queues it at the top. On a
  job waiting for a computer that is down, Start now is refused with that
  reason and changes nothing.
- **Hold** keeps a queued job from starting; it reads **Held**. **Resume** puts
  it back in the queue, and Start now on a held job lifts the hold and starts it.

Both are recorded in the factory's activity with who made them.

A computer attaches once per slot each time its factory moves to a new
address; the factory inside the app takes a new port on every start. When the
same person's same computer attaches again, the factory forgets its older
registrations that have been silent past the runner timeout and hold no job,
so the list of registrations does not grow without end.

Idle Follow PR monitoring uses the factory's background GitHub poll, with no
execution slot or model turn. New actionable review feedback, failing CI or a
conflict queues a bounded maintenance pass. Only the worker executing that pass
holds a slot; completing it releases capacity and resumes background monitoring.
Budgets, ownership, publication consent and duplicate-repair protection still apply.

The config keys are `name`, `listen`, `access_token`, `repos` (a list of `owner/name`), `github_client_id` (an OAuth app with device flow on), `team` (GitHub handles), `mode` (`solo` or `team`) and the HTTPS keys `tls`, `domain`, `cert_file`, `key_file`, `cache_dir` and `trusted_proxies`. Unknown keys are refused. Serving plain HTTP on a non-loopback address prints a warning, because access tokens and sign-in sessions would cross the network unencrypted. `--model slug` and `--one-model` pin the model its triage reads use. `--new-operator-link` prints a fresh one-time operator recovery link. Only one service runs per codeaf home (`serve.lock`); a second one says so.

## give someone access — codeaf factory token, revoke, revoke-sessions and name

The service is joined with an access link, which the operator creates for one person and which can expire. Commands on the machine that hosts it:

- `codeaf factory token add [--expires 7d]` mints an access token and prints it alone; `codeaf factory token list` lists the live ones by id; `codeaf factory revoke <id>` ends one.
- `codeaf factory revoke-sessions` ends every signed-in session at once; people sign in again.
- `codeaf factory name` shows or changes the name the factory goes by.
- `codeaf factory steps` prints the built-in step prompts; `codeaf factory convert` turns a Markdown pipeline into `.codeaf/factory.yaml`.

An access link looks like `codeaf://factory/join?url=https://factory.example#token=…`. The secret is in the part after `#`, so it is never sent as part of an address. A link checks, in this order: the factory answers, it is not revoked, not expired, not used up, and the person's GitHub can see at least one of its repositories. Each failure is one plain sentence.

## codeaf attach and codeaf detach — use another machine's factory

`codeaf attach <url> [--name n] [--token t]` writes `attach.json` in the factory folder (`~/.codeaf/v3/factory`, readable by you only) so this machine's windows and runners use that factory instead of serving their own. It does not dial the factory: a team box that is down now is still the one you mean. `--name` is the name this machine goes by there (its hostname by default) and `--token` is a session you already hold. `codeaf detach` removes the file, and this machine's own factory is used again. Only `http://` and `https://` addresses are accepted, and a link with a user name in it is refused.

## the app keeps a factory sign-in — signed out after a restart, keychain, sign out removes it

In the desktop app, a sign-in to a team factory (GitHub sign-in, an access link, or a pasted token) is kept in the system keychain, one item per factory, and handed back when the app next opens, so a restart does not sign you out. It is never written to the app's saved factory list or to a log. Signing out, ending every session, and disconnecting the factory each remove the kept sign-in. A kept sign-in the factory no longer accepts reads **Signed out** once and is removed; sign in again. The factory on your own machine keeps no sign-in: the app makes its key again at each start.

## codeaf runner join — run a factory's jobs on this machine, sign in with GitHub

`codeaf runner join <link>` joins this computer to a factory and runs its jobs here until you press ctrl-c. `<link>` is an access link (`codeaf://factory/join?url=…#token=…`); you sign in with GitHub on the page it names, using the code it prints, and the factory issues a session for you alone, so the runner works as you and never as the operator. Other spellings: `<secret> --url <factory>` (the link's secret, with the address spelled separately), or `<url> --token <session>` when you already hold a session. `--name` names the machine. `--attach-only` writes the attachment and returns, which is what `codeaf attach` does too.

A runner runs only its owner's jobs, with its owner's keys; keys are shown by name and never by value. One process per codeaf home holds the runner role; a second `runner join` shares the first one's session and waits to take over when it stops, so joining twice registers one machine. An access link that was used or expired is refused with a plain sentence and no session is kept. The service has no separate expiring runner token: a link made to expire in an hour is the one-time command.

## from chat to the factory — factory_add, factory_recipe, factory_item and the manager's tools

On a machine with a factory set up, a conversation can hand work over. Every one of these raises a card and writes nothing until you say yes; none of them starts a job. The terminal chat draws the `factory_item` card (answered as described below); see the limit in that section for the other two.

- `factory_add` offers one piece of work (a title, a brief, a repository) to the factory. Your `add it` writes the item, as new.
- `factory_recipe` offers one line for a repository's recipe: a stage, a policy or a habit, saved to `.codeaf/factory.yaml` on your `bank it`.
- `factory_item` offers a change to the one item a conversation is about: its stages, gate, budget or thinking, within the recipe's bounds.

Banking a recipe never writes into a folder held by a running program. A separate change worktree can still be written when it is outside that hold. If the change has to be saved in the held checkout, nothing is written: wait for that run to end, or stop it, then bank the line again. A Git error and your confirmation do not bypass this check.

The conversation that manages an item also has `factory_run` (shape its run), `factory_start` (start, resume or continue it when you ask), `factory_follow_pr` (request maintenance of the same open PR after publication approval) `factory_status` (read current saved progress and its pending decision), and `factory_answer` (answer the temporary inbox question or save an answer to the exact persisted step question; send critical decisions to you). The foreman conversation has `factory_floor` (read the whole floor and select items for you to run). These are absent from every other conversation, from `--once` runs and from windows over `--host` or `--at`.

## the default steps — what a job runs with no factory.yaml, how many rounds, security pass

A repository without `.codeaf/factory.yaml` runs the shipped steps, and so does every job on it, whether a person started it or a start rule did: the same steps, asks and rounds. `codeaf factory steps` prints each one with its prompt, its until and its rounds.

- **issue**: plan › approve › write › test › review › proof.

| Step | Until | Rounds |
| --- | --- | --- |
| plan, write, read, checks, bisect, fix | done | 2 |
| test | green | 3 |
| review, security | clean | 3 |
| proof | proven | 3 |

A step that falls short goes round again until its rounds run out, and only then asks you what to do. New jobs default to Auto, which passes routine optional approvals and publishes passing proof as a draft PR within the job budget. Manual asks before enabled approval gates and publication; the job header switch saves that choice. Fixed gates and critical unresolved decisions still ask. Merge remains your decision. proof is fixed: an edit cannot reword or skip it. A repository can explicitly fix an approval too. security is shipped but in no list: a job typed with the word `security` (`fix the login, security`) gains it after review, and a pipeline file names it to run it. A pipeline file's `max:` replaces a shipped step's rounds; a step of the file's own, under a name codeaf does not ship, runs once unless it says `max:`.

## the factory's own rules — rules for every repository, instructions, protected paths, ask first, never, "don't touch auth without checking with me"

A repository's rules live in its `.codeaf/factory.yaml` (`rules:` and `start: {ask_first, never}`); in the app, the factory's Steps tab, in its Repository view, edits them with its pipeline, as one pull request, and a repository with no file yet gets one. **Use on this factory** runs the edit at once instead (see the next section). The factory can also keep **its own rules**, for every repository it watches, so a rule like "don't touch auth without checking with me" is said once instead of copied into each repository. They are kept by the factory itself (on a team, the shared server, not one person's machine), and only its operator can change them; everyone signed in can read them. In the app they are the Global view of the factory's **Steps** tab.

They hold three lists:

- **rules**: sentences every step of every job is told, below the repository's own, in a "rules for this work" paragraph. A step that would break one stops and asks instead. A sentence codeaf recognises, such as "never close an issue" or "tests pass before anything posts", is also applied by code, exactly like the repository's.
- **ask first**: protected paths, written `touch auth/`, and sentences a job asks about before it starts on its own, like `start.ask_first`. In the app the paths are **Protected folders and files**.
- **never**: kept so older files still work. A path listed here is a protected path, exactly like `touch <path>` under ask first; it is not held more strictly or briefed differently. Write new ones under ask first.

The factory's rules only ever ADD to a repository's: they can never remove or loosen anything its file says. A job carries the rules that stood when it was queued, so a runner on another machine follows the factory's rules, not its own machine's. A rules file the factory cannot read refuses to start jobs rather than starting them without the rules.

## use edited steps now — Use on this factory, the factory's own copy of a repository's steps, no pull request, stop using it

A repository's steps are its `.codeaf/factory.yaml`, read from the main branch, so an edit made in the app's Steps tab (Repository view) runs only once its pull request merges. **Use on this factory**, beside **Open pull request**, saves the edited file in the factory itself instead, as **its own copy** of that repository's steps: no push and no pull request. It is checked exactly as a pull request's file is, and the copy is the very file that pull request would write.

- Jobs this factory starts afterwards run the copy instead of main's file. The factory's own rules and protected paths still apply on top, as they do on main's file.
- A job's file is decided once, the first time it starts (or when a start label stamps its steps, if that comes first): the copy, when the factory keeps one then, else main's file. It keeps that file for its life, and starting it again does not change it: saving or stopping a copy changes only jobs not started yet. **Bring back** forgets it with the job's steps, and its next start decides again.
- A job carries that file, so a runner on another machine runs the same steps, and everything that bounds or starts the job reads it: an edit of its steps (from its tab, its manager or the `factory_item` card), its manager's brief, its plan's bounds, its rules (ask first, never), what it may post, the budget a start gives it and how it merges. A job decided on main's file reads main's file, never a copy saved later. Its record says `Runs this factory's copy of .codeaf/factory.yaml, saved <when> by @<who>, not main's`, and the log says `recipe from factory copy`.
- The page says what is in effect: `This factory uses its own copy of these steps, saved <when> by @<who>.` and whether main has no file yet or a different one. **Open pull request** still shares the steps with the repository.
- When main's file becomes the same file (the pull request merged), the copy clears itself and new jobs run main's file. **Stop using this copy** makes new jobs run main's file; jobs already started keep the copy they started with. When the file cannot be shown (GitHub does not answer, or the copy spells settings the factory has not confirmed), the factory still says it keeps a copy, who saved it and when, so it can still be stopped.
- Precedence: the factory's copy, else main's file, else the built-in steps.

Only the factory's operator may save or stop a copy, as with the factory's own rules; on a factory you run yourself, that is you. The copy is kept on the factory (on a team, the shared server), never in one Mac's settings. A copy the factory cannot read refuses to start jobs rather than quietly running main's steps. Steps that use kind, when, effort or fanout are saved as a copy only when the factory has confirmed it runs them for the file they were edited from, as for a pull request. A factory older than this feature answers `This factory doesn't support that yet; nothing was changed. Update the factory to use it.`

## a rule asks first — protected paths, Auto paused at the plan, a plan that touches auth, why a job waits on a rule

Ask-first paths and never paths are one kind of rule, **protected paths**. When a job starts, the protected paths it starts under (the repository's and the factory's) go with it, and every step is told them on one line, "ask before touching: auth/, infra/". A job is held at its start, after its plan and before its change is published whenever a file it names or changes is under one (`touch auth/`, or a bare `auth`, covers everything under `auth/`). The approval is never routine, even in Auto: the job waits with `protected · auth/ · auth/login.go`, shown as "A protected path: auth/ · auth/login.go". A plan step is told to end its plan with its file list in the one shape the factory reads, a line that says only `Files:` and then a `- path` line per file, outside any code block; nothing else in a plan is read as its files. A plan without that list waits with `protected · auth/ · the plan names no files` (every protected path in force), shown as "The plan didn't list its files, so the factory couldn't check them against auth/. Approve, or send it back to list them." Answering it clears the hold. The job keeps what each check found: on its Steps panel, **Before it starts** and **Before publishing** show a green ◆ and "passed" when nothing a rule guards was touched, and stay amber with "asked" when a rule made the job wait for you. A job a person started was not checked at its start, so that row shows no finding. Ask-first sentences, which nothing can check against files, are told on their own line, "ask before you: …", and hold a job before it starts with "ask first ·" and the sentence.

## pick up issues — labelled work starts by itself, its labels, who owns those jobs

**Pick up issues** is the factory's switch for starting new issues by itself. It is not a job's Auto: Auto lets one running job pass its routine approvals, while Pick up issues lets the factory start jobs nobody started. It is off until you turn it on, from the yellow switch on the factory's Jobs tab; Labels beside it lists the labels it picks up. While it is on, the factory starts an issue by itself when it carries one of those labels, or its repository's own `start.labels`, and clears every rule (size, cost, ask first, never). An issue a rule flags waits with "Approval needed: start ·" and the rule. A job started this way belongs to the factory's owner (its operator) unless the issue is assigned to someone, so Auto can carry it and its approvals come to the owner. While it is off, a factory on your own machine starts nothing a person did not start; a team server always runs its repositories' own start labels, and adds the factory's only while it is on. Turning it off stops the next start at once.

## let the plan reshape the steps — adapt, ask, fixed, a pipeline tailored to each job

A job's plan step may change the steps the rest of the job runs, within bounds codeaf holds in code: it may add a step, switch one on or skip one, but never skip proof, a fixed step or a step a rule names, and never touch a step that already ran. How much freedom it has is one word:

- **adapt** (the default): the plan reshapes the steps for this issue, and the run goes on.
- **ask**: the plan may reshape them, and a person approves before anything after the plan runs.
- **fixed**: the steps run as the pipeline wrote them; only you can change them.

A repository says it with `adapt:` in its `.codeaf/factory.yaml` (one word for every pipeline in the file). The factory can say it for every repository (the Steps tab's Global view, "Plans may reshape a job's steps"), and like every factory rule it only tightens: a repository that says fixed stays fixed, and the factory's ask makes an adapt repository ask. A personal `~/.codeaf/factory.yaml` may tighten it too, never loosen it. The word travels with the job, so a runner on another machine holds the same one. Your own changes to a job's steps are never bound by it.

## issues only — pull requests are outcomes, old PR jobs removed

Issues are the factory's only work items, including briefs handed over from chat. GitHub imports issues only; pull requests never become queue rows or review jobs. New jobs cannot select a PR or standalone CI pipeline.

An issue keeps the PR it opens, its checks, reviews and merge status. One final Follow PR step handles bounded CI, requested-review and conflict repairs until merge or closure. Initial publication approval permits checked maintenance pushes to the same PR within budget; merge remains your decision. Take over pauses automation for an ordinary working conversation; Resume pipeline checks that work and continues. On upgrade, the factory permanently removes old standalone PR jobs, their factory-owned conversations, local worktrees, generated local branches and activity records, including worker copies. No archive of those jobs is kept. Issue jobs and their linked PR information remain. Nothing on GitHub is deleted or changed by this cleanup.

## change one job's steps — landed, proof has started, add after proof, switch on a step

An edit changes one job's steps, not the repository's pipeline. A step that has already run cannot be changed. The recipe's fixed steps cannot be reworded or skipped; proof cannot be skipped.

Once a job has landed, a nonempty step edit is refused with `the job has landed, so the steps can no longer be edited`, whether or not it had a proof step. Landed jobs do not offer step editing.

Proof is the step named proof, a send-back's prove step, or any step that runs until proven, whatever it is called; checks and posts are not proof. Before landing, once any proof has started, a nonempty step edit is refused with `proof has started, so the steps can no longer be edited`. A refused step edit leaves the job, its proof and its recorded changes as they were. An empty edit changes nothing.

Before proof starts, new work belongs before the earliest proof step, so proof can see it. Adding work after proof, switching on work there, or changing its ask or thinking is refused. A person's approval gate may follow proof, provided it is added before proof starts; it does no work. Skipping unstarted work after proof is allowed within the recipe's existing bounds.

## give one step more or fewer tries — max tries, 1 to 3, how many times a step runs

On the app's job page, the job's person can set how many tries one conversation step of that job gets, from 1 to 3, before the step runs. A try that does not meet the step's rule (until clean, until proven) goes again until the step runs out of tries, then the job asks you. The change is for this job only, never the repository's steps.

It is refused above three or below one (`a step gets 1 to 3 tries`), for a check or an approval (`test is a check or an approval; only a conversation step's tries change here`; a check says its rounds when it is added), for a step that has run, a fixed step or a step after proof, and from anyone but the job's person (`only the person sets how many tries a step gets`). A factory older than the change answers that it doesn't support that yet.

## remove or move a step of one job — delete a step, reorder, run it after, skip versus remove

Skipping a step leaves it in the job, switched off. Removing it takes it out of the job's steps altogether; moving it puts it after another step, or first. Both change this job only, never the repository's pipeline, and both go through the same edit and bounds as every other change: tell the job's manager (`remove pretest`, `run docs after write`), or use the step's menu on the job's tab.

A step that has already run, is running, waits on you or failed is never removed or moved, and nothing is moved before one. Proof is never removed or moved (`you may not remove proof; proof vouches for the work before it`), nothing but your approval or a post is moved after proof, and a fixed step or a step a rule names stays (`review is fixed by the recipe`, `you may not move test; the policy names it`). Only you remove or move an approval (`only you may remove approve, where the run holds for you`) or a post (`only you may move pr, which posts outward`); the manager may move a conversation step across an approval. A post is never moved before the proof or approval it stood after (`you may not move pr before approve; nothing posts before what holds it`), and an approval is never moved after a post it stood before (`you may not move approve after pr; nothing posts before what holds it`). Under a recipe's `fixed` only you change the steps, and under `ask` the manager's change gets an approval after plan, as for any edit. A job keeps at least one step, and a step moved to where it already stands changes nothing.

An edit is whole or nothing: a step removed is not also changed by the same edit, nor moved after, and a step is moved once per edit. The job's record says `manager removed pretest` and `you moved docs after write` (or `moved docs first`), after any set, added, switched on and skipped lines.

A removal or a move reaches the run at once, from whichever door makes it (the job's tab, its manager or the `factory_item` card): a removed step never runs and a moved step runs in its new place, whether the job is queued or running; the steps that already began keep their place. A factory older than remove and move answers `This factory doesn't support that yet; nothing was changed. Update the factory to use it.`


## two steps with the same name — step names in capitals, "more than one stage is named", cannot change a step by name

A step is found by its name without regard to case, so `tidy`, `Tidy` and ` TIDY ` all reach a step stored as `Tidy`, and the answer spells it as it is stored: nothing is renamed. Jobs made by this build never have two steps whose names differ only in case, and a step added with a name an existing step already has in another case is refused as `there is already a stage named tidy`. An older saved job can have two steps with one name (`tidy` and `tidy`, or `Tidy` and `tidy`). Naming such a step in an edit, in the `factory_item` card's step changes or in a skip or switch-on, is refused whole with `more than one stage is named tidy, so it cannot be changed by name until the steps have one name each`, and nothing in that edit is applied, not even a budget or a note on the same card. The `factory_item` thinking change names only the conversation steps it would change (not begun, not fixed, not already thinking that way): it is refused when one of those shares its name with another step, and leaves the pair alone, refusing nothing, when neither is one it would change, as when a step spelled exactly that way has already run. There is no command that renames a step, so for now such a job's steps cannot be edited by name. A step that has already run stays protected under whichever spelling it was saved with. The same lookup places a new step: `after tidy` or `before review`, whether typed in the step's ask or given as the edit's after, lands next to the step stored as `Tidy`, and the answer says `added inspect after Tidy`. A name two steps answer to refuses the edit whole. Under a recipe's `ask`, the approve step lands after a step stored as `Plan` too, and two steps named plan refuse the manager's or plan's change whole with `more than one stage is named plan, so there is no one place to ask for approval until the steps have one name each`. A step the recipe file fixes stays fixed under any spelling of its name, so `Review` is refused as `Review is fixed by the recipe`.

## change one job's thinking — think strong, think cheap, back to usual, fixed steps, landed

The `factory_item` card's thinking (`think strong`, `think cheap`, `default` for usual) sets how hard the job's conversation steps that have not begun will think, each step on its own. It goes through the same step edit as above, as you, so a recipe set to `fixed` does not stop a card that changes only the thinking (a card that also adds or skips a step is still refused whole there). A step that has already run, was rewound by a rerun, or is fixed by the recipe keeps its thinking, the rest still change, and the card names the steps that change rather than the first. A step that is switched off is changed too, so it thinks that way when it is switched on later; the card marks it, as `security(off) cheap → strong`. A conversation step placed after proof refuses the whole card, nothing saved. A landed job, or one whose proof has started, refuses the change with the sentences above, budget and note included. Asking for the thinking a job already has changes nothing and is never refused.

After your yes, the answer names the thinking the item was saved with, step by step (`thinking write strong, test strong`; `default` resets the changed steps to usual), and the steps that did not take it: `left as they were: plan cheap`. A budget or note alone says nothing about thinking. If another field changes but no step needs the requested thinking, the answer says `thinking unchanged`. A later turn's opening note lists thinking per running step when they differ (`thinking plan cheap, write strong`), one word when they agree, and nothing when none was set. A level other than cheap, strong or default is refused before any card, as `Invalid arguments: effort is one of cheap, strong, default, or left out.`

## answer the factory_item card in the terminal — yes, keep it, say what to change, esc, the card never appeared

In the terminal chat a `factory_item` call puts its card above the message box: a head that asks the question (`#12 · think strong?`), one row for each thing that would change (`thinking  write — → strong`, `budget  $5 → $8`, `note:`, `why:`), and two answers.

- `1` **yes** changes the item exactly as the card shows, once. Pressing it again, or pressing enter, finds the card gone and changes nothing more.
- `2` **keep it** changes nothing.
- Typing a sentence under `say what to change… (enter sends it)` and pressing enter is a correction, never a yes: the item stays as it was and the chat is handed your words so it can propose again.
- `esc` puts the card off for later: nothing is answered and nothing changes, and `space` over an empty box brings it back.
- `enter` over an empty box never answers the card, wherever the pointer stands. Only `1` or a click on the yes row does.

Interrupting the turn takes the card down unanswered, and so does leaving it for 15 minutes; either way the item is untouched and the model is told so. A tool permission question for `factory_item` can come first. It is a separate question, and allowing it changes nothing on the item.

What changed in the terminal chat: it used to draw no `factory_item` card, and the call sat at `running factory_item` until you interrupted. It now draws a plain question block with the same box, keys and receipt as any other card, not a special item card. The card shows only rows that would change, so a fixed or begun step is absent from it; the chat is told afterwards which steps were left as they were. A yes sent from the desktop app with text attached is read as a correction and changes nothing. The `factory_add` and `factory_recipe` cards are not drawn here, so those calls wait until the turn is interrupted or 15 minutes pass.

## ready for review — draft PR, final review, automatic recovery, Resume pipeline

When current-head CI passes, Follow PR checks the actual PR diff, description,
feedback and unfinished work. In Auto, a passing final code review automatically
marks the draft **Ready for Review** through the factory's existing GitHub
connection. It does not wait for a human approving review. Manual asks before
this transition. Requested feedback must be assessed and addressed; GitHub's
required reviewer approvals still apply to the final merge.

Transient connection or server failures while marking ready receive up to three
attempts in Auto. Each attempt re-reads the PR first: an uncertain success is
observed, never blindly replayed; changed facts require a fresh final check,
and permission denials do not retry as network errors.

A zero-match output counter is not a failed test. The factory records actual
check exits separately from output formatting. A missing final-review checklist
is returned to the model while it can correct its report. An unresolved
execution problem receives up to two automatic correction turns in the same
conversation, within the live job budget and stage wall; remaining shortfalls
use the step's bounded rounds. A critical decision, exhausted allowance or
missing access can still pause the job, with the specific cause shown.

**Resume pipeline** after a failed final review retries that review, even if the
PR head is unchanged. Returning from manual work or sending explicit repair
instructions verifies that work first. Publication rechecks the live PR facts;
changed code, CI or feedback cannot reuse stale readiness approval. Marking ready
never grants merge authority. The repository's separate auto-merge setting is
the explicit opt-in for automatic merge.

## merge a job's pull request — factory auto-merge when checks pass, or why it will not merge

A job whose pull request is open has **Merge**. Only its assignee may press it, and pressing waits five seconds with **Undo** before anything is sent. Then the runner that holds the assignee's GitHub account reads the pull request again and merges it, handing GitHub the exact commit, so a branch that moved is refused. The job is done when the pull request reads merged.

One check, in code, decides every merge; no model does, and no tool on any belt merges. It refuses, in these words, when:

- the pull request has commits you did not approve (a push after the proof was approved needs the proof approved again);
- the pull request is a draft, or has conflicts, or GitHub has not said whether it can merge yet;
- a check is failing or still running, or a reviewer asked for changes, or the branch needs approving reviews;
- the job's pull request is someone else's: codeaf merges only pull requests it opened.

**Auto-merge** is off unless the repository's `.codeaf/factory.yaml` says `merge: {auto: on}` (`method: merge`, `squash` or `rebase`; omitted lets the repository's settings choose). A personal `~/.codeaf/factory.yaml` may only turn it off. The setting is fixed on a job when its proof is approved. An auto-merge also needs every claim proven and at least one passing check, and until then it waits quietly. A merge that failed is the person's to look at, says why, and is never tried again on its own.

## stop a job and start it again — Stop, Undo a stop, Run again, stopped by you, what a stopped job keeps

**Stop** ends a running, queued or waiting job at once. When it cut a step that was running or waiting, the job says so and waits for you: `Step stopped`, `Stopped by you`. That step reads failed with the same words, so no step reads running, and what the run spent stays on the job. A job stopped before any step began, or after its last step ended, lost nothing: it simply goes back to Backlog with no mark, ready to start. Either way the job's log says `stopped · branch kept`. A no to a step that ran out of rounds, to the budget, or to an approve step with nothing before it is the same stop. A job whose run already ended on a failure or a stop has nothing to stop and refuses with `#12 is not running`.

**Undo**, for five seconds after your own Stop, puts the run back exactly as it stood: the same step, its conversation, what it spent and the question it was asking.

**Run again** starts a job that says `Step stopped` over, taking the stop mark off; a job stopped with no mark has **Start** instead. It begins a new run at the first step. What the stopped run's steps reported is not carried over, and the job's spend starts again from nothing; its budget stays what it was. What is kept is the job's worktree and branch on the runner that ran it: the new run works in that same folder, on that branch, with every commit and every unsaved edit the stopped run left there. A different runner that picks the job up makes a fresh branch from the repository's default branch, because a job's branch leaves its machine only when the pull request is opened. Your notes on the job are kept, and every step's brief still carries them.

To go on from a step that was cut instead, keeping the steps before it and the money spent, run that step again (try again) rather than starting the job.

## drop a job — Drop, delete a job's branch, uncommitted work, throw away a job's work and its conversations

**Drop** takes a job off the list and throws away everything of its work on this machine: a running job is stopped first, then its worktree folder, its local branch (`factory/<number>-<title>`), its own conversation, every step's conversation from every round, and the team the factory made for it (`#12 · <title>`, under the `factory` team) are deleted, and the job reads done. Nothing on GitHub is touched: a branch already pushed and a pull request already opened stay where they are, and a changed issue does not revive a dropped job. A conversation you put in the job's team yourself is not deleted; it only leaves the team. Only the job's assignee, or anyone while it has none, may drop it.

**Drop asks first when there is work to lose**: commits on the job's branch, or changes in its folder that were never committed. It is refused with a sentence naming them, such as `codeaf#1844's branch has 3 commits and uncommitted changes.`, and nothing is stopped or deleted; dropping it again and saying yes to discarding that work deletes it. A branch that could not be read counts as work. What was said in its conversations is not asked about: dropping a job drops what was said about it. Dropping a running job waits for its step to stop.

If a delete fails, the job is dropped anyway, no longer names what was kept, and its activity says so: `dropped · the local branch and folder were kept:` or `dropped · its conversations were not all deleted:` and why. A team that cannot be deleted is closed instead. A job that ran on another computer of a team factory keeps its folder and conversations there.

A dropped job cannot be changed: opening its chat, and `factory_item` or `factory_run` on it, are refused with `#12 was dropped · Bring back returns it to Backlog`.

## bring back a dropped job — Bring back, undo a drop, restart a dropped job, fresh start, old conversation still there

**Bring back** is the one way out of a drop. Only a dropped job offers it, and only to the person who may act on the job. The job returns to Backlog as a fresh job, as if it had never been worked on: no steps run, nothing spent, no failure, no pull request, and no conversation, because the drop deleted them. Nothing starts; your start runs it from the first step on a new branch, and opening its chat starts a new conversation. A pull request it had opened stays on GitHub, no longer linked to the job. Starting a dropped job is refused with `#12 was dropped · Bring back returns it to Backlog`, and a job that was not dropped with `only a dropped job can be brought back`.

**Fresh means nothing said earlier comes back.** Your notes for its steps are cleared, and its steps are the repository's recipe again, with any change you or its agent made to them undone. What stays is what the job is: its issue, assignee, priority, budget, the model you chose for it, and its activity.

**Anything of the old run still here goes first**, before the job is back: a folder or branch something made again after the drop, and the conversations and team of a job dropped before drops deleted them. If one cannot be deleted, the job still comes back and its activity says `brought back · the local branch and folder were kept:` or `brought back · its conversations were not all deleted:` and why; the job no longer names it, so its next run never reads it.

## comment on a job's issue — teammates' comments, does the manager read GitHub comments, write access

A comment typed on a job's Issue tab is posted to the job's GitHub issue as you, with your own GitHub sign-in; a solo factory uses its host's sign-in. Only a job from a GitHub issue takes one, up to 8000 characters. A sign-in GitHub will not let comment there is refused with "Your GitHub sign-in can't comment on owner/repo", and nothing is posted. If GitHub's answer is lost, the comment may have posted: read the issue before posting again.

The job's manager reads every new comment on the issue once the factory's next GitHub read brings it, about a minute. Comments already there when the job first read the issue are its context. Under each comment it read, one line says what it did:

- from someone without write access to the repository: "read as context · no write access to the repo";
- on a job with a run under way: "asked you in the manager chat whether the plan should change", and the manager conversation quotes the comment and asks. Reply there with what to do;
- on a job that has not started, or has finished: nothing changes, and the line says why.

The manager never changes the plan from a comment on its own yet, even one that only narrows the work.

## starting a job checks GitHub first — closed issue, wasn't started, Check GitHub now, last checked

The factory reads GitHub on its own about once a minute, but those reads list open issues only, so a job never learns from them that its issue was closed. So when a person starts a job that came from GitHub (start, run or run again), the factory reads that one issue again first, in one request. If it was closed on GitHub, the job is not started: it is marked done (closed) and the answer is one sentence, such as `#12 was closed on GitHub, so it wasn't started.` If GitHub cannot be reached, the job starts anyway on what the factory already knows.

**Check GitHub now** asks the factory to read GitHub at once instead of on its next tick. It answers straight away and does not wait for the read; what the read finds arrives the way every read does. While it reads the factory says so, and afterwards it says when GitHub was last read without a failure, or what went wrong (`token refused`, `not reachable`). A factory whose poll is not running (no token, nothing watched) says nothing about GitHub at all.

## a job refused for a missing key — no key, which has no key, add a key for its service

On a factory on your own machine, a person's Start, Run, Start now or Run again is refused before anything is written when the job's own choice (or the default, when the job names none) is on a service with no key: "This job can't start: its model, …, is on …, which has no key." Only that choice counts: a call on a service with no key is reported, never moved to a fallback, so a fallback with a key would not be asked. Add a key for that service in Settings, or choose another for the job. A team server never checks, since it holds none of its people's keys, and neither does a profile set to one model for every task, which checks that model itself.

## a job's repository isn't checked out — clone, cloning, private repository, Start refused, not checked out on this machine

**The factory gets the repository itself.** When a job's repository has no checkout on the computer that runs it (codeaf was never opened in its folder), the run's first step clones it into the factory's own folder, `~/.codeaf/v3/factory/repos/<owner>/<name>`, and the job's log says `cloning acme/api…` while it does. The clone goes over https with the same GitHub connection the factory reads and pushes with, never over ssh, so a private repository clones too. From then on it is that repository's checkout for every job: it is never cloned twice, and two jobs started together share the one clone. Every job still works in its own worktree and branch, never in the checkout itself.

**A start is refused only when nothing can be cloned**, and before anything about the job changes: `acme/api isn't checked out on this machine · connect GitHub so the factory can clone it.` when GitHub is not connected, and `notes isn't checked out on this machine · watch it in Repos so the factory can clone it, or open codeaf in its folder.` for a repository the factory cannot find on GitHub. Start, the chat's `factory_start` and a new job started at once all refuse in the same words. A job's page can tell in advance: its repository is checked out, will be cloned on the first step, or cannot be.

**A clone that fails** (a repository that was renamed or deleted, a token that cannot read it) fails the job's first step with git's reason, such as `could not clone acme/api · repository not found`, and leaves nothing half-made behind; Run again tries the clone again.

## turn a repository off — Off, stop watching a repo, pause a repository, where did its jobs go, turn it back on

**Off means the factory stops working on that repository entirely.** Its jobs leave the queue, in every state. GitHub is not read for it and its pull requests are not followed. No job of it is started or asked about by its labels, or read by a model. Starting, running, running a step again, publishing or merging one of its jobs is refused with `acme/api is off · turn it on in Repos to run its jobs.`, and so is a new job on it. A job page already open still shows the job and says its repository is off. Stop, Drop, priority and who a job is for still work.

**Turning it off asks before stopping work.** While any of its jobs is running, queued or waiting on you, the switch is refused with a sentence naming them, such as `acme/api has 1 job running (api#12) and 1 waiting on you (api#7).`, and nothing changes. Saying yes stops each of those jobs exactly as its own Stop would (its branch and work are kept, and a cut step is marked stopped by you), then turns the repository off. If one will not stop, the repository stays on and the answer names that job. Changing the whole list at once asks once, naming the work on every repository it turns off.

**Turning it back on brings every job back as it was.** Nothing is deleted or made again: each job returns in the state it was left, GitHub is read again from where it stopped, and a job stopped on the way off waits for your Start or Run again.

A factory that was never given repositories to watch has no switch, so nothing on it is off.

## how big is a job — size of a job, complexity, the size: filter, why there is no size, risky, no price guess

A job's size comes from the first of these that exists, and nothing else is a size:

1. **A size label** on the item (`size:S`, `size/s`, XS to XL). It wins.
2. **The completed issue read**, shown as `read says M`. The read scores five factors 0, 1 or 2, each with a why of at most six words: scope (one spot, one area, several areas), clarity (is the wanted result stated), coupling (shared code, data, migrations, public API), proof (how a finished change is checked) and novelty (follows existing code or new design). Their sum is the complexity, 0 to 10: 0–3 is small (S), 4–6 medium (M), 7–10 large (L). The read sees only the item's text, not the repository, so its scope and coupling are marked low confidence.

The length of an issue's body is not a size, and neither is the guess on a chat card. An item with neither shows no size, as nothing at all. `size:s` finds only items whose shown size is S; `cheap` matches items the read scored small; `risky` matches when the read named a risk or the shown size is L. Editing an item's title or body drops the read's size until the next read.

Only a size label decides whether a job starts on its own. A size from a read never starts, holds or orders a job; with no label the factory asks instead, saying "no size label".

The read never guesses a price: a model cannot know what the factory's agent spends, so no read, card or chat guess writes dollars on a job. The floor tool's `cheapest` lists the least complex first and each row says `complexity 4/10, medium (read)`.

Limits: items read before this change show no size until their text changes or someone labels them. Older items are not reread just to fill this gap; editing the title or body allows one fresh read through the normal worker. There is no "read again" action yet.

## what will a job cost — estimate, cost range, from N jobs, no cost history yet, max_cost

A job's cost and time are measured from the factory's own finished jobs, never guessed by a model. An open job's range is the median to the 80th percentile of up to 12 finished jobs nearest to it: the same repository first, then every repository; near means close on the five complexity factors and the same kind of work. The app shows it with its basis, as `$0.40–1.10 · from 12 jobs in api`, and minutes the same way. A dropped job is not counted, because it spent only what it spent before it was dropped; a job run again counts once, with its latest run.

With fewer than five similar finished jobs there is no figure: the job shows its band and `no cost history yet`.

Each finished job is checked against the range the jobs before it would have given; the factory shows how many landed inside (`8 of 10 inside the range`). When fewer than 6 in 10 land inside, ranges are widened by how far real spend strayed from the median.

Start rules: jobs like this one that cost more than `max_cost` ask first (`jobs like it cost up to $3 · over $2`). An unknown cost asks first only when the repository's file writes `max_cost` (`no cost history yet · max_cost $2 asks first`); under the default ceiling an unknown cost starts, because the job's budget still stops a running job.

## which model runs factory jobs — Task models and one model

Factory reads, manager turns and step conversations follow Task models. With one model selected, every text role uses that exact model on its selected connection. A missing, unreachable or mismatched choice refuses the turn; it does not choose a fallback. The daily crew cap also refuses new factory admission when reached. A job's own spending ceiling remains separate.

An explicit `--one-model` launch keeps its pin. Interactive chats keep their own model. Under the saved one-model policy, a factory manager turn uses its governed factory conversation. If an open chat holds that manager conversation, close it before the background turn can proceed. A running conversation keeps the model it started with.



## pick one step's model for one job — a step's own model, the job's model, use_model

On the app's job page, the job's person can pick the model one conversation step of that job runs on, before the step runs. The step then opens on that model and does not choose models automatically; a model chosen for the whole job does not move it. Choosing the job's model again gives the step back. The pick changes only this job, never the repository's steps.

A pick is refused when the step has already run (`you may not change review, which has already run`), when it is a check or an approval (`test is a check or an approval and runs no model`), when the recipe fixes the step, when the step comes after proof, and when anyone but the job's person makes it (`only the person picks the model a step runs on`). The app offers only models it lists; another is refused as `vendor/x is not a model this engine offers now; models.list names them`. A factory older than the pick answers that it doesn't support that yet. The manager and the plan cannot pick a step's model.
## budget recovery and external PR commits

Spending-owned Follow PR stops keep a durable budget cause. In Auto, raising
Budget wakes a fresh GitHub read and releases only that stop, within the existing
repair-attempt limit. Manual mode, takeover, critical questions, exhausted attempts
and uncertain publication retain their own recovery controls. Cumulative spend,
local work and other contributors' commits are kept. Interrupted automatic step
recovery keeps its attempts while waiting for budget: time spent waiting is not
an attempt, and raising a limit does not grant more retries.

GitHub observations continue while spending is blocked. Before paid Follow PR
work resumes, the runner reads the existing PR again. Merge or closure ends the
watch; changed commits are fetched and reconciled without force-pushing. Failure
to read or reconcile is reported, never treated as permission to overwrite work.

A new head on an already-ready PR needs complete current-head CI and a fresh
read-only diff, description and feedback review. Observation alone does not mark
that head checked. Only a successful review of the exact clean checkout records
it; the job then returns to monitoring without pushing, marking ready again or
merging. Incomplete facts keep it waiting; ordinary findings return to bounded
repair. New feedback is included even when the remote head did not change.

## corrected test commands and saved verification — invalid Cargo arguments, retry Follow PR

A malformed Cargo test invocation is an execution problem, not proof of a failing test. Follow PR accepts later measured checks that cover the same test scope on the current checkout: a corrected multi-name invocation, all named tests individually, or the matching full target. A passing subset, other target, environment or stale checkout cannot clear it. Expected regression failures on old code stay in history; passing checks on the repaired checkout can replace them. Unknown commands keep exact matching.

A retry reuses saved measured checks only while the checkout and scope still match. It keeps the old transcript and charges, and never replays a commit or push. Changes invalidate the saved results. An older Auto job stopped only by the malformed-Cargo guard can receive one bounded recheck after GitHub is refreshed and its saved checks pass the corrected rules. Manual, takeover, Stop, closed jobs, exhausted budgets and genuine decisions remain paused.

## review tracking while a repair is paused — outdated inline comments, contributor agreement

Review comments are tracked by stable identity and edited content. An outdated diff position does not resolve a thread. Resolving or moving an old comment does not start another paid repair. A finding is marked handled after its repair completes, not when a worker starts. Completion resets that repair episode's attempt count; later feedback gets its own bounded episode. Reopening a resolved thread is actionable again. Partial feedback reads cannot claim all concerns were handled.

GitHub observations continue while a repair is paused. The job shows pending feedback separately; eligible Auto execution pauses can resume for new actionable feedback within repair and spending limits. A contributor agreement remains a GitHub merge prerequisite and is shown as CLA pending, not running CI. The factory does not sign it, and it does not stop independent review maintenance.

## runner recovery window — running time versus waiting for reconnect

Automatic interrupted-step recovery allows three retries, counted in attempts, not minutes: a long step that loses its connection a few times keeps its retries however long it has run. The only clock is for the runner: a queued retry waits up to 15 minutes for its runner to come back, so a laptop closed briefly does not turn into needs you. A computer that went to sleep holds its job for 24 hours at most; after that the job is queued again like any silent hold. Waiting, backoff and attempts survive restarts and never replay uncertain publication. A person's explicit Retry remains available after exhaustion; saved checks and previous work are retained.
