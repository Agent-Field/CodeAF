# AI actions and agent runs in an app

## Let an agent work in my app or draft replies for these records

Declare an app Action that starts a run with an `ai` step. Add a positive
`budget.per_run`, a prompt that reads declared record fields, an `output`
JSON Schema, and an `example` response for Check. Use a declared `ref`
parameter such as `mention` and set `propose: $params.mention` to stage the
step's object output as a proposed change. This run has one step. The output
schema names the fields the proposal may change. Check uses the example.
Live runs use the connected model.

Use `agents.launch` to run the declared Action on one to 50 records or on a
query that selects them. The engine divides the run budget across that
selection. The AgentRun panel links the drafts to one queue. Review and
Apply selected results, decline them, or accept safe proposals for empty AI
fields. The engine checks the displayed record revision for every selected
draft before committing the batch. Editing the record while an agent works
lets your edit stand.

## AI fills this field when I create or change a record

Declare an optional scalar field with `ai` instructions and
`ai_on_change: true`. Give the app the `sort` AI capability, write access to
the record type, and a positive per-run budget. The app computes that
type's automatic fields after a record is created or changed. A newer
revision takes priority and values you entered or cleared stay pinned.
Automatic model output carries AI provenance.

## Use Claude Code on these records or choose a harness

Choose `codeaf` or an available harness in the Action's `ai.harness`.
See the app's harness catalog for the adapters that can run under the
app's limits. Claude Code needs tool-free API-key mode to be available for
this Action. A model-backed Action is unavailable if the app cannot reach
its harness. Deterministic records, Actions and runs work without a model.

## Approve the exact text before posting

An outbound connector step waits at a send approval. The pending effect
shows the operation, destination, payload and app version. A standalone
`gate: {approval: send}` can also show exact text from the run in its
`args`. An approval applies to that displayed effect only. A stopped or
uncertain delivery stays incomplete. A connection must be available to
deliver an approved connector step.
