# Routing A/B — arm A against arm B

**Question.** Phase A settled offline that a cascade over a small panel beats the
single default model on the cost–quality frontier. Does that survive the real
`plan → run` pipeline?

**Answer.** No, and the way it fails is more useful than the headline. Routing
did not help (**3/9 → 2/9**), it saved 32% on spend that the planner's own
variance swamps, and the panel's strong model **served zero calls in the entire
experiment**. The learning check came back positive and *harmful*: the ledger
demonstrably changed the router's mind between run 1 and run 3, and both tasks
it touched collapsed from working to zero.

The apparatus, panel and baseline are in [`DESIGN.md`](DESIGN.md),
[`panel.json`](panel.json) and [`BASELINE.md`](BASELINE.md).

---

## 1. Head to head

Nine cells per arm, three replicates of three tasks, identical briefs, identical
graders, same machine, same day.

| task | A success | B success | A score | B score | A $ mean | B $ mean | A nodes | B nodes |
|---|---|---|---|---|---|---|---|---|
| t1-logstore | 0/3 | 0/3 | 0.667 | 0.667 | $0.144 | $0.038 | 11, 8, 10 | 1, 1, 9 |
| t2-synthesis | **3/3** | **2/3** | 1.000 | 1.000 | $0.073 | $0.071 | 26, 5, 5 | 5, 13, 18 |
| t3-shiftplan | 0/3 | 0/3 | 0.714 | 0.571 | $0.075 | $0.089 | 3, 5, 8 | 3, 8, 4 |
| **overall** | **3/9 = 0.333** | **2/9 = 0.222** | 0.714 | 0.667 | $0.877 total | $0.593 total | | |

Wall clock: A 99.1 min (three concurrent streams), B 94.6 min (sequential).
Not comparable — see section 6.

**The one cell that moved is the one that broke.** t2 went 3/3 → 2/3, and the
lost cell is replicate 3, which scored **0.000** having scored 1.000 in every
other cell of both arms. t1 and t3 are unchanged at 0/3. Nothing improved.

**The 32% cost saving is not a routing result.** Arm B's t1 drew 1-node graphs
in two of three replicates where arm A drew 8–11; that is the planner, not the
router, and `BASELINE.md` already recorded arm A's own replicates spanning 5 to
26 nodes for a byte-identical brief at 5.4x the cost. The node columns are in
the table so the two cannot be read apart.

### Which models the router actually used

| model | role | attempts in arm B |
|---|---|---|
| `~deepseek/deepseek-v4-flash-latest` | mid | **1280** |
| `google/gemma-3-12b-it` | base | 131 |
| `qwen/qwen3-30b-a3b-instruct-2507` | base | **0** |
| `z-ai/glm-4.7-flash` | base | **0** |
| `moonshotai/kimi-k2.6` | **top** | **0** |

kimi-k2.6 is the reason the panel exists — the only model in either phase
significantly above the pack, 20/20 on the anchor battery, theta +4.09. **It was
never called once.** Three of five panel members were never called at all.

---

## 2. The learning verdict: yes, measurable, and harmful

`learning-diff.py` against the fresh-ledger control:

```
reordered with a shared ledger : plan.audit, plan.bind, plan.ensemble,
                                 plan.expand, plan.fanout, plan.ground,
                                 plan.size, plan.spine        (8 of 11)
reordered with a fresh ledger  : none                          (0 of 11)
attributable to the ledger     : all eight
```

The control is what makes this a result rather than an anecdote. With a fresh
ledger per cell the rung order is **identical in run 1 and run 3 for every
class**; with the ledger carried forward, eight classes reorder. The router
learned.

**What it learned was to drop its best model.**

| class | run 1 | run 3 |
|---|---|---|
| plan.spine (and 7 others) | flash > gemma > **kimi-k2.6** | flash > gemma > **qwen3-30b-a3b** |

And the outcome, same tasks, same briefs:

| task | shared r1 | shared r3 | fresh r1 | fresh r3 |
|---|---|---|---|---|
| t1-logstore | 0.667 | 0.667 | 0.667 | 0.778 |
| t2-synthesis | 1.000 | **0.000** | 1.000 | **1.000** |
| t3-shiftplan | 0.571 | **0.000** | 0.714 | **0.857** |

The control run 3 scored 1.000 and 0.857 on the two tasks the shared run scored
0.000 on. The collapse is attributable to the ledger, not to variance, and it
reproduced in an accidental fourth pass
(`results-armB-pass4.jsonl`: t2 0.000, t3 0.000).

---

## 3. Why it happened — two mechanisms, both precise

### 3a. The leaf rating is estimated entirely from failures

Across all nine arm-B cells there were **108 settled `exec.leaf` verdicts**:

```
unverified_success : 103    (move nothing — correctly, nothing checked them)
budget_stop        :   5    (graded negative)
```

A leaf that finishes returns `unverified_success`, because the harness has no
grader in the loop — the deterministic graders in this experiment run *after*
`aforge run` exits. So the only leaf outcomes that ever reach the ledger are the
failures. Flash's `exec.leaf` rating was therefore fitted to a sample that is
**100% negative**, and finished at:

