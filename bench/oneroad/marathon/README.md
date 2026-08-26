# bench/oneroad/marathon — one SWE-Marathon task, three harnesses, one price table

The task is [`rust-java-lsp`](https://github.com/abundant-ai/swe-marathon) (paper
arXiv 2606.07682): build a Java language server in Rust from scratch whose JSON
responses match Eclipse JDT-LS across ~68,000 test points on 1,007 real Java
files, in ten hours. `difficulty = "hard"`, `expert_time_estimate_hours = 20`.

Three arms, one model — `deepseek/deepseek-v4-flash` over OpenRouter:

| arm | what it is |
| --- | --- |
| `aforge-crew` | `aforge chat --yolo --model …` in a real TTY, **registry tiers** — the reader, division and repair roles resolve to their own tiers, exactly as `swe/cell.sh`'s crew arm does |
| `aforge-flash` | the same, with every tier collapsed onto the one model (`models.tiers.*`) |
| `pi` | `pi -p --provider openrouter --model …` |
| `opencode` | `opencode run --auto -m openrouter/…` |

## Running it

```bash
# build the image once (plain ubuntu:24.04 + rustup 1.86.0; no arm64 patch needed)
docker build -t swe-marathon/rust-java-lsp:v1.1 \
  -f <repo>/tasks/rust-java-lsp/environment/Dockerfile <repo>/tasks/rust-java-lsp/environment

bash preflight.sh rust-java-lsp        # ALWAYS FIRST: proves the toolchain and the egress allowlist
bash cell.sh <arm> rust-java-lsp        # one cell, the task's own 10 h wall
bash wave.sh rust-java-lsp aforge-crew pi opencode   # all three, arm-fair
```

`preflight.sh` builds a throwaway container the way `cell.sh` builds a real one
and asserts the two things that are invisible from inside a run and fatal to its
row — which compiler the agent is holding and what the network reaches. It takes
about a minute and it has already caught the defect described under **The
toolchain** below. Run it after any change to `cell.sh` or `netlock.sh`, and
before any wave whose numbers are going to be quoted.

The three 10 h cells of 2026-08-25 were launched as:

```bash
cd ~/af-oneroad/bench/oneroad/marathon
AFORGE_BUILD_COMMIT=6e956a1a nohup bash wave.sh rust-java-lsp aforge-crew pi opencode \
  > ~/af-bench/marathon/.wave.out 2>&1 &
```

To launch the aforge arm alone against a freshly built binary — the crew config
is the default, so nothing is pinned:

```bash
cd ~/af-oneroad/bench/oneroad/marathon
AFORGE_BUILD_COMMIT=<commit the binary was built from> \
NEW_BIN=~/af-oneroad/bin/aforge \
nohup bash cell.sh aforge-crew rust-java-lsp > ~/af-bench/marathon/.aforge.out 2>&1 &
# watch it:  tmux attach -t oneroad-mar-aforge-crew-rust-java-lsp-s1
```

`AFORGE_BUILD_COMMIT` is the binary's own provenance and is recorded beside its
sha256. It is not the same thing as the repository's HEAD: several lanes share
this checkout and HEAD moves under a ten-hour cell, so the record keeps both
(`build_commit` and `repo_commit_at_record`).

**s2, aforge-crew only, to be launched when the new binary lands** — fill in the
commit the binary was built from and check the sha it prints:

```bash
cd ~/af-oneroad/bench/oneroad/marathon
sha256sum ~/af-oneroad/bin/aforge            # confirm it is the new build
SEED=s2 AFORGE_BUILD_COMMIT=<new build commit> NEW_BIN=~/af-oneroad/bin/aforge \
  nohup bash cell.sh aforge-crew rust-java-lsp \
  > ~/af-bench/marathon/.aforge-s2.out 2>&1 &
# watch:  tmux attach -t oneroad-mar-aforge-crew-rust-java-lsp-s2
# tail:   tail -f ~/af-bench/marathon/aforge-crew-rust-java-lsp-s2/cell.log
```

