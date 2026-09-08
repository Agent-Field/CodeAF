---
kind: fixed
title: a covering refusal teaches which machines this account cannot reach
pr: 649
surface: [engine, chat, docs]
invalidates:
  - "Review found that cooldowns could expire while a refusal was in flight, turning our transmitted veto into a supposed account exclusion. Learning now reads provider.ignore and provider.only from the actual HTTP request returned with the response, and learns nothing when that evidence is unavailable."
  - "#586's law had one denominator without a demand: the lanes this process has itself timed. It has two now — that list minus the machines the router has proved it would not have sent to (`velocityLedger.reachableLanes`). A lane switched off on the OpenRouter account is no longer counted as somewhere a request can land."
  - "The `All providers have been ignored` memo was taken and could then do nothing when the account's own ignored providers were half the cause: a model served by two machines, one excluded on the account and one honestly struck here, emptied the set on every request for the whole cooldown and paid a hidden round trip each time. It now costs one refusal per model in a session, as the law always claimed."
  - "This process was said to have no way of knowing which machines its account cannot reach. It has one, and it was already arriving: a request that demanded no machine, carrying our own list, answered `All providers have been ignored` proves that every lane this process knows and was NOT refusing at that moment is a lane the router would not have sent to. The ledger records them (`velocityLedger.learnUnreachable`), reading its own live cooldowns rather than rebuilding the preference object, so it learns a subset of what the refusal proved and never a guess beyond it."
  - "A refusal is not evidence about machines outside the set it was refused for. A refusal of a request carrying `provider.only` teaches nothing here, because `allow_fallbacks: false` makes the demand its own set; and the subtraction is applied only to the second denominator, never to a demand, so #584's release of a demand a veto covers is unchanged."
  - "Nothing put a machine back once it had left the count. A served answer does, at the sighting itself: switching a provider back on in the account settings needs nothing from the person and no restart."
  - "`internal/manual/chat/lanes.md` promised that `All providers have been ignored` costs at most one wasted round trip in a session, and said nothing about the two lists composing. It has its own section for the account's ignored-providers list now: that aforge cannot read it, what the two lists do together, and that a machine which answers is counted again at once."
---

The residual of #584 that #586 could not close, and it needed a denominator this
process was thought not to have. It has one: the router's own sentence, which
says the set was empty and is therefore a fact about every machine in it — not
just about the list that was sent.
