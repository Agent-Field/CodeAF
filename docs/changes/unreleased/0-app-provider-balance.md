---
kind: added
title: codeaf app asks a provider what it reports an account has left
pr: 0
surface: [engine]
---

Was true: the engine read OpenRouter's balance only to decide whether it was
low or the key expired, and threw the amounts away; the app had no way to ask
for them.

Is true now: `providers.balance` answers the account's remaining credit, the
key's own cap left and the lesser of the two, each only when the provider sent
it, with `status`, `last_success_at`, `last_error_at` and a stable `error_kind`.
Only OpenRouter reports a balance; every other provider answers
`supported: false` with no request, and a plan or subscription is never read as
a balance. The engine asks an account at most once a minute, shares one request
between concurrent callers, keeps the last good figures through a transient
failure and drops them when the key changes. Nothing is stored on disk. Engines
without the capability answer `unknown_method`. The existing low and expired
checks read through the same single implementation, unchanged.
