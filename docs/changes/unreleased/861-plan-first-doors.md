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
  - "MiniMax's plan and metered balances share a host, a bearer, the models and the request, so nothing on the wire says which answered. It ships as ONE door with no plan claim; two would have labelled ordinary metered spending as a subscription."
  - "The Z.ai coding endpoint's `/models` route lists ten models and its documentation covers two. The plan door offers the four documented ids — `glm-5.3`, `glm-5.3-flash` and their `[1m]` forms — rather than eight choices the plan refuses with `1311`."
  - "aforge identifies itself as aforge on every direct request, including the tool-call loop, which previously bypassed that transport entirely. Zhipu lists the tools its plan covers and aforge is not among them; a listing request is drafted and unsent, and the page says exactly that."
---

Stacked on #800. Verified against a real Z.ai Team Plan key with both hosts behind recording
proxies: the plan host answers `GET /models` with 200, and a key with no credit answers 429 `1113`
on the metered host while the plan door serves normally.
