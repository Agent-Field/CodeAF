---
kind: fixed
title: a standing end that stands before the item's own first firing is refused, not stood up
pr: 203
surface: [chat, engine, docs]
invalidates:
  - "`stand` measured `rails.expires` against the clock alone, so an end still in the future but earlier than the item's own `when.at` was accepted and written onto the item — and `internal/standing/tick.go` asks whether an item has run out of time as rail one, before anything can be due, so the item retired with `runs: 0` and `retiredWhy: expired` without ever firing. Such a proposal is refused now, before any card is drawn."
  - "`standingRails` took the waking KIND (`standing.WhenKind`) and the clock. It takes the whole resolved `standing.When`, because an end is a claim about the item's own life and cannot be judged without the moment that life starts at."
  - "The `expires` schema line said only \"A stamp already gone is refused, as when.at is.\" It also states that an end at or before the item's own first firing is refused, and that a one-off needs no end at all."
  - "Every refusal this tool makes spelled its moments to the MINUTE (`standingClock`). The end-before-firing refusal spells both to the second (`standingClockExact`), because the defect it catches lives inside one minute: `in 1 minute — 23:11` for the words, `23:11` for the end, against a moment resolved to `23:11:11`."
---

The half of the law that was missing. The file already refused an end already
gone, for the stated reason that it would retire the item before it ever fired;
an end merely earlier than the reminder it was attached to did exactly the same
damage and was taken. A person asked for a one-off reminder, was shown a card,
said yes, and nothing ever happened.

`internal/manual/chat/keeping-an-eye.md` gains a section on ends — what an
`expires` is for, the refusal quoted word for word, why rail one makes an early
end fatal by every road, and that a one-off never needs one.
