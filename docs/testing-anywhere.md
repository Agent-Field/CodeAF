# Testing a chat that moves between computers

This page says how to test every path a chat takes between two computers, from the
fastest check to a walk by hand. Run the sections in order. Each one costs more than the
one before it and proves more.

Moving chats is on by default, so no test or rig sets a cell variable. Only a test of
the off path sets `CODEAF_CELLS=0`.

Settings. Every script that names a machine, a folder or a relay reads it from an
environment variable (through `scripts/measure/rigenv.py`) and stops with a message that
names the variable when one is missing. Nothing is edited in a script. Export these once:

| Variable | Meaning | Default |
|---|---|---|
| `CODEAF_RELAY` | the relay under test: a hosted one (`https://...`) or your own (`codeaf relay`, see [relay.md](relay.md)) | none |
| `CODEAF_SECOND_HOST` | the second machine, reachable with `ssh $CODEAF_SECOND_HOST` and no password prompt; a Mac or a Linux box | none |
| `CODEAF_SECOND_ROOT` | an absolute folder on the second machine that a rig may fill and wipe | none |
| `CODEAF_FIRST_ROOT` | the same on this machine | `~/caf-rig` |
| `CODEAF_CORPUS` | a folder holding the corpus repos `r18-pareto-c365`, `r02-mj-base`, `r06-agentfield` | none |
| `CODEAF_RIG_BIN` | a folder holding the built `codeaf`, `codeaf-vd` and `s1probe` | none |
| `CODEAF_EVIDENCE` | where `hosted-validate.py` writes its result folders | `~/caf-evidence` |

Other names read by one rig: `CODEAF_SRC` and `BENCH_BUILD` (`bench-move.sh`: the checkout to
build, where it builds), `CONT_PORT` and `CONT_SUFFIX` (`continuity.py`: a dev-server port and a
tmux name, so two rigs can run side by side), `FID_REUSE` and `FID_PLAIN` (`fidelity.py`: keep the
pairing between runs; build the older fixture), `BILL_DOC` (`relay-bill.sh`: the decision
document it rewrites).

Anything that calls a model uses `deepseek/deepseek-v4.1-flash` for every role. The key must
already be in your login environment; none of these scripts print it.

## What each test proves

| Test | Proves | Runtime |
|---|---|---|
| `make test-quick` | the tree builds, is formatted, passes the laws and the manual gates | a few minutes |
| `go test` on the sync packages | seal, upload, fetch, take, pairing and the setup flow work in process | under 5 minutes |
| `relay/hosted` `npm test` | the hosted relay's units: store, directory, leases, pairing, watch | under 2 minutes |
| `TestPairJourney` | a person can pair two homes and move a chat, by keystrokes in a real terminal | about 2 minutes |
| `scripts/relay-conformance.sh` | both relays (Go and hosted) obey the same contract | about 10 minutes (about 3 with `SKIP_E2E=1`) |
| `scripts/durability-hosted.sh` | nothing a chat completed is lost when its machine dies | about 20 minutes |
| `scripts/hosted-validate.py` | the whole loop on two real machines: pair, work, move, take back, resume card, integrity | about 30 to 60 minutes a pass |
| `scripts/continuity.py` | the chat on the other machine carries all the work, so the agent continues and does not redo it | about 20 minutes a run |
| `scripts/fidelity.py` | a folder arrives byte for byte, awkward cases included | about 10 minutes |
| `scripts/bench-move.sh` | what a cold take, warm take, take back and publish cost in time, requests and dollars | tens of minutes to hours by size |

## 1. Fast local gates

From the repository root, after `make build`:

```sh
make test-quick
go test -count=1 ./internal/cellsync/ ./internal/handoff/ ./internal/pair/ \
  ./internal/syncsetup/ ./internal/cellstore/ ./cmd/codeaf/
(cd relay/hosted && npm ci && npm test)
```

Some tests need the real engine (`furrow`), which `make build` makes. Without it they skip
with the words `no engine binary`; a skip is not a pass. Run them with the tag:

```sh
go test -count=1 -tags engine ./internal/cellsync/ ./internal/cellstore/ ./internal/syncsetup/
```

Pass: every package says `ok`, no `engine` test skips.

## 2. Two homes on one machine, with the Go relay

Two state roots on one machine are two computers as far as the product can tell.

**Pairing and moving, driven by keystrokes.** `internal/e2e/pairjourney_e2e_test.go` starts
the Go relay, two homes and the real binary in tmux. It needs `tmux` and `go`:

```sh
make build
go test -tags e2e -count=1 -v -timeout 10m -run '^TestPairJourney$' ./internal/e2e/
```

Set `UX_SEGMENT=A` to stop after the approval and run only the fresh-install check. Per-step
times and verdicts go to `$UX_EVIDENCE` (default `~/ux-evidence/e2e-run.json`).
Pass: every step prints its screen and the test ends `ok`. The journey budget is two minutes.

**Durability, against the Go relay or any other.** The same tests as section 3 run against
`$CODEAF_RELAY`, the same variable every script reads, with no default; a run with it unset stops
(the Go suite skips) and names it. Point it at your own relay to keep the load off a hosted one:

