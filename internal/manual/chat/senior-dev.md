# senior-dev

## What /senior-dev does — hand one large change to senior-dev, an autonomous coding agent

`/senior-dev <brief>` hands the whole brief to **senior-dev**, an autonomous coding agent
codeaf carries. At a shell the same program is `codeaf senior-dev <brief>`. It is built
into codeaf and runs only through it: there is nothing to install and no senior-dev of
its own to start.

It works alone in a copy of your folder. It writes your brief down word for word, reads
the repository, keeps a checklist of what the brief asks for, pins a command that shows
the work passes, and edits until it believes the change is done. Then it **submits**:
the tree is frozen at that moment, so nothing it does afterwards can change what it hands
back. It then runs the project's own build and tests on the frozen tree, and if anything
moved after it submitted, the tree is put back to what it submitted.

Use it for one change big enough to want an agent of its own for an hour, and specified
well enough that nobody will be asked anything: a rewrite across a package, a migration,
a feature with its tests. A change you would make in a few steps is not worth it.

## How do I ask senior-dev for a change — writing the brief, what to put in it

The brief is everything senior-dev knows about what you want. It is saved as
`.senior-dev/spec.md` in its copy exactly as you wrote it, and it is read back from there
whenever senior-dev summarises its own history, so the words you chose are never
paraphrased away.

Write it the way you would hand work to someone who cannot reach you:

- the files, packages or commands involved, by name;
- what done means, and how to check it (the test to run, the output to see);
- the constraints: what must not change, and the wrong answer to avoid.

In the chat, `/senior-dev` followed by the brief starts it as a task. At a shell, flags go
before the brief, and `--` ends them: `codeaf senior-dev run --variant high -- rename the
config loader`. Everything from the first word that is not a flag onwards is the brief,
so a flag written after the brief becomes part of it.

## Running senior-dev on a repository you have not cloned — a benchmark task, another project

senior-dev works in a copy of the folder it is handed, and only what it changes in that
copy lands. So it has to be handed the repository the work belongs in.

In the chat, ask for the work and name the repository, and the commit if the work names
one. The model clones it first, into a new folder, onto a branch at that commit, and hands
senior-dev that folder. A benchmark task works this way: senior-dev gets a copy of the
project's own repository and not of the benchmark's, so the benchmark's files, its
reference solution among them, are not in its copy.

At a shell, clone the repository yourself, then run `codeaf senior-dev` inside it or pass
the folder with `--dir`.

A brief that tells senior-dev to make a checkout of its own somewhere else does not work.
It has no copy of that folder, so nothing it does there lands: its file tools refuse to
write outside its copy, and what a shell command changes out there stays where it is.

## What senior-dev cannot do — it cannot ask you anything, no step cap, no Windows

**It cannot ask you anything.** Nobody is at its keyboard: a question its model tries to
ask is turned down inside the program, and after three it is told questions are not
available. Put everything it would stop and ask into the brief.

**It has no step cap.** It is held to the conversation's dollar and time ceilings instead,
and codeaf enforces both from outside whatever it does.

**It reaches a model only through codeaf.** It holds no key and reads none; a
`senior-dev.json` in your folder that sets `apiKey`, `baseURL` or `providerRouting` is
refused by name, because codeaf decides which model service serves each call.

**It writes only inside its copy.** Its file tools (`write`, `edit`, `apply_patch`)
refuse any path outside the folder it was handed, including one reached through a link,
and say so to its model; it can still read files elsewhere. Its shell is not fenced the
same way, and nothing a shell command changes outside the copy lands.

**It keeps its record in git**, unless it runs `--in-place`; from the chat, codeaf chooses
that for a folder with no git history (see the section on plain folders).

**On Windows it is absent**: there is no `/senior-dev` and no `codeaf senior-dev`. Its
engine needs a Unix shell, process groups and file locks, so Windows builds leave it out
rather than carry something that fails every time.

## senior-dev on a folder that is not a git repository — a plain folder, no git, --in-place

From the chat, codeaf reads the task's folder before it starts senior-dev. A repository
with at least one commit gets a copy, as every task does. **A folder with no git history —
a plain folder, or a repository with no commit yet — has nothing to copy from**, so
senior-dev works in that folder itself, and codeaf starts it with `--in-place`: it commits
nothing, and keeps its checkpoints outside the folder.

