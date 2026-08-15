# Meta-learning — the loops learn how to learn

First-order learning is landed: beliefs, skills, experiments, playbooks,
routing, surprise. This document designs the second order — the system
observing its own learning and tuning it, and learning the *shape* of
its user, not just their preferences. The governing insight: **the
journal is already a record of how learning went.** Every supersession,
reversal, declined proposal, and answered question is labeled training
data about the learning process itself. Nothing here invents a new
telemetry stream; everything is a projection of events we already write.

## Decision 1 — Channel credibility: not all learning is equally believable

Beliefs arrive through channels: *stated* (the user said it), *inferred*
(read from one action), *distilled* (post-job reflection), *trial*
(experiment verdict). The journal knows each channel's survival rate —
how often its beliefs were later superseded, retracted, or quarantined.
Maintain per-(kind × channel) survival statistics with empirical-Bayes
shrinkage toward the global rate (the surprise ledger's `n/(n+8)`
pattern), and let credibility gate behavior: low-credibility channels
need more evidence before their beliefs reach prompts; a channel's
measured credibility renders in `/notebook` as the dim confidence of its
beliefs. This is Kalman-style gain learned from data: how much to trust
a new observation from *this* sensor.

## Decision 2 — The user's meta-nature: traits are measured, never assumed

Second-order user facts — the shape of the person — stored as a new
fact kind `trait`, so they ride the existing notebook machinery
(evidence, supersession, /notebook visibility, retraction) for free.
The v1 trait set, each a measurement `{value, n, updated}` derived
purely from journal events:

- **correction style** — immediate vs batched; mean latency from a
  wrong assumption to its correction. Consulted by: how long to wait
  before treating silence as consent.
- **default acceptance** — per question category, how often the user's
  answer equals the offered default. Consulted by: the ask policy
  (Decision 3) — a category accepted ≥95% over enough n stops asking.
- **proposal appetite** — ratio of accepted/declined charter and
  experiment proposals. Consulted by: proposal cadence (an eager user
  can see more; a decliner earns quieter seasons).
- **spec granularity** — median brief length and correction density of
  their asks; do they give vibes or specifications? Consulted by: how
  much the compiler assumes vs asks, and voice verbosity.
- **exploration tolerance** — do trial-carrying jobs (⚖) draw
  corrections at a higher rate than plain jobs? Consulted by: how much
  curiosity budget runs on user-visible work vs idle time.

Traits update slowly (they are priors, not moods), carry their n, and
are consulted — never printed as psychoanalysis. **Rejected:**
personality taxonomies, sentiment analysis, anything not derivable from
journaled behavior.

## Decision 3 — Asking as a decision: empirical value of information

Every askback has an interruption cost and an information value. Both
are now measurable: per question category, the probability the answer
differs from the default (from Decision 2's acceptance stats) times the
journaled cost of having guessed wrong in that category (rework spend
after mis-assumed defaults). Ask only when expected regret exceeds
interruption cost; otherwise assume-and-declare with the default. The
one exception stays absolute: charter ratification is always asked —
standing spend is consent, not calibration.

## Decision 4 — Curiosity follows learning progress, not error

The curiosity loop picks knowledge gaps to close in idle time. Raw
error is a trap: a domain can be noisy forever (weather-shaped, high
irreducible error) and would eat the budget. Allocate by **learning
progress** (Oudeyer/Kaplan, developmental robotics): the *derivative*
of the surprise ledger per domain — spend curiosity where prediction
error is falling fastest, retreat from domains where practice no longer
moves it. Noise-immune by construction: a domain that doesn't improve
loses its budget no matter how wrong we remain about it.

## Decision 5 — Forgetting is a learned rate

Belief aging currently uses fixed constants. ACT-R's base-level
activation (log Σ tᵢ^−d over a belief's re-access history) gives a
principled alternative: beliefs re-derived or re-used often consolidate
(decay slowly); beliefs untouched in volatile scopes fade faster. Learn
the decay exponent per kind from revisit statistics — preferences may
prove near-permanent while workspace facts rot in days. Aging remains
visible and reversible as today; only the clock becomes empirical.

## Decision 6 — The retrospective audits itself

A meta-retrospective, run rarely (every Nth reflection): compute the
reversal rate of each retrospective action class — consolidations later
undone, promotions later quarantined, territories later split,
proposals declined — and tune the thresholds that produced them
(promotion occurrence counts, aging constants, proposal cadence) inside
hard rails. Every tuning is a journaled parameter-change event with the
evidence attached, visible in the reflected line (`· reflected — eased
skill promotion, 0/6 reversals`), and bounded: no parameter may move
more than one notch per audit, and every parameter has a floor and
ceiling set in code. Self-tuning, never self-rewriting.

## What this is not

- Not a psychology engine. Traits are measured behavior with sample
  sizes, stored as retractable notebook facts, never narrated.
- Not autonomous self-modification. Tuned parameters live inside coded
  rails; the audit adjusts dials that were designed to be adjusted.
- Not new telemetry. Every measurement is a projection of events the
  journal already records — the second order costs no new writes.