```sh
CODEAF_RELAY=https://relay.example.com scripts/durability-hosted.sh KillAfterLoneCall
```

The name after the script picks one test (`KillAfterLoneCall`, `KillAfterBurst`,
`KillAfterTurnEnds`, `KillAfterLaterLoneCall`, `KillDuringCall`, `NetworkCutThenKill`,
`LidClose`, `FirstCallKillRate`). Without a name it runs them all. Each prints one `DURABILITY`
line, `PASS`, `FAIL` (work missing on the other machine) or `FAIL-RECORD` (the work arrived,
its transcript line did not), or `UNREACHABLE` (work is missing, but the kill came sooner after the
call than the relay takes to store a frame, which the run measures with three probe puts and prints
as `relayAckFloor`; nothing could have been durable yet, so it does not fail the run). It uses a
scripted model, so it makes no model calls.

**Real engine round trips, in process.**
`go test -tags engine ./internal/cellsync/ ./internal/cellstore/ ./internal/syncsetup/`
(section 1) covers dedup, held moves and the setup round trip on the real engine.

**The old single-box remote check.** `make test-remote` (tag `docker_e2e`) runs
`TestRemoteTwoMachines`; it needs docker.

## 3. Two real machines against the hosted relay

You need a Linux and a Mac, the second reachable as `$CODEAF_SECOND_HOST`. Build `bin/codeaf` and the
engine on both machines from the same commit (`make build`; on the Mac, `export
PATH=/opt/homebrew/bin:$PATH` first). A copied binary is killed by macOS until it is signed:
`codesign -s - -f <binary>`.

Export the settings above first; each script's docstring says which of them it reads and the
layout it expects under the roots. Run `--help` on any of them to see the options.

The relay under test is whatever `CODEAF_RELAY` says; the rigs put it into each home as
`CODEAF_SYNC_URL`, which is the variable the product itself reads.

**The whole loop.**

```sh
python3 scripts/hosted-validate.py 1                # pass 1: pair, work, move, take back, integrity
python3 scripts/hosted-validate.py 2 --card-test 6  # six cold takes of small chats, counting resume cards
python3 scripts/hosted-validate.py 1 --resume       # redo push and integrity on a pass's saved state
```

It writes raw files and `results.json` under `<out>/pass<N>/`; `--out` picks the folder.
It makes real model calls and takes a lock on the Mac (`~/caf-bench.lock`) because it times
things. Helpers are in `scripts/measure/hosted/`: `setup.sh` (a clean rig home on one
machine), `tuidrive.py` (types into and reads the terminal), `cellread.py`, `treehash.py`,
`durable.py`, `clockoff.py` (the clock difference between the machines) and `report.py` (a
table of pass results).

**Continuity of a move.** Does the other machine carry all of the work?

```sh
python3 scripts/continuity.py pair    # once: pair the two homes
python3 scripts/continuity.py cold    # a fresh machine takes a finished session
python3 scripts/continuity.py warm    # a machine with an older copy takes again
python3 scripts/measure/continuity/report.py <run folders>
```

`pair` uses one pairing from the relay's budget (see the limits below). The checks live in
`scripts/measure/continuity/check.py`; `rig.sh <root>` makes a clean home pinned to the one
model.

**Folder fidelity, with the awkward cases.**

```sh
python3 scripts/fidelity.py run      # build the fixture, move it there, change it, move it back
python3 scripts/fidelity.py probes   # the single awkward cases, one at a time
```

The fixture (`scripts/measure/continuity/fidelity-fixture.sh`, about 350 MiB) holds a full
`.git` with a stash and a submodule, empty directories, a path over 200 characters, names in
different Unicode forms, names that differ only in case, big binaries, every kind of symlink,
a hard-linked pair, odd file modes and secret files. `fidelity-manifest.py` reads the tree on
each machine and `fidelity-compare.py` compares them. The move uses a private take program
(`scripts/measure/hosted/vdemotake.go.txt`, built as `vdemo-take`), because the home screen
has no door for a chat the machine already lists. Pass: every row PASS except the known case
below.

**What it costs.**

```sh
scripts/bench-move.sh --envs same,second --sizes S --reps 3
```

`--envs` picks `same` (two homes on this box, a Go relay in docker), `second` (the second
machine, which reaches the relay through `ssh -R`) and `hosted` (both machines talk to
`$CODEAF_RELAY`). It needs `CODEAF_SECOND_HOST` and `CODEAF_SECOND_ROOT` (for `second`
and `hosted`) and `CODEAF_CORPUS`; `CODEAF_SRC` is the checkout to build (default this one).
It builds, runs scripted edits (no model), and writes `rows.jsonl`,
`summary.json` and `summary.md` under `.lane/bench-move/`. Size S is a few seconds; M and L
take minutes to an hour a cell on a slow link. Run it on a quiet machine: it measures time.

**Durability on the real machines.**

```sh
scripts/durability-hosted.sh            # all scenarios, about 20 minutes
```

