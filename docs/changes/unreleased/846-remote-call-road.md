---
kind: fixed
title: a keystroke is not refused at ten seconds behind an unrelated getter
pr: 846
surface: [chat, remote]
invalidates:
  - "The engine's read loop dispatched every call serially. A getter held the reader and a keystroke behind it waited out `callDeadline` (10s), then the surface was told `the connection to the engine is gone` while the engine went on to apply the answer. Getters and small acts now run off the reader; stream-opening and shape-changing calls stay on it, in order. `classify` in `internal/remote/callclass.go` is the one predicate."
  - "A timeout answered with `Client.gone` — `the connection to <machine> is gone` — about a link that was carrying that turn's events at that moment. It never buried anything (only the reader calls `Client.bury`, on a pipe that actually failed); the sentence was the whole of the damage, and the surface believed it. A timeout on a live connection now says `<machine> did not answer in time`, and `Client.gone` is kept for a pipe that broke."
  - "A LONGER DEADLINE FOR A PERSON'S ACT IS THE FIX THAT DOES NOT WORK, and it was tried on this branch before it was taken out — anyone reaching for it again should read `callclass.go` first. `answerQuestion` asks its door straight from the update loop, so a 30s act window is 30s a terminal can sit without drawing: eighteen copies of the questions e2e six at a time gave two 53-second copies against 22 for every other, both losing the receipt, where twelve copies of the parent gave none over 33. `callDeadline` stays one window for every call; the road is what stops an act needing more."
  - "`app.answerQuestion` swallowed `ResolveQuestion`'s error (`if err != nil { return nil }`). A refused act is now said on screen in the door's own words, through `app.note`, the same door rewind, autonomy, connect and a permission already use."
  - "The questions manual said a window over `--host` cannot yet answer. It can: the key crosses to the engine that owns the work, and a refusal comes back in that engine's own words."
---

Measured on 2026-09-10: six copies of `TestQuestionsE2E/TheOrdinaryRoadCarriesAQuestionAndItsAnswer`
at once, and one of them spent exactly ten seconds — `internal/remote`'s
`callDeadline` — before the surface logged `REFUSED err=the connection to the
engine is gone`, while the engine had applied the answer and the model had
already said `They picked "delete it"`. The five that passed round-tripped in
one millisecond. The receipt half of that (a key drawn as `another window`)
landed on the diet branch; this is the road.
