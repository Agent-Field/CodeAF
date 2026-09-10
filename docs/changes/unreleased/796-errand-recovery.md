---
kind: fixed
title: an errand is bounded as an errand, and a rung that is answering is not cut by a wall clock
pr: 796
surface: [chat, engine]
invalidates:
  - "An errand's ladder divided the caller's patience evenly between the rungs still to come (`perRung`, auxiliary.go) and armed each share as a hard `context.WithTimeout` on the call, so a rung that was streaming an answer was killed by the wall clock exactly like a rung that was silent. That even split — the 2026-08-28 rule — is GONE. The tier's patience bounds the whole ERRAND, once, and what ends a rung that is not working is the stream guard. What is kept of the arithmetic is a RESERVE and not a share: each rung still below the one being asked is owed a fifth of what remains (`errandReserve`), so the first of two rungs gets four fifths rather than half. It exists for the one shape the guard cannot see — a gateway that answers a whole completion in one piece and arms no stall watch — and it is far enough above the guard's own wall never to cut a stream the guard was happy with."
  - "It was believed that an errand had to arrange its own bound because a call made with `provider.WithoutStream` was not guarded. It is guarded: `Client.CompleteWithMessages` takes the streamed path whether or not an observer is attached, so streamguard.go's first-delta bound, its mid-stream gap and its pace-re-arming wall (#786) already apply to every errand. No `internal/provider` change was needed and none was made."
  - "The errand ladder read the boundary's verdict and dropped it — 'the verdict is not acted on'. It is acted on now, through #794's own door: `readErrandFailure` asks `readLadderFailure` with `transportLadder{fallback: a rung remains}`, which is the fact only the ladder has, and `taxonomy.ActionHop` is what moves it to the next rung. `ActionGiveUp` — the same budget spent with nowhere to go — ends the errand. `ActionRetry` is read against the ERRAND's cap rather than the model's: `errandTriesPerRung` is ONE, because the rung below is a better move than another try and is reached at once with no backoff, so a retry with a rung below is this ladder's hop. Honouring it as a same-rung retry needs the transport budget to be the caller's; measured on 2026-09-10 it fires on the first failure, and the 2s-4s-8s pauses timed out the naming job, two turns a person waits through, and the wedged-rung law. And one failure the taxonomy cannot decide alone: everything unplaceable lands in `Work`, which is right for a 4xx that named NO upstream (our own bytes, refused everywhere) and wrong for `that model is down`, so the doomed request is recognised by the EVIDENCE narrowly and every other work verdict still falls through one rung."
  - "The division review carried `divideReviewPatience = 3 * time.Minute`. That constant no longer exists and the review sets no deadline of its own: it is bounded by its role's tier, ten minutes, like every other errand. Three minutes was BELOW the stream wall's own five-minute floor for a lane with no history, which made it a guillotine on streams the guard was still happy with rather than a bound on a wedged one."
  - "A division admitted on the fail-open road — nobody could be reached, or the answer was unreadable — wrote `decision: admitted` and nothing else, which is byte for byte the row a REVIEWED admission writes. `journalDivision.Error` now carries the reason there was no reading. The word stays `admitted`, because that is what happened to the parts."
  - "`legendSegments` cut a sketch's legend only at newlines, semicolons and commas, so a legend that wrote `**A:** … **B:** … **C:** …` on one line was one clause: A's words swallowed the whole line and every other part fell to `sketchName`'s `part N`. A label opening a clause is now a boundary wherever it stands — `**B:**`, `B:`, `B —`, `B - `, `(B)` — and a separator after a single letter is required, so `(wired at line 519…)` and `folder-pick` are not labels."
  - "The sizing phase carried no text: `TestTheReadingThatSizesTheWorkIsDrawnAndThenClears` asserted the move's `Text` was empty. While the reading runs the row now says `asking <model> · 1 of 2`, then `<model> did not answer in time · asking <next model>` (or `· asking again` on the same rung), and on the fail-open road `nobody answered · going with the parts as drawn`. It rides `EventTaskPhase`'s existing `Text` into `internal/tui3`'s `phaseFinding`; no surface change was needed."
  - "The manual said the reading is 'given at most three minutes'. It now says ten minutes for the whole reading — the tier's own patience — and explains that a model that goes quiet is cut in tens of seconds by the guard, so the ten is what a reading being written may take. A new section, *What the row under sizing the work says*, holds the three lines."
---

Measured on 2026-09-10, task node 1 of conversation `57d51779f63ac603`. The
division review asked `z-ai/glm-5.3`, which wrote its first token in one second
and was still writing at ninety when its share of the three minutes ran out; the
fall-through rung `deepseek/deepseek-v4-flash-0731` first-tokened at 4.8s and was
cut the same way. The parts went out unread, called `read seam.start in` and
`part 2`, on a journal line nothing could tell apart from a reviewed admission —
after three minutes and twenty seconds in which the person watching saw the words
`sizing the work` and nothing under them.

Every one of those is one machine's fault and they are fixed as one: the guard
bounds a rung, the tier's patience bounds the errand, the verdict decides what
the ladder does next, the record says what happened, the legend names the parts
it named, and the row says which model is being asked.