```
exec.leaf   deepseek-v4-flash-0731   -0.98   n=5
```

against gemma's untouched cold-start of -1.00. On `Ability(rating)/price` that
is gemma 0.269/0.150 = **1.79** against flash 0.273/0.180 = **1.52** — so gemma
opens the leaf. Five budget stops, all from t1, flipped leaf routing for every
task on the panel.

`exec.leaf` is **one global class**. There is no conditioning on task, on leaf
size, or on anything else, so a rating learned from t1 — the one task whose
leaves carry 2.2M prompt tokens and hit the 300k budget — was applied unchanged
to t2's document-reading leaves and t3's small repair leaves, where flash had
never failed. That is the whole of the t2 and t3 collapse.

This is exactly the caveat Phase A wrote down, biting in the one place the
harness cannot honour it: *"the cascade's economics require a cheap, trustworthy
verifier."* For planning calls one exists — the schema check — and those classes
behaved sensibly throughout. For a leaf there is none.

### 3b. Measuring the incumbent demotes the top rung

`order()` force-promotes the highest-rated model to the last rung, but only if
`chosen[0] != best.rung`. On a cold panel `best` is kimi-k2.6 (role `top`, +1)
and the rule fires, which is why run 1's cascade ended in kimi.

As flash accumulated *positive* planning observations its rating climbed past
kimi's cold-start prior — `plan.expand +2.31 (n=48)`, `plan.spine +2.21 (n=39)`
— so **flash became `best`**, `chosen[0] == best.rung`, and the promotion stopped
firing. The last rung reverted to whatever ranks third on `Ability/price`, which
is qwen3-30b-a3b at $0.193 over kimi at $2.48.

The panel loses its strong escalation target **precisely as it becomes confident
about the cheap one**, and it loses it because of successes, not failures.

---

## 4. Escalation on the two hard tasks

The brief asked specifically whether the cascade rescued t1's contract-conformance
failures and t3's interval bug. **It did not, and it could not have.**

**t1-logstore.** 21 leaf attempts above rung 0 across the arm. Every escalation
that changed model went `ds-v4-flash -> gemma-3-12b` — from the model measured at
18/20 on the anchor battery to the one measured at 11/20. Score stayed 0.667 in
all three replicates, failing the identical three tests as in arm A
(`garbage_only_segment_opens_empty`, `a_hand_written_segment_is_readable`,
`active_segment_rolls_at_the_threshold`).

**t3-shiftplan.** One escalation in replicate 2, also to gemma. Score 0.571,
below arm A's 0.714 median. The empty-interval family failed in every replicate
of both arms.

Three structural reasons the cascade cannot reach these failures:

1. **The failures are not escalating verdicts.** A leaf that writes a wrong
   `overlaps()` finishes `done` -> `unverified_success`, which does not escalate.
   Only budget stops, turn caps and empty responses do. The contract-conformance
   failures are invisible to the escalation trigger by construction.
2. **Escalation climbs cost-effectiveness, not ability.** `leaf()` picks
   `order[call.Attempt()]`, and `order` is sorted by `Ability/price`. Rung 1 is
   gemma. Escalating *from* an 18/20 model *to* an 11/20 model is a downgrade.
3. **`Escalations = 1`.** Even if rung 2 were the right target, a leaf gets one
   retry, so kimi at rung 2 is unreachable for a leaf under any circumstances.

---

## 5. Router defects found

Reported rather than fixed: each changes what the router does, and patching the
subject of a measurement while measuring it would invalidate both.

| # | defect | evidence | suggested fix |
|---|---|---|---|
| 1 | **Leaf ratings are fitted to failures only.** 103 of 108 leaf outcomes are `unverified_success` and move nothing, so the rating comes from a 100%-negative sample. | `exec.leaf` rating -0.98 from n=5, all `budget_stop` | Require a minimum count of *graded* observations — and of both signs — before a learned rating may override the cold-start prior. |
| 2 | **`exec.leaf` is one global class.** A rating learned on one task's pathological leaves routes every other task's leaves. | 5 budget stops on t1 collapsed t2 (1.000 -> 0.000) and t3 (0.571 -> 0.000) | Condition the leaf rating on node `size`, which is already on every node and is exactly the axis separating t1's oversized leaves from t3's atomic ones. |
| 3 | **Success demotes the top rung.** As the incumbent's rating passes the top model's cold-start prior, the force-promotion rule stops firing and the strong model leaves the cascade. | 8 of 11 classes went `... > kimi-k2.6` -> `... > qwen3-30b-a3b` between run 1 and run 3 | Keep the highest-*role* model terminal regardless of measured order, or compare against the top model's own rating rather than letting `best` be reassigned. |
| 4 | **Leaf escalation climbs cost-effectiveness, not ability.** Rung 1 is the second-best value, which on a barbell panel is the *weakest* model. | every t1/t3 escalation went flash -> gemma | For a leaf retry, pick the highest-rated untried rung rather than `order[attempt]`. |
| 5 | **Leaf escalations record no chain.** `leaf()` passes `nil` for `tried`, so a retried leaf logs `rung: 1` with an empty `escalation`. | every leaf escalation in this arm | Pass the pinned-and-failed slug through, as `cascade()` already does. |
| 6 | **No exploration.** Only the chosen model accumulates evidence, so a model never chosen is never measured. | 3 of 5 panel members had zero observations after 9 cells and ~1,000 calls | An occasional forced trial, or seed new models from an anchor battery — `panel/anchor_battery.py` costs $0.02/model and already exists. |

