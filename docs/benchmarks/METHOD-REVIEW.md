# Method review: would a competitor's engineer accept these numbers?

reviewed tree: `e5fde7fa49467ebd625a018cc43409e0cb0cab50` — this branch as the review found it
review taken: 2026-09-18, morning EDT. Fixes applied since are listed under "Fixes applied" below.

## The question, answered

**Would a competitor's engineer, handed `measure-cli.sh` and `README.md`, accept every
number in `results-2026-09-17.md` as a fair like-for-like measurement?**

**No — not every number.** The startup, first-frame, idle and startup-work columns would
survive their reading: the script is inspectable, every CLI ran under the same isolated
profile, private tmux socket, pinned geometry and same TERM, and the README states most
of the caveats that matter. Three things would not be accepted, and none of them is in
the startup comparison itself:

1. **Three of the seven version rows name versions that have not existed on this box
   since before the session ran.** pi 0.73.1, opencode 1.18.31 and omp 18.2.4 — every
   conflicting install's mtime predates the 2026-09-17 evening session, so the box's
   PATH could not have printed those versions that day. A competitor re-checking gets a
   different answer from the table.
2. **The on-disk column's counting rule is stated nowhere** (`measure-cli.sh` does not
   measure disk at all), and its rows cannot be reproduced: two match natural trees
   exactly (one of them the wrong version), four match nothing on the machine.
3. **The one-turn column cannot be audited**: its sampler is not in the repository, so
   nothing committed lets a second engineer re-run or check it.

Neither gap invalidates the headline startup comparison — but both are the kind of thing
a competitor's engineer finds in an afternoon, and the first is a table going out while
we compete on it.

## Conditions of this review

| time (EDT) | load 1m | 5m | 15m | note |
| --- | --- | --- | --- | --- |
| 08:33 | 6.80 | 5.27 | 4.29 | above the bar — no measurement |
| 08:35 | 5.79 | 5.65 | 4.61 | above the bar |
| 08:41 | 2.38 | 3.52 | 3.99 | under the bar from here on |
| 08:46 | 2.02 | 2.34 | 3.31 | probes |
| 08:54 | 2.79 | 2.43 | 2.95 | probes |

No timing figure was taken during this review. The work was file inspection, one-shot
`--version` identity probes and `du` (disk bytes are not load-sensitive). The box was
above the one-minute bar until 08:41 and sat at 1–3 afterwards, so a rerun was possible
in principle; none was required (see "Reruns" below).

## Verdict, column by column

| column in `results-2026-09-17.md` | verdict |
| --- | --- |
| version row | **unfair, fix named** — three rows conflict with the machine |
| on disk (bytes, vs codeaf) | **unfair, fix named** — the rule is nowhere; one row is from the wrong version |
| cold start (`--version`) | **fair with caveat, caveat stated** |
| first frame | **fair with caveat, caveat stated** |
| idle memory (procs, RSS, PSS, peak RSS, threads, fds) | **fair with caveat, caveat stated** |
| idle CPU and voluntary context switches | **fair with caveat, caveat stated** |
| one turn (every figure, including `answered`) | **cannot decide** — evidence that would decide it is named below |
| startup work (strace openat/connect/execve) | **fair with caveat, caveat stated** |

## What each CLI's `--version` actually executes

The proxy question: a wrapper that prints a string without loading the product is not
measuring a booting binary. Checked on the box, not assumed:

