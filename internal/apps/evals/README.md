# Fluid app evals

Run the paid gallery against a binary built with `make build` on the integrated
app branch. Use the test filter to limit the first run to one case.

```sh
CODEAF_APP_EVAL_BINARY=/absolute/path/to/bin/codeaf \
CODEAF_APP_EVAL_REPORT=/tmp/app-eval.json \
go test -tags e2e ./internal/apps/evals -run 'TestLiveGallery/founder-crm' -count=1 -timeout 10m -v
```

Use `-run '^TestLiveGallery$'` and a 50-minute timeout for the seven model
cases. The key is resolved through the normal profile and the builder is
pinned to `deepseek/deepseek-v4.1-flash`. Each case creates a disposable engine
home. `TestDeterministicGallery` uses the same binary with a fresh home and
no provider credentials. Use a distinct report path when running it separately.

The eight prompts and their expected observations are in
`cases/gallery.yaml`. A step can assert a JSON pointer or capture a value and
use it in a later request as `$name`. `app.*` runs the matching CLI command.
Other methods go to the person's app wire. `wait-and-apply` waits for the
draft's successful Check and applies it through that wire. The runner checks
reuse offers before accepting one. A build only sees the prompt and the normal
offer acceptance from the person.

The adversarial suite checks empty results, paginated reads after 2,000
records, and the stale-write refusal through the data wire. The process
adapter also runs six platform probes:

- AI precedence and field rename use the published gallery app and its
  grants. The disposable engine releases the home while the native data
  writer applies the AI write or rename. The reopened engine reads the result.
- Guarded undo applies a signed transition through the process wire and checks
  the typed undo refusal and the record state. The platform publishes a fixture
  in its own home so the probe can run even for apps without a lifecycle.
- Trigger replay uses a committed record event and a durable run receipt across
  an engine reopen. Record and email instructions use the run engine's data and connector
  inputs and check committed rows and effect calls. The fixture connector
  never sends real mail. Paid cases also pass the hostile text to the app
  run's headless model door, and their reported usage includes those calls.
  The keyless lane checks literal delivery of the same text.

Run the platform probes without a provider using `TestPlatformStimulus` and
the same binary variable. A build that cannot publish still reports its
probes as unrun.

The report includes static depth for every readable saved draft, including a
draft that fails Check. It scores one point each for two AI steps, a trigger
pointing to a run, a tool step with an approval gate, and at least 30 sample
rows. The raw counts are included. These are definition counts and do not
establish that the steps execute successfully.

Set `CODEAF_APP_EVAL_SNAPSHOTS` to export each generated app's manifest,
screens and synthetic records for a separate Desk capture pass. Each file
`<case>/<draft>.json` contains `app` (the manifest), `screens` (screen IDs
mapped to compiled definitions), and `records` (types mapped to sample rows).
Use a fresh directory for each live run.

For the design judge, set `CODEAF_APP_EVAL_LOOKS` to a directory of Desk
captures made with synthetic data. Save each PNG under
`<case>/<screen>/<dark|light>-<800|1120>.png`. `TestLiveGallery` includes the score, individual observations and separate
Critic usage when the capture directory is supplied. Use
`TestLiveDesignRubric` to score an existing capture set. Set
`CODEAF_APP_EVAL_PAID_LOOKS=1` for captures from the seven paid cases alone. Set
`CODEAF_APPS_CRITIC_MODEL` to select a judge, or leave it unset to use the same
model as the live Builder. An unsupported or unknown vision capability is
reported as skipped without substituting a model.
The judge records scores and visible evidence alongside the recipe, theme and
width. Missing captures and invalid scores fail the rubric run. Both
model-backed tests skip if the profile has no key.

The `support-shares-crm-identity` case checks the CRM and Support in the same
space. It accepts existing Contacts and Companies from Support's plan,
then reads the original type and record IDs through Support.

The rubric includes each case's gallery brief. Set
`CODEAF_APP_EVAL_DESIGN_REPORT=/tmp/app-design.json` to save per-recipe
scores and capture errors. Process-only builds report
`look: skipped (desk not running)` during Check and can publish.

## Builder baseline

At engine revision `231c3040c`, the seven paid cases passed their existing
behavioral checks in 194 calls and $0.164137. Static depth counted 2/4 for
Marketing desk and 0/4 for the six other cases. The sample counts ranged from
four to nine. Vision measurements use the synthetic captures and the
separate Critic usage recorded by the rubric test.
