---
kind: changed
title: Every usage row names the seat the call ran under
pr: 1093
surface: [engine]
invalidates:
  - "The usage ledger's header said a row could not answer which class of model took the money, and `UsageLine.Role` was carried by only a handful of auxiliary calls. A row now carries `seat` — one word of a closed vocabulary, `reflex`, `low`, `worker`, `high`, `mastermind` or `talk` — and `role` is written by every call that goes through the role registry, so a spend page can say whether the money went on the seat that does the work or the seat that thinks."
  - "`session.UsageLine` gained a `Seat` field. It is `omitempty` like every additive field before it, so every row already in the file still decodes and reads back with an empty seat and an empty role, which is the truth about it."
  - "`Agent.recordUsageLine` took five arguments — the usage, the model, the role, the lane facts and the reconciled flag. It takes the one `bankedCall` those came off, because the seat needs a sixth fact from the same value and a door with six arguments is a door somebody passes in the wrong order."
---

The seat is derived and never typed at a call site: an agent's own turns bill
to the seat its kind answers for — `talk` for a conversation, `worker` for a
task node, `high` for the checker and a repair round — and every other row
falls back to the tier the role registry already has for that role
(`session.SeatOfRole` is `roles.TierOf` followed by one hand-written table of
five tiers to five words). A call that resolved its model outside the registry
— a media pin, a document reader, a tool ask — is nobody's seat and stays
wordless rather than guessed, and no free text ever reaches the field.

The seat names the chair, not the id that answered, so it stays true of a row
whose model was a fallback, a pin or a rescue. It is NOT the router's slot:
nothing in the program records which slot a call ran under, and a page that
labelled this column with those words would be inventing the join.
