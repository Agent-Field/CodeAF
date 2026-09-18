---
kind: added
title: the relay can purge fixture vendors and refuse them at the door
pr: 1167
surface: [engine]
invalidates:
  - "The relay accepted any row whose model and judge matched `<vendor>/<id>`, and a stored fixture row — model vendor `crew`, judge vendor `other` — could only be removed one key at a time with `wrangler kv key delete`. `validateRow` now refuses a row whose model or judge vendor is not in `ALLOWED_VENDORS` when that variable is set, and `relay/tools/purge.js` lists and deletes the stored keys that carry a fixture vendor."
---

Two things under `relay/` only. The purge is `relay/src/purge.js`: it reads the
`sheet/` keys through the same list/delete calls the Worker's KV binding and the
tests' fake KV both answer, keeps the ones whose model or judge vendor is in the
named set, and deletes them — the tested logic. `relay/tools/purge.js` is the
thin wrapper that drives `wrangler kv key list` and `wrangler kv key delete`
through it, printing the keys under `--dry-run` and the count otherwise. The
stored keys are the only place a judge lives, so a purge followed by a publish
drops the judge from the document.

The rule is a third argument to `validateRow`, an allowed-vendor set applied to
the model and the judge vendors, each refusal naming its field. `worker.js`
reads it from `ALLOWED_VENDORS` through a `listVar` beside `intVar` and passes
it into the row loop; unset or blank, the set is null and every vendor passes as
it always did, so the default behaviour does not move. Neither `relay/` nor the
document names the vendors the pool serves, so the runbook points at the
published index — its cells' `model` vendors and its `judges` — as the source to
read them from before setting the variable.
