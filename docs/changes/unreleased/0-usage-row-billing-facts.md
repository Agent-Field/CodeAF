---
kind: added
title: every usage row names itself and says what kind of money it was, where that is stated
pr: 0
surface: [engine]
---

Was true: a `usage.jsonl` row said how much and on what, but not which row it
was, whether the call was metered or covered by a plan, or where its dollar
figure came from. Nothing could tell a process's own notice of a call from the
same call read back off disk, and nothing in the ledger distinguished a plan
door's call from a pay-as-you-go one.

Is true now: `RecordUsage` stamps every row with an `id` once and never
rewrites one a writer supplied. A row carries `billing` (`metered` or
`included`) and `billing_basis` only when the billing door the call went out
through states it — a bound door's `Metered` bit, or the plan overflow the
person opted into, captured per attempt — and carries nothing otherwise; a
zero cost, a direct key, a `:free` name or a sign-in is never read as
included. A late receipt keeps the billing of the attempt it belongs to. `price`
says `provider`, `receipt` or `list` where the writer knows. All fields are
additive and omitted when absent; old rows decode unchanged. Nothing is
enforced yet: no call is admitted, refused or priced differently.
