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
records, and the stale-write refusal through the data wire. Event replay,
hostile text in records and email, identity after renames, guarded undo, and
AI writes under an agent principal have independent assertions behind
`Stimulus`. The stock process adapter reports these as unrun and the case as
incomplete until trusted stimulus support is connected. An engine
that cannot publish a case still reports every unrun probe.

For the design judge, set `CODEAF_APP_EVAL_LOOKS` to a directory of Desk
captures made with synthetic data. Save each PNG under
`<case>/<screen>/<dark|light>-<800|1120>.png`. Run
`go test -tags e2e ./internal/apps/evals -run '^TestLiveDesignRubric$' -count=1 -v`.
The judge records scores and visible evidence alongside the recipe, theme and
width. Missing captures and invalid scores fail the rubric run. Both
model-backed tests skip if the profile has no key.

The `support-shares-crm-identity` case checks the CRM and Support in the same
space. It accepts existing Contacts and Companies from Support's plan,
then reads the original type and record IDs through Support.
