# Working in this repo

## Which surface is which

Three chat surfaces live here. Getting this wrong wastes a whole recon pass, so check
before you read.

| Path | What it is |
| --- | --- |
| `internal/tui3` | **v3 — the live surface.** Entry `cmd/aforge/chatv3.go`. Bare `aforge` and `aforge chat` both open it. |
| `internal/session` | **the v3 engine** — the agent, the turn loop, the toolbelt, tasks. |
| `internal/tui2` | REMOVED as a surface on 2026-08-31. What remains (`tokens`, `blocks`, `prose`, `modelui`, `reltime`) is the shared component library v3 draws with. |
| `internal/tui` | v1, and the visual north star: restrained, dim telemetry, no borders. |
| `internal/head`, `internal/resident` | the v1 **resident** — a different product in the same binary. |

v3 is a **session you sit in front of**. The resident is an employee that keeps working
while the terminal is closed. They share a repository and almost nothing else — do not
carry vocabulary or assumptions between them.

## Branches — where work goes

`dev` is the trunk and the default branch. Five rules, and they are here rather
than only in `docs/rules/` because they are the ones that must never be looked up:

- **Branch off `dev`, and open the pull request against `dev`.** Never against
  `main`, which is parked fifteen hundred commits back at the released v0.1.0.
- **Never push directly to `dev`, `staging` or `main`, and never force-push any
  of the three.**
- **`staging` moves by fast-forward onto a commit that is already on `dev`** —
  `git push origin <sha>:staging`, never a merge. `main` is not in the pipeline
  yet and nothing promotes to it.
- **Nothing publishes by itself.** A release is a semver tag on a commit that is
  on `staging`, cut by a person; the workflow refuses a tag that is anywhere else.
- **Every pull request carries a change entry** in `docs/changes/unreleased/` —
  `make changelog-new PR=<n> KIND=<kind> SLUG=<slug>`, and the `check` job
  demands it.

The pull-request gate into `dev` is deliberately light — build, vet, the packed
corpora, the change entry, the manual law, a few minutes. The whole suite runs on the way into
`staging` and nightly against `dev`. So **`dev` is where things are allowed to be
briefly wrong**, which is the trade that keeps it fast, and the reason a commit
soaks on `dev` for a couple of days before anyone promotes it.

**AND IF YOUR MEMORY OF THIS REPOSITORY IS OLDER THAN A FEW DAYS, READ
`docs/changes/unreleased/` BEFORE ACTING ON IT.** That is what those entries are
for, and it is the one thing `git log` cannot tell you. They do not say what
shipped; they say what somebody now believes **wrongly** — the branch that
stopped existing, the default that moved, the refusal that became a capability.
This has cost real hours: this file ordered work pushed to `origin chat-v3-task`
for days after that branch stopped existing, and `generate_image` was documented
as impossible right up until the wave that shipped it. In both cases the code was
right, the tests were green, and what was wrong was what somebody remembered.

```sh
grep -rn 'invalidates' -A6 docs/changes/unreleased/    # everything that moved
grep -rln 'surface:.*chat' docs/changes/unreleased/    # only the v3 surface
```

When you land a change, write yours the same way: what was true, and what is true
now. `docs/rules/changelog.md` says why it cannot be generated from the diff.

Read on demand, not up front: [docs/rules/branching.md](docs/rules/branching.md)
for the model and why promotion is a fast-forward,
[docs/rules/ci.md](docs/rules/ci.md) for what runs where and the known-red ledger
in `.github/known-red.txt`, [docs/rules/changelog.md](docs/rules/changelog.md)
for what an entry carries, [docs/rules/promotion.md](docs/rules/promotion.md)
for the promote-and-release runbook.

None of it is enforced by the server yet — the org is on the free plan and a
private repository gets no branch rules there, so today every line above is
convention. `.github/rulesets/` holds the rules ready to apply.

## Build and ship — the owner's standing orders

- **Always build with `make build`**, which writes `bin/aforge`. Never a bare
  `go build -o` to some other path: `bin/aforge` is the ONE binary the owner
  runs, and every stray copy becomes a shadow that rolls them back silently
  (the root `./aforge` did it once, `~/.agentfield/bin/aforge` did it again on
  2026-08-24 — if a shipped feature "stopped working", run `which -a aforge`
  and `shasum` before debugging anything).
- Rebuild after every merge. Never `cp` over a binary that may be running —
  `rm` first, then install — or the next launch dies with `Killed: 9`.
