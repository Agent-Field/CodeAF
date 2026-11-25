---
kind: fixed
title: A hosted chat at its spending limit reads the organization's sentence, not a payment failure
pr: 2936
surface: [chat, engine, factory, docs]
invalidates:
  - "A hosted chat that reached its spending limit or its person's monthly limit read \"<service> accepted the key but the account cannot pay\" in front of the organization's sentence, because the organization's model door answers with an HTTP 402 shaped like a provider's. It now reads only the organization's own sentence, and a provider's own payment refusal still reads as it did."
  - "A hosted chat's limit sentence wrote every limit with two decimals, so a limit of a tenth of a cent read $0.00. A limit that is a whole number of cents still keeps two decimals ($5.00, $0.50), and a finer one is spelled as it was chosen ($0.001)."
---

Every error the organization's model door writes now carries `"door":"organization"` in its error envelope, and the
engine and the page read that mark, never the sentence, to tell the organization's own words from a refusal a provider
passed through. The door's limit refusals stay terminal: nothing about retries, model hops or the spend path changed.
