---
kind: fixed
title: whether a base carries a routing preference is learned from the base, never from its hostname
pr: 547
surface: [engine, chat]
invalidates:
  - "`Client.isOpenRouter()` decided whether a routing preference reached the wire — the base URL containing `openrouter.ai`, or the model spelled `openrouter/…`. So a pin on a proxy, a mirror, a self-hosted router or the router reached by its IP was silently left off the request while the settings row went on reading `pinned: X`. That decision is gone. Every site that decided a preference now asks `Client.carriesPreferences()`, which is what the BASE answered."
  - "`Client.isOpenRouter` no longer exists by that name. `Client.shippedRouterHint()` is what survives, and it decides exactly two things, neither about lanes: the attribution headers OpenRouter's own ranking page reads, and the choice of this adapter's transport over the SDK's. Wiring a new gate to it reopens the defect class of #373 and #433 both."
  - "\"aforge only sends a routing preference to something it believes is a router, and it decides that from the base URL or the model id\" — the sentence at the head of `internal/lane/lanestub` — is not true of anything any more. A test that spells its model `openrouter/…` or uses `Server.RouterURL()` to get a preference on the wire is asserting against the dress: the plain `Server.URL()` gets lanes, a sheet and a preference from what the stub ANSWERS. `RouterURL()` survives for the shipped router's fast path alone (proving it pays no extra request) and says so."
  - "A base with no lane sheet got no `provider` object at all. It gets one ONCE now — the asking is the sending, and there is no other way to learn — and its own answer is remembered per base: an answer that names the lane which served carries, a 400 of its own naming the `provider` field does not, and a 200 with no lane information at all is read as \"does not\", which is the safe reading. The refused request is widened and sent again once, at the rung that drops the whole provider object, so the work still goes out."
  - "A pin that cannot be sent is no longer silent. aforge says one line in the conversation, once per (base, pin), naming both: `<base host> does not take a lane choice; <lane> is not being asked for, and your requests still go out`. The lane row says it too — `pinned: cloudflare (not taken on this base)` — and somebody who pinned nothing is told nothing."
  - "`internal/lane`'s sheet answered one question about a base. It answers two: `PrefsCarried(base)` beside `SheetServes(base)`, filed under the base they were asked of and cleared together when the base moves. The two point opposite ways on purpose — an unasked base CARRIES (you cannot learn without asking) and an unasked base does not SERVE (a set of endpoints is a claim it has to show)."
  - "The endpoint-refusal ladder's router clause was `isOpenRouter`. It is `Client.baseServesLanes()` — the endpoints page the base answered — because what that clause asks is whether there is a SET of endpoints behind a model that could be emptied, and a single endpoint has none."
  - "`TestAdapterRequestsAreDeterministicForTheSameInput` compared the first two calls. It compares the second and third, and pins that the first is the asking. Determinism here has always been a law about the encoder GIVEN THE SAME KNOWLEDGE — a knob a model refuses is discovered by sending it once and then never sent again — and a base's answer about the `provider` field is the same class of learned fact."
---

The general form of #373 one layer up, and closed the same way. Nothing in an
OpenAI-compatible answer says which machine served it, so a preference honoured
silently and a preference dropped on the floor cannot be told apart from the
transport — and where the evidence runs out this build takes the reading that
produces a sentence rather than the one that produces a silence. The person is
told; the request still goes out.
