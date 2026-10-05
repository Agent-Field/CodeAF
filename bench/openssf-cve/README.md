# The OpenSSF CVE rig

Does a tool report the CVE a real repository revision carries, and does it stay
quiet on the revision that fixed it? That is the whole measurement. The
[OpenSSF CVE Benchmark](https://github.com/ossf-cve-benchmark/ossf-cve-benchmark)
is 223 real JavaScript and TypeScript CVEs with the vulnerable and the fixing
commit recorded for each. In April 2026 DeepSource ran eight tools over 165
rows of it and [published a table](https://deepsource.com/benchmarks) with
Claude Code, Codex, Cursor Bugbot, Devin, Greptile, CodeRabbit and Semgrep on
it, judged by Claude Opus 4.5. This rig reproduces their protocol closely
enough that a row for codeaf can be read beside theirs, and it says out loud
where the two differ.

It is to the security review what `bench/deepswe/` is to `/senior-dev`: the one
public table the release note can quote. Read "What the number means" before
quoting it.

## Run it

```sh
make build                                        # the codeaf driver runs bin/codeaf
bench/openssf-cve/fetch.sh                        # dataset, OSV prose, DeepSource's rows
bench/openssf-cve/prepare.sh                      # checkouts for the 85 CVEs (~3 min)
bench/openssf-cve/run.sh null  - floor            # the floor: no findings, no model
bench/openssf-cve/run.sh gold  - oracle           # the oracle: must score F1 100
bench/openssf-cve/run.sh codeaf deepseek/deepseek-v4.1-flash s1   # the product
```

A run writes `~/bench-openssf-cve/results/<tool>-<mode>-<model>-<seed>/`
(`BENCH_ROOT` moves it; it is never inside the checkout, because the checkouts
it holds are whole repositories and `go build ./...` walks the filesystem), with one directory per
row holding the tool's report (`findings.json`), what the driver kept
(`do.json`, `run.log`, `brief.md` and the run store under `home/` for codeaf), the judge's verdict
(`judge.json`) and the answer the judge was shown (`truth.json`, copied in
only after the tool exited). `summary.json` is the row, and `score.py` prints
it under DeepSource's rows:

```
tool                                        F1    prec  recall     acc    TP  FP  TN  FN  rows
gold (diff, -)                          100.00  100.00  100.00  100.00     2   0   2   0  4
  deepsource [DeepSource 2026-04]        84.51  100.00   73.17   86.67    60   0  83  22  165
  cursor-bugbot [DeepSource 2026-04]     80.45   74.23   87.80   78.79    72  25  58  10  165
  ...
```

The provider key is found the way the rest of this laptop finds one and is
never written under the rig: `API_KEY`, `OPENROUTER_API_KEY`,
`~/.config/openrouter/key`, the profile's `api_key`, then the macOS Keychain
item named by `KEYCHAIN_ITEM` (default `delta-openrouter`). The codeaf driver
hands the key to the binary through the shell variable, which outranks every
profile rung, so no profile under `results/` ever carries it.

Knobs, all environment variables: `MODE` (below), `SET` (`deepsource`, `all`,
or a file of `cve<TAB>variant` rows), `SAMPLE=N` (a seeded subset),
`JOBS` (cells at once, default 2), `TIMEOUT` (seconds per cell, default 900),
`RESUME=1` (keep the result directory and skip every cell already past its
tool stage, for a campaign a wall or a closed lid stopped), `NO_JUDGE=1`
(judge later with `judge.py <results-dir>`), `JUDGE_MODEL`
(default `anthropic/claude-opus-4.5`, DeepSource's), `CODEAF_BIN`,
`KEEP_WORKSPACE=1`. Pass CVE ids after the seed to run only those.

## Two shapes

`MODE=diff` (default) is what DeepSource measured. For every row the checkout
is a two-branch repository: `main` is the tree at that commit with the files
the fix touched removed, `review` adds them back in full, and the tool is asked
to review the pull request. A reviewer of their draft PR saw exactly that: the
whole file, as new code, with the rest of the repository as context. The files
on the PR are everything the fix changed plus the recorded weakness location,
because they are not always the same file (CVE-2017-16003's fix is in
`constants.js`; the weakness is recorded in `download.js`).

`MODE=repo` is the tree at that commit, one commit on `main`, and the tool is
asked to audit the repository. Nothing points at the file. This is sec-af's
native shape and the harder question; a repo-mode number is a different number
and must not be put on DeepSource's table.

## The judge

`judge.py` is blind: it is shown the CVE's public prose (OSV, or the text
DeepSource's rows carry where OSV has none), the CWEs, the weakness location
the dataset records in the vulnerable revision, and the tool's findings as
`file`, `line`, `explanation`. It is told neither which tool wrote them nor
which variant it is looking at. It applies DeepSource's three criteria — the
same security impact, the same attack pattern, the exact vulnerability instance
— and two refinements their published reasonings apply: a finding that a fix
is inadequate, bypassable or incomplete is not a hit, and a different weakness
in the same file is not a hit. The variant is applied afterwards:

| variant | hit | no hit |
| --- | --- | --- |
| unfixed | TP | FN |
| fixed | FP | TN |

A cell with no findings is decided without a call. Temperature 0, one call per
cell, about $0.006 on Opus 4.5; a full codeaf run judges at most 165 cells.

**Calibration.** `tools/replay.sh` feeds our judge the findings DeepSource's
judge was shown for one of their tools (their `processed` rows) and
`calibrate.py` compares verdict with verdict, so the only thing that can differ
is the judge:

```sh
REPLAY_TOOL=claude-code SAMPLE=40 bench/openssf-cve/run.sh replay - cal1
bench/openssf-cve/calibrate.py ~/bench-openssf-cve/results/replay-diff-nomodel-cal1
```

Measured on 2026-10-05, 40 rows per tool, seed `cal1`, judge `anthropic/claude-opus-4.5`:

| replayed tool | cells | agree | ours only | theirs only |
| --- | --- | --- | --- | --- |
| claude-code | 40 | 39 (97.5%) | 0 | 1 |
| semgrep | 40 | 38 (95.0%) | 0 | 2 |

Every disagreement is ours being stricter: a Semgrep "non-literal RegExp" rule
firing at line 8 when the CVE's polynomial regex is at line 64, path-traversal
findings on a different code path of the same file, command-injection
findings on other calls in the same file. Their judge counted those; ours does
not. So this rig's judge can only pull a number down relative to their table,
never up, which is the direction a comparison should err in. About $0.13 per
40-row replay.

## What the number means, and what it does not

- **"165 CVEs" is 85 CVEs.** DeepSource's 165 rows are 82 vulnerable and 83
  fixed revisions of 85 CVEs; five CVEs appear on one side only. The table's
  denominators are rows, and this rig's are too.
- **Precision here measures one thing: recognising the patch.** A false
  positive is the tool reporting the CVE's vulnerability on the revision that
  fixed it. A tool that reports twenty unrelated things on every revision pays
  nothing for it on this benchmark. Noise is not measured.
- **The CVEs are from 2016–2021 and the dataset has not moved since 2023.** The
  repository was last pushed in March 2024 and has 174 stars; its 2026 life is
  DeepSource's table and one academic fork (the "OpenSSF CVE Benchmark for
  AI", SoftwareX 2025) that adds method-level matching for AI tools. Every model has seen these CVEs,
  their advisories and their fixes. A high recall here is consistent with
  recall from memory; it is not evidence against it. The dataset's own
  `postPatch` is the only defence, and only against the crudest form.
- **DeepSource's rows are vendor-run.** They published the judged JSONL and the
  raw tool outputs; they did not publish the harness. `comparison/build.py`
  recomputes every row from their JSONL with this rig's own formulas, and the
  table carries those figures. One row differs from their page: Claude Code is
  62.40 F1 on the page and 62.99 from the rows; the rows are what we can check.
- **Each vendor's tool ran its own prompt.** Claude Code ran its own
  `/security-review`, which excludes denial of service — and 50 of the dataset's
  223 CVEs are ReDoS. codeaf's brief (`tools/codeaf.sh`) names ReDoS. That is a
  difference in what each tool was asked, and it is inherent to a table of
  products.
- **This host is not theirs.** They opened real draft pull requests on fresh
  GitHub repositories; this rig builds the same two branches locally. The judge
  is the same model; the prompt is this rig's, calibrated above.

So the honest sentence is: *on DeepSource's 165-row slice of the OpenSSF CVE
Benchmark, judged by the same model with a published prompt that agrees with
theirs on 96% of cells, codeaf scores F1 N beside Claude Code's 63 and
Codex's 78.* It is not evidence that codeaf finds new vulnerabilities, and it
says nothing about Go.

The first measurement is the `s1` run of `codeaf` on `deepseek/deepseek-v4.1-flash`
in diff mode; its `summary.json` is the record. The smoke before it, one CVE
through the whole pipeline, hit the vulnerable revision and stayed quiet on
the fix at $0.04 and 225 seconds for both cells.

## The drivers

A driver is `tools/<name>.sh`. It runs with the workspace as its directory and
these in the environment: `BENCH_CVE`, `BENCH_VARIANT`, `BENCH_MODE`,
`BENCH_WORKSPACE` (a fresh copy of the checkout, discarded afterwards),
`BENCH_FILES` (the PR's file list), `BENCH_OUT` (the cell directory),
`BENCH_MODEL`, `BENCH_TIMEOUT`, and for the two that need a model,
`OPENROUTER_API_KEY`. It must write `$BENCH_OUT/findings.json`:

```json
{"findings": [{"file": "src/download.js", "line": 38, "title": "…", "description": "…", "cwe": "CWE-829", "severity": "high"}]}
```

and may write `$BENCH_OUT/cost.json` with `cost_usd` when the tool reports its
own spend. A driver that exits non-zero or writes nothing is recorded as a tool
error AND scored as an empty report, which is what the tool delivered;
`score.py` prints the error count beside the score so a crash can never pass
for a quiet, correct review.

| driver | what it is |
| --- | --- |
| `gold` | the oracle: reports the dataset's weakness on `unfixed`, nothing on `fixed`. The only driver allowed to read the answer. A run of it must score F1 100 or the judge is wrong. |
| `null` | the floor: nothing, ever. Accuracy 50.30 on the DeepSource set, by construction. |
| `codeaf` | the product through its headless door, `codeaf do`, pinned to one model in an isolated profile. **When `/security-review` lands, the brief in this file becomes that command and nothing else changes.** |
| `sec-af` | `af call sec-af.audit` on the workspace, `confirmed` and `likely` findings (`SECAF_VERDICTS`). Run with `MODE=repo`. Not yet exercised on this laptop: neither the `af` CLI nor a node was installed when the rig was written. |
| `replay` | DeepSource's processed rows for `REPLAY_TOOL`, for calibrating the judge. |

## Where things are

- `sets/deepsource-165.tsv` — the rows, as their judged files list them.
- `comparison/deepsource-2026-04.json` — their table, recomputed from the
  JSONL at revision `0f9a1e00`, which `fetch.sh` pins and re-checks.
- `~/bench-openssf-cve/work/` — the dataset clone, bare blob-less clones of
  every repository, the checkouts, OSV records, DeepSource's rows.
- `~/bench-openssf-cve/results/` — runs. Each one carries the rig it ran
  under in `rig/`, because the run re-executes from that copy: editing a
  script while a run is in flight cannot change the run.
- Both sit outside the tree on purpose: a checkout under `work/` carried
  `apn-go/apn.go`, and `go build ./...` found it. `BENCH_ROOT` moves them.

One of the 186 repositories is gone (`linxiaowu66/swagger-ui`, CVE-2016-1000229,
not in the DeepSource set). `prepare.sh` records it as unavailable and
`score.py` reports such rows separately rather than as misses.
