# Frozen interface contracts — pareto crewing

Frozen 2026-10-20, per IMPLEMENTATION-EXPERIMENT-PLAN.md §5 ("Contracts frozen
before parallel work"). This package is the wire shape and semantics the three
lanes (evidence, learner, integration) build against. It implements none of
the predictor, policy or grader.

## Version constants

Every constant in `contract.go` is named here; a change to a field's MEANING,
its removal, or its redefinition bumps the constant. Adding a field does not
bump a version (forward compatibility: unknown fields are ignored, unknown
versions are refused).

| Constant | Value | Pins |
| --- | --- | --- |
| `DecisionEventVersion` | 1 | the `DecisionEvent` wire shape and its field semantics |
| `OutcomeEventVersion` | 1 | the `OutcomeEvent` wire shape and its field semantics |
| `PredictorStateVersion` | 1 | the `PredictorEnvelope` export/import envelope (NOT the payload's interior format, which `StateVersion` inside the envelope pins) |
| `SharedSnapshotVersion` | 1 | the `SharedSnapshot` wire shape and its field semantics |
| `ProbTolerance` | 1e-6 | how far a recorded categorical distribution may drift from a sum of exactly 1 |

Reader rule: a reader that meets a schema version it does not know refuses
with an `ErrVersion`-wrapped error rather than half-reads. The marshal
helpers refuse to write a zero (unset) version.

## DecisionEvent

- IDs: `run_id` (whole job), `parent_run_id` (empty at root), `node_id` (the
  role/seat-class boundary: reflex, low, worker, high, mastermind).
- `decision_time` is UTC.
- `feature_version` + `features`: the PRE-DECISION feature values. Frozen
  allowed keys: `role`, `task_class`, `context_size_tokens`,
  `budget_remaining_usd`, `checks_observed`. Everything recorded must be
  provable to exist at decision time — a feature observable only after
  execution is a future-feature leak and must never appear here.
- `candidates`: the eligible set AFTER all filters (availability,
  compatibility, budget); each carries the `descriptor_version` of the
  catalog document it was read under. Descriptors themselves live in the
  catalog, not in the event.
- `probabilities`: **the exact final categorical distribution** — after all
  filters and after the exploration mixture, recorded BEFORE the draw. This
  is a hard requirement from the plan: the draw is replayable and the
  propensity is declared, not reconstructed. Values in [0,1], sum to 1 within
  `ProbTolerance`, and name exactly the `candidates` (eligible-but-unassigned
  mass is listed as 0). `selected_action` need not be the argmax but must be
  in the set.
- `policy_version`, `catalog_version`, `source_version`: so any decision is
  reproducible.
- `budget_envelope`: `cap_usd` (spend this decision may commit from here),
  `reserved_usd` (outstanding reservations at decision time), `spent_usd`
  (actual spend so far), `currency` (fixed `usd`).

## OutcomeEvent

- Linkage: `run_id`, `decision_id`, `job_id` — a reader walks
  `DecisionEvent.RunID -> OutcomeEvent.RunID` without guessing.
- Grader provenance: `grader_name` + `grader_version`; a verdict without
  provenance is not a label.
- `verified_success`: the WHOLE-JOB label from independent grading (not the
  model's own claim), and never copied onto individual seats — one job, one
  event; several decisions may link to it.
- `spend`: ACTUAL token/cache/cost accounting (`total_usd`, `tokens_in`,
  `tokens_out`, `cached_tokens_in`, `currency`).
- `cost_cap_censored`: set when the run terminated on the cost cap. The
  incurred spend above is observable; the unconstrained cost-to-completion
  is CENSORED and must be treated as censored, not observed.
- `harness_error`: set when the failure was the harness's, not the model's —
  not a success/failure signal about the model.

## Predictor

```go
type Predictor interface {
    Predict(task FeatureVector, candidates []Candidate) (Prediction, error)
    Update(ev OutcomeEvidence) error
    Export() ([]byte, error)
    Import(blob []byte) error
}
```

- `Predict` returns success probability, a cost DISTRIBUTION
  (`CostDistribution`: lognormal `(mu, sigma)` or samples — the lognormal
  mean is `exp(mu + sigma²/2)`, not `exp(mu)`), and uncertainty. The
  exploration layer is a separate decision layer and is not part of predict.
- `Update` takes outcome evidence including the predictor's own recorded
  features. **NAMED APPROXIMATION**: a vanilla Gaussian Kalman update on 0/1
  outcomes is NOT an exact posterior. Binary success requires logistic
  filtering with an explicitly named approximation — Laplace /
  assumed-density / extended filtering — stated in the implementation and its
  docs. Continuous log-cost observations admit exact Kalman updates under the
  linear model. Censored completion cost is handled as censored.
- `Export`/`Import` move versioned state. `Import` refuses an envelope
  version it does not know and a state whose `feature_version` differs from
  its own frozen coordinates (old coefficients are invalid in a rotated
  basis).

## SharedSnapshot

- `coordinate_version` pins the shared representation the parameters are
  expressed in; migration on a representation update is a versioned act,
  never a merge.
- `contribution_ids`: deduplication keys of contribution batches already
  folded in. A duplicate contribution id is REJECTED, not re-counted —
  averaging local posteriors as independent observations double-counts common
  evidence, and this field is what prevents it.
- `parameters` / `uncertainty` / `counts`: shared values, their uncertainty
  (non-negative), and evidence counts, under a common keying.
- `model_ids`: the models the parameters cover; a snapshot that does not
  mention a model is never read as scoring it.
- `generated` (UTC) / `generated_by`.
- **Text, prompts and per-task embeddings MUST NOT appear in a snapshot.**
  The schema carries no free-text payload field at all; string-typed content
  is identity and version naming only.

## What may never change without a version bump

Any field's meaning; the frozen allowed feature keys; the requirement that
`probabilities` is the exact final pre-draw categorical distribution; the
whole-job (never per-seat) nature of `verified_success`; the censoring
semantics of `cost_cap_censored`; the no-text rule on `SharedSnapshot` and the
predictor payload; the uniqueness rule on `contribution_ids`.
