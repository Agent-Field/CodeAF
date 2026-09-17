---
kind: added
title: The pool client gains the relay's real addresses, an install nonce, and the index signer's key
pr: 1093
surface: [engine]
invalidates:
  - "The pool's default addresses were `https://pool.invalid/index.json` and `https://pool.invalid/submit`. They derive from one base now — `DefaultRelayURL` is `https://codeaf.agentfield.ai/pool`, the index at `…/index.json`, the submit at `…/v1/rows` — and `CODEAF_MODEL_POOL_RELAY_URL` moves the base. The two per-address pins (`CODEAF_MODEL_POOL_URL`, `CODEAF_MODEL_POOL_SUBMIT_URL`) still override, and `pool show` prints a `relay` line beside the two derived addresses."
  - "`pool verify` refused with `no public key built into this build; pass --key` because the build carried no key. The index signer's key is decoded into `poolPublicKeys` at init now, so `verify` fetches without a flag; `--key` checks a document signed under some other key, and the new `models.pool.public_key` row (env `CODEAF_MODEL_POOL_PUBLIC_KEY`) replaces the built-in key when set — and only it: a stored word that does not decode trusts nothing."
  - "A stored pool index was read only at the fetch that brought it. `refreshPoolIndex` now runs daily under the config's TTL from a build that carries the key, and a changed document is still read at the next start."
  - "An outbox batch carried no identity. Every batch over http now rides `X-Codeaf-Install` with the install's nonce — 32 lowercase hex in `<poolDir>/install`, replaced when it stops being that shape — and when `model_pool` is on the outbox is pushed after each judged run and once at start-up, budget 5 s each, errors at debug level only."
---

The relay wants a nonce on every batch and signs its index under one key; this
wave gives the client both, so an install with `model_pool` on reads the
signed index and sends its rows the day the relay answers. Until it answers,
every path falls back the way it did: the cache, the seed, and the install's
own sheet, with nothing said at a person.
The relay's index has a second address — the same signed document copied to
GitHub — held as `DefaultMirrorURL` (env `CODEAF_MODEL_POOL_MIRROR_URL`, a
value set and empty to turn the fallback off) and printed by `pool show` beside
the other addresses. A refresh that fails for any reason other than a signature
failure falls through to the mirror, which shares the cache directory, so the
document with the higher version is the one kept; a signature failure is a
statement about the primary's bytes and does not fall through at all. `pool
status` now asks the relay, and the mirror when the relay does not answer, under
a three-second budget and TTL 0, and prints one line: whether each answered,
the index version it served, or the reason it did not — with `--key` to check a
document signed under a key other than the one built in. Status still exits 0
whether or not anything answered.
