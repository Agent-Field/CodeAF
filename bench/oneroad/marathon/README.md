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

bash cell.sh <arm> rust-java-lsp        # one cell, the task's own 10 h wall
bash wave.sh rust-java-lsp aforge-crew pi opencode   # all three, arm-fair
```

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

Knobs, all with the task's own value as the default: `SEED`, `CELL_SECONDS`
(default `[agent] timeout_sec` = 36000), `SILENCE_SECONDS` (900), `SNAPSHOT_EVERY`
(3600, `0` disables the curve), `SNAPSHOT_TIMEOUT` (1800), `MODEL`, `NEW_BIN`,
`MARATHON_REPO`, `IMAGE`, `MAR_OUT`.

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

**The verifier runs whole.** Unlike the SWE track — where three of five stages are
LLM judges we have no key for — marathon's is entirely deterministic: rebuild,
`anti_cheat.py`, `verify_integrity.py`, the cached-golden scan, decrypt the
pristine golden with the key embedded in `test.sh`, score the visible corpus, score
the holdout, merge. Nothing is skipped and the verdict is the benchmark's own.
Timeout `[verifier] timeout_sec` = 3600 s.

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

The curve is **indicative, not the verdict**: it scores a fresh container's
pristine `/workspace/java` and `/workspace/golden.jsonl`, so it cannot show the
integrity or cached-golden failures only the real verifier — looking at the
agent's own container — can see.

## The record

`record.json` per cell: arm, harness version (aforge: repo commit + binary
sha256 + which tier config; pi/opencode: their `--version`), model, image id and
ref, task repo commit, started/ended/wall/verify wall, outcome and settle reason,
reward, partial_score, pass_rate, passed/total, holdout block, `per_method`,
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

## Deviations from the benchmark, stated

1. **Network is not the task's allowlist.** `task.toml` asks for
   `network_mode = "allowlist"` over `crates.io`, `index.crates.io`,
   `static.crates.io`, `github.com`, `static.rust-lang.org` plus the model
   endpoint, for both the agent and the verifier phases. These cells run on
   docker's **default bridge with full egress**. No egress allowlist was built
   for this run. It is recorded in every `record.json` (`network_deviation`) and
   in `/logs/verifier/oneroad_stages.json`.
2. **No per-container storage quota.** `task.toml` asks for 20480 MB; docker's
   overlay2 on this host enforces no per-container disk quota.
3. **The wall is the runner's, measured from container start.** The container is
   created immediately before the prompt is sent, so the agent's `timer.sh`
   reading and the cell's own wall agree to within a few seconds — but the few
   seconds are the runner's, not the benchmark's.
4. **Snapshot scoring is extra load the benchmark does not have** — two CPUs and
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