| CLI | what the PATH entry is | what `--version` runs | does it load the product? |
| --- | --- | --- | --- |
| codeaf | ELF aarch64, dynamically linked against libc | the binary itself — 1 execve | yes — Go package init runs before `main`, which is exactly why the figure is a floor for this binary |
| codex | the real musl **static** binary (`~/.codex/.../0.154.0/bin/codex`) | itself — 1 execve | yes |
| claude | symlink → `versions/2.1.274`, a native bun-compiled build | itself — 1 execve | yes — the product is embedded in the executable |
| pi | symlink → `dist/bundle/cli.js`, `#!/usr/bin/env node` | node boots, then parses the bundle (the product) — 1,581 openats | yes — through the node runtime pi's users actually run |
| omp | a bun single-file executable, 150 MB | itself — 2 execves | yes — bun runtime plus the embedded CLI |
| opencode | **not on PATH**; two installs exist (`~/.opencode/bin/opencode` compiled binary, and the npm `opencode-ai` wrapper) | the compiled binary — 1 execve; which path the bench invoked is unrecorded | yes |
| cursor-agent | a bash wrapper that execs a **bundled node** on `index.js` — and first runs `node --use-system-ca --version` as a probe | bash + node + index.js — 18 execves, 18 connects (9 to the network) | yes — plus cursor's own update/telemetry chatter, which is part of what its users' `--version` costs |

Verdict on the proxy: **fair with caveat, caveat stated.** The column measures each
CLI's own entry path as installed — like-for-like as a *floor*, not as "time to a
working session" (the first-frame column covers that). The results file already names
the command (`--version`) and its strace table publishes how different the paths are
(1 execve against 18), so a reader is not misled about uniformity of work. One genuine
asymmetry to state: for a CLI whose `--version` phones home (cursor-agent), the figure
includes network round-trips. That is what the real command costs and it is measured
honestly, but it is a different *kind* of work from a static binary's 15 opens — the
table should say so in one line. Wording nit while here: codeaf's ELF is dynamically
linked against libc (codex is truly static); "one static binary" is better written
"one self-contained binary".

## Warm versus cold, and whether the runs were uniform

The script discards `STARTUP_WARMUP` (default 2) warm-up runs and reports the best of
`STARTUP_RUNS` (default 7): by construction a **warm page-cache figure, warm compile
caches included**. The results file's sentence "Cold start is best-of-20" says the N was
set to 20 for that session — the script default is 7 — and no phase=meta line was
archived, so **whether all seven CLIs got the same N, the same warmup count and the same
tool is not verifiable from the repository**. hyperfine is not on PATH today; if it was
absent on the 17th, every figure carries the shell loop's two `date` forks (~2 ms) inside
each sample — uniform, but it puts a floor under 5–7 ms figures. The script does record
`tool=` per run; the table does not quote it.

Also in this column: the table quotes a **range** for six CLIs (5–7 ms) and a **single
figure** for codeaf (12.5 ms). The script emits min/median/mean/stddev for every run;
one statistic must be quoted for all seven.

Verdict: **fair with caveat, caveat stated.** The measurement (best-of-N, warm-ups
discarded, minimum reported — the sample least contaminated by other work) is uniform
and honest. The caveats: it is a warm figure wearing a "cold start" label; N/tool
uniformity is unarchived; the statistic is mixed. Precise fixes: retitle the column
"startup (`--version`, warm)", quote `min_ms` for every row with the median beside it,
and archive the seven `phase=meta` lines with the table.

## Whether the installed form is what a real user gets

- **pi** — the npm-global tree symlinked into `~/.local/bin`: exactly what `npm i -g`
  gives, running on the system's node (v24.19.0). Yes.
- **cursor-agent** — its installer's own layout: bash wrapper + bundled node + JS. Yes.
- **codex** — its own standalone release binary. Yes.
- **claude** — the **native** build (`versions/2.1.274`). claude also ships as an npm
  package that runs `cli.js` under the user's node; the two have different startup
  costs. Both are real distributions, but the table does not say which was measured —
  the unarchived `version_cmd=` line would. Caveat, not unfairness.
- **opencode** — not on PATH at all. A real user installs the npm wrapper; the bench
  evidently invoked a compiled binary by path (1 execve says the wrapper was not run).
  Which path was used is unrecorded. This is the weakest installed-form story of the
  seven: **evidence that would decide it: the run's `cmd=` line.**
- **omp** — a standalone bun-compiled executable at `~/.local/bin/omp`. Fine as a form;
  its *version* is the problem (below).