- **Finished work is pushed and opened as a pull request against `dev`** in the
  same wave — never left sitting on a local branch or an unpushed worktree. If
  the shared checkout is dirty with another session's work, push through a
  temporary detached worktree (`git worktree add --detach … origin/dev`) rather
  than touching their tree. (`chat-v3-task` was the trunk until 2026-08-31 and no
  longer exists; anything still naming it is stale.)

`make check` is vet, the tests, the build, and the binary-size ratchet in `SIZE-BUDGET`.
The performance laws it and the suite enforce — and the rule that changing any cap
changes the doc in the same commit — are in [PERF.md](PERF.md).

`make demo-home` builds a **throwaway home with something on every place** — three
projects, twelve conversations, standing orders, memories, a fourteen-day spending
ledger — and opens `bin/aforge` against it with `HOME` pointed there. Use it when you
want to SEE a page full: on a machine that has just started using aforge the standing,
memory and spend pages correctly draw nothing, which is indistinguishable from a page
that is broken. It never touches `~/.aforge`. The seeder is `cmd/aforge-demo-home` and
`docs/design/home-rethink/HANDOFF.md` says what is in the fixture and how to add to it.

```sh
make demo-home                                       # a fresh one, in a temp directory
make demo-home DEMO_HOME=/tmp/aforge-demo            # somewhere you can name
make demo-home DEMO_HOME=/tmp/aforge-demo KEEP=1     # open the one that is already there
```

## THE MANUAL LAW — a feature is not done until the manual knows about it

`internal/manual/chat/` is v3's own account of itself, compiled into the binary. The
running chat reads it with the `manual` tool to answer "what can you do?", "what does this
key do?", "why did you just do that?". It is the **only** authoritative source about
aforge for the model: its training data does not contain this program, so anything not in
those pages is something the chat will either improvise or deny having.

**So: if you add, change, or remove a feature, you update the pages in the same change.**
That means a new slash command, a new key, a new tool on the belt, a changed default, a
new limit, a refusal whose wording changed. Not a follow-up task — the same change.

Three gates fail the build if you forget:

| Gate | What it demands |
| --- | --- |
| `internal/tui3/manual_test.go` | every slash command **and every alias**, spelled with its leading slash, appears somewhere in the corpus |
| `internal/session/manual_test.go` | every tool on the belt appears by its exact registered name |
| `internal/manual/chat_test.go` | every probe in its table — real questions in a person's own words (over a hundred by now) — still reaches the page that answers it |

The failure message names the exact missing string.

The corpus ships **packed** — `make build` generates ignored
`internal/manual/{pages,chat}.pack.gz` artifacts from the folders and selects them for the
release binary (see `internal/packed`). Edit and commit only the Markdown. Ordinary
`go build`/`go test` embed that Markdown directly so a clean checkout works without generated
files; `make test-packed-manual` exercises the compressed path the shipped binary uses.

**When a question reaches the wrong page, fix the page, never the test.** Write the
asker's vocabulary into a `## ` heading — people search for "saved" where a writer wrote
"kept", for "delete a file" where a writer wrote "write". Retrieval is the feature; a page
that is complete, correct and unreachable is a page the chat talks over the top of.

Rules for the pages themselves:

- `# Title` once at the top; every topic gets its own `## ` heading, because headings are
  the search index. Keep each section under ~2000 characters and **self-contained** — it
  gets retrieved and read with none of its neighbours.
- Write what the code does, not what a design doc intends. Quote person-facing strings
  exactly as the code spells them.
- **State limits and refusals.** Someone asking the manual usually wants to know whether
  something is possible; "no, and here is what it says instead" is the most useful answer
  on the page. Never describe half-built machinery as though it worked.
- **When you make something possible, hunt down the page that says it isn't.** The gates
  check that a name is *mentioned*, never that the claim around it is true — a tool named
  in a "what aforge cannot do" section satisfies them perfectly while lying. So a lane that
  lands a capability greps the corpus for the old denial and removes it in the same change.
  `generate_image` was documented as impossible right up until the wave that shipped it.
- The chat's corpus may not use resident vocabulary (`alt+1`, the board, the self page,
  standing watches, front desk). A test enforces this.

The resident's own pages are `internal/manual/pages/` and are a **separate corpus** — the
two cannot reach each other, and a page name may not exist in both.

