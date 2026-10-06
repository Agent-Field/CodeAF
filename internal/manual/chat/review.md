# review — a code review of a GitHub pull request

## What /review is — review a pull request, code review of a PR, get findings on a GitHub PR, pr-af

`review` is a program codeaf carries that reviews one GitHub pull request the way a careful reviewer would: it reads the change, plans which parts to look at, sends a separate reviewer at each, checks what they found against the code, challenges each finding, looks for what nobody covered, and hands back every finding — most urgent first, each with its file and lines, why it matters, whether it should block the merge and a suggested fix.

It is pr-af, the pull-request reviewer from AgentField, built into codeaf under the name `review`. There is nothing to install and no key to set: every model call it makes goes through codeaf, so it is priced into your spending and held to the run's ceiling like any program's (see *Programs codeaf carries*). It does not run on its own outside codeaf.

**It changes nothing and posts nothing.** It checks the pull request out in a folder of its own, which it removes when it ends, and reads nothing of your folder but its git remote and branch. Its answer comes back to the conversation, and its full report goes to the task's record folder. Posting a review to GitHub is a separate step you say yes to.

## How do I review a pull request — /review, codeaf review, review this PR, the link, owner/repo#123, focus on

In the chat, type `/review` and the pull request, then anything the reviewers should weigh:

```
/review https://github.com/owner/repo/pull/123 focus on the retry logic
/review owner/repo#123
/review #123 is the error handling safe
/review
```

The pull request can be its link, `owner/repo#123`, or `#123` when the conversation's folder is a checkout whose `origin` is on GitHub. Everything else in the brief goes to every reviewer as guidance. It starts at once, shows on the rail which stage it is in, and can be stopped.

**Asking in words works too.** "Review PR 123 in owner/repo" makes the chat propose the work with `via: "review"`; its brief is the pull request and the focus, and that is all it is handed.

At a shell, `codeaf review <pull request> [focus]` reviews it from the folder you are in, or the one `--dir` names. `--max-cost` and `--max-hours` set its ceilings; `codeaf review run --help` lists every flag.

## Review my current branch's pull request — a bare /review, this branch, no link

`/review` on its own, or a brief that names no pull request, reviews **the open pull request of the branch the conversation's folder is on**, with the brief as the reviewers' focus. `gh pr view` finds it when `gh` is installed; otherwise it asks GitHub for an open pull request from the branch's upstream (a fork's too) into `origin`'s repository. It reviews the pull request as GitHub has it, not your uncommitted edits: push first. With none it ends at once, for example `review did not finish: the branch fix-it has no open pull request on owner/repo; name the pull request, such as /review owner/repo#123`.

## Private repositories — GH_TOKEN, GITHUB_TOKEN, gh auth login, repository not found

A public repository needs nothing. For a private one it uses `GH_TOKEN`, else `GITHUB_TOKEN`, from the environment codeaf was started in, else the token `gh auth token` answers when you have signed in with `gh auth login`. The token reaches git only for the one clone, in its environment, and is never written into the checkout. Without one, a private repository fails to clone: `review did not finish: could not check out owner/repo#123: git clone failed: …`.

## What /review does, step by step — why a review takes so long, how it works, the stages

Its stages are on the rail and head the lines of its page:

1. **setup**: finds the pull request and checks it out.
2. **intake** and **anatomy**: reads what the pull request says it does, and maps the change.
3. **plan**: chooses what to review, through three lenses (semantic, mechanical, systemic).
4. **review**: one reviewer per part of the change, up to eight at once; each reviewer that finishes is a line under it with what it found.
5. **filter**, **evidence**, **challenge**, **deepen**, **cross-ref**: drops findings not worth raising, checks each against the code, argues against it, deepens what stands, and looks for findings that interact.
6. **obligations** and **coverage**: lists what the change must do and checks it, and looks for gaps.
7. **report**.

