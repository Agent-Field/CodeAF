---
kind: fixed
title: A faulted gate's row and handover name the fault that happened, not one fixed sentence
pr: 647
surface: [engine, docs]
invalidates:
  - "A delivery whose gate produced no verdict wrote ONE fixed sentence whatever had gone wrong. `revision.GateFaultWords` and `revision.GateFaultHandover` took a `fault string` and never read it, so a review that came back as prose, a review cut off at the output ceiling and a review that broke the shape it was asked for all left the same `delivery_gate` row, the same last line on the door, and the same note riding the delivery: `the review could not be read, so this delivery was never checked`. The reason was in hand the whole time — `faulted` composes it onto `Judgment.Fault` and `cmd/aforge/chat.go` passed that straight into both functions — and both discarded it. NOW both sentences carry it: the row reads `the review could not be read, so this delivery was never checked — <reason>` and the handover reads `I'm handing this over unchecked: the review of it could not be read (<reason>), so nothing has confirmed this is what you asked for.` Two runs that failed differently no longer end with the same line. An empty or blank fault still returns the previous sentences byte for byte, so no caller learns a special case and `cmd/aforge/chat.go` is unchanged."
  - "This is the residual #593 left. That change carried the UNJUDGED reason all the way out — `GateUnreached`, the ` · ` clauses, `unjudgedWords` — while the FAULTED path, which is the other arm of the same `if` (`shaped.Unreadable(err)` → `faulted`, else → `unjudged`), still dropped it. Both arms now say why. The behaviour of neither is otherwise changed: a gate that could not be REACHED is still the fail-open pass with its note untouched, and a gate that ANSWERED with nothing readable still ends the run partial rather than ok."
  - "`cmd/aforge/shaped_test.go`'s `TestADeliveryWhoseGateFaultedIsNotWhole` wrote the old constant into the store by hand, so it passed whether or not `GateFaultWords` read its argument — which is how the sentence stayed fixed for as long as it did. It now builds the row the way `cmd/aforge/chat.go` builds it, from a real unreadable answer's fault, and reads the door's own line back through `gateStanding` and `partialWords`."
  - "`internal/manual/chat/adaptive-runs.md`'s \"When the review itself could not be read\" section quoted both sentences without their reason, which is no longer what the binary prints. It quotes them with it, and states the rule the words alone do not: the tail names WHY the review could not be read — the model's own reply when the answer was prose, or that it was cut off — so two unchecked runs that failed for different reasons end with different lines. Two probes hold the section, which nothing held before."
---

The fix is where the sentence is spelled, not at the caller: both functions
already receive the fault and neither read it, so composing anywhere else would
have made a second place these words live. The lead clause `faulted` puts on the
note is folded out before the rest is appended — "the review could not be read"
and "the gate answered with nothing this could read" are one fact in two
vocabularies, and `unreachedStreamWords` already makes exactly that fold, for
exactly that reason, on the unjudged note. Nothing new bounds what is appended:
it is bounded where it is composed, by `internal/shaped`'s `replyDetailBytes` and
`noteBytes`, and a second limit here would be a second idea of how long the
sentence may be.

What neither sentence does is allege anything about the work. Nobody read it;
that is the whole point of the ending. It says what did not happen, and now also
why.
