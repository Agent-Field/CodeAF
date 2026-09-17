---
kind: added
title: The Model Pool reaches its first surface — a `model_pool` setting and a `codeaf pool` verb
pr: 1093
surface: [chat, engine]
invalidates:
  - "`internal/pool` was reachable from nothing outside itself. `config.ModelPoolAt` resolves the stored word and the four pool names, and `codeaf pool show|status|verify` answers from it, so the packages have a surface and the resolver in internal/config is the one place the process environment is read for the pool."
  - "`CODEAF_MODEL_POOL` sat in `OperatorEnvPins` with a comment saying the word had no row because nothing in the binary called poolcfg. It fronts the `model_pool` row now — choices `on`/`read`/`off`, default `on` — and renders through the row the way `CODEAF_DOC_ENGINE` does; the two URL names and the TTL stay plumbing."
  - "`codeaf --help` had lines to spare; it did not — the page was already at its 110-line cap, and the pool entry was paid for by trimming three cells (`--model` off plan new, `--out file` and `--debug` off exec, the devices-revoke sentence folded) whose content the page or the per-command pages carry elsewhere."
---

The setting is one choice row in the models group, pinned by
`CODEAF_MODEL_POOL`. The verb has three forms: `show` — also bare
`codeaf pool` — prints the resolved config with the word saying where each
value came from (`default`, `setting`, `env` or `ci`) and the cached index
with its age, or `no index cached yet`; `status` adds the outbox's pending
count and whether the mode allows sending and reading; `verify` fetches a
fresh index through `pull` with TTL 0 and checks its detached ed25519
signature under `--key` or a built-in key, refusing at the door with exit 2
while no build carries the published key and leaving on exit 1 when a fetch
or a signature fails. `--json` prints one object on every form that answers.
Nothing sends anything yet: no run records a measurement, so the outbox is
always empty and `verify` is the one form that reaches the network.
