---
kind: changed
title: the lanes page answers the floating alias, the headless run, and the third routing word
pr: 453
surface: [chat, docs]
invalidates:
  - "The chat manual's lanes page had no probe in internal/manual/chat_test.go, so nothing held its retrieval in place and questions about the machine behind a model reached whichever page repeated the word. It has one probe per heading now, seven of them, plus the phrasings each landed mechanism is met in."
  - "The lanes page said nothing about the shipped default being a floating alias. It now says that a leading `~` is the router's marker for a name that points at whichever build is current, that beliefs and the endpoints sheet are filed under the dated build the pointer names, and that `via cloudflare` names the machine rather than the model."
  - "The page described lane routing as a thing a conversation does. `aforge do`, `aforge run` and `aforge run subharness` fetch the same sheet and rank it with the same arithmetic since #354, and the page says so — a machine that only ever runs work headlessly is choosing between endpoints rather than between none."
  - "The page quoted the all-lanes-slow line as `all lanes slow · still waiting`. It carries a count-up: `all lanes slow · still waiting · 12s`, and the clock is how long you have waited rather than how long is left."
  - "The slow-lane offer read as though it had a decline key. `y` is the only key it takes; there is nothing to press to say no, and the question takes itself down when an answer starts arriving, when the request ends, or when it ages out. keys.md's `Keys when aforge asks you a question` now lists the offer beside the approval, proposal, connect and harness ones."
  - "The page described the `routing` row as on or off. It has three answers — `latency`, `price` and `off` — and `price` ranks on price alone on every call, your own turns included. `price` still measures endpoints and still chooses between them; only `off` stops both."
  - "No chat page pinned a model id against the code that owns it. internal/manual/truth_test.go now carries a `quotedFact` row for `config.DefaultModel`, so moving the shipped default fails the build on the lanes page still explaining the old name."
---

The lanes page was written for the surface and left the parts a person only meets
sideways: the name on their own status line resolving to a different one in the record,
a headless run they assumed was unrouted, a clock they had not been told the meaning of,
and a question they were looking for a way to refuse. Each is now on the page in the
words somebody asks it in, and each has a probe holding that phrasing in place.
