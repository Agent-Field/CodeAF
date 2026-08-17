# Sub-harnesses — subharness/ (design)

## Object model
A sub-harness is a durable registry entry (`~/.aforge/harnesses/<name>.hjson`) with: identity (name, desc, author, version int), program (DAG over node kinds), tool whitelist, verify (ladder arg: accept/schema/invariants/loop/report/rederive/adversarial/human), dynamism (fixed/branch/width/metaprompt/recursive/selfmod; integer cap), tests, run history (each run emits a trace dag JSON). Version is a pointer (v1, v2, ...).

## Node kinds (registry)
`agent.loop` (internal/session/loop orientated model+whitelist), `tool.call`, `parallel.split/join`, `branch`, `loop.until`, `human.gate`, `verify`, `subharness.call`, `trigger` (hosted/idle/watch/source.command). Library-in-binary, not generated code.

## Adoption from Agent-Field skill
Autonomy spectrum (typed-vs-loop annotation per node), verification ladder as arg, dynamism ladder + budgets: output-traces of executed loops saved per run. Meta-point: templates derive from the problem, never from a menu.
