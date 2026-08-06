# Phase B2 — the routed panel against the fixed router

**Question.** Phase B found routing neutral-to-harmful: 3/9 to 2/9, the top
model never called, and a ledger whose only measurable effect was to demote it
and collapse two working tasks to zero. Six defects were fixed. Does routing
work now?

**Answer.** Yes, on this suite, with two things still open. Success went
**4/12 to 7/12**, the collapse is gone and cannot be reproduced from the ledger
that caused it, every panel member was exercised, and the top model was called
36 times — all of them leaf escalations, all upward. What has *not* arrived is
learning: the gate that stops the harm also stops the ledger reordering
anything, so continual learning is currently inert rather than beneficial.

---

## 1. Head to head

Four tasks, n=3, identical briefs and graders. Arm A is the recorded
single-model baseline (t1–t3 from Phase B's baseline, t4 from its calibration).

| task | A success | B2 success | A scores | B2 scores | fresh control |
|---|---|---|---|---|---|
| t1-logstore | 0/3 | 0/3 | 0.667, 0.667, 0.667 | **0.889**, 0.667, **0.000** | 0/3 |
| t2-synthesis | 3/3 | 2/3 | 1.000 x3 | 1.000, **0.875**, 1.000 | 3/3 |
| t3-shiftplan | 0/3 | **2/3** | 0.857, 0.571, 0.714 | 0.857, **1.000**, **1.000** | 0/3 |
| t4-pathmatch | 1/3 | **3/3** | 1.000, 0.929, 0.929 | **1.000 x3** | 2/3 |
| **overall** | **4/12** | **7/12** | | | 5/12 |

**t3 and t4 are where routing paid.** t3 went from 0/3 to 2/3 with two cells
reaching a perfect 7/7 defect families — the first time any arm has repaired all
seven. t4 went 1/3 to 3/3, clearing the performance group that failed twice in
calibration.

**t1 did not move, and one cell got worse.** Two cells matched or beat the
baseline (0.889, 0.667) and one scored 0.000 — a cell whose plan produced a
single oversized leaf that ran out of budget before writing an importable
package. That is the same at-scale failure t1 has always had, and escalation did
not rescue it: the leaf was escalated to kimi 21 times across the arm and the
work still did not land inside the budget.

**The fresh-ledger control sits between them at 5/12**, which is the useful
comparison: shared 7/12 against fresh 5/12 is two cells, well inside the
variance this suite shows, so the shared ledger is not doing the work — the
fixed *routing* is.

### Cost and wall

| | arm A | B2 shared | B2 fresh |
|---|---|---|---|
| spend | $1.497 (12 cells) | $1.766 (12 cells) | $2.023 (12 cells) |
| per cell | $0.125 | $0.147 | $0.169 |
| wall | 99.1 min (3 streams) | 123.5 min (sequential) | — |

**+18% cost per cell for +3 successes.** That is a real price and a defensible
one, and it is inside the planner's own node-count spread — the same caveat that
made Phase B's -32% meaningless applies in the other direction here.

---

## 2. The assertion scoreboard

All eight fail on Phase B's data, which is what makes a pass mean something.

| # | assertion | Phase B | B2 |
|---|---|---|---|
| A1 | the terminal rung is kimi-k2.6 | FAIL | **PASS** |
| A2 | kimi is reached by an escalation on a failed hard task | FAIL | **PASS** — 4 of 4 failed hard cells escalated |
| A3 | no leaf escalation lands on a weaker model | FAIL | **PASS** — 36 leaf escalations, all upward |
| A4 | every panel member has at least one observation | FAIL | **FAIL** — kimi still unobserved |
| A5 | the ordering respects the gate (n < 8 reads the prior) | FAIL | **FAIL** — one call |
| A6 | exec.leaf ratings are conditioned by leaf shape | FAIL | **PASS** — `exec.leaf/atomic`, `exec.leaf/synthesis` |
| A7 | every escalation records its chain | FAIL | **PASS** — 0 empty chains |
| A8 | exploration opens calls the ordering would not | FAIL | **PASS** — gemma, glm-4.7-flash, qwen3-30b |

**6 of 8.** Three assertions had to be corrected first, and that is worth being
explicit about because "adjust the assertion until it passes" is the exact
failure mode an assertion suite exists to prevent. Each was written against the
pre-fix design and was testing the wrong thing:

- **A1** compared the terminal rung's *rating* against rung 0's. The fix pins
  the role-top model terminal by construction, and kimi's rating is its ungraded
  prior (+1.00) while flash has earned more — so the original phrasing failed
  precisely when the fix was working. It now asks what it was written to catch:
  is the top model still in the cascade at all?
- **A3** conflated two mechanisms. Leaf escalation must climb ability, and does
  (36/36 to kimi). The planning classes cascade cheapest-first behind a schema
  verifier, where trying gemma at rung 1 and being corrected is the design.
- **A5** asked whether thin ratings *exist* rather than whether one was *used*.
  It now rebuilds the order the router should have produced — measured rating at
  or above the gate, prior below it — and compares.

### The two that still fail

**A4 — kimi has 36 attempts and zero observations.** Every one was
`exec.leaf`, and a finished leaf returns `unverified_success`, which moves
nothing. So the model doing the escalated work accumulates no evidence about
itself, ever. This is the unfixed half of defect 1: the gate stops bad evidence
driving the order, but nothing supplies *good* evidence for a model that only
ever works on unverifiable leaves. kimi's position is held by its role prior
alone, which is correct today and would not survive a panel change.

**A5 — one `plan.ground` call opened on qwen where the gate-respecting order
opens on flash.** One call out of ~1,500 attempts. It is adjacent to an
exploring call with a different call id and the same candidate ordering, so the
most likely explanation is an exploration whose flag did not reach a sibling
attempt rather than an ordering fault. Recorded as an open question; it is too
small to diagnose from this data and too small to matter to the outcome.

---

## 3. B2-warm — the direct test of the gate

t2 replayed against the **preserved ledger that collapsed it in Phase B**, twice:

| | score |
|---|---|
| Phase B run 3, this ledger state | **0.000** |
| B2-warm replicate 1 | **1.000** |
| B2-warm replicate 2 | **1.000** |

Same task, same brief, same ledger file. `aforge models` says why on sight:
flash's `exec.leaf` rating still reads **-0.98**, and beside it now sits
*"under the gate — ordering uses the prior until n=8"*. Five observations no
longer outvote a prior.

The live B2 ledger shows the same mechanism holding on fresh evidence:
`exec.leaf/atomic  -0.25  n=4  under the gate`. That is the identical shape that
collapsed Phase B, gated this time.

---

## 4. The learning verdict: inert, not harmful

```
reordered with a shared ledger : none
reordered with a fresh ledger  : none
attributable to the ledger     : none
```

**The router did not change its mind between run 1 and run 3.** That is a real
answer rather than a missing one, and it is the direct consequence of the fix:
`MinGraded = 8` and twelve cells over four tasks did not accumulate eight graded
observations for any model on any class that would have reordered it. The
ledger's final state has every sub-8 rating explicitly gated.

So continual learning is currently **inert**. Phase B's learning was measurable
and harmful; B2's is neither. That is a strict improvement and it is not the
same as working. Nothing in this experiment shows the ledger *helping*.

**Exploration, by contrast, works.** The deterministic 1-in-10 rule opened calls
on qwen3-30b (7), glm-4.7-flash (2) and gemma (1) — models the cost-effectiveness
ordering would never have chosen, and exactly the mechanism whose absence left
three of five panel members unmeasured in Phase B. Every panel member was
exercised this time.

The two mechanisms are in tension in a way worth naming: exploration is buying
evidence at 1-in-10, and the gate needs 8 graded observations per model per
class. On this suite's volume those rates do not meet — exploration supplies
evidence more slowly than the gate consumes it.

---

## 5. Did kimi rescue the hard tasks?

**Partly, and the split is informative.**

| task | kimi escalations | outcome |
|---|---|---|
| t1-logstore | 21 | no change — 0/3, and one cell to 0.000 |
| t3-shiftplan | 7 | **0/3 to 2/3**, two cells at 7/7 families |
| t4-pathmatch | 8 | **1/3 to 3/3** |

t3 and t4 are the tasks whose failures were *reasoning* failures — an empty
interval that needs to be inferred from half-open semantics, a performance floor
that needs the patterns compiled once. A stronger model fixed both.

t1's failures are not that shape. Its leaves carry 2.2M prompt tokens and run out
of budget; escalating to a stronger model does not give the leaf more budget, and
the one cell that scored 0.000 failed by never producing an importable package at
all. **This confirms what t4's calibration suggested**: t1 is an at-scale
failure, not a capability failure, and routing is the wrong instrument for it.
The right one is decomposition or budget, not a better model.

---

## 6. Spend

| condition | cells | spend |
|---|---|---|
| B2 shared | 12 | $1.766 |
| B2 fresh control | 12 | $2.023 |
| B2-warm | 2 | $0.116 |
| **B2 total** | **26** | **$3.905** |

**$3.91 of the $8 budget (49%).** Programme total across all phases: **$7.73**.

No cell hit a timeout. Two harness interruptions (the task manager stopped both
long-running conditions once) were resumed with `START_REP` against the existing
ledger, and are recorded as harness events rather than model failures.

---

## 7. Recommendation: **iterate, then ship behind a flag**

Not "ship" and not "park".

**What is fixed and demonstrated.** The collapse cannot reproduce, from the
ledger that caused it. Escalation reaches the ceiling and only ever climbs.
Leaf ratings are conditioned. Chains are logged. Exploration reaches the whole
panel. Six assertions that all failed before now pass, and success improved
4/12 to 7/12 for +18% cost per cell.

**What is not.** Learning is inert — the ledger reordered nothing in twelve
cells, so the continual-learning claim is unproven rather than proven. And the
model doing the escalated work accumulates no evidence about itself, because
leaves are unverifiable; kimi's terminal position rests entirely on an operator
role hint. That is fine while the hint is right and silently wrong when it is
not.

**Ship behind `AFORGE_MODELS` as it already is** — the router is inert unless a
panel is named, which is the correct default and makes this a per-operator
opt-in rather than a change to everyone's harness.

**Iterate on the evidence problem before making it the default**, in this order:

1. **Give leaves a verdict.** The single highest-value change in the system. A
   leaf that ends `done` is unverifiable only because nothing checks it; the
   scheduler already knows whether downstream nodes consumed its artifacts, and
   that is a cheap, honest signal that would let *any* model accumulate evidence.
   Without it the ledger can only ever learn from failures.
2. **Reconcile the exploration rate with the gate.** 1-in-10 against
   `MinGraded = 8` does not converge on realistic volumes. Either explore more
   early or gate on a confidence interval rather than a count.
3. **Re-run B2 with more replicates before believing the size of the win.**
   7/12 against 5/12 for the fresh control is two cells. The direction is
   consistent across three tasks and the mechanism is understood, but n=3 on
   four tasks cannot size an effect.

**What this still cannot say.** Every task here has a cheap deterministic
verifier by construction, which Phase A named as the cascade's load-bearing
assumption. Nothing in either phase measures work without one — and §4's finding
that leaves accumulate no evidence is exactly that gap showing up from the inside.
