# Measuring and improving the conversation harness

## What we are trying to establish

Aforge should let someone discuss the next idea while their work progresses in tasks. Its result must be correct, its latest instructions respected, and its progress visible. Nested tasks are an implementation choice, not a quality score. Extra agents count only when their useful parallel work outweighs startup, context, coordination, verification, and integration.

We cannot establish optimality on every possible task or against every future harness. The testable claim is narrower: at a pinned model and effort, under declared operating conditions, no measured competitor offers meaningfully better quality, cost, and time together on the supported workload slices. Being nondominated while failing basic requests is not a product success. Quality is a gate before optimization.

## The measurement contract

| Quantity | How it is measured | What cannot count as success |
|---|---|---|
| Outcome quality | All critical checks pass: correct artifact/answer, preserved constraints, genuine completion | A high fraction of passed infrastructure checks with a wrong answer |
| Billed cost | Provider prices for every admitted call, including auxiliary calls, retries, cancellations, and unsuccessful attempts | A partial sum presented as a total, or a harness's stale price table |
| Completion time | Current end-to-end cell launch through surface completion, including startup, coordination and verification; separate warmed interaction timestamps where available | Time to first token presented as time to finish |
| Chat availability | Steering submission to first correct useful visible response, while the original work is still running | A correct response queued until after work finishes |
| Steering fidelity | New direction applied before publication; superseded output absent; accepted/read/applied evidence distinguished | Acknowledgement without effect, or silently changing an unrelated task |
| Reliability | Duplicated actions, lost results, panic/hang, false completion, recovery and resume outcomes | A retry erased from the attempt ledger |
| Human effort | Clarifying turns, interventions, repeated instructions, and inspection needed to get an acceptable result | An agent asking the person to repair its own recoverable tool failure |

The current automated suite measures narrow objective outcomes. It does not yet judge writing taste, broad research completeness, multi-hour autonomy, or human intervention effort. Those require additional fixtures and blinded human rubrics; no model judge substitutes for them silently.

Cost per accepted result is total cost of all attempts divided by successful results. A timeout keeps its cost and measured time. Unknown billing stays in the quality denominator and prevents a cost comparison. Infrastructure failures are retained and stop the campaign for adjudication. No outcome is retried away. Provider cancellation metadata may reconcile a missing stream receipt; it never creates another inference request or invents a zero.

## Fair comparisons

Compare Aforge, Pi, and OMP with the exact `deepseek/deepseek-v4-flash-0731` model, low effort, fresh owned state, the same fixtures and permissions, and no other simultaneous live benchmark on this machine. Pin every auxiliary role; the forwarding guard refuses other models and fallback entries before inference. GLM 5.3 and native Claude Code Opus used for implementation reviews are separate development expense, not runtime benchmark cost.

Use each harness's real supported implementation. Preserve its built-in prompts and useful tools. Remove personal skills, extensions, MCP servers, memories, and project contamination. OMP 18.1.2 supports disabling third-party discovery through `disabledProviders` in the owned profile; the earlier statement that MCP isolation was impossible was incorrect. This is documented in its [versioned settings reference](https://github.com/can1357/oh-my-pi/blob/v18.1.2/docs/settings.md#provider-and-source-disabling).

Freeze executable hashes (and installed package contents for JavaScript launchers), exact rig/fixture/judge hashes, model/effort, timeout, slow-work duration, fixture variants, profile, run order, and expected arms before execution. Randomize arm order inside complete scenario/repetition blocks; keep print and interactive measurements separate. Report machine, cache policy, provider routing, and harness versions. Default routing variation is part of the measured stack; do not call it purely local harness overhead. A provider-pinned experiment is a separate condition.

Small repeated samples are calibration, not a demonstrated frontier. Use paired block differences and uncertainty; publish attempts and missingness before reporting means. Equal all-success samples cannot establish quality equivalence. Cost/time intervals are withheld when billing/time coverage is incomplete. Never pool unrelated scenarios, harness versions, or conditions just to obtain a larger sample.

## The optimization sequence

