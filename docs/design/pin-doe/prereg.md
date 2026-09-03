# Pre-registration: what a pin means after the wire refuses it

Owner aforge-v2-e6. Written 2026-09-02 before any arm was built or any cell run.

## Question
After the router refuses a person's pinned lane for a model (`permits only:` 404),
which pin semantics is Pareto-best on cost, quality and wall time, on real issues,
on deepseek-v4-flash, through the chat door?

## Arms (factor 1, four levels)
- A  pin absolute — santosh-75's #368 branch at its rebased tip, byte for byte. Re-demands the pinned lane on every request; the ladder drops the demand after each 404.
- B  pin yields after refusal, one visible line: `X cannot serve this model; routing on auto for this model until you pin again`. Built on A; retirement in lanepin.go consuming laneRefusal at laneChoiceFor; the line travels as RescueNews.
- C  pin yields silently — control only. Same as B without the line. Cannot win (no-silent-substitution law); its numbers bound the cost of the line.
- N  null baseline — A's binary with no pin (`lane.talk` unset, auto). Bounds what pinning costs at all.

## Briefs (factor 2) and replicates
k = 4 to 6 validated anchors from aforge-v2-14's pool (`~/af-canary/bench/canary/pool.json`), cited by pool id, chosen BEFORE any arm runs as the first k validated in pool order; n = 2 per (arm, brief). Cells = 4·k·2 (32 to 48).
k is fixed after the #407 baseline table: the largest k in [4,6] with 4·k·2·(mean baseline cost per cell) ≤ $8, keeping $2 reserve under the $10 budget.

## Held constant
Model deepseek/deepseek-v4-flash on model.talk and all four tiers, `-one-model`; chat door via canary_chat (lib/chat.sh) with canary_home rows; cap $1 per cell, wall 900 s; the pin is `lane.talk: anthropic` on A, B, C — a lane that never serves this model, so the refusal is deterministic (confirmed on the live wire 2026-09-02: HTTP 404, "…permits only: anthropic", metadata.failed_routing_step "Filter by Allowed Providers"). Same price table: OpenRouter's flash endpoint prices snapshotted at run start and applied to every cell from token counts; the ledger's own cost is reported beside it as a check. Same base sha under all four binaries.

## Order and blinding
Run order is a seeded random permutation of all cells, blocked by brief so each brief's 8 cells are spread across the run, and no arm runs twice in a row where avoidable (router load drifts by the hour). Each cell's output dir is named by an opaque id; the id→(arm, brief, replicate) map is written sealed before the run and opened only after every judge.json exists. Scoring is lib/judge.py, mechanical: fix-PR tests overlaid after the door stops, f2p must be green, no new failures against base.

## Metrics
Primary: cost_usd (price table × tokens; ledger cost as check), wall_s (door start to the canary end rule), quality (judge pass, 0/1). Secondary: redemand_count (calls whose end row is the router's routing refusal, i.e. the pinned lane demanded again after a refusal), ttft_ms (median over the cell's calls, and the first call alone), and visible_lines: the count of person-visible sentences the arm prints about the pin, the refusal, or auto — status-line rows and conversation lines — counted mechanically from the cell's frames.log and screen.txt by a fixed needle list written before the run (`refused`, `permits only`, `cannot serve`, `routing on auto`, `switch to auto`, `pinned`). What the person had to read is part of quality under the user's law; B's one line, A's repeated refused rows and C's silence are the difference a person feels.

## Pre-registered expectations
H1 redemand_count(A) ≥ number of turns; (B) = (C) = 1; (N) = 0. H2 wall(A) − wall(B) ≥ one 404 round trip per turn; wall(B) ≈ wall(C). H3 quality equal across A, B, C (same model answers every turn; the pin only adds refused round trips) — any gap is noise at n=2 and will be reported as such. H4 cost equal across A, B, C (a 404 is not billed). If these hold the front is {B, C} and B is the pick because C cannot win. If H2 fails (A's penalty inside the noise band) the front is {A, B, C} and the decision reverts to argument; that outcome is reported, not hidden.

## Decision rule and tiebreak
Minimise (cost, 1−quality, wall). Front = non-dominated arm means. Tiebreak, in order: quality (passes out of 2k), then cost, then wall. Noise band: two arms tie on a metric when their means differ by less than the mean absolute difference between replicates over all cells for that metric.

## Exclusions (reported, never silent)
A cell with an unclosed ledger row (aforge-v2-14's sighting; likely #334, fix #357) is excluded from wall and ttft and listed. A cell that hits the cap counts as quality 0 and is listed. A cell where the refusal did not occur on A/B/C is invalid and re-run once.

## Pilot before the main run (one cell, arm A, anchor 1)
Verifies that the 404 refusal is observable in `<home>/logs/calls.jsonl` and the turn recovers, and that all five metrics extract from the cell's files. If the ledger does not record the refusal, redemand_count is taken from AFORGE's debug log; if neither, the arms run against santosh-75's lanestub with SheetOnly for redemand and wall only, and quality is not claimed from that run (the stub does not answer with a real model).

## Builds
A: #368 tip, `make build` in its own worktree. B, C: Opus lanes (Fable if Opus is still limited) on branches off A; diff confined to lanepin.go, laneChoiceFor, one RescueNews line; santosh-75's names kept. Each arm's bin/aforge is passed as CANARY_BIN. The winning arm lands later as a PR on top of #368's merge, pin policy only.

## Deliverable
The table of arm means with replicate ranges, the Pareto front, the secondary table, the exclusion list, and the sealed map. The user or santosh-3c picks.

## Amendment before the first cell (2026-09-02 15:52 EDT)
The pool holds nine validated anchors in this order: reef-145, attrs-1416, packaging-1318, click-3740, tox-4031, build-860, virtualenv-3072, astroid-2305, sphinx-11437. The rule "first k in pool order" would take packaging-1318, whose fail-to-pass set is 53,420 tests and whose grade is slow enough that eight cells of it would dominate the run's wall for a reason that has nothing to do with the arms. It is skipped, and the briefs are the next four in pool order: reef-145, attrs-1416, click-3740, tox-4031. Declared here before any cell ran; the sealed map was written with these four. Cells run three at a time; the box's one-minute load is recorded at each cell's start and end and reported beside wall.

## Scoring note added during the run (16:20 EDT), before any cell was scored
aforge-v2-14 reports that pytest's cleanup of the shared /tmp/pytest-of-santosh can die under other sessions' pytest runs and leave a grade reading "0 passed 0 failed" with an rm_rf traceback in f2p.log. The driver copy this run uses predates the per-cell TMPDIR fix and is not edited while cells are in flight. Rule: after the last cell, every cell whose judge shows zero tests collected and an rm_rf traceback in f2p.log is re-graded with CANARY_REGRADE=1 through the canary lib on dev (no door re-run, no spend), and the report lists which cells were re-graded. A grade is never changed by hand.

## Amendments at scoring (18:25 EDT), declared before the front was read
1. The exclusion for unclosed ledger rows is withdrawn: they proved endemic (a start row per cancelled hedge arm, the #334 accounting class), and wall is the door's own clock while first-token latency sits on end rows, so neither depends on them. The count is reported per cell instead.
2. One cell (tox-4031, arm N, replicate 1) has no judge record because the judge crashed when the whole-suite step hit its cap; its f2p.log shows the issue's own tests passing, so the pre-registered quality (the issue's tests pass) is read from that log and the cell is marked.
3. Quality is exactly the pre-registered one, the issue's tests pass with no regression; the driver's "pass" also folds in the wall and is not used.
4. Visible lines count distinct sentences, not sampled frames.