Everything else is the s1 configuration unchanged: same task, same
`deepseek/deepseek-v4-flash`, same 36000 s wall, same 4 CPUs / 16 GB, same
crew (registry tiers) config, hourly curve. `SNAPSHOT_OFFSET` is not needed for a
single cell.

Knobs, all with the task's own value as the default: `SEED`, `CELL_SECONDS`
(default `[agent] timeout_sec` = 36000), `SILENCE_SECONDS` (900), `SNAPSHOT_EVERY`
(3600, `0` disables the curve), `SNAPSHOT_TIMEOUT` (1800), `MODEL`, `NEW_BIN`,
`MARATHON_REPO`, `IMAGE`, `MAR_OUT`, `NETLOCK` (1; `0` runs on full egress and
says so in the record), `NETLOCK_REFRESH` (30 s), `PIN_TOOLCHAIN` (1).

Artifacts land in `~/af-bench/marathon/<arm>-<task>-<seed>/`, and `_results` here
is a **symlink** to `~/af-bench/marathon`. That is not tidiness: a cell's bind
mounts are root-owned while it runs, and a root-owned directory anywhere under
the repository breaks `go build ./...` and `internal/config`'s registry test,
whose `filepath.Walk` does not follow symlinks.

## The shape, and why

**tmux on the host, driving `docker exec -it`.** No image is derived, nothing is
apt-installed into the dataset's container, and the pane is a real TTY. The
profile directory is a bind mount from the cell's own host directory, so the
journal the settle detector polls and `lib/road.py` reads is on the host at a
path the existing readers already understand.

**PID 1 is the clock.** `environment/timer.sh` reports `36000 - $(ps -o etimes= -p 1)`,
so `--entrypoint sleep … infinity` makes `bash /app/timer.sh` correct for free —
the same thing Harbor's own container does. The cell records what the timer said
at t=0 (`timer-at-start.txt`, `"Remaining time (hours:minutes): 10:00"`).

**Nothing from `tests/` or `solution/` is mounted while the agent runs.**
`tests/holdout/golden.jsonl` **is** the hidden test set and `solution/` is the
oracle; an agent that can read either has been handed the answer. The container
is created with no `/tests` at all — `oracle-absent.txt` in every cell is the
container's own `ls` proving it — and `tests/` is `docker cp`'d in only after the
harness has been reaped. Docker cannot add a bind mount to a live container, and
the copy is the honest equivalent: same container, same filesystem, staged after
the agent can no longer read anything.

**The harness is killed inside the container before the verifier runs.** Killing
the tmux session only kills the `docker exec` *client*; the process it started
lives on as an orphan, and an orphan holding a `cargo build` would fight the
verifier for the same `target/` and the same four CPUs.

**The agent's own score is not the verifier's.** The image ships `run_tests.sh`,
which writes `/logs/verifier/reward.txt` and `metrics.json` against the **visible**
corpus. Everything the agent left in `/logs` is moved to `/logs/agent-phase/`
before `tests/test.sh` runs — kept, because what the agent believed about its own
progress is evidence, just never the verdict.

**The agent's toolchain and the agent's network are the runner's job, and both
were once wrong.** The agent compiles with the image's rustc, out of the image's
`/root/.rustup`, and reaches only the hosts `task.toml` names plus the model
endpoint. See **The toolchain** and **The network** below; `preflight.sh` proves
both against a throwaway container before a wave is launched.

**The verifier runs whole.** Unlike the SWE track — where three of five stages are
LLM judges we have no key for — marathon's is entirely deterministic: rebuild,
`anti_cheat.py`, `verify_integrity.py`, the cached-golden scan, decrypt the
pristine golden with the key embedded in `test.sh`, score the visible corpus, score
the holdout, merge. Nothing is skipped and the verdict is the benchmark's own.
Timeout `[verifier] timeout_sec` = 3600 s, and the whole hour can genuinely be
needed: `score_golden.py` gives each of the 68,186 requests its own 30-second
deadline, so a server that **answers** is scored in seconds (the s1 aforge cell:
6 s) while one that **hangs** can burn the full budget. Both outcomes are real
readings, not runner faults.

