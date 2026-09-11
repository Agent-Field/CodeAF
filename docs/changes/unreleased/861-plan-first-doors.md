---
kind: added
title: a service's subscription door is tried before its metered one
pr: 861
surface: [chat, engine, docs]
invalidates:
  - "A connected service had ONE address. A vendor that sells both a subscription and metered credit now carries ordered doors, and connect binds the first that answers — the subsidised one first. The bound door is persisted and re-probed only on an explicit reconnect; a row written before doors existed keeps its metered address and does not drift onto a subscription."
  - "`internal/paymentrefusal` read a vendor's prose before its numeric code, and Z.ai's exhausted-window message contains the words \"Insufficient balance\". A five-hour window that will reset was therefore reported as a terminal \"the account cannot pay\". The code decides now; prose is the last resort for a vendor that sends none."
  - "An exhausted plan window was treated like a refusal and walked to the next door, which meant probing a metered account with somebody's money and binding it. It is proof the plan door works — the key is valid and the window resets — so it binds that door and stops."
  - "Nothing sends a paused turn to a metered door unless `when the plan is paused` is set to `use pay-as-you-go`. An absent OR unrecognised value reads as `wait`, and with the opt-in set the status line names the door while it is in use."
  - "The metered overflow is bought AT MOST ONCE FOR A WHOLE TURN, not once per request. It was a local of one dispatch, so a repaired body, a relaxation rung, a hedge arm or the turn's own model hop could each have bought another metered attempt after the first one was already authorised. One guard now travels with the turn's context and every re-entry derives from it."
  - "MiniMax's plan and metered balances share a host, a bearer, the models and the request, so nothing on the wire says which answered. It ships as ONE door with no plan claim; two would have labelled ordinary metered spending as a subscription."
  - "The Z.ai coding endpoint's `/models` route lists ten models and its documentation covers two. The plan door offers the four documented ids — `glm-5.3`, `glm-5.3-flash` and their `[1m]` forms — rather than eight choices the plan refuses with `1311`."
  - "aforge identifies itself as aforge on every direct request, including the tool-call loop, which previously bypassed that transport entirely. Zhipu lists the tools its plan covers and aforge is not among them; a listing request is drafted and unsent, and the page says exactly that."
  - "Not every request aforge made said who it was. The completion door stamped `User-Agent: aforge` and the receipt route, built beside it, sent Go's default `Go-http-client/1.1`. One method on the client decides identity for both now, and the attribution headers that name aforge to one router's ranking page travel only to that router rather than to every base."
  - "A direct stream that ended without its usage block was sent to that vendor's nonexistent `/generation` route with the plan bearer and OpenRouter's attribution, then written as `unbilled` even though the plan charged nothing per call. A direct service is never asked for a routed receipt now; the unmeasured call writes no ledger row, while OpenRouter still receives its receipt request with `User-Agent: aforge` and its own attribution."
  - "A service with regions asked for one as a TYPED answer: `your region` was a box with `International, China` beneath it, and a mismatch put `pick one of: …` in the feed before opening the box again. It is a CHOICE now — one named row per catalog region, the first under the cursor, walked with up and down and taken with enter — in `/connect` and on the settings service row alike. Nothing types a region and there is no refusal, because every answer comes from the service's own closed list."
  - "A service whose written name collided with an OpenRouter model author was REFUSED, and the person had to retype the region and key to connect under the suggested name. The suggested name is taken on the first attempt now, and the connect line names it."
  - "A turn was believed to need the default provider's key even while its conversation model was on a connected service, so enter opened the browser connection instead of sending. The connected service carries the turn now, and the surface says nothing about OpenRouter in that state."
  - "Every background seat was believed to resolve through the default service, so a direct-only conversation's crew found no key. A seat the service cannot fill now falls to the conversation's live model on that connected service; a present default-provider key keeps the configured crew unchanged."
---

Stacked on #800. Verified against a real Z.ai Team Plan key with both hosts behind recording
proxies: the plan host answers `GET /models` with 200, and a key with no credit answers 429 `1113`
on the metered host while the plan door serves normally.
