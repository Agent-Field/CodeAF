---
kind: added
title: a second place models come from, connected from /connect
pr: 800
surface: [chat, engine, docs]
invalidates:
  - "aforge talked to models with ONE key and ONE address. `config.Config.APIKey` and `.BaseURL` were the whole story; `ClientConfigFor` now takes the service set and the model id decides the account, so `deepseek-direct/deepseek-v4-pro` reaches a different vendor than an unqualified id does. An unqualified id still belongs to the default service and means exactly what it meant before."
  - "The first-run OpenRouter step was the only way a key ever got in, and `getting-started.md` called it a prerequisite full stop. It is still a prerequisite for the DEFAULT service and first run is unchanged — but a second service is connected from the `models` group in `/connect`, or from the Providers tab, and needs no restart."
  - "`sessions-and-rewind.md` said the record redacts \"keys in the `sk-` family\". A Z.ai key is not `sk-` shaped. Every key aforge holds is now registered with `trace.Secret` at the moment it is resolved and redacted by its exact value, whatever its shape; the familiar shapes are the fallback scan for values that did not come from aforge's own settings."
  - "`starting-aforge.md` said a custom `AFORGE_BASE_URL` is never offered the OpenRouter connection, and offered no alternative. Still true, and connecting a service is now the supported road to a different endpoint."
  - "The model catalog cache was one file per base. It is now keyed by the pair — service and normalised base — because two services can share an address; v3's own picker cache at `~/.aforge/v3/models.json` is keyed the same way through one exported derivation. A cache written before this is read as the default service's, so no ordinary install pays a cold fetch."
  - "A model catalog with no rows meant the built-in fallbacks. Those rows are the default service's ids, so a service that publishes no model list now gets an EMPTY catalog and `generate_image`, `speak`, `generate_music` and `generate_video` are off the belt there — absent, not broken."
  - "Nothing stopped a credential reaching `remote.Hello`. It is pinned to an allowlist of the fields somebody reviewed, so a new field fails the law until its reason is stated. A service's NAME stays legal: a spend row will carry one when pricing lands."
  - "A direct service books `Cost == 0`. That is correct in this phase rather than an invented rate — pricing, the vendored models.dev catalog, per-service tiers and coding plans are all Phase 2 and later."
---

Phase one of five. The noun on screen is **service** and the verb is **connect**;
`source` is the internal word and stays in the code. Five services ship as data —
DeepSeek, Z.ai, Moonshot, Ollama and "Something else" — and two of them publish no
model list, which is why a connection is proved either by `GET /models` or by a
one-token completion.

This pull request also carries four fixes that were open separately: the one client
door (#750), the base-scoped catalog cache (#748), a bare id no longer matching a
vendor-qualified price row (#749), and the `AFORGE_QUESTION_DEMO` settings pin (#754).
