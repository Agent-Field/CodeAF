---
kind: added
title: a forked hand may declare an empty scope and only read
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "A fork used to refuse a hand that declared no write scope, so a reply that wanted several parallel READERS — four sources, four datasets, four files to compare — had to hand each of them writable paths it never meant to touch. `\"scope\": []` is now a hand with no `edit` and no `write` on its belt at all. The `scope` key is still required: a missing key is refused rather than read as read-only."
---

Read-only hands claim no paths, so two of them may inspect the same file and neither
collides with a writing hand beside it. Writing hands are unchanged in every particular,
including the refusal of two writers that claim one path.

The mechanism is the BELT and not the scope: an empty `Config.writeScope` is unrestricted
at `writeGuard`, so a read-only hand that kept its writers would have been the least
bounded hand in the building. Its `bash` keeps the existing orientation-only policy, which
is a capability boundary against an honest model and not a sandbox.

The PR number above is a placeholder: this landed on a local lane and has not been opened.
