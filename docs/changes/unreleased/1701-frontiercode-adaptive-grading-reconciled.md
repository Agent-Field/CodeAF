---
kind: fixed
title: bench/frontiercode — the adaptive grading path runs and the pilot's 15 outcomes reconcile
pr: 1701
surface: [build, docs]
invalidates:
  - "A classical criterion whose reference test overlay conflicted was `rig`, and that rig poisoned the whole run even when the judge had adapted the tests. Three of the pilot's trials (conflicted-files-refname-crash s1-s3) were lost this way. The overlay now applies with one line of reduced context when the agent's own test moved its anchor, phase B runs (the container gets /logs/grade and the adapted patch, and phase B creates its output directory), and a phase-B verdict answers the conflicted classical criterion."
  - "A patch that did not apply raised `UnboundLocalError: judge_input` in `rubric.py phase_a` and produced no verdicts at all, instead of grading the legitimate 0 the code intends. The judge input is now always present in phaseA.json, and the adaptive trigger does not fire for a patch that never applied."
  - "`combine` read phaseB.json as if its criteria were the top-level keys, so a phase B that ran still reported 'judge adapted the tests but phase B did not run'. The verdicts are read from the file's `criteria` key."
  - "`rubric.py phase_b` rebuilt every task with the fixture's hardcoded `make configure compile`, which is not a target most tasks have; it now uses the task's own `[verifier] rebuild_command`, the fixture recipe only as fallback."
  - "The pilot report's aggregate read '11 passing, mean score 0.83 over the 13 graded' while the retained evidence holds 12 numeric grades and 3 rig failures. The correct original rubric mean is 10.75 / 12 = 0.8958, and the shipped pass count is 6 of 15 because a hard scanner flag zeroes a run. ATTEMPT-LEDGER.md carries the corrected per-attempt categories and denominators."
  - "The scanner hard-flags conflicted-files-refname-crash s2/s3/s5 on `github.com/pre-commit/pre-commit/issues/300` found inside the task's own source returned by a `read`; those runs' proxy logs show no github-family egress. The flags are policy outcomes and are kept, recorded in the ledger as false positives of the observation-vs-action distinction."
  - "`fetch-results.sh` silently dropped every `*.log` from the committed evidence because the repository-wide ignore excluded it, so the pilot's raw egress-proxy.log files are not in the branch. The proxy log is now a required artifact and is re-included under evidence/, and --check refuses a run missing it."
  - "A rubric scope prefix of `\"./\"` matched no patch path and `\"\"` matched every path by accident. Both now mean 'no path restriction', and a `./src/` prefix normalizes to `src/`."
---

The adaptive-classical machinery was implemented and self-tested but had never
run against a real agent, and the first pilot found it broken at every seam:
the phase-B container had no grade directory and no adapted patch, phase B did
not create its output directory, combine misread phaseB.json, and phase B
rebuilt with the fixture's recipe. The overlay application also gave up on a
context drift a one-line context reduction resolves. This change fixes those,
adds a stdlib regression suite (`grade/test_rubric.py`, wired into `go test` by
`regression_test.go`) and a fetch-completeness regression
(`tests/fetch-results-check.sh`), and regrades the three retained conflicted
patches to 0.75 / 0.75 / 1.0 with the originals kept beside them.