**The per-method table is kept before it is overwritten.** `score_golden.py`
writes `partial_score, pass_rate, passed, total, per_method` to
`/logs/verifier/metrics.json` — and then `test.sh`'s holdout merge **replaces**
that file with a summary that has no `per_method` in it. `verify.sh` copies the
first complete file aside as `metrics_main.json` (first writing wins, and a
half-written file is rejected by a `json.loads` guard). `record.py` falls back to
parsing the table `score_golden.py` printed to the log.

## The settle rule

Exactly the SWE track's: the cell ends at the wall, or earlier only when the
store has been quiet for `SILENCE_SECONDS` **and** — for the aforge arms — no task
is live per `lib/tasklive.py`. Both readings are taken **inside** the container:
the harness runs as root and creates its session folder mode 700, so from the
host the profile is unreadable, `find` returns nothing with no error, and a
fingerprint of nothing is perfectly stable from the first poll — a cell reading
from the host would settle blind no matter what the agent was doing.

`SILENCE_SECONDS` is **900 here, not 180**. The SWE track's three minutes suits a
forty-five minute wall; this wall is ten hours and one `cargo build --release` of
a tree-sitter grammar runs for minutes with nothing written to the store.

**The fingerprint must not see the heartbeats.** aforge's standing-work ticker
appends one line to `/prof/v3/standing/wake.log` every **300 s** for as long as
the profile is open — and the line it writes when there is nothing to do says so
itself: `examined=0 checked=0 fired=0 said=0`. A fingerprint over the whole store
was therefore reset every five minutes by a record of *nothing happening*, so
`stable` could never reach 900 and `quiet` could never reach the 2700-second
escape hatch either. **The s1 aforge cell's settle rule was arithmetically
unreachable**: it went idle at 12:14 with its only task landed and would have run
to the ten-hour wall. `presence.json` and everything under `standing/` are now
excluded — both are the harness reporting that it is *alive*, which is the
opposite of the question being asked. Everything a turn or a worker writes
(`transcript.jsonl`, `tasks.json`, `tasks/*.jsonl`, `logs/jobs/*`) still counts,
so a wake that starts real work still registers, in the files that work writes.

`lib/tasklive.py` was **not** at fault: it counts `unverified` as landed, as its
`LANDED` set says, and reported `live 0` correctly throughout.

**A signal ends a cell.** `trap cleanup EXIT INT TERM` runs `cleanup` on TERM and
then *resumes the script* — the container is gone but the settle loop keeps
polling a name that no longer resolves, reads an empty fingerprint as a stable
one, settles on it, and writes a record over the record already there. It is now
`trap cleanup EXIT` plus `trap 'cleanup; exit 130' INT TERM`.

### finish.sh — ending a cell that is already up

`bash finish.sh <arm> <task> <seed> "<reason>"` performs cell.sh's own ending
from outside: reap the harness, stage `tests/`, run the verifier, hand the files
back, write the record, then release `cell.sh` so its `cleanup` tears the cell
down. It exists because the two obvious rescues are both wrong — **editing
cell.sh in place** corrupts a running bash, which reads a script incrementally
from a byte offset (the verify-and-record tail is exactly what would be
misparsed), and **killing cell.sh** takes the container, and the workspace the
verifier has to score, with it. It never touches `/workspace`: a worker's own
arrangement in there is part of the container state the benchmark scores, and a
runner that tidies before judging is scoring something the agent did not leave.

Outcomes: `OK` (settled, every task landed) · `OK(wall)` (wall reached but work
landed — the clock ran out, the attempt did not) · `DNF` (wall reached with
nothing landed) · `STRANDED` (settled with a task live but silent past
`ONEROAD_STRANDED_SECONDS`) · `KILLED` (the harness died on a signal) ·
`INVALID` (the verifier produced no metrics.json at all — recorded as *no score*,
never as a zero).

"Work landed" has no diff to measure here: this task ships no git repository. The
image ships exactly one file in the working directory (`run_tests.sh`), so
`verify.sh` lists the workspace at the moment of judgement and anything above one
file is the agent's own.

