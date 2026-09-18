---
kind: added
title: `codeaf pool status` says the install identity, every landing judged in all, and that nothing has landed
pr: 1169
surface: [chat]
invalidates:
  - "`pool status` said `last judge: none yet` both for an install whose pool never ran a judge and for one with no landing judged. With no judge record it says `last judge: none yet (no landing judged)`, and `--json`'s `last_judge` stays null."
  - "`pool status` counted only what the last sweep judged. It now folds every landing judged in all, from the judge's markers, into the sweep line as `judged N in all`, and `--json` carries `judged_total`."
  - "`pool status` never said whether the install had minted the nonce it sends under. It ends the outbox line with `· identity set` when `pool/install` is present, and `--json` carries `identity` — and the reading form never mints the file, because a reading form writes nothing."
---

The three are status-only: `pool show`, its words and its `--json`, are the
ones they always were, and none of the readers creates a file. `judged_total`
counts the markers under `pool/judged/`, so it is every landing-and-attempt
ever scored, not just the last sweep's own count; `identity` is the presence of
`pool/install`, read the way the install's own nonce reader must not — by
asking, never by minting.