## Geometry: 160x48

Pinned per run (`window-size manual` + explicit resize), identical `TERM` for all seven,
and every CLI here paints a full-pane TUI, so a wider pane costs all of them roughly
proportionally — it shifts absolute numbers, not the ranking. The README already states
the geometry dependence. Verdict: **fair with caveat, caveat stated** (stated in the
README). A favoritism claim would need a second geometry: re-measure at 80x24 and
compare the ordering — named here as the evidence that would decide it, not taken.

## The three missing one-turn rows

codex (session died under scripted input), pi (no credits), opencode (prompt did not
submit). The table does the honest thing already: reasons stated in the row, no
estimates, the surviving memory figures labeled a floor and not a turn cost, and
codex's absence explicitly *not* claimed as a codeaf win. That is what an honest table
does with a row it could not measure. The fixes are operational, not methodological:
codex needs an input path its session survives (its own automation surface rather than
raw keystrokes), pi needs credits, opencode's submit needs debugging. No edit to the
table makes any of them measurable.

## "On disk": what it counts

The rule is stated nowhere — `measure-cli.sh` measures startup, frame and idle only, so
this column has no committed method at all. Reconstructed from the box:

| CLI | table says | what the box shows |
| --- | --- | --- |
| codeaf | 53,149,961 | the bench build (825ef2c4 + init changes) is not in this tree; not re-derivable without rebuilding at that commit. The codeaf installed today is a different, newer build (74,852,786 bytes) — noted, not a contradiction |
| codex | 296,778,927 | **exact match**: `du -sb` of `releases/0.154.0-aarch64-unknown-linux-musl`. The rule is recoverable here: apparent bytes of the one installed version's tree |
| claude | 223,862,184 | **exact match to the wrong version**: that is `versions/2.1.270` to the byte. The session ran **2.1.274** (installed 17:22 Sep 17, before the session; the version row is right). Under the rule codex proves, the cell should be **230,482,160** — and "vs codeaf" moves 4.2x → **4.3x**. Version dirs are immutable once installed, so this is the same number the bench could have taken |
| pi | 150,721,987 | the npm tree is **111,156,634**, unchanged since 2026-08-24 20:22 — 39.6 MB smaller than claimed, and it was that size on the day too |
| opencode | 184,534,520 | `~/.opencode/bin` is 184,068,240; the npm wrapper tree is 184,075,768 — within 0.25% of both, exactly neither. If the npm wrapper is what a user installs, counting only one of the two trees understates opencode's real install |
| cursor-agent | 536,238,899 | the version dir (unchanged since 2026-09-15 19:43) is **571,446,941** apparent bytes — 35.2 MB apart. The bundled node alone is 125,906,320, so the gap is no obvious single exclusion. Not reconstructible |
| omp | 1,134,595,589 | the only omp on the box is one **156,551,464**-byte bun-compiled executable (mtime 2026-09-07, before the session). No tree remotely near 1.13 GB exists |

The runtime question is real and unresolved: pi runs on the system's node (which its
users already had — not counted), while cursor-agent bundles its node (counted inside
its tree, since it is in the dir). "What the CLI's own install puts down" is a
defensible rule that gives exactly that outcome — but it is stated nowhere, so nobody
can check that it is the rule that was used.

