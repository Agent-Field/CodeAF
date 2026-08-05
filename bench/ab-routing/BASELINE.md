# Arm A — the single-model baseline

What today's aforge does on three complex tasks, run end to end through the real
CLI. The design that produced these numbers is in [`DESIGN.md`](DESIGN.md); the
panel the routed arm will use, and the measurements behind it, are in
[`panel.json`](panel.json).

Arm A is not a reconstruction of the default configuration — it *is* the default
configuration. `run-arm.sh` with `ARM=a` sets no environment variables at all,
so the model, the reasoning levels, the spine sampling and the node budget are
whatever `internal/config/config.go` ships.

---

## 1. Calibration rounds

Routerlab's round 1 passed 28 of 36 tasks with every model and carried almost no
information. The protocol here is its fix: run arm A once per candidate task,
and harden anything it aces.

**Two of the three tasks were degenerate on the first try.** Both are recorded
below with their round-1 numbers; the round-1 briefs, answer keys and reference
solutions are kept under `tasks/t*/round1/` rather than deleted, and the cells
are in `calibration-round1.jsonl`.

| task | round 1 | verdict |
|---|---|---|
| t1-logstore | score **1.000**, success, $0.0388, 433 s, 9 turns, 1 node | degenerate — hardened |
| t2-synthesis | score **1.000**, success, $0.0698, 761 s, 47 turns, 5 nodes | degenerate — hardened |
| t3-shiftplan | score **0.714**, fail, $0.1335, 767 s, 143 turns, 11 nodes | calibrated — stands |

### t1-logstore, round 1 → round 2

Arm A wrote a complete and correct `tinylog` **on its first tool call** and
scored 6/6. The diagnosis is not that the model is strong; it is that the brief
specified the record frame byte for byte over a single file, so the task was
transcription. A model that has seen a length-prefixed CRC-checked log before —
and all of them have — reproduces it without deciding anything.

Round 2 keeps the frame, because the format group has to pin something the model
cannot invent around, and adds the parts that cannot be transcribed:

- a **segmented** log that rolls at a `segment_bytes` threshold, with sequential
  zero-padded segment files;
- a **manifest** that recovery is explicitly told not to trust — missing,
  unparseable, or naming a segment that is not there, all fall back to
  discovering the directory, and a segment the manifest forgot must still be
  read;
- **compaction that merges every segment into one** and rewrites the manifest;
- damage **confined to the segment it happened in**, so a torn early segment
  does not discard later ones;
- **100,000 keys**, which forces an index rather than a rescan, and a `scan`
  that is a generator rather than a list.

Re-graded against round 2, the round-1 submission scores **1/9**. The reference
passes 47/47, so the task is hard rather than impossible — which is the only
thing the reference exists to establish.

### t2-synthesis, round 1 → round 2

Arm A scored a perfect 8/8, and *how* is the interesting part. aforge planned
t2 as an **ensemble**: three independent audits of the whole corpus, then a
synthesis node. The three members individually made mistakes — one invented two
contradictions — and the synthesis produced an answer with none of them. Round 1
measured that aforge's ensemble strategy works on a judgment-shaped goal. That
is worth knowing and it is not what this experiment is for.

Round 2 adds two hardenings chosen to punish the merge step specifically:

- **Kafka moved to an account-level volume tier.** Round 1 was flat pricing
  summed per service, which is a loop. The tier is assessed on the *combined*
  volume of every service on the technology, so summing per service never
  reaches the threshold and returns the old flat answer — a wrong number that
  looks arithmetically clean. 257,820 cents becomes 224,820.
- **The 2026-02-27 outage is filed twice**, by two teams, three days apart, with
  differently-measured windows. Counting reports instead of incidents turns 490
  minutes into 675. The re-filing is superseded, it contradicts the earlier
  filing, and it is not a fourth incident; both filings carry explicit
  timestamps so "earlier" is a fact rather than an inference.

### t3-shiftplan — no hardening

6 of 7 defect families repaired, failing only the empty-interval edge case, at
143 turns across an eleven-node graph. `success` requires all seven, so arm A
fails it while getting visibly close. That is the band the brief asked for and
the task stands unchanged.

Worth stating plainly as a limit: **t3's discrimination rests substantially on
one edge case.** `Interval(600, 600).overlaps(...)` is False because an empty
interval shares no minute with anything, which follows from half-open semantics
but is not spelled out in the docstring. A run that reasons it out passes; one
that writes the natural `start < other.end and other.start < self.end` does not.

### A defect in the answer key, found by the run rather than by the checker

The round-1 t2 cell returned a contradiction between the two vendor pricing
schedules. It was marked wrong. **It was right.** Under the brief's own
definition — a disagreement between two documents about the same fact, resolved
by `RULES.md` — doc 07 states a value for a field doc 08 owns, exactly as docs
03 and 09 do for theirs. Round 1 had quietly assumed "superseded" and
"contradicting" were exclusive.

The tell was there before any spend and the selfcheck did not look for it:
`unit_price` sat in the field vocabulary with nothing in the key pointing at it.
Offering a field and then penalising its only correct use is a trap, not a task.
The selfcheck now fails if any field in the vocabulary is unused.

This is the 27th ground-truth defect the three labs have caught between them
(routerlab 10, probelab 16, here 1) and the first caught by a real run.

### Graph shapes, from three briefs of comparable length

| task | nodes | shape the planner chose |
|---|---|---|
| t1-logstore | 1 | a single node, flagged `oversized`, never decomposed |
| t2-synthesis | 5 | ensemble: 1 setup, 3 independent full audits, 1 synthesis |
| t3-shiftplan | 11 | 3 audits → 4 parallel fixes → refactor → exports → synthesis |

Cost tracked node count almost exactly and turns tracked it better: 1 node / 9
turns / $0.039, 5 nodes / 47 turns / $0.070, 11 nodes / 143 turns / $0.134.

**t1 collapsing to one node is a finding, not a configuration mistake.** The
planner marked it `oversized` — it knew the leaf was too big — and still shipped
it as one leaf under the default `AFORGE_MAX_DEPTH=2`. Arm A was not tuned to
avoid this, and arm B must not be either; if the router improves t1, it will be
improving a single leaf's model choice rather than a decomposition.

---

## 2. Results

*(filled in from `results-armA.jsonl` — see §3 for the raw record)*

---

## 3. Spend

| stage | cells | spend |
|---|---|---|
| panel selection (anchor battery, 11 arms × 10 tasks × 2 reps) | 220 | $0.6739 |
| calibration round 1 (3 tasks × 1 arm-A cell) | 3 | $0.2421 |
| arm A, n=3 × 3 tasks | 9 | *(below)* |

Against the $15 cap for the baseline arm.

Cost is the run's own usage accounting, summed by the scheduler from the
provider's per-response figures, for the reason `bench/README.md` gives: the API
key is shared, so an account-level delta around a run measures other traffic
too. It is a different number, not a noisier one.
