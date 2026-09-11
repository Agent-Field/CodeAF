---
kind: changed
title: every wait is spoken within a second, and a ceiling that fires always acts
pr: 853
surface: [chat, engine, docs]
invalidates:
  - "Ten seconds (`lane.VisiblePatience`) was the first moment a person heard ANYTHING about a slow call, because it was both when this build acts and when it speaks. Those are now two clocks: `lane.SpokenWithin` (1 s) is when the line stops being blank and `VisiblePatience` (10 s) is when a move is made. The ten seconds did not change and the measurement says it should not — a watched turn's first token is 8.4 s at the ninetieth and 13.1 s at the ninety-fifth, so 10 s is this build's own ninety-second percentile; five seconds would touch 28.9 % of calls against 13.9 % AND rescue fewer per extra send (17.6 per hundred against 22.1)."
  - "A call that queued for a process-wide slot (`sharedLimiter.acquire`) said nothing at all — the request was not on the wire, so none of the stream's own phases had started, and a 429 storm that halved the ceiling left every other call in the process on a blank line for the length of somebody else's burst. It now posts `connecting` within a second and repeats on the phase beat, with NO countdown: nothing in this process knows when a slot comes free."
  - "A connection wait posted a zero deadline, correctly drawn as nothing, so a person on dead Wi-Fi was told the wait was real and never how long this build would give it. It now draws a countdown to `connectionRecoveryWindow` past the moment the wait began, which is the moment it really ends with `ConnectionUnavailableError`."
  - "There was no law against a silent wait; the waiting design's `a wait that is real is reported` had four exceptions and nothing noticed. `TestEveryWaitInTheRequestPathIsSpoken` (go/ast) now fails any `select` on a timer in `internal/provider` with no phase in the same function. Three names are allowlisted, each with the sentence that says why nobody is waiting on them — including `abandonGrace`, which the recovery design lists as a silent wait and which is entered only after the caller's context is already done."
  - "`deadline_ms` was read as `when this call was going to be ended` and is `streamWatch.armed`, the moment the hazard was going to start THINKING about a second machine: 2,720 of 11,841 finished attempts read as running past twice their own bound, the field is `10000` on 7,937 rows, and the worst pairs `deadline_ms 10000` with `ms 937777`. The bound that REALLY ended an attempt is now recorded separately (`waitFacts.applied` / `appliedWord`, from the cut's own figure and reason word) and the two travel as two fields."
  - "`census-20260910.md` §8 finding 1 says the 869 `decode stream: context deadline exceeded` rows with `ms` at exactly 90,000 and 60,000 — 780 of them with a first token already taken — are the stream wall guillotining live streams. They are not the wall and they are not in `internal/provider` at all: `clientFor` already gives a streamed request `Timeout: 0`, `attemptContext` gives it a cancel and never a timeout, and every bound in `streamguard.go` announces itself as a `*StreamCut` with its own sentence rather than as a context error (`nothing came back from the model in …`, `the model stopped mid-reply and went quiet for …`, `the reply ran past … without finishing and was cut`). Those deadlines are the CALLER's, at two and three times a role's ceiling. Three tests now pin the removal so it cannot be undone by restoring a sensible timeout."
  - "A losing arm of a hedge race wrote `context canceled` and every reading of the log counted it as a failure — 1,204 of 3,906 bad rows in ten days, the single largest cause family in the census, not one of which is a thing that went wrong. Its row now says `cancelled: lost the race` (`waitFacts.exhaust`), unless the race had something more specific to say. The mechanism is inert until `hedge.go`'s cancel site calls `watch.lostRace()` — one line, owed by the lane that owns that file."
  - "A ceiling that fired and whose purse refused the hedge did NOTHING, so `docs/design/waiting/DESIGN.md` §A clause 1 — the ceiling is hard `regardless of belief` — silently meant `when we happen to be able to afford it`. 2,186 attempts are in that state and 648 of them ran past six times the silence that had just been refused (p99 272 s, worst case 938 s); four quick tasks on 2026-09-10 sat on one machine for 260, 370, 375 and 428 seconds before its first token. A hedge is the PAID way to act and a cut is the FREE way, and the ceiling now picks one of them. It cuts only on a dead wire (`no heartbeat` — no token and no router comment), because the measurement says the other readings end cleanly anyway: `drift` 92 %, `ceiling` 75 %, `no heartbeat` 31 %."
  - "`bufferedQuietBound` was multiplied by the role's patience, so a task node tolerated 450 s of a keepalive trickler and a standing pass 900 s — which is how those four streams reached six and seven minutes while staying inside their bounds. It is now flat at 150 s for every role. The first-token and mid-stream bounds are PATIENCE and still scale; the cap is a measured ceiling on what an ENDPOINT may do while keeping its line warm (the largest honest server-side lump took 86 s end to end), and a machine does not earn longer because nobody is watching."
  - "A sighting was dropped whenever the act had been charged to the path (`streamWatch.fault`), so the worst first tokens this build has ever seen taught the ledger nothing: four streams named their machine, took 260–428 s to write, and every one was discarded — which is why that machine's sheet row went on claiming an eight-second first token. The claim is made from what had arrived at the moment of the act, and a stream that later wrote from a named machine has disproved it; only a stream that never wrote at all is still dropped. `LagTTFT` (2 s) already puts such a sighting on the wrong side of the lag line."
  - "The wire was forgiven four times doubling from two seconds (`taxonomy.DefaultTransportAttempts` 4, `DefaultTransportBackoff` 2 s), which is eight seconds of a turn spent asleep before anything moved, on a fault the connectivity gate already owns — and the `no such host` chains that resulted ran 1,113 s, 1,005 s and 787 s re-sending into a dead resolver. It is now three attempts doubling from one second."
---

The owner asked how long is long — *"is it 10 or 5, or how long, 2 min or shorter
— look at logs or standard practice to make the user feel fast."* Answered from
16,427 finished attempts in `~/.aforge/logs/calls.jsonl` (10,028 carrying
`ttft_ms`) and `usage.jsonl`'s independent `tps`, against Nielsen's 0.1/1/10
second limits and Dean & Barroso's rule for when a hedged request pays for
itself. The full study is the wave report; the short version is that **ten
seconds was already the right number and was being used for the wrong thing**,
and that every other bound in the guard is confirmed by measurement rather than
moved — including `streamWallCeiling` at 20 m, which no clean stream in seven
days came within eight minutes of.

New tests: `TestAStarvedSlotQueueSaysItIsWaiting`,
`TestEveryWaitInTheRequestPathIsSpoken`,
`TestNoFixedDurationEverBoundsAProducingStream`,
`TestADripAtItsLanesPaceOutlivesEveryDurationBound`,
`TestASilentStreamIsCutAtItsLanesOwnGapAndNotTheFlatOne`,
`TestTheBufferedCapIsNotTheRolesToStretch`,
`TestTheRowSaysWhichBoundReallyEndedTheAttempt`,
`TestALosingArmIsExhaustAndNotAFailure`,
`TestARaceWithSomethingOfItsOwnToSayKeepsSayingIt`,
`TestACeilingThePurseRefusedStillActsOnADeadPath`,
`TestAPurseRefusalOnALiveWireIsLeftAlone`,
`TestALateFirstTokenStillTeachesTheLedgerAboutItsMachine`.