Each reviewer is an agent session that reads the checkout, told it is part of a code review, with four read-only tools — read a file, list a folder, find files by name, look for text in files — for as many turns and minutes as its part of the review is given — from ten turns for the smallest to thirty for a reviewer — after which it is made to answer. At a shell, `--max-turns` and `--session-wall` set one limit for every reviewer instead. A review of a mid-sized pull request commonly takes half an hour to an hour.

## What /review shows while it runs — each reviewer's line, what it found, which files

Its page shows each stage once as the review enters it, then one line per finished step under it with what it found: `reviewed internal/auth/middleware.go, internal/auth/token.go and 2 more` · `3 findings`, and the step opened shows the first findings by title. A step that failed says `failed` and why. `ctrl+y` shows the raw model calls.

## What /review reports and where the full review is — review-report.md, review-report.json, blocking, severity

When it finishes, the chat is handed a short account whose first line says what it found — `Review of owner/repo#123 (Add retries): 5 findings, 2 blocking the merge (1 critical, 1 important, 3 suggestion).` — then each finding on a line, blocking first: severity, title, `file:line` and a one-line fix. The account ends `Nothing was posted to GitHub.` and names the full report, two files in the task's record folder:

- `review-report.md` — every finding in full: where, why, whether it blocks and why, the suggested fix and the code it rests on
- `review-report.json` — the review as data, including the review it would post and the commit it reviewed

A shell run's record folder is under `~/.codeaf/v3/carried/review/`, one folder per run.

## Post the review to GitHub — comment on the pull request, request changes, /review post

`review` never posts during a review. When a review found something, the chat offers to post it and waits; only after you say yes does it propose `review` with the brief `post <the review-report.json path>`, which you can also type yourself: `/review post ~/.codeaf/v3/…/review-report.json` (the record folder works too). That run posts the review **as it stands** — its summary, its inline comments, and its event (a request for changes when something blocks, a comment when nothing does, an approval when it found nothing) — on the commit that was reviewed, even if the pull request has moved since. Posting needs a token that can write to the repository (see *Private repositories*); without one it ends `review did not finish: nothing was posted: posting to owner/repo#123 needs a GitHub token; set GH_TOKEN or sign in with gh auth login`. GitHub refuses a request for changes on your own pull request, so there it goes as a comment, and the account says so.

## What a code review costs and how long it takes — limit, ceiling, $5, two hours

A run nobody set a limit for stops at **$5 and two hours**, or sooner where this conversation has less left; `/budget conversation` can lower that, and at a shell `--max-cost` and `--max-hours` set either. At 80% of its dollar ceiling it stops starting new reviewers and finishes with what it has, and its account says it covers part of the change.

It keeps three minutes of its time ceiling back to write its review. **A review its time ceiling cuts still reports**, and says so in its second line: `It reached its time ceiling of 2h while reviewing, so the reviewers still running were stopped and their parts of the change were not reviewed.` A run that reaches a ceiling before it has any review says `it reached the run's dollar ceiling before it finished, so it has no review` (or `time ceiling`).

**Which models.** From the chat, the conversation's working seat reads the code and its light seat makes the single structured calls; a model you name for the hand-off reads the code instead. At a shell, `--model` and `--light` name them, else the profile's work seat does both.

## Fixing what /review found — hand the findings to senior-dev, fix the blocking findings

When the review found something, the chat also offers to hand the blocking findings to senior-dev as one piece of work, when the conversation's folder is the pull request's checkout, and names which. It starts nothing until you say yes. You can also ask directly: "fix the two blocking findings with senior-dev".

## What /review cannot do — GitLab, local changes, uncommitted work, telling it something, Windows

- **It reviews GitHub pull requests only**: not GitLab or other hosts, not a branch with no pull request, and not uncommitted edits in your folder. Push and open the pull request first.
- **It runs nothing**: no tests, builds or your app. What it finds, it finds by reading.
- **It cannot be told anything while it runs.** Stop it and start it again with other words.
- **It never posts on its own**: posting is its own run, on your yes.
- **It is absent on Windows**, as every program codeaf carries is there.