1. **Repair the measuring instrument.** Binary outcomes, immutable attempted-run evidence, complete billing identity, isolated peers, paired scheduling, and counterexample tests. This is the current wave. The old pilot remains evidence of bugs, not a valid frontier baseline.
2. **Calibrate all six comparable slices.** Arithmetic, source synthesis, constrained writing, code repair, side questions during work, and mid-work revisions. Start with two randomized repetitions per arm. Inspect every failure and receipt gap before buying a larger run. Result recall is an Aforge product gate until peer doors are calibrated for the same behavior.
3. **Establish a repeatable baseline.** After calibration is sound, run at least ten blocks per slice over multiple time windows. Treat that as an initial estimate, not an equivalence certificate. Publish completion and responsiveness distributions, success counts, call/token breakdown, unknown prices, and failed-attempt costs. Re-estimate required sample size from paired variance and desired quality precision before confirmation.
4. **Optimize one measured bottleneck at a time.** First isolate unnecessary auxiliary work, repeated static context, admission/rephrasing rounds, task opening latency, polling, serial dependencies, and verification loops. Evaluate minimal ablations on development fixtures. Keep the simplest change that improves the declared objective without breaking another gate. Never replace a general failure with a hard-coded benchmark answer.
5. **Test adaptive task orchestration.** Add independent work, dependency fan-in, shared-file conflicts, cancellation, nested steering, task-room steering, ambiguity, long tool output, compaction, provider stall/failover, and restart/detach cases. Assert artifacts and visible behavior, rather than insisting on a fixed task graph. Measure the critical path and integrate cost of failures; spawning more tasks is not inherently faster.
6. **Confirm on held-out variants.** Freeze candidate and acceptance margins before opening an unseen fixture set. New datasets, different document constraints, independent code defects, changed correction timing, and longer histories must exercise the same capabilities. Do not use the public reversal fixture as a proxy for all conversational correctness.
7. **Keep the result production-safe.** Regression suites and the real chat door must pass. Document actual limits, status semantics, and capability availability in the manual whenever behavior changes. Roll out only through the owner's requested local integration until explicitly authorized otherwise.

## Initial acceptance targets, to preregister before confirmation

These are proposed product targets, not measurements already achieved:

- Zero observed destructive duplication, silent instruction loss, false completion, or unrecoverable panic in the prescribed deterministic and fault-injection suites. This does not prove zero production risk.
- At least 95% successful objective outcomes on each declared slice; establish the lower confidence bound with enough independent cases, rather than claiming it from five green runs. About 59 independent successes with no failures gives a 95% one-sided exact lower bound near 95%; repeated runs of one fixture are not 59 independent kinds of task.
- Correct chat replies while work continues; report p50/p95 response delay separately from job completion. Initial responsiveness target: p95 within five seconds when the provider itself permits that latency. If this is not met, publish both provider and local contributions rather than excluding slow runs.
- For a candidate optimization, preregister a practical cost/time benefit (initially 10%) and a maximum quality loss (initially 2 percentage points), then use a held-out comparison with appropriate one-sided confidence bounds. Do not reinterpret an inconclusive result as equivalent. Multiple workload/competitor claims require simultaneous bounds or explicitly exploratory labels.
- Every cost comparison has complete reconciled billing. Report cold launch time separately from warmed chat interaction; keep all user-visible delays in the applicable metric.

If a competitor dominates a slice, that slice becomes optimization work. If tradeoffs remain, expose a small number of meaningful policies only after evidence shows distinct useful choices. Do not burden people with internal worker/router/checker controls to disguise overhead.

## Running it

`bench/conversation/campaign.py plan <manifest.json> --id <name>` creates a frozen randomized plan without spending. `campaign.py run <manifest.json> --out <evidence-directory>` executes sequentially and resumes completed cells; a partial cell requires adjudication. `summary.sh <evidence-directory>/results.jsonl` reports exact scenario/door/model/effort slices and refuses unsupported comparison claims.

Each campaign uses fresh state. Its per-cell cap limits runtime, not a hard dollar budget. The manifest states the maximum cell-time sum. No automatic scheduled campaign exists, and no expensive confirmation run is implicit in merely generating a plan.

## Calibration boundary correction

The first new campaign was stopped when its driver closed an idle chat while a requested background action was still running. Those interactive outcomes require adjudication; they do not establish that a competitor abandoned work. The corrected driver observes the action's completion marker independently of chat readiness. Its idle confirmation interval and terminal teardown remain included in raw door duration; fine interaction witnesses are separate and have the driver's polling resolution. `STOP` in the campaign output directory requests a stop after the current cell, preserving all evidence.

## Cancelled request accounting

A real cancelled DeepSeek stream exposed a provider identity distinction: the requested catalog ID and the billing receipt's canonical slug differ. Reconciliation accepts that relationship only when the official model catalog explicitly maps the exact requested ID to that slug, and retains that identity evidence with the receipt. It never accepts a family-name match. The original ledger remains unchanged.

Receipts can arrive after inference ends. The runner spends up to 30 seconds on read-only metadata recovery outside the timed interaction; unresolved cost stays unknown. A later explicit `lib/reconcile.py SOURCE FRESH_OUTPUT --wait-seconds 60` can recover delayed receipts without replaying inference. Derived reports must name the recovered ledger; do not overwrite raw observations or silently turn incomplete prices into zero.

## Attribution and reproducibility boundaries

The guard retains provider-reported cached input, cache-write and reasoning token counts when present; absent fields remain unknown. These explain billing variation without replacing the actual charged price. Package fingerprints cover an installed JavaScript harness's own code, prompts and data, rather than only its launcher. External runtimes and nested dependencies are not frozen by this fingerprint; their environment remains a declared reproducibility limitation. The manifest records operating system, architecture and CPU count.
