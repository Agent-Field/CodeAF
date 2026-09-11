---
kind: fixed
title: a keystroke is not refused at ten seconds behind an unrelated getter
pr: 846
surface: [chat, remote]
invalidates:
  - "The engine's read loop dispatched every call serially. A getter held the reader and a keystroke behind it waited out `callDeadline` (10s), then the surface was told `the connection to the engine is gone` while the engine went on to apply the answer. Getters and small acts now run off the reader; stream-opening and shape-changing calls stay on it, in order. `classify` in `internal/remote/callclass.go` is the one predicate."
  - "`callDeadline` was one 10s window for every call, and a timeout answered with `Client.gone` — `the connection to <machine> is gone` — about a link that was carrying that turn's events at that moment. It never buried anything (only the reader calls `Client.bury`, on a pipe that actually failed); the sentence was the whole of the damage, and the surface believed it. A timeout on a live connection now says `<machine> did not answer in time`. A person's act waits `actDeadline` (3× the getter window, and no longer, because the question block asks its door from the update loop). `Client.gone` is kept for a pipe that broke."
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
