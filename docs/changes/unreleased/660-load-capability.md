---
kind: changed
title: Rarely-reached tools wait on a named shelf and are fetched by load_capability
pr: 660
surface: [chat, engine, docs]
invalidates:
  - "Every tool a conversation could ever reach for rode in the tool block of every request. The five making verbs, the settings pair and the four saved-procedure hands now wait on named groups — `media`, `settings`, `harnesses` — and one `load_capability` call appends a whole group to the tool list."
  - "Nothing was said about when an armed family becomes callable, and what was said elsewhere said `NEXT turn`. Both `load_capability` and `use_service` now state that the tools arrive on the NEXT REQUEST, which is still the same turn, and tell the model to carry straight on rather than wait for the person."
  - "Loading claimed to be permanent. It lasts the running session, and a reopened conversation re-arms the groups by replaying its own transcript's `load_capability` calls — with the one gap that a load compacted out of the transcript is not replayed."
  - "A worker and a task node shelve nothing and carry these tools directly, so their pages are composed from the direct wording and never name `load_capability`."
---

## What moved

`internal/session/tools_capabilities.go` is the whole of it, and it is a
partition rather than a gate: `tools.go` builds the belt exactly as it always
did, and the last step splits what it built into what the model carries and
what waits. A group is armed through `Agent.armFamily` — the same door a
connected account's tools arrive through, bound by the same append law — so the
tools are the same `bare.Tool` values, approved, hooked, evented and charged by
exactly the machinery that would have handled them had they been carried.

The catalog rides in `load_capability`'s own description, and **each group line
is its own surviving tool names**. A group whose members were all gated off does
not exist, a build with nothing shelved carries no loading verb, and no line can
promise a capability this machine lacks.

## The bytes, measured

On the shape `prefixbudget_test.go` weighs — a conversation with no media
models — the tool block went **27,081 → 23,779**: 4,296 bytes held back for the
994 the loading verb costs. The page went **20,527 → 20,909**, paying 382 bytes
for the sentences that say how a shelved verb arrives. The fixed prefix is
**44,688**, 3,312 under its 48,000 budget, and the budget is not lowered to meet
it. On a fully-wired machine the tool block goes **40,597 → 27,081**, holding
back 14,565 bytes for a 1,049-byte verb.

## Where it does not apply

`Config.shelvesCapabilities` is false inside a task, and it is the one reading
of that question: `Agent.shelveDeferred` and the page's load-this-group
sentences are both composed from it. A node is briefed once and lands, the
saving is only paid on a prefix re-sent every turn, and the landing belt needs
the making verbs directly — so a node's belt is byte-identical to what it was,
and its page never names a verb it does not have. A hand and an auditor replace
the belt wholesale and have their shelf cleared with it.

## Limits

- A load that cannot happen — an unknown group, an arming that fails — is a
  **tool error**, not a success whose tools never turn up.
- Loading is not permission: every gate answers exactly as it did before.
- Reopen re-arms from the transcript. A `load_capability` call that has since
  been compacted away is not replayed, and the line claiming it is gone too, so
  the two stay in step and the model simply loads again.