When it ends there is nothing to commit, because its changes are already in the folder.
The task's page says `its work is in <folder>, which has no git history, so nothing was
committed`. Its `.senior-dev/` notes (the brief, its checklist) stay in the folder
afterwards; delete them when you are done with them.

At a shell, pass `--in-place` yourself. Without it senior-dev stops at once with
`workspace is not a git repository: <folder>; run with --in-place to work in a plain
folder`.

To have its work isolated and landed as one commit instead, make the folder a repository
with a first commit (`git init`, `git add -A`, `git commit`) before you ask.

## Where senior-dev's work lands — one squashed commit on your branch

senior-dev commits every file it writes inside its copy (`wip(write): <path>`,
`wip(edit): <path>`), which is how it keeps a record to restore from. None of those
commits reaches your branch. When the run ends, they are squashed into **one commit**
whose subject is `task:` and the task's title, and whose body is senior-dev's own ending;
that commit comes home the way every task's work does.

The ending keeps two witnesses apart: what senior-dev's model said it did when it
submitted (`senior-dev's model said: …`) and what senior-dev itself saw when it ran the
project's build and tests (`senior-dev observed: …`). Read the second for "did it work".

Its own notes live in `.senior-dev/` in the copy: the brief, its checklist, the command
it pinned and its session database. That folder is kept out of git, so it never lands.

When a run changed nothing, there is nothing to land and the task says so. On a folder
with no git history nothing is committed at all: the work is already in the folder.

## What a senior-dev run costs — model calls, the dollar ceiling, which models

Every model call senior-dev makes goes through codeaf, which serves each run its own
model API. So every call is priced like one of codeaf's own, shows in the conversation's
total and in `/cost`, and is held to the run's dollar ceiling: **codeaf refuses the call
that would cross it**, before it is made. A refused call ends senior-dev's turn; it runs
the project's build and tests on the tree it has, and ends there, and the task says
`senior-dev reached the run's dollar ceiling of $5.00: …` with senior-dev's own words
after it.

The time ceiling is kept by senior-dev as well as by codeaf. It holds back the last part
of its time to land: two fifteenths of the run, at least 45 seconds, at most 12 minutes,
and never more than a quarter of it. When that window opens it gets one last turn to
submit.

senior-dev picks its model call by call from its own list of open models, and avoids one
for a while after it fails. `--high` replaces the list, and `--variant` sets the
reasoning effort every call asks for. **When none of your model services can serve the
model it asks for**, codeaf answers the call on the run's own work model — the one a
task's own worker would use — and the conversation on the task page names the model that
answered. A call is never refused only because this machine does not know a model's id.

## senior-dev's flags — run, --variant, --in-place, --high, --max-cost

`codeaf senior-dev <brief>` is `codeaf senior-dev run -- <brief>`. codeaf gives every
program it carries four flags:

- `--dir DIR` — the folder to work in (the current one by default);
- `--max-cost USD` and `--max-hours H` — the ceilings;
- `--json` — the program's records on stdout instead of readable lines.

senior-dev's own flags on `run`:

- `--variant NAME` — reasoning effort sent with every call: `low`, `medium`, `high`,
  `xhigh`; unset leaves the model's own default;
- `--in-place` — work in a folder without git: no commits, and its checkpoints kept
  outside the folder;
- `--high`, `--low`, `--frontier` — comma-separated models it routes among; `--low`
  (its history summaries) and `--frontier` fall back to `--high`.

`codeaf senior-dev help` describes it and its one command, `run`;
`codeaf senior-dev run --help` prints all of them, codeaf's four included.

## Why did senior-dev stop — how a run ends, its log, crashed or stopped

A run ends in one of these ways, and the task's ending says which:

- `finished: …` — it submitted, and the words after say what the project's build and
  tests did on the frozen tree;
- `senior-dev did not finish: …` — it ended without submitting, or what it submitted fails
  the project's own build or tests;
- `senior-dev reached the run's dollar ceiling of $5.00: …` — codeaf refused a model call
  at the dollar ceiling; the words after are senior-dev's own ending;
- `senior-dev stopped on its own ceiling: …` — it stopped itself at the time ceiling;
- `senior-dev crashed: …` — the program itself broke, or could not start (no brief, a
  refused `senior-dev.json`, no git repository at a shell without `--in-place`);
- `stopped by the run: …` — you, or the run it belonged to, stopped it; what follows is
  what senior-dev said on its way out, usually `stopped before it finished`.

When it ends without submitting, it still checks the tree it leaves. If the project's
tests cannot even start there, the tree is put back to the last state whose build and
tests could run, or to where it began.

Everything senior-dev said while it worked (each stage and what it knew at the time)
is kept in `delegate-stderr.log` in the task's record folder. A run started at a shell has
no task, so its record — that log, its conversation with codeaf and its stages — is kept
in a folder of its own under `~/.codeaf/v3/carried/senior-dev/`, one per run.
