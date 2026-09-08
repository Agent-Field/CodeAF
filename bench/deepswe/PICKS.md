# The five tasks this rig runs, and why they are the five

DeepSWE ships 113 tasks. Running all of them against a small model costs more
than the question is worth, so this rig fixes a **five-task easy set**: the point
of the measurement is to find out where the harness breaks, and a task the model
could never have solved tells you nothing about the harness.

Corpus: `~/src/swe-pro/tools/deepswe-bench/tasks` (dataset `deep-swe-1-1`).

## What "easiest" was scored on

No single field in `task.toml` says how hard a task is, so the ranking combines
five that between them do:

| signal | why it predicts difficulty | where it comes from |
| --- | --- | --- |
| reference-solution lines | how much code the fix actually is | `solution/solution.patch` |
| reference-solution files | how far the change has to reach | same |
| fail-to-pass count | how many separate behaviours are graded | `tests/config.json` |
| pass-to-pass count | how much existing behaviour can regress, and how long a grade takes | same |
| instruction length | how many clauses the brief carries | `instruction.md` |

Two priors from the earlier `codeaf` sweeps (`ANALYSIS.md`, `out/run-*.json`)
were weighted on top of the ranking:

- **A task a small model has already solved once is the best kind of easy** —
  it is proof the tier can reach reward 1, so a zero is the harness's to explain.
  `ofetch-per-origin-circuit-breaker` is the only task in the corpus with a
  recorded `deepseek-v4-flash` reward of 1 (`out/run-flash-hard.json`).
- **Language and toolchain matter more than repo size here.** The host is
  arm64 and every task image is amd64, so everything runs under qemu. Python and
  Node suites emulate acceptably; Rust and Go suites spend the budget compiling.
  All five picks are Python or TypeScript for that reason.

Tasks with a big pass-to-pass whitelist were dropped even when they scored well
otherwise: `httpx-streaming-json-iteration` ranks high on solution size but
carries 108 fail-to-pass and 1404 pass-to-pass tests, and under emulation that
is a grade measured in tens of minutes for no extra signal.

## The five

| task | language | repo | ref patch | f2p / p2p | why it is in the set |
| --- | --- | --- | --- | --- | --- |
| `ofetch-per-origin-circuit-breaker` | typescript | unjs/ofetch | 614 lines, 3 files | 47 / 13 | the control: `deepseek-v4-flash` has already scored reward 1 on it |
| `ink-grid-box-layout` | typescript | vadimdemedes/ink | 485 lines, 3 files | 25 / 49 | smallest combined score in the corpus that is not Go or Rust |
| `textual-richlog-follow-state` | python | Textualize/textual | 628 lines, 3 files | 20 / 6 | tiny graded surface — 26 whitelisted tests in total |
| `igel-persist-feature-schema` | python | nidhaloff/igel | 603 lines, 5 files | 24 / 2 | a small repo, and only two pass-to-pass tests to regress |
| `happy-dom-deterministic-intersectionobserver` | typescript | capricorn86/happy-dom | 766 lines, 8 files | 14 / 9 | fewest fail-to-pass tests of any short-brief task |

Every one of the five scores reward 1 on its own reference solution through this
rig (`gold.sh`), which is the only evidence that a zero from a model run means
what it says.

## Three more, for the 2026-09-04 chat-versus-do campaign

Eight tasks were wanted rather than five. The same five signals, scored over the
v1.1 corpus restricted to Python, TypeScript and JavaScript, and a preference for
a task whose verifier image was already on the host, picked these three:

| task | language | repo | ref patch | f2p / p2p | why it is in the set |
| --- | --- | --- | --- | --- | --- |
| `cattrs-partial-structuring-recovery` | python | python-attrs/cattrs | 632 lines, 3 files | 69 / 7 | seven pass-to-pass tests: the smallest regression surface left in the corpus |
| `aiomonitor-task-snapshots-diff` | python | aio-libs/aiomonitor | 612 lines, 5 files | 53 / 8 | eight pass-to-pass tests, a short brief |
| `ts-pattern-match-each` | typescript | gvergnaud/ts-pattern | 625 lines, 5 files | 85 / 6 | a second control beside ofetch: `deepseek-v4-flash` solved it outright on the codeaf harness |

All eight score reward 1 on their own reference solution through `gold.sh`.

Two things moved under the rig between s14 and this campaign, both recorded in
every result's meta.json. The corpus is `github.com/datacurve-ai/deep-swe` now
(117 tasks at v1.1; the old `~/src/swe-pro/...` path is gone) and `CORPUS`
names the clone. And `task.toml` says `agent.timeout_sec = 10800` where the
sweeps ran under 5400; the campaign pins `AGENT_SECONDS=5400` so its walls read
against s1–s14, and `agent_seconds_budget` says which wall a row ran under.

## The host deviation that has to be published with any number

The task images are `linux/amd64`; this host is arm64. Both the agent container
and the verifier container therefore run under `qemu-x86_64`, which needs the
binfmt handler registered on the host first:

```sh
docker run --privileged --rm tonistiigi/binfmt --install amd64
```

Under that emulator a multi-threaded Go program dies with `runtime:
lfstack.push invalid packing` — Go packs a pointer into 48 bits and sign-extends
it back, so it needs every address below 2^47, and this kernel hands qemu mmap
results above that line. It is raised from the garbage collector, so `GOGC=off`
(with a `GOMEMLIMIT` backstop at three quarters of the container's own memory
cap) avoids it entirely; the rig sets both whenever it detects it is emulating.
This matters because it is silent: esbuild crashes, vitest reports no tests, and
the grader scores a clean **0 against the task's own reference solution**.
`ofetch` did exactly that before the guard went in.

Emulation also makes every run slower than the same run on amd64 hardware, so
wall-clock numbers from this rig are comparable **to each other** and not to the
Modal figures in `out/run-*.json`.
