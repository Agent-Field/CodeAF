# Sub-harnesses — subharness/ (design)

## Object model
A sub-harness is a durable registry entry (`~/.aforge/harnesses/<name>.hjson`) with: identity (name, desc, author, version int), program (DAG over node kinds), tool whitelist, verify (ladder arg: accept/schema/invariants/loop/report/rederive/adversarial/human), dynamism (fixed/branch/width/metaprompt/recursive/selfmod; integer cap), tests, run history (each run emits a trace dag JSON). Version is a pointer (v1, v2, ...).

## Node kinds (registry)
`agent.loop` (internal/session/loop orientated model+whitelist), `tool.call`, `parallel.split/join`, `branch`, `loop.until`, `human.gate`, `verify`, `subharness.call`, `trigger` (hosted/idle/watch/source.command). Library-in-binary, not generated code.

## As built — internal/subharness
Pages are strict JSON, not hjson: nothing in the tree parses hjson and a page is written by this package and by the distiller, so a format with one reader is not worth a dependency. Layout is `~/.aforge/harnesses/<name>/v1.json`, `v2.json`, … with runs beside them under `<name>/run/<ts>.json`. There is no head file — the head is the highest page present, so a pointer cannot disagree with the pages it points at.

The two ladders are enforced, not annotated. Each node kind declares the lowest dynamism rung a harness may hold it at (`branch`/`loop.until` need branch, `parallel.*` need width, `subharness.call` needs recursive), so a fixed harness cannot quietly contain a decision; `Dyn.Cap` is a whole-run budget spent by loop rounds past the first, and a run that spends it stops deciding rather than failing. A verify node may verify below its harness's rung but never above it, and a rung above `accept` needs something in the program that could keep the promise.

Validate is one function because the checks are not independent (kind → fields, rung → kinds, whitelist → tool names), and Save validates before writing, so every page on disk is one the runner can run. `Run` walks the shape and hands each node to an `Exec` the caller owns — the session loop, the tool call, the ask — which is what keeps this a registry and not a second engine. Its output is the condensed trace: `Trail` per executed step (a loop's rounds are separate steps) plus the edges actually taken.

## Adoption from Agent-Field skill
Autonomy spectrum (typed-vs-loop annotation per node), verification ladder as arg, dynamism ladder + budgets: output-traces of executed loops saved per run. Meta-point: templates derive from the problem, never from a menu.
