# Builder model bench

Measured 2026-10-13 with `cmd/apps-buildbench` against OpenRouter: PLAN, then WRITE, then FIX (up to 3 rounds) on four owner-style paragraphs (`crm`, `tracker`, `inventory`, `booking`; see `cmd/apps-buildbench/presets.go`). Reasoning is switched off (`reasoning.enabled=false`); Gemini refuses that, so it runs at `effort: low`. Seconds are one call's wall time with all runs in parallel; cost is OpenRouter's reported USD. **Pass** = `spec.Validate` + decode + the bench's binding check (every bound type, field, screen, run, command and part exists; agent tools are inside the capabilities; connectors are connected) + coverage (every record type has a list, a detail, a create and an edit).

Reproduce: `go run ./cmd/apps-buildbench -model <id>[,<id>] [-preset crm|tracker|inventory|booking] [-v]`. Files land in `.tmp/buildbench/<model>/<preset>/`.

## Default model, three repeats (final prompts)

`deepseek/deepseek-v4.1-flash`, 12 of 12 passed, 0 fix rounds, no coverage gaps.

| app | plan s | write s | cost |
| --- | --- | --- | --- |
| crm | 1.3 – 1.4 | 8.0 – 8.5 | $0.0036 |
| tracker | 1.2 – 1.3 | 6.3 – 8.0 | $0.0025 – 0.0054 |
| inventory | 1.4 – 1.6 | 9.3 – 12.6 | $0.0043 – 0.0058 |
| booking | 1.3 | 11.1 | $0.0051 |

An app costs about half a cent and takes 8 – 14 s in total.

## Bake-off, one run each (final prompts)

| model | app | plan s | write s | fix | cost | pass | notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| deepseek/deepseek-v4.1-flash | crm | 1.3 | 8.0 | 0 | $0.0036 | yes | stages Lead → Lost, contacts, board, quiet-deals list |
| | tracker | 1.3 | 8.0 | 0 | $0.0037 | yes | board by status; summary is an agent button, no email |
| | inventory | 1.6 | 12.6 | 0 | $0.0058 | yes | computed `low` flag drives a Running low list and the reorder trigger |
| | booking | 1.3 | 11.1 | 0 | $0.0051 | yes | one `starts` datetime, upcoming list above the board |
| anthropic/claude-haiku-5.5 | crm | 1.9 | 11.3 | 0 | $0.0038 | yes | same shape as the default |
| | tracker | 1.9 | 8.6 | 0 | $0.0032 | yes | no automation, summarise button |
| | inventory | 2.2 | 12.1 | 0 | $0.0039 | yes | |
| | booking | 2.0 | 10.6 | 0 | $0.0037 | yes | |
| google/gemini-3.6-flash | crm | 2.4 | 16.5 | 0 | $0.0278 | yes | good, but 7× the cost |
| | tracker | 3.2 | 13.1 | 0 | $0.0242 | yes | |
| | inventory | 3.5 | 22.6 | 0 | $0.0339 | yes | |
| | booking | 4.0 | 15.0 | 0 | $0.0271 | yes | |
| openai/gpt-5.4-nano | crm | 3.7 | 16.6 | 0 | $0.0041 | **no** | WRITE reply: screens/companies.json is not valid JSON |
| | tracker | 3.2 | 22.9 | 0 | $0.0057 | yes | |
| | inventory | 4.7 | 40.6 | 1 | $0.0121 | yes | one fix round |
| | booking | 3.1 | 15.5 | 0 | $0.0053 | **no** | WRITE reply: screens/bookings.json is not valid JSON |
| minimax/minimax-m3 (earlier prompts) | crm | 61.1 | 38.9 | 0 | $0.0257 | yes | reasons whatever is asked; far too slow |
| | tracker | 78.6 | 6.5 | 1 | $0.0288 | **no** | stray key in the WRITE reply; fix round could not parse |
| | inventory | 41.8 | – | – | $0.0106 | **no** | WRITE timed out |
| | booking | 69.0 | 93.0 | 0 | $0.0392 | yes | |

## Recommendation

