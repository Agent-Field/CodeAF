---
kind: changed
title: an unconnected account is reached through one question and the same turn carries on
pr: 1037
surface: [chat, engine, docs]
invalidates:
  - "On the shipped approval posture, `services` and `use_service` each raised a tool-approval question before the connect card. They now reach the card without those duplicate questions: `services` joins the read-only lift, and `use_service` is lifted because its own question is its consent. An explicit `services:prompt` or `use_service:prompt` rule still asks, and a deny blanket still denies."
  - "The `use_service` description told the model that newly armed tools arrived on its next turn, which reads as an instruction to stop and wait for the person. It and the success result now share one sentence saying they arrive on the very next request of this same turn and should be used now."
  - "The prompt's only cue that the person has accounts was one tool description. The `use_service` belt fact now says the person has accounts the model can act in, never to answer \"I don't have access to your X\" before calling `services`, and that an account not connected yet is asked for through `use_service`. Without a hub the prompt still says nothing about accounts."
  - "A declined connect card, five minutes without an answer, and words typed beneath a browser connect question all told the model that the person refused. They are three distinct results now, and the moved-on result carries the person's words verbatim and asks the model to act on them."
  - "An answer that arrived at the five-minute connect boundary could still be reported to the model as silence. The resolver and the clock now compete for one winner, and a claimed answer always wins."
  - "A browser sign-in could keep drawing `waiting in your browser…` after the turn and its listener had ended, and that row stayed outside the render cache for the life of the session. Turn settlement now changes it to the existing `connection didn't complete` row and stops its animation."
  - "Words typed beneath a browser connect question opened a `checking your <Name> key…` card for a service that has no key. Only a question that asked for a key opens one."
  - "A failed browser callback put its OAuth state nonce and `authorization error: state does not match` into the transcript, the model result, the `/connect` panel and the Connections tab. Every browser road now crosses one deny-by-default boundary in `internal/connect`: an internal failure becomes `the sign-in came back wrong and nothing was connected`, and a vendor refusal keeps only its plain-words reason."
  - "The accounts manual said aforge never opens a browser for you and that two credential writers could lose each other's last change. It now says the browser is opened and the address is written down with a copy affordance, and that the store takes a cross-process lock and reloads before every write. `permissions.md` lists `services` and `use_service` among what runs without asking."
---

The connect card remains the one place the person agrees to connect an account.
Every tool that arrives from that account is still judged when it is called.
The chain itself — `services`, `use_service`, the card, the browser, the wait,
the same-turn arming — was already there and was proved on the real binary
before this change; what changed is the friction around it and three places it
lied.
