---
kind: added
title: the canary runs real GitHub issues through both doors and grades them with the fix's own tests
pr: 465
surface: [build, chat, engine]
invalidates:
  - "There was no standing measurement of whether a build could still finish an ordinary issue; bench/run.sh compared harnesses on four bambara issues by hand, and bench/e2e ran seven synthetic task shapes. There is now `bench/canary/`: a frozen pool of nine real closed issues (each fixed by a merged in-repo pull request, validated fail-to-pass at base and gold-green offline), run through `aforge do` and a tmux-driven `aforge chat` on one build pinned to deepseek/deepseek-v4-flash, each cell in its own AFORGE_HOME, graded by the fix pull request's own test files. Every run appends a scoreboard to issue #407."
  - "A benchmark row carried one pass/fail. The canary reports two verdicts as separate columns — the door's own (ok, partial, asked, wall, stall, crash, noframe) and the tests' (green, red, regressed) — because a door that calls green work partial is a product defect the table must show, not fold away; the delivery-gate round count is its own column for the same reason."
  - "bench/e2e's bare `pytest -q` summary was assumed to start with `=`; under -q pytest prints it bare (`1 failed, 11 passed in 0.25s`). bench/canary/pick.py reads both spellings; a picker that read only the decorated line rejected every correctly failing candidate as already passing."
---

The baseline is 35c1a79e (checkpoint/2026-09-02/dev-green-after-revert). Cost is
the call log's sum over calls that came back, wall is the door's own clock, and the
box's one-minute load average is recorded at the start and end of every cell,
because a wall under load 100 is not a wall under load 5. Nothing in `make check`
reaches the rig; it spends real money and is on demand.