Keep `deepseek/deepseek-v4.1-flash` as the default: 12 of 12, about 1.3 s to plan, 6 – 13 s to write, half a cent an app. `anthropic/claude-haiku-5.5` is the fallback: 4 of 4, a little slower, same price, steadier JSON. Do not use `gpt-5.4-nano` (invalid JSON in 2 of 4 first writes) or `minimax-m3` (a minute to plan).

## What moved the quality

- The old few-shot used `DS Flash` as the model name, a plan with no `name`, and `open` pointing at its own screen; the model copied all three. The example is now valid against the schemas and shows the whole pattern: a list with a New button, the selected record in a panel with a detail page, an edit form and a confirmed delete.
- Every record type gets a screen with that pattern, so every type has list, detail, create and edit. A status enum gets a board; a subset the owner asked to see (running low, upcoming) gets its own list above the main one.
- A summary the owner wants to read is an agent button (`agent.ask`), not a run that emails it. Nothing is sent that nobody asked to send.
- Queries use plain tokens only, and `when` / `computed` name fields bare, as `docs/apps/CONTRACT.md` does.

## Open points for other lanes

- WRITE emits an edit form's `set` mapping for every field (about 700 characters a screen). If the desk's Form can save itself, dropping `on.submit` makes WRITE smaller and faster (not measured).
- `prompt.Plan` has no `name`; the bench keeps the model's own object and stamps `$schema`, `ask` and `needs`.
- The WRITE call is one request. Splitting it per file and running the calls in parallel would cut the wait to the slowest file.

## Runs regression proof · 2026-10-13

All four paragraphs passed the production `internal/apps/check` service
within three Check rounds on `deepseek/deepseek-v4.1-flash`, with that model
also used for run agent steps. The bug-tracker paragraph was the UX tester's
exact ask: “A bug tracker for our small web team. Bugs have a title, steps to
reproduce, severity (S1 to S3), status (new, triaged, in progress, fixed, won't
fix), an assignee and the release they are fixed in. The agent triages new
bugs: it suggests a severity and spots duplicates. I approve every status
change to won't fix.” The other paragraphs are the CRM, inventory and booking
presets in `cmd/apps-buildbench/presets.go`.

| paragraph | Check rounds | model seconds | total USD | Check pass |
| --- | --- | --- | --- | --- |
| crm | 1 | 13.15 | $0.005620 | yes |
| bug-tracker | 1 | 9.89 | $0.006699 | yes |
| inventory | 2 | 16.50 | $0.008598 | yes |
| booking | 2 | 15.45 | $0.009547 | yes |

Seconds sum PLAN, WRITE, FIX and behaviour model calls; costs include all
those calls. Jobs ran concurrently. Round 1 is the initial WRITE; at most two
FIX calls were allowed. Booking's first report rejected a misplaced `summary`
property; inventory's first report rejected `$input.note` without an ask. FIX
repaired each using the full current files and rule hints. The bug triage run
completed against the real sample store without any FIX call.

The command's original verifier checks schemas and bindings, not production
Check. For this proof, its pipeline was copied to `.tmp/c1-buildbench`, the
tracker preset was replaced with the paragraph above, and `verify` was
replaced by a call to `check.New(...).Run(...)`. The adapter used the real
sample generator, records store, capability checker, runs engine, approvals
and fake connectors, and included behaviour-model spend in each result.
Run output and generated definitions are in `.tmp/c1-results/`; the per-round
Check reports are in `.tmp/c1-bench.log`. No command-package files were changed.

The deterministic `TestCheckBugRunsAgainstRealSampleStore` separately proves
the two-run case from the UX report: triage finishes, the won't-fix run pauses
at its gate, and both receive an actual sample bug id. Schema tests reject
bare param references and gate-only steps. Query tests assert the offending
query and repair hint reach the Builder's report.

An agent-runtime dependency remains outside this lane: `records.delete` is
absent from `internal/apps/agent`'s tool allowlist and dispatch. Generated
agent definitions and Check now require it for rw records; the agent lane
needs to expose that existing records-service operation.
