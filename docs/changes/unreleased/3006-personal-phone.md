---
kind: added
title: A phone paired once is served by all your computers and starts chats on the one you pick
pr: 3006
surface: [remote, app, chat, docs]
invalidates:
  - "The manual said there is no codeaf phone app and no phone client, and that a phone could only open a pairing page that helps approve a computer. A phone is now paired as a phone, once, at any one of your computers, and every computer holding the same identity serves it: it starts chats on the computer you pick, reads your chats and answers questions."
  - "The manual said a home that has never paired sends no sync traffic. That still holds for a home with no phone, but a computer you have approved a phone on makes the requests serving that phone needs: the directory list, the serving watch, its own device record and the phone's queue and slots. Approving a phone leaves a never-shared computer solo, so it does not start sharing chats between computers; approving another computer still ends solo, and removing the last phone makes the computer fully quiet again."
  - "A phone's pairing grant was only said to be private to the phone. It is sealed under a key derived from the pairing link's key, so the relay that stores it can neither read nor forge one, and the phone refuses a grant that names another request or phone, or whose certificate or card does not verify."
  - "A repeated claim of a phone's queued call was accepted from the same device with no proof it was the first claimer. A claim may now carry a random token, and once an entry was claimed with one, a repeat claim without it is refused with 409."
  - "A client could hold 64 slots. It can now hold 128."
  - "A call that failed after it had started could be taken for one that never ran. It now ends as refused with the reason `failed`, because it may have taken effect, and a call that has started is never dispatched again."
  - "Creating a chat from a phone made one chat for every delivery. A create is now keyed by the phone and its `client_id`, so the same request twice makes one chat, and a create that made no folder does not leave its name pointing at a chat that never existed."
  - "The live directory conformance suite expected a blind forced take of a chat's lease to succeed. Both live relays keep the directory in a consensus store, where a conditional force wins exactly once, so the suite now runs the consensus rigs and that expectation is gone."
---

A phone reads each computer's chats from sealed slots, queues calls sealed to one computer, and reads back one outcome per call. `codeaf phone serve` serves a phone on a computer with no window open, and `/devices` and `codeaf devices` list and remove phones. The manual's *Your phone* page says what a person can ask of it.