## The progress curve

`snapshot.sh` runs beside the cell. Every `SNAPSHOT_EVERY` seconds it streams the
workspace out as a tar (**without** `target/`, which is build output and can be
gigabytes) and scores it in a **short-lived container of its own** from the same
image, on two CPUs, killed at `SNAPSHOT_TIMEOUT`. Scoring in the agent's own
container would hand its four CPUs to a release build and a 68,000-point scoring
run several times over a ten-hour cell, and every curve point would be paid for
out of the result it measures. A per-cell docker volume holds the snapshot
builds' cargo registry so each hour's build is not a fresh download; the agent's
container never sees it, so nothing is warmed for the run under test.

`curve.csv` (`csv.QUOTE_MINIMAL`): `elapsed_s, partial_score, passed, total,
reward, note`. A snapshot that fails to copy, fails to start, or runs out of its
half hour writes a row with `partial_score` 0 **and the reason**, and goes back to
sleep. Nothing in that loop can take the cell down.

**The whole of `/workspace` travels, not the crate — and the first three s1 curve
points are what that mistake looks like.** The snapshot copied only the crate
leaf, on the reasoning that the crate is the work. It is not what the benchmark
scores: `score_golden.py` addresses every test point as
`file:///workspace/test-files/<rel>`, and the s1 worker met that by creating
`/workspace/test-files -> /workspace/java` — one symlink, one level **above** the
crate, therefore outside the copy. Three hourly points came back `0/68186` from a
server that was answering correctly for 132 of them.

Replayed both ways against the s1 crate, in the scoring container, with the
verifier's own `test.sh`:

| snapshot path | main | holdout | partial |
| --- | --- | --- | --- |
| old (crate leaf only) | 0/68186 | 0/276 | 0.0 |
| **new (whole `/workspace`, symlinks preserved)** | **132/68186** | **29/276** | **0.0535** |

The new path reproduces the real verifier's verdict on the live container
exactly. The tar carries no `-h`, so a symlink stays a symlink — following it
would replace a one-byte link with a second copy of the corpus and, worse, hide
whether the agent's arrangement actually works. The image's `/workspace` is
removed before the agent's is unpacked over it, so the scorer never judges a
blend of two workspaces that never existed. Excluded: `target/` (the verifier
rebuilds it), `.git`, `node_modules`, `__pycache__`, `.venv`.

Because the corpus now travels too, the snapshot **can** show the integrity and
cached-golden failures the real verifier would find — it hashes the agent's own
`java/` and `golden.jsonl`, exactly as the benchmark does (checked: `Integrity:
PASSED (1008 files match manifest)` on the replay). The corpus is left out of the
*unpacked* host-side copy only, since ten identical copies per cell of the
dataset's own thirty megabytes is the one thing on this disk that would grow for
no reason.

The curve is still **indicative, not the verdict** — it is a point in time, and
only the end-of-cell verifier runs against the container the agent actually left.

## The record

`record.json` per cell: arm, harness version (aforge: repo commit + binary
sha256 + which tier config; pi/opencode: their `--version`), model, image id and
ref, task repo commit, started/ended/wall/verify wall, outcome and settle reason,
reward, partial_score, pass_rate, passed/total, holdout block, `per_method`,
the `toolchain` block (the image's toolchain and rustc, and whether the compiler
was pinned), `network_deviation` as the cell computed it,
tokens in/out/cached, requests, `cost_usd`, workspace file count, the timer's
reading at t=0, the curve, and for the aforge arms the road summary from
`lib/road.py` (marks, ceiling decision, divisions, parts, peak concurrency).

**One price table for all three arms is the law.** `cost_usd` is always
tokens × the OpenRouter list price for the pinned model
(`lib/competitor_cost.py:list_prices`), computed from each harness's own usage
records — aforge's `call` journal lines via `lib/aforge_list_cost.py`, pi's
session jsonl and opencode's sqlite store via `lib/competitor_cost.py`. What a
harness believed it spent is recorded beside it as `native_usd`, for the record
and never for the comparison.

