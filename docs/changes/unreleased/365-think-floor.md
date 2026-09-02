---
kind: changed
title: a model that has been watched thinking is waited against what it really does
pr: 365
surface: [engine, docs]
invalidates:
  - "`lane.Thinks` passed the flat `lane.SpreadFloor` to `Chain.Survival`, so the duration clock's predictive spread was a constant: σ could never fall below 1.0 nat however many thoughts were folded in, its abnormality quantile sat at about fifteen times the believed median — 82.9 s against a 10 s ceiling — and §B's second test could not close on that clock for any model that deliberates for more than 0.663 s. It passes `lane.Hierarchy.ThinkDraw(model, rung)`, the dispersion the think chain has measured for itself, and `lane.SpreadFloor` is now only the prior it starts from."
  - "`lane.Hierarchy` had eight methods. It has nine: `ThinkDraw(model, rung string) float64`, the observed twin of `Draw(id)`. A ledger that answers `lane.Hierarchy` has one more method to answer."
  - "`lane.SpreadTightest` is new — 0.15 nat, the narrowest one draw is ever believed to be. A dispersion learned from observations can reach zero and a spread of zero says a tail is impossible; this is the bound on that claim."
  - "The persisted `chains` gained `spread`, a Welford count/mean/M2 per leaf. It is `omitempty`, so a state file written before this change loads unchanged and starts from the prior."
  - "`bench/lanelab/gosim`'s `TestWhenTheThinkGateCloses` asserted that the gate NEVER closes and failed the build if it ever did. It asserts the n it closes at — 31 observations of one model at one rung, allowed to drift no later than 40 — and a second law, `TestAModelWhoseThinkingReallyVariesKeepsItsGateOpen`, asserts that a model whose deliberation really varies keeps its gate open."
  - "`docs/design/waiting/DESIGN.md` §K said the long-think floor never lifts and named two candidate fixes as untaken. The first is taken. The 4.95 s p50 time-to-action on a stalled thought is still NOT recovered and §K now carries the arithmetic that says why, which is a finding rather than a defect: the duration clock's payoff term prices leaving a thought at a whole fresh thought."
---

The wire clocks warm and the duration clock did not, and the reason was one word
in `waiting.go`. `Chain.Survival` takes the larger of the estimate's spread and
how much ONE DRAW of the quantity varies. A lane's draw is published — the
distance between a sheet's p50 and its p90 — and **nobody publishes how much one
run of thought varies**, so `Thinks` stood on the prior for ever, the prior was
always the larger term, and the quantile could not come inside any role's
ceiling. Measured through the real `lane.NoteThought` door at 0, 1, 2, 5, 10, 20
and 60 observations, σ was exactly `SpreadFloor` at every one of them.

**A thing nobody publishes is still observed.** Every thinking duration folded
in is one draw of exactly that quantity, so the think chain keeps its own
account of them — Welford's count, mean and sum of squared deviations per
(model, rung), three floats a leaf, folded on the same lock and in the same call
as the belief. The law is the sample spread pooled with the prior at one
observation's weight, `σ² = (SpreadFloor² + Σ(z − z̄)²) / n`, floored at
`SpreadTightest`: one thought answers the prior exactly, two identical ones
answer it divided by √2, and a model that really does think for two seconds on
one question and forty on the next keeps the width it really has. It is the same
law the rest of the package keeps everywhere — a measured thing outranks a
prior — applied to the one quantity that had no measurer.

**Measured on the proof rows' own model, the gate closes at n = 31** and σ
settles at 0.174 against the 1.0 prior. The half that makes the close safe is
the other test: sixty thoughts that really vary by 0.9 nats leave σ at 0.909 and
the quantile at 68 s, so a model whose long thoughts are legitimate is not
suddenly abnormal for having one.

**And the 4.95 s p50 on a stalled thought is not recovered by this, which is a
finding and not an omission.** #316's acceptance asked for it back. The gate is
only half of §B: the duration clock's payoff term prices leaving a thought at a
whole fresh thought, `A = cost + Think.Mean()`, so acting needs the remaining
life of THIS thought to exceed an entire new one — which for a log-normal is far
into the tail. At σ = 0.15 and a 5.5 s median that is 15.8 s, past the ceiling,
and **there is no σ at all at which both tests pass before a 10 s ceiling** for a
model with that median. A stopped thought is abnormal in its GAP and not in its
duration, which is #316's second candidate — a think-phase drift quantile — and
that stays open. The `warmed · shipped · natural` arm's stalled-think p50 is
therefore 10.00 s before and after, and the §K bounds it must not have bought
speed with are unmoved.

**One more bound worth writing down, because it decides where the effect is
visible at all.** `hazard.verdict`'s payoff branch is `wait > cost + margin`, and
`cost` is `+Inf` for a call with no alternative lane or with λ at zero — every
background-intent call. A headless call therefore reaches a verdict only through
`silence >= Ceiling`, never early on payoff, so a relaxed think floor shows up on
the chat/foreground path and never on `aforge do`. The two new
`internal/provider` scenarios stage a foreground intent with a finite cost for
exactly that reason.
