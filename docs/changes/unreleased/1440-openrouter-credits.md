---
kind: added
title: codeaf reads the OpenRouter balance and starts a near-zero account on free models
pr: 1440
surface: [chat]
invalidates:
  - "codeaf does not know your OpenRouter balance: a fresh account got a paid chat default and five paid crew seats, worked for a few turns and then died on a 402 whose top-up link was cut at 120 characters. It now reads the balance (account credit less usage, capped by the key's own limit) at first run, on a key change, after a payment refusal, and — while low — at launch and on a switch to a paid model. At $0.50 or less, a new conversation nobody chose a model for and every crew seat nobody set default to free models, a paid model draws `Your OpenRouter account is low on credits — some models may not be available` under the message box, a 402 that names an affordable output cap is retried once with it, and the refusal row keeps the vendor's whole sentence."
---

The reading lives in the profile's `credits.json` as a low flag, a known flag, a
key fingerprint and a time — never a dollar figure and never the key. Reading it
is two account lookups and no model call. A model, crew or tier row somebody chose
is never replaced, and the free defaults are never written into `config.json`.