Verdict: **unfair, fix named.** Fix: state the rule in the README ("`du -sb`, apparent
bytes, of the single installed version's tree the launcher resolves to — no caches, no
data dirs, no runtimes the user already had"), correct the claude cell to 230,482,160,
reconcile omp/pi/opencode against the re-pinned versions, and re-take the column once
under that rule (disk bytes are load-insensitive, so it can be taken any time).

## Fact 3: the load average was never recorded

Real problem; it does not invalidate a row. It makes the wall-clock figures
**unverifiable** rather than wrong: the best-of-N minimum bounds the damage for startup,
memory figures are load-insensitive, and the idle CPU figure is the CLI's own
`utime+stime` — foreign load does not enter its numerator (scheduling can still perturb
it). The script now records `load1/5/15` in `phase=meta` and brackets the startup and
idle windows with readings. The 2026-09-17 figures carry the caveat that no load was
recorded that day; the README says to compare runs at similar load.

## Fact 4: two builds in the codeaf row

Disclosed by the table's own header — honest, and **not invalidating**. `--version`
(12.5 ms) is measured on the post-package-init build; the first-frame figure (208 ms)
predates those changes, so it is **conservative against codeaf** — the pre-init build is
the slower one — and the memory rows sit at the noted commit. A competitor can accept
each figure as a real measurement of a disclosed build; what fails the read is that the
header buries which figure is from which build. Fix: one per-figure clause ("`--version`
and the memory rows are post-package-init; first frame predates them"). The named run
fix — re-measuring codeaf's first frame on the final build — was not taken (see
"Reruns"); when it is taken, it should be the whole seven-CLI column in one session, so
the column does not acquire a second day.

## Fixes applied

1. **`measure-cli.sh`** — load average recorded: `load1/load5/load15` in `phase=meta`,
   plus `startup_load1_before/after` and `idle_load1_before/after`. Telemetry only:
   builtin read of `/proc/loadavg`, no fork in the sampling path, no default changed,
   no dependency added. Smoke-run verified end to end (`name=smoke2`):
   `teardown_survivors=0`.
2. **`README.md`** — the invocation example fixed (the old `--name/--home` form parses
   as `NAME=--name`, `HOME=codeaf`, `VERSION_CMD='--home --version'` and cannot run at
   all); the startup bullet now states the warm-up discard, the best-of-N default, the
   warm-cache nature, the uniform-N requirement, and the rule to archive `phase=meta`
   lines; the output claim now names which tables in a results file are this script's
   output; the load recording is documented under the quiet-box rule.

Named but deliberately **not** applied, because `results-2026-09-17.md` is the record of
a session and this review does not rewrite it — carry these over when the corrections
land: retitle the cold-start column and quote one statistic for all seven rows; the
claude on-disk cell 223,862,184 → 230,482,160 (4.2x → 4.3x); re-derive the omp, pi and
opencode version rows; state the disk-counting rule; add the per-figure build clause for
codeaf; add one line saying cursor-agent's `--version` includes network round-trips.

## Reruns: none taken

No column's measurement method changed — the load fix is telemetry and the README is
documentation — so per the instruction, columns with unchanged methods were not rerun.
Independently, the box was above the one-minute bar until 08:41 this morning, and figures
from 2026-09-18 cannot sit beside the 2026-09-17 table under the README's own
same-session rule. If a rerun is wanted: one session, all seven CLIs, correct the version
rows first, then take the startup and disk columns with every `phase=meta` line archived;
the one-turn column additionally needs its sampler committed and codex's input problem
solved before its rows mean anything.

## Evidence that would decide what is still open

- **Which versions the bench actually ran** (omp 18.2.4, pi 0.73.1, opencode 1.18.31):
  the seven runs' `phase=meta` lines — `cmd=` and `version_cmd=`. Without them, the
  version rows stand unreconciled against the box.
- **Uniform N and tool across the seven**: the same meta lines (`startup_runs=`,
  `startup_warmup=`, `tool=`).
- **Which opencode path was invoked, and whether claude ran native or npm**: `cmd=`.
- **Which CLIs met a folder-trust gate before their first frame** (a gate makes
  `frame_ms` include the harness's own keystrokes): the runs' `trust_gate=` values.
- **The disk-counting rule** (the 35.2 MB gap for cursor-agent, the 39.6 MB gap for pi,
  omp's 1.13 GB): the du commands or paths used that day.
- **First-frame geometry fairness**: a re-measure at a second geometry (e.g. 80x24),
  comparing orderings.
- **The one-turn column**: the sampler, committed, or its raw output archived.
