# What the first run shows

One full run on 2026-10-05: `codeaf do` headless with every model knob pinned to
`deepseek/deepseek-v4.1-flash`, reviewing the pull-request shape of all 165 rows
DeepSource judged, three cells at a time, 900 s wall each, judged blind by
`anthropic/claude-opus-4.5`. Rig at `9d1dd0a6`, binary `9a0f8d…` (meta.json has
the full hash). The evidence is in `evidence/`, exported by `export.py` in the
same row shape DeepSource published; the bulk is under
`~/Code/openssf-cve-bench/`.

**The row sits at the top of their table on F1, accuracy and recall, and
second on precision.** That is the finding; the rest of this file is what it
rests on and what it does not say.

## The table

| tool | F1 | precision | recall | accuracy | TP | FP | TN | FN | rows |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **codeaf, deepseek-v4.1-flash, `s1`** | **93.25** | 93.83 | 92.68 | 93.33 | 76 | 5 | 78 | 6 | 165 |
| DeepSource | 84.51 | 100.00 | 73.17 | 86.67 | 60 | 0 | 83 | 22 | 165 |
| Cursor Bugbot | 80.45 | 74.23 | 87.80 | 78.79 | 72 | 25 | 58 | 10 | 165 |
| Devin | 78.08 | 89.06 | 69.51 | 80.61 | 57 | 7 | 76 | 25 | 165 |
| OpenAI Codex | 77.70 | 94.74 | 65.85 | 81.21 | 54 | 3 | 80 | 28 | 165 |
| Greptile | 68.61 | 85.45 | 57.32 | 73.94 | 47 | 8 | 75 | 35 | 165 |
| Claude Code | 62.99 | 88.89 | 48.78 | 71.52 | 40 | 5 | 78 | 42 | 165 |
| GitLab Duo | 61.42 | 92.86 | 45.88 | 71.18 | 39 | 3 | 82 | 46 | 170 |
| Semgrep CE | 36.70 | 74.07 | 24.39 | 58.18 | 20 | 7 | 76 | 62 | 165 |
| CodeRabbit | 36.19 | 82.61 | 23.17 | 59.39 | 19 | 4 | 79 | 63 | 165 |
| *gold (the oracle)* | *99.39* | *100.00* | *98.78* | *99.39* | *81* | *0* | *83* | *1* | *165* |
| *null (the floor)* | *0* | *—* | *0* | *50.30* | *0* | *0* | *83* | *82* | *165* |

Their rows are recomputed from their judged JSONL at `0f9a1e00` with
`score.py`'s formulas (`comparison/build.py`); Claude Code reads 62.99 here
against 62.40 on their page, and the rows are what can be checked.

## What it cost

| | |
| --- | --- |
| tool spend, self-reported by `codeaf do` | $4.93 for 165 cells, 3.0¢ a cell |
| judge spend | $0.70 for 90 calls; 75 cells had no findings and needed none |
| tool time | 8.9 h summed; median 145 s, mean 193 s a cell |
| wall | about 3.3 h at three cells, across two sittings (a two-hour ceiling stopped the first at 111 rows; `RESUME=1` finished it) |

## The three cells that hit the wall

`CVE-2017-16003/fixed`, `CVE-2019-10759/fixed` and `CVE-2019-10090/unfixed`
ran out their 900 s. Each is scored as the report it delivered. The two fixed
ones delivered nothing and landed as TN, which is a crash counted as a quiet
review and flatters by up to two rows; the vulnerable one had written its
findings before the wall and was judged on them. They are left as they ran,
because a number changed after it was seen is the number a reader distrusts.

## Where the misses are

Six vulnerable rows were not hit. In every one of them codeaf reported
something; the judge refused the match:

| CVE | what codeaf reported | what the CVE is |
| --- | --- | --- |
| CVE-2017-16224 | path traversal | open redirect |
| CVE-2017-18355 | a different disclosure | absolute paths leaking through `_where` in package.json |
| CVE-2018-3783 | a different query | blind MongoDB injection in password reset |
| CVE-2019-5483 | environment exposure in `lib/common.js` | the recorded weakness in `lib/print.js` |
| CVE-2020-7660 | a different path | RCE through `deleteFunctions` during deserialisation |
| CVE-2020-7763 | a different weakness | uncontrolled path expression at `lib/conversion.js:14` |

Per CWE on the vulnerable rows: command injection 22/22, XSS 20/20, ReDoS
20/20, argument injection 12/12, code injection 12/12, prototype pollution
10/10, path traversal 8/9.

The five false positives are all cases where codeaf reported the CVE's
vulnerability on the revision that fixed it: CVE-2017-16029, CVE-2017-16100,
CVE-2018-16478, CVE-2018-7651, CVE-2020-7699. On CVE-2017-16029 DeepSource's
judge read the same kind of finding as "a flaw in the fix" and let it pass;
ours called it the CVE. Both readings are in `evidence/`.

## The judge, checked three ways

- **Oracle.** `gold` reports the dataset's own weakness location on every
  vulnerable row and nothing on the fixed ones: 81 of 82 hits, zero FP. The one
  miss is CVE-2017-18353, where the OSV text describes an unauthorised shutdown
  and the benchmark's label points at an XSS line; the dataset disagrees with
  itself, and that caps any tool's recall at 98.78.
- **Floor.** `null` scores accuracy 50.30, the share of fixed rows.
- **Against theirs.** Their own findings for Claude Code and Semgrep, replayed
  through our judge on 40-row samples: 39/40 and 38/40 agree, every
  disagreement ours refusing a hit theirs allowed (`evidence/calibration/`).

## What this does not say

- "165 CVEs" is 85 CVEs; the rows are revisions.
- Precision here is only "stays quiet on the fix". Unrelated noise is not
  measured by this benchmark at all.
- The recall gap to every other row is far too large to be reviewing skill. The
  CVEs are 2016–2021 JavaScript, among the most-cited of their kind, and the
  brief names their classes; a model that has read the advisories finds them.
  The precision column is the one worth pointing at: 5 FP under a judge that is
  stricter than theirs, against Bugbot's 25 and Devin's 7.
- Their rows are products as of April 2026; `deepseek-v4-flash` itself reached
  OpenRouter on 2026-04-24, after their table. A contemporaneous comparison
  means running Claude Code or Codex through this rig today, which the driver
  contract makes a thirty-line job.
- One run, one seed. The interim score moved between 91.5 and 94.1 as rows
  accrued; a second seed is what would turn that into a spread.

The sentence the number supports: *on DeepSource's published slice of the
OpenSSF CVE Benchmark, with their protocol and a judge calibrated against
theirs, codeaf's row is the top one.* It says nothing about finding unknown
vulnerabilities and nothing about Go.
