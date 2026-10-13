# App-agent model bench

Run the agent inside built apps, with the production `internal/apps/agent`
turn loop, prompt, tools, capability checks, records and model adapter:

```sh
go run ./cmd/apps-agentbench -model deepseek/deepseek-v4.1-flash -preset crm
go run ./cmd/apps-agentbench -apps .tmp/buildbench/deepseek_deepseek-v4.1-flash \
  -model deepseek/deepseek-v4.1-flash,openai/gpt-5.6-sol -preset all
```

`OPENROUTER_API_KEY` must be set. The production model adapter uses that key
through the normal provider stack and returns its reported USD usage. The
command runs directly through `Agent.Send`; it needs neither an app server nor
a WebSocket. Like `cmd/apps-buildbench`, this separate command is absent from
`cmd/codeaf` and `make build`.

## Bundles and tasks

`-apps` selects the directory **containing** `crm/`, `tracker/`, `inventory/`
and `booking/`. The default is
`~/af-w2/models/builder/files/deepseek_deepseek-v4.1-flash`. The builder's
`save` helper writes `<out>/<model-with-slash-replaced-by-underscore>/<preset>/`;
select that model directory, not the top-level output directory. `app.json`,
`agent.md`, screens and runs are loaded with `spec.Load`.

Five owner-style tasks per app live in `presets.go`. Every model/app/task job
gets a fresh temporary app registry, thread and real SQLite sample database.
The product's sample generator fills required fields and relationships for
three rows per collection. Fixture hints give those rows familiar names,
quiet/recent deals, overdue issues, low stock and upcoming bookings. The delete
contact task starts with Ana present, independent of the add-contact task.
Only the two model selectors change in a private copy of the bundle; the
source files, prompt body, tool list, capabilities and budget stay as built.
Field alternatives select existing schema names; an unsupported schema becomes
a setup error with its cause in the result.

Jobs run concurrently with `-parallel 8` by default. `-timeout 4m` bounds each
agent turn, excluding fixture setup. Runs and connectors use the same services
as the app server: the current connector implementation returns canned replies;
records and model completions are real. These presets ask only about records.

## Results

The stdout table separates execution (`OK`) from the task's data check (`DATA`).
Go/model errors, tool-error feedback, a step limit or missing final speech mark
execution as `error`. Intermediate speech before a tool call does not count as
a final reply. A refusal can be `ok` with `DATA fail`. The original agent's
JSON-or-plain-speech parsing remains in place: malformed protocol speech is
available for human review rather than being corrected by the bench.

`data_changed` compares before/after snapshots. `data_ok` checks:

- Create: collection count increases by one and the requested fields exist.
- Update: collection count stays fixed and requested fields change to the target values.
- Delete: collection count decreases by one and the seeded target disappears.
- Read: at least one records read occurs and the content stays unchanged.

Read checks do not score factual quality. Each JSONL record includes the task,
model, app, final reply, error, wall seconds, USD cost, tool-call count, events,
expected fields and before/after records so a person can judge that quality.
A requested capability absent from a bundle stays absent: for example, the
current deepseek CRM bundle does not list `records.create`, and the app agent
has no delete tool. The bench exposes their unmet data checks.

The production model adapter currently sends tool-result messages without
native tool-call IDs. An endpoint that requires `tool_call_id` rejects later
steps; these are measured as errors, rather than hidden behind another model
adapter. Successful completions before the failure still contribute cost.

Each invocation creates a new `-out/run-<id>/` directory (default
`.tmp/agentbench`). Each task gets one numbered `.jsonl` file containing one
JSON line; previous runs remain untouched. For blind review, shuffle filenames
and print only the task and reply, withholding model identity:

```sh
find .tmp/agentbench/run-<id> -name '*.jsonl' | shuf | \
  xargs -r -n 1 jq -r '[.task_text, .final_reply] | join("\n")'
```

The command exits nonzero when any execution or data check fails, after writing
all results. A benchmark failure is evidence about the built app and model;
it is separate from the Go build/vet/test gate.

## Verification

```sh
GOFLAGS=-buildvcs=false go build ./... && \
  go vet ./cmd/apps-agentbench ./internal/apps/... && \
  go test ./internal/apps/... ./cmd/apps-agentbench/...
```

Tests use scripted completions only in tests, with the real agent and SQLite
services, to check model overrides, source preservation, task isolation,
create/update/delete assertions, recovered tool errors, missing final speech,
JSONL text retention and cancellation without real sleeps.