Also true and easy to forget: `internal/session/prompts/system.md` is what the model is
told it can do. If a tool is conditional or absent, the prompt must not promise it. It has
been wrong before — it advertised `note`/`forget` (which need a `Config.MemoryFile` that
no door sets) and claimed `read` lists directories (it is `os.ReadFile`; a directory is an
error).

## Design laws the codebase enforces

Violations get rejected in review, and some are pinned by tests.

- **The emptiness law.** Unknown or zero renders as *nothing* — never `$0.00`, never
  `0 tok`. (One deliberate exception: the live status line keeps `$0.00` so its segments
  do not jump sideways. `/status` and `/cost` drop the line.)
- **No machinery vocabulary in anything a person reads.** `auditor`, `verdict`,
  `verified`, `refuted` are banned. Work is *running*, *finishing*, *done*, *incomplete*,
  or *your call*. (`needs your look`, `awaiting review` and `unverified` were the old
  spellings of that last one and are deleted — docs/design/task-states/DESIGN.md.)
- **A capability that cannot work is absent, not broken.** A tool with nothing behind it
  is left off the belt entirely, so the model does not have the verb — rather than
  present and failing every time it is called. `memoryTools` and the remote harness
  designer are both written this way.
- **Comments are full-sentence prose** stating the *why*, with ALL-CAPS for a stated law.
  Match the surrounding density; this codebase comments heavily and deliberately.
- **Every icon comes from the vocabulary, through its one door — in EVERY surface
  package.** `internal/tui2/tokens` holds every mark a person sees — task states, the step
  gutter's action families, file kinds, chrome — each a slot with three spellings (a Font
  Awesome 4 icon, the geometric floor, one ASCII character for a screen reader), resolved by
  `tokens.GlyphSet.Glyph(id)` and reached from the surface through `palette.glyph` /
  `app.icon` in `internal/tui3` and `Model.icon` in `internal/tui`. A mark spelled as a
  literal draws the plain floor forever, because a literal cannot know which repertoire the
  terminal is on. One terminal shows ONE tier everywhere: every surface folds the Display row
  (`step icons`) over `tokens.DetectGlyphSet` the same way. `internal/iconlaw` walks
  `internal/tui`, `internal/tui3`, `internal/head` and `internal/resident` and fails the
  build on one, on every pull request;
  [docs/design/icons/DESIGN.md](docs/design/icons/DESIGN.md) is the law and the table.
- **One source of truth.** A number that appears in two places will drift — interpolate it
  from the constant. `propose_task`'s schema said the step default was 40 while the
  executor applied 200, and every model that read it reasoned from the wrong figure.

## Working alongside other sessions

Several Claude sessions often work this repo at once, in the same working tree.

- **Never `git add -A` or `git add .`** — stage your own explicit paths. It is easy to
  sweep another lane's in-flight untracked files into your commit.
- Run `ListAgents` before assuming whose work something is.
- Re-run `go build ./...` after fetching: another lane's half-finished file can break the
  tree for everyone.
- Feature waves are built in git worktrees off `dev` (`git worktree add
  ~/af-<name> -b <branch> origin/dev`), land through a pull request, then the
  worktrees and branches are removed. GitHub deletes the remote branch on merge.

## Filing an issue