The peer arms run with `HOME=/peer`, which is bind-mounted out so their stores can
be read afterwards. `/peer` is deliberately **not** under `/tmp`, `/root`, `/home`
or `/workspace`: `tests/test.sh`'s cached-golden scan walks exactly those four for
`.json`/`.jsonl` files over 1 MB, and a harness store that happened to hold a
large one would otherwise read as a cheat that never happened.

## The toolchain, and the ten hours it cost

The image installs rust as root: `curl … | sh -s -- -y --default-toolchain 1.86.0
--profile minimal`, into `/root/.rustup` and `/root/.cargo`, with
`ENV PATH="/root/.cargo/bin:$PATH"`. `tests/test.sh` exports exactly that PATH and
runs under `docker exec` as root, so **the verifier's compiler is
`/root/.rustup`'s**.

The cell ran the agent with `HOME=/chome` — the right instinct, keeping the
harness's dotfiles out of `/root` — and rustup finds its store through
`$HOME/.rustup`. So the agent's first `rustc --version` answered:

```
error: rustup could not choose a version of rustc to run, because one wasn't
specified explicitly, and no default is configured.
help: run 'rustup default stable' to download the latest stable release …
```

Every model did what the help text said. On an open network that installed a
newer stable into `/chome/.rustup`, and the workspace then drifted to whatever
that compiler allowed. Seed **s6** is the clean example: its `Cargo.lock` pinned
`url 2.5.8` → `idna` → `icu 2.3`, which needs rustc 1.88. It built in the cell,
all day. It could not build under `tests/test.sh`, so the benchmark's own
verifier scored it **0.0** — and the hourly snapshot scorer, building in a fresh
container of the image, reported the same 0.0 for the same reason. The agent was
never told, because inside the cell everything worked.

The fix is three environment variables on the agent's `docker exec`, and it keeps
`HOME=/chome`:

| variable | value | why |
| --- | --- | --- |
| `RUSTUP_HOME` | `/root/.rustup` | the store the verifier reads |
| `CARGO_HOME` | `/root/.cargo` | the registry and the shims the verifier reads |
| `PATH` | `tests/test.sh`'s PATH, verbatim | `cargo` is found where the verifier finds it |
| `RUSTUP_TOOLCHAIN` | the image's own toolchain | see below |

The first two are simply what the official environment has — there the agent runs
as root with `HOME=/root` and shares the store with the verifier by default.

`RUSTUP_TOOLCHAIN` is **a deliberate deviation, one notch stricter than the
benchmark**. `task.toml`'s allowlist includes `static.rust-lang.org`, so an agent
may legitimately install a newer stable; sharing `/root/.rustup`, the verifier
would inherit it and agree. But the hourly snapshot scorer builds in a *fresh*
container of the image, which has only the shipped toolchain, and a curve that
cannot build what the cell built is a curve that lies. Pinning makes all three
compilers the same one. `cell.sh` hands the same pin to the verifier's
`docker exec`, so a `rustup default stable` run by the agent cannot move the
verifier either.

The pin is proven rather than assumed — `preflight.sh` step 5 runs
`rustup default stable` inside the locked container and shows rustup installing
1.98.0 while still reporting

```
info: note that the toolchain '1.86.0-aarch64-unknown-linux-gnu' is currently in
use (overridden by environment variable RUSTUP_TOOLCHAIN)
```

and `rustc --version` still answering 1.86.0.

One consequence worth stating: with `CARGO_HOME=/root/.cargo` the crate registry
now lands under `/root`, which `tests/test.sh`'s cached-golden scan walks. That is
also true of the official environment, and it is the hazard already measured at
the bottom of this file — the largest JSON a tree-sitter build leaves there is
`grammar.json` at 186 KB, an order of magnitude under the 1 MB threshold.

## The network, and what an address can and cannot say

`instruction.md` tells the agent, verbatim:

> Network access is restricted — only the package registries needed to fetch your
> crate dependencies are reachable; all other internet egress is blocked.

