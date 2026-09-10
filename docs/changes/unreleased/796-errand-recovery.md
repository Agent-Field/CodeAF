---
kind: fixed
title: an errand is bounded as an errand, and a rung that is answering is not cut by a wall clock
pr: 796
surface: [chat, engine]
invalidates:
  - "An errand's ladder divided the caller's patience evenly between the rungs still to come (`perRung`, auxiliary.go) and armed each share as a hard `context.WithTimeout` on the call, so a rung that was streaming an answer was killed by the wall clock exactly like a rung that was silent. That even split — the 2026-08-28 rule — is GONE. The tier's patience now bounds the whole ERRAND, once; every rung and every retry runs on what is left of it, and what bounds one rung is the stream guard."
  - "It was believed that an errand had to arrange its own bound because a call made with `provider.WithoutStream` was not guarded. It is guarded: `Client.CompleteWithMessages` takes the streamed path whether or not an observer is attached, so streamguard.go's first-delta bound, its mid-stream gap and its pace-re-arming wall (#786) already apply to every errand. No `internal/provider` change was needed and none was made."
  - "The errand ladder read the boundary's verdict and dropped it — 'the verdict is not acted on', because the rung below WAS the only retry an errand had. It is acted on now: `taxonomy.ActionRetry` asks the same rung again after the verdict's backoff, inside the errand's remaining patience, and every other action — `ActionGiveUp` included — means the next rung. `taxonomy.Limits.TransportAttempts` bounds the tries, counted across the whole errand."
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