`.github/ISSUE_TEMPLATE/defect.md` is the shape, and two of its lines are the law.
**The replication is something a stranger can run** — a stub, a fixture, a `-tags e2e`
run — never a path on your machine: #185's evidence lived in a private store on a
benchmark box nobody else can reach, which left a real bug unactionable. **The
acceptance is end-to-end first**, naming the real door and the exact string or receipt
field asserted — the TUI e2e suite was unit-green and e2e-broken for a week (#184).
#185 and #207 are the worked examples. Blank issues stay enabled: most of what is filed
here is a proposal, and the template is for defects.

## Tests

The 2026-09-08 constrained-runner baseline (`GOMAXPROCS=4`, `GOFLAGS=-p=2`) put
`internal/tui3` at 563 seconds and `internal/session` at 210 seconds. Give tui3
`-timeout 15m`, never `8m`, or the ceiling can report whichever test happened to
be running as though it hung. Use the repository targets for shorter loops:

```sh
make test-focus PKGS=./internal/tui3 RUN='^TestTheRegression$$' # one named test
make test PKGS='./internal/tui3 ./internal/session' TEST_FLAGS='-count=1'
make test-quick                                                # light feedback, not acceptance
make test-report PKGS=./internal/tui3 REPORT=/tmp/tui3.json    # fresh tests, timings and progress
```

`make test-report` keeps Go's build cache but supplies `-count=1`, so test
results are fresh. Its JSON distinguishes cached packages, lists incomplete
packages after an abrupt end, and sorts completed tests slowest-first. The
quick target checks build, vet, formatting, the packed manual, and laws; it does
not replace the full affected-package run or a final uncached relevant suite.

**The tests that fail on a clean tree are listed in `.github/known-red.txt` and
nowhere else.** `make test` skips them by name, and so does CI, through the same
target — so `make check` passes on a clean tree and a red in either place means
the change caused it. The ledger only shrinks (`internal/ci` ratchets its count):
fix a test, delete its line, lower `knownRedEntries` in the same commit. Never
add a line. Confirm any other red with a stash-and-rerun before chasing it.

There is no longer a "flakes under load" list here. The three that were on it —
`TestOnlyADesignsOwnThreadCarriesTheReviseVerb`,
`TestInterruptedTurnDoesNotWakeOnTheNoteItDrained` and
`TestAChangeWithdrawsTheCardRewritesThePageAndAsksAgain` — shared one cause with
two more that were never written down, and it was fixed rather than described
(#176). A `internal/session` test that fails only when other suites are running
beside it is now a bug report, not a known shape: **reproduce it, do not rerun it
in isolation and move on.**

**The laws and the touched packages run on every pull request, and both block.**
`make test-laws` is every test that reads the tree itself with `go/ast` or
`go/parser`, found by that import (`scripts/laws.sh`) and run in about twenty
seconds; `touched packages` is the full suite of every package the change touched,
with `-count=1`; `check` is green only when both are. Write a structural test with
that import and it is on the gate the day it lands. A red `touched packages` on
your pull request is yours to read before anything merges.

**The tmux TUI suite** is the only test that drives the real binary in a real
terminal against a real model, and it is how a wave verifies that the surface
still behaves:

```sh
go test -tags e2e -count=1 -timeout 40m -v ./internal/e2e/
go test -tags e2e -run TestTUIE2E -count=1 -timeout 40m -v ./internal/e2e/   # just the nine TUI subtests
```

It needs `OPENROUTER_API_KEY` and `tmux`, costs a few cents, and takes about
**seventeen minutes** for the whole tagged package (`TestTUIE2E` alone is about
ten, most of it one subtest waiting out a five-minute standing pass). It SKIPS
rather than fails with no key, no tmux or no `bin/aforge`, so run `make build`
first. Iterate one subtest at a time — `-run 'TestTUIE2E/<name>'` — rather than
paying for the whole thing, and capture the output to a file: the screens it logs
are far too wide to read through a pipe.

Its needles all come out of one table, `internal/e2e/tuiwords_test.go`, which an
**untagged** test in the same package reads back against `internal/tui3`'s own
sources — so `go test ./internal/e2e/` (no tag, no model, under a second) fails
the moment the surface stops spelling a sentence the suite waits for. That gate
exists because the suite silently rotted for a week after the home redesign
(#184); if you respell a person-facing string, expect it to name you.

**The router's own laws** have one live-key test beside the fake-router suites,
because a claim about how many machines are behind a model is a claim about the
world and no fixture can answer it:

```sh
go test -tags e2e ./internal/provider/ -run TestRealRouter -v   # ~45s, a fraction of a cent
```

It thins a real serving set — striking every machine it sees answer until its own
vetoes cover all of them — and asserts the router refuses that at most once. On
2026-09-03 against `deepseek/deepseek-v4-flash` and its sixteen machines, dev
`713945e3b` paid 8 refusals over 36 calls and the fix paid 1 over 23 (#586). It
SKIPS green without `OPENROUTER_API_KEY`; the package's `TestMain` re-roots
`AFORGE_HOME`, so the profile's key is not found and the variable is the way in.
`internal/lane` has the sibling live test, `-run TestReal`.

**Remote access** (`--host`, `--at`, attachments) has three layers, and they are cheap:

```sh
go test ./internal/remote/ ./internal/enginehost/ ./internal/pair/... ./internal/relay/ ./internal/furrow/
make test-remote          # three containers, no API key, ~50s; SKIPS GREEN with no docker
```

`make test-remote` builds its container binaries for **this machine's** architecture; an
`Exec format error` from `modelstub` means that pin was reintroduced. With no second
machine, `--host localhost` is a real connection over a real ssh pipe and exercises
everything except the shared-disk law — `docs/remote-access-testing.md` §3.0 has the tmux
recipe for driving the surface and killing the link on purpose.