Defect 6 is what makes the others hard to see coming: the ledger is confident
about exactly one model and has no information about the four it chooses against.

### Harness bugs of my own, fixed

- The per-cell progress line was rewritten from a broken `python3 -c` into a
  heredoc, which broke it worse — the heredoc *is* stdin, so the piped row never
  arrived. It is `lastrow.py` now.
- `summarize_events` keyed escalation on the `escalation` field, which defect 5
  leaves empty, so it counted zero leaf escalations. It counts `rung > 0` now,
  and `reroute.py` recomputed the existing rows from their stored events rather
  than re-running the cells.
- Output paths took their case from `$ARM`, writing `results-armb.jsonl`.

---

## 6. Method notes and what damages what

**Two background kills interrupted the shared-ledger arm**, and my second resume
was launched while the first was still running, so two writers briefly shared one
ledger and one results file. What that does and does not damage:

- **the ledger is intact** — locked per update, event log append-only; no
  observation lost or double-counted;
- **the scores are intact** — every cell had its own plan, workspace and grader;
- **per-cell event attribution in the overlapping passes is approximate**, since
  the runner slices the append-only log by line position.

`dedupe-armB.py` splits the passes; `results-armB-pass4.jsonl` holds the
accidental fourth pass, kept because it independently reproduced the collapse.

**`results-armB.jsonl` was then deleted by my own `rm results-armb.jsonl`** —
this filesystem is case-insensitive, so the two names are one file. Every cell
directory survived, so the nine rows were recomputed from them rather than
re-run: cost, tokens, graph and grade come from each cell's own `done.json`,
workspace and `ledger-after`, and wall clock was restored from the run logs.
The reported scores are byte-identical to the originals. One consequence is
visible in the numbers: the replicate-3 directories had been overwritten by
the fourth pass, so replicate 3 above *is* that pass. Its scores are the same
0.000 / 0.000 either way, which is why the collapse survives the accident
unchanged, but its costs and event counts are the fourth pass's.

**The per-cell router events are in `events-armB.jsonl` and
`events-armB-fresh.jsonl`**, keyed by (task, replicate), rather than inline in
the results rows — one log inlined nine times pushed the results past 3MB.
`learning-diff.py` loads them, and stamps each row with the file it came from:
the two arms share (task, replicate) keys, so a lookup that searched both tables
would answer the control with the shared arm's events and report them identical,
which is precisely the conclusion the control exists to test.

**Wall clock is not comparable between the arms.** Arm A ran three concurrent
streams, arm B sequentially. Uncontended per-cell timings are the calibration
round's, in `BASELINE.md`.

---

## 7. Spend

| stage | cells | spend |
|---|---|---|
| panel selection (anchor battery) | 220 | $0.6739 |
| calibration round 1 | 3 | $0.2421 |
| arm A | 9 | $0.8769 |
| arm B, shared ledger (12 cells run; 9 reported, 3 superseded) | 12 | $0.7646 |
| arm B, fresh-ledger control | 9 | $0.6376 |
| **total** | **253** | **$3.1951** |

Arm B used **$1.40 of its $8 budget (18%)**. The whole programme, including
panel selection and the baseline, cost **$3.20**.

---

## 8. What to do next

**Do not ship the router in this configuration.** On this suite it is neutral at
best and actively harmful once the ledger has three runs of history behind it.

Cheapest fix first:

1. **Defect 1** — gate a learned rating behind a minimum count of *graded*
   observations. It alone would have prevented the entire collapse: five graded
   observations should never have outvoted a prior.
2. **Defect 3** — pin the terminal rung to the highest-role model, or the
   panel's reason for existing evaporates on contact with success.
3. **Defect 4** — leaf escalation should climb ability; escalating to a weaker
   model cannot help by construction.
4. **Defect 2** — condition `exec.leaf` on node size.
5. **Defect 6** — seed unmeasured panel members from the anchor battery.

Then re-run this: the harness is parameterised, the graders hold at 84
selfchecks, and the arms are one environment variable apart.

**The suite also needs a stronger discriminator first.** t1 and t3 both failed
0/3 in both arms on the *same* items — a stable boundary, good for detecting
change, but with no cell in either arm moving fail to pass this experiment could
only ever have detected harm. A task where the strong model plausibly succeeds
and the cheap one plausibly fails is what would let a routed arm show a gain,
and the anchor battery already identifies the band it should sit in.
