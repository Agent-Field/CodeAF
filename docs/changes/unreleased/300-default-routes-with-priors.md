---
kind: fixed
title: the shipped default keeps one ledger key instead of three, so it routes with priors
pr: 300
surface: [engine]
invalidates:
  - "The lane ledger was believed to key one model under one name once the tier suffix came off. It did not for a floating alias: the sighting side filed deepseek/deepseek-v4-flash-latest, the beat asked for a sheet under ~deepseek/deepseek-v4-flash-latest, and the machines that answered were deepseek/deepseek-v4-flash-0731's — three keys for the model every install routes by default, which is why the shipped default chose between no lanes at all."
  - "catalog.Concrete and catalog.Identity read as wired mechanisms; they were built by #186 and had zero production callers until now. catalog.Servable is the fold the ledger uses, and it is installed at launch from cmd/aforge/subharness.go beside profile.UseIdentity."
  - "Folding an alias through catalog.Identity looked like the fix and is not. Identity prefers the canonical slug, which for ~deepseek/deepseek-v4-flash-latest ends at deepseek/deepseek-v4-flash-20260731 — an id OpenRouter lists nowhere and publishes no endpoints page for. alias_target is the only servable answer, and the bare deepseek/deepseek-v4-flash is the 0423 snapshot, a different model."
  - "internal/lane/lanestub could only stage a router that publishes an endpoints page for every model it answers for. Server.Alias now stages the real asymmetry: a floating id is answered on the completions endpoint and 404s on its endpoints page."
---

The wire is unchanged: it still sends the alias, the panel still shows it, and
the profile still keys its history the way it did. Only the two reads that name
a belief resolve — the sighting side and the beat — so one model has one ledger
key, the sheet lands under it, and the chooser has something to rank.

Measured on the shipped defaults, 2026-09-02: one `aforge do` against
`~deepseek/deepseek-v4-flash-latest` left a ledger with ONE identity in it,
`deepseek/deepseek-v4-flash-0731`, where before it would have left two.

Two limits, so nobody reads more into this than it says. `aforge do` runs no
lane beat at all — `startLaneBeat` is on `session.Agent`, which the chat paths
build and the headless ones do not — so a headless run still has no sheet and
still learns only from what it has served. And a process that dies before its
catalog warms files its records under the alias, because a cold catalog answers
the id as written; `profile.Load` adopts such early keys and the lane ledger has
no equivalent merge yet.