## 4. Conformance for both relays

```sh
scripts/relay-conformance.sh              # the whole matrix and one table
SKIP_E2E=1 scripts/relay-conformance.sh   # without the hosted Worker's end-to-end script
```

It starts the Go relay and the hosted Worker (under `wrangler dev --local`) on free ports,
never against a hosted deployment, and exits 0 only when no suite failed or ran zero cases.
To hold one live relay to the suites by hand:

```sh
go test -count=1 -tags relayurl ./internal/relayconf/ -relay-url=$CODEAF_RELAY
```

The suites are store, directory, isolation, pairing, rotation, big take, watch and lease. Run
the whole suite only against a relay with a short pairing time and a lifted per-address
identity cap (a local one, or staging); production lacks both on purpose. The Worker's own
end-to-end scripts: `(cd relay/hosted && npm run e2e)`.

## 5. The manual walk

Use two terminals, one per machine, both with `CODEAF_SYNC_URL=$CODEAF_RELAY`. A is the machine that
already has your work, B is the new one. This is what each step should look like.

1. **Home on A.** Run `codeaf`. If this machine never synced, the home screen shows a card,
   "Add another machine", with the pitch "pick up your work anywhere".
2. **Pair B.** On B run `codeaf pair`. B prints a link ending `codeaf.agentfield.ai/p/<code>`
   (staging prints its own host), a "Check number", and "Waiting for approval".
3. **Approve on A.** Open the card, choose to add a machine, and paste the link. A shows
   "wants to join your fleet" with the device's name, its platform, how long ago it asked and
   the same Check number B shows. If the numbers differ, deny. Press `a` to approve.
4. **Both sides confirm.** B prints "Paired - N workspaces available". A shows a news line,
   "joined your fleet".
5. **Work on B.** On B open a chat in a folder, ask for a small change, and let it finish.
6. **See B from A.** A's home shows a devices row with both machines online (filled dots).
   `/devices` lists both.
7. **The continue card.** Close the chat on B (or leave B idle), reopen A. A offers "Continue
   where you left off on <B>?". Enter chooses "continue here".
8. **Move.** A asks "Continue this chat here?". Confirm. A prints a banner "Moved from <B>
   in N s" and opens the chat with a resume card: the files not yet committed, the last test
   run and what B saw. Your folder is the same, file for file.
9. **Take it back.** Do step 5 to 8 the other way: work on A, then on B continue the same chat
   from B's home. It arrives in B's folder with A's work in it. (The devices screen says
   "undo", not "take back".)
10. **Offline row.** Close the app and sleep or disconnect B. Within a minute A shows B as
    `○ <B> (offline)`, its chats no longer say "running", and the continue card says the
    device is off. Reconnect B: the dot fills and the card goes away.
11. **Revoke.** On A open `/devices`, move down to B's row (not "this device") and press `r`.
    A says B "was revoked". On B, opening a chat says it was revoked or turned away, and B's
    own device list says so. B no longer reads A's chats.

## 6. What a pass looks like, and the limits

A pass: every command above ends with exit 0 and its own PASS line; no `FAIL`,
`FAIL-RECORD`, skip or zero-case suite; the two folders compare equal (the fidelity rows, and
`diff -r` apart from `.git`, `.furrow` and `node_modules`); and in the manual walk every step
shows what is written above.

Known limits, from the measured runs (the benchmark notes of the program have the tables):

- **Time follows the size of the repo, not of the change.** At the measured build, a warm take
  after a one-file change fetched about 15 requests and 0.1 MB but took over a minute on a
  200 MB repo, because the receiving machine rewrote the whole tree. A later build changed the
  sealing and upload path; the numbers were not repeated on it. Treat a slow warm take on a
  big repo as known, not as a regression, until you compare with a fresh bench run.
- **Cold takes fetch one object per request.** Over a hosted relay a 17,000-object repo took
  about 20 minutes cold at 70 to 90 ms a request. Small repos take seconds.
- **The benchmarks are not a real WAN for all rows.** The same-box row has no network, and the
  tunnelled row adds the latency of ssh. Runs were repeated three times at most; on a shared
  machine the spread is large (a warm take of 54 to 88 s on one box).
- **Dollar figures are a price table, not a bill.** They count the client's requests against
  list prices with every free tier used.
- **A moved chat's folder differs from the original in a few known ways.** Names that differ
  only in case cannot both exist on a case-insensitive Mac, so that take is refused. `.git/index` and `.git/logs` are compared by kind and size only, because
  git rewrites them. A `node_modules` folder is withheld and rebuilt with `npm ci`.
- **The pairing rate limit.** The hosted relay allows about 12 pairings per 40 minutes from one
  network (staging says `too many pairings from this network; wait N min`). Pair once and reuse
  the homes; do not run `continuity.py pair` in a loop.
- **The conformance suite and load runs against staging** cost real requests; run them with
  the owner's agreement, not by habit.
- **Only the Mac and Linux pairs were tested.** The fidelity runs were Linux to a Mac on APFS.
