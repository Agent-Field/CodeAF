---
kind: added
title: Desktop plugin methods share terminal connections and capability settings
surface: [engine, app, docs]
invalidates:
  - "The desktop protocol had no plugin connection methods. Engines advertising plugins now expose the existing connect catalog, browser and key connections, capabilities and disconnect actions."
---

Seven plugin methods use the existing account and capability stores.
Connect questions emit plugin events with the same ask id used by chat and
Inbox. The desktop answers through asks.answer after connecting, without
starting a second sign-in. Provider plan-pause settings update existing source
rows through settings.set.

Featured copy is engine data. Short blurbs derive from catalog descriptions,
and connected tool-server lists retain the server's own titles and read flags.

Round 2 exposes provider account emails, credential variable names and observed
plan pauses with vendor reset values. A bounded background probe reports local
Ollama availability. Plugin connections now save connection and key dates in
the existing credential store; older entries and provider dates remain absent.
Connection questions use short catalog labels, with the fact and key supplied
separately on the wire. The providers.list response remains unchanged.

Cancelling browser sign-in returns while an account lookup is pending and prevents
that attempt from saving credentials. Browser connections also recheck cancellation
inside the credential commit after waiting for its lock. Plugin ask ids include
the owning agent, so answering or stopping one agent cannot dismiss another
agent's connection request with the same session question key.

Key connections now return `checked`, true only after a catalog probe passes.
Listed connectors expose `key_checkable`; saving a key without a catalog probe
does not verify it. An unset key environment variable keeps its name in the
refusal instead of replacing that name with `[key]`.