and `task.toml` names the hosts, identically for the agent and the verifier
phase: `crates.io`, `index.crates.io`, `static.crates.io`, `github.com`,
`static.rust-lang.org`. Cells before this ran on docker's default bridge with
full egress, which made that sentence in the prompt **false**.

`netlock.sh` writes the policy with the **host's** iptables into the
**container's** network namespace (`nsenter -t <pid> -n`). Two other mechanisms
were considered and rejected:

* **An HTTP proxy on an `--internal` docker network.** Enforces perfectly, but
  only for clients that honour `HTTPS_PROXY`. node's undici (`pi`) and opencode's
  runtime do not by default, so the peer arms would lose their model endpoint and
  die. A policy only half the arms obey is a bias, not a policy.
* **iptables inside the container with `--cap-add NET_ADMIN`.** The image is plain
  ubuntu:24.04 and ships no iptables, so this means `apt-get install` into the
  image under test. The whole point of this runner is that the dataset's image is
  the one measured.

The netns approach adds no capability to the container, installs nothing in it,
and puts no proxy variable in its environment — every process in it, in whatever
language, is filtered by the kernel. The last rule is `REJECT`, not `DROP`, so a
blocked connection fails in about 50 ms and the agent's tool reports a refused
connection instead of hanging on a timeout it cannot see.

**Allowed, and nothing else:**

| host | why |
| --- | --- |
| `index.crates.io`, `static.crates.io`, `crates.io` | `task.toml` `[agent].allowed_hosts` |
| `github.com` | `task.toml` — cargo git dependencies |
| `static.rust-lang.org` | `task.toml` |
| `openrouter.ai` | the model API. `task.toml`'s own note: "Oddish adds only the selected model API endpoint to the agent phase" |

plus DNS to the container's own resolvers, and loopback. IPv6 is refused
outright.

**What an address-based allowlist cannot express.** `index.crates.io`,
`static.crates.io` and `static.rust-lang.org` all resolve to the *same* Fastly
address (151.101.138.137, measured here). No address filter can allow the crate
registries and refuse the toolchain server; separating them needs SNI inspection.
It costs nothing, because `static.rust-lang.org` is on the benchmark's own
allowlist and because the toolchain pin above makes an installed compiler
inert. `github.com` round-robins across a block, so `140.82.112.0/20` —
GitHub's own published git/codeload range — is allowed rather than a single
resolved address; the Fastly and Cloudflare names are allowed by address, because
`151.101.0.0/16` is shared Fastly space that would let through half the internet.

The watcher re-resolves every 30 s and only ever **adds** addresses; removing one
would cut a live `cargo` connection for no reason. It also re-installs the whole
policy if it ever finds the rules gone.

`preflight.sh` step 3 is the proof, from inside the container:

```
  PASS  https://index.crates.io/config.json answered 200 (84ms)
  PASS  https://static.crates.io/ answered 403 (328ms)
  PASS  https://crates.io/ answered 403 (184ms)
  PASS  https://github.com/ answered 200 (317ms)
  PASS  https://openrouter.ai/api/v1/models answered 200 (136ms)
  PASS  https://pypi.org/simple/ refused in 52ms
  PASS  https://registry.npmjs.org/ refused in 46ms
  PASS  https://www.google.com/ refused in 58ms
  PASS  http://archive.ubuntu.com/ refused in 52ms
  PASS  https://huggingface.co/ refused in 51ms
```

(403 from a CDN for a bare path is an answer: the connection was allowed. `000`
is curl failing to connect at all.)

A failure to install the allowlist is **fatal to the cell**. A run that quietly
fell back to full egress would be a run whose prompt lies to the agent and whose
row cannot be compared with a leaderboard number.

### Cells run before this

**Seeds s4–s9, launched 2026-08-25/26, ran on the open default bridge and with
the split toolchain.** Their agents saw the rustup error, installed their own
stable from `static.rust-lang.org` into `/chome/.rustup`, and compiled against a
compiler `tests/test.sh` does not have. Their rows are not comparable with rows
produced after this commit, and `record.json` for each of them carries the old
`network_deviation` sentence ("default docker bridge, full egress"), which is the
truth about them.

