# Independent GitHub holdout

The five repeatedly used GitHub issues are a development set. They cannot by
themselves establish that a harness change generalizes. This campaign draws a
separate validation set before consulting solution outcomes.

`selection.json` freezes the population, random seed, ranking, five primary
repositories, ordered reserves, and comparison settings. `metadata.json` records
the linked GitHub issues and upstream test files. `fixture-hashes.json` identifies
the private evaluation inputs; reference solutions and withheld tests never enter
an agent's workspace. The original issue text is the task, with the same neutral
completion instructions for every arm, without a harness-specific delegation cue.

The population is **cached SWE-rebench-v2 fixtures on Spark**, excluding all five
repositories used in this optimization campaign. This is a convenience sample,
not a random sample of all GitHub, and does not establish absence from model
training data or from unrelated earlier experiments on this shared machine.

The frozen primary draw is:

| Repository | Issue | Request |
| --- | --- | --- |
| Hypothesis | [3567](https://github.com/HypothesisWorks/hypothesis/issues/3567) | Special-use domains |
| Woodwork | [570](https://github.com/alteryx/woodwork/issues/570) | Pandas extension-array inputs |
| RDT | [750](https://github.com/sdv-dev/RDT/issues/750) | Extract rounding inference into a helper |
| Copier | [1483](https://github.com/copier-org/copier/issues/1483) | Updating a disabled previous choice |
| python-semantic-release | [358](https://github.com/python-semantic-release/python-semantic-release/issues/358) | GitLab issue versus merge-request references |

## Calibration before scoring

`prepare.py` runs only on Spark. It verifies fixture hashes and the repository
base, retains the source archive, and records the runtime image identifier. The
identical offline grader must find assertion failures on the unmodified source
and pass on the upstream solution. Collection/setup errors do not count as a
useful failing baseline. All targeted original tests and the upstream test patch
are included; no case-specific test exclusion or acceptance rewrite is allowed.

Preparation is sequential, bounded, and records every rejection. An unusable
environment can advance to the next preregistered reserve before any inference.
A scored solution failure can never trigger replacement. If fewer than five
different repositories calibrate, preparation reports `needs_review`; it does
not silently change the draw or lower the bar. This calibration job does not
start model calls.

The external evidence directory is
`/home/santosh/bench-artifacts/af-random-holdout-20260910`, outside fleet's rsync
destination. Submit from the repository root with `fleet run --cpu --rsync`.
Keep the job receipt and exact input hashes. Never run full acceptance locally.

## Frozen harness comparison

The intended scored comparison is five accepted issues, two seeds, four arms:

- Aforge baseline `c9aff5b757d14fbeacb02d01d9e2c5e10513a2d3`.
- Aforge optional-progress candidate `faf9c5ac79c33440cd6b711d2ee8cde0f3527843`.
- Pi 0.84.2 with the existing generation-default request hook.
- mini-SWE-agent 2.4.6 at `04d809ceab9df28f9adaed044884180159172930`.

These are the existing experimental versions, not a claim to benchmark the
moving dev head. Record binary and adapter hashes in the scoring manifest before
launch. Use `deepseek/deepseek-v4-flash-0731` everywhere, provider-default reasoning
and generation by omission, and matched seeds 1 and 2. Preserve the existing
strict request guard. Each tool environment has two CPUs and 8 GB. Each scored
trial has the same 3600-second external wall bound and $5 known-cost soft stop;
unpriced or in-flight requests can exceed that dollar floor. No generation or
reasoning token cap is introduced.

Keep arms within the same issue/seed block, rotate launch order, and respect
Spark's existing worker capacity. Report quality, normal completion, elapsed
time including failed trials, settled cost, and unpriced admissions. Compare only
common completed cases; do not hide failed attempts or retry scored cells.
Native mini receipts must be reconciled without double counting the guard.

The scoring stage follows successful calibration and an offline actual-client
preflight. It is not part of the preparation job. Keep validation results
separate from the original development-set table. If these results guide a new
harness change, this cohort has become development data and the next validation
requires another unseen draw. Existing non-coding controls remain necessary;
more coding issues alone do not validate a general-purpose harness.