The **snapshot scorer was not changed** and did not need to be: it already builds
in a fresh container of the image, which is why it correctly reported s6's 0.0
while the cell itself was building happily. It remains the honest oracle. It also
remains outside the allowlist — a snapshot container has full egress — which
changes no score, because it only ever *builds and scores* a tarball of the
workspace and never runs the agent.

## Deviations from the benchmark, stated

1. **The egress allowlist is the task's, plus the model endpoint.** `netlock.sh`
   enforces `task.toml`'s five hosts and `openrouter.ai` in the container's
   network namespace; everything else is REJECTed. What REMAINS a deviation:
   `openrouter.ai` is reachable (the benchmark's own agent phase adds a model
   endpoint too, so this is a deviation in *identity* rather than in kind), the
   filter is by address so it cannot separate `static.rust-lang.org` from the
   crate registries, and `github.com` is allowed as GitHub's published
   `140.82.112.0/20` rather than one rotating address. Each cell's exact policy is
   recorded in `record.json` (`network_deviation`) and in
   `/logs/verifier/oneroad_stages.json`, computed from what the cell did rather
   than written down here. **Seeds s4–s9 predate this and ran open** — see above.
2. **The agent's compiler is pinned to the image's**, one notch stricter than the
   benchmark, so that the cell, the verifier and the hourly curve all build with
   one toolchain. See **The toolchain** above.
3. **No per-container storage quota.** `task.toml` asks for 20480 MB; docker's
   overlay2 on this host enforces no per-container disk quota.
4. **The wall is the runner's, measured from container start.** The container is
   created immediately before the prompt is sent, so the agent's `timer.sh`
   reading and the cell's own wall agree to within a few seconds — but the few
   seconds are the runner's, not the benchmark's.
5. **Snapshot scoring is extra load the benchmark does not have** — two CPUs and
   up to half an hour, once an hour, in a separate container. Sized to keep the
   host under its 20 CPUs with three cells running.

CPUs (4) and memory (16 GB) are the task's own, read out of `task.toml` by the
runner rather than spelled here, so this runner takes `<arm> <task>` and works for
any marathon task whose Dockerfile builds on this machine.

## Two zero shapes, and they are different facts

`tests/test.sh` runs under `set -euo pipefail`, so `cargo build --release 2>&1 |
tail -20` **failing exits the script** before it can reach `write_zero_metrics`.
That is the benchmark's own behaviour, and it means:

| the workspace | what the verifier leaves |
| --- | --- |
| no `Cargo.toml` at all | `metrics.json` with `partial_score 0.0, pass_rate 0.0, passed 0, total 0, per_method {}` and `reward.txt` `0.0` — **checked**, on an untouched container |
| a crate that does not compile | **no `metrics.json` at all**; only `reward.txt` `0.0`, written at the top of the script — **observed**, on the pi smoke (`test_sh_exit=101`) |
| a crate that compiles but whose server never answers `initialize` | the full merged `metrics.json` with `main` and `holdout` blocks at `passed 0 / total 0` — **observed**, on the aforge smoke |

`record.py` records the second as `partial_score 0.0` with `score_source` saying
why, not as a missing score — the verifier did reach a verdict and wrote it. Only
a run with **neither** `metrics.json` nor `reward.txt` is `INVALID`.

## A hazard that was checked, not assumed

`tests/test.sh`'s cached-golden scan fails any run with a `.json`/`.jsonl` over
1 MB under `/tmp`, `/workspace`, `/root` or `/home` (outside
`/workspace/golden.jsonl` and the crate's `target/`). A tree-sitter build puts
grammar JSON into `/root/.cargo/registry/src/`, so this was measured rather than
hoped: with `tree-sitter`, `tree-sitter-java` and `serde_json` vendored, the
largest is `tree-sitter-java-0.23.5/src/grammar.json` at **186 KB** — an order of
magnitude under the threshold. A run that pulls in a crate shipping a bigger one
would be failed by the benchmark's own rule, equally, in every arm.
