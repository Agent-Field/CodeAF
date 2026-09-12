---
kind: changed
title: The turn decides whether to become a task, and a task it starts inherits what it already read
pr: 923
surface: [chat, engine]
invalidates:
  - "A REPLY WAS MOVED TO A TASK AT ROUND TEN. The checkpoint's first two marks
    each woke a separate model to read the transcript, and a `split` from that
    reader ENDED the turn and handed the work to a worker. Neither mark reads
    anybody now and neither moves anything: they put one note into the running
    turn at its next step — `[taking stock] 10 rounds so far, 9 files opened,
    121 KB of results in front of you.` plus the three roads on — and the model that is holding
    the context decides. `checkpointSplitNote`, the `split` decision and the
    seam mark are gone; `checkpointNotes` is the count of rungs that tell, and
    `checkpointMarks` is still where the last one stands."
  - "THE CEILING WAS A COUNT OF ROUNDS. It is a runaway net, and `turnHasRunAway`
    reads ONE fact: the turn's context has passed the room it can still work in
    (`compactThresholdOf` of its own window, the build's own line for how far a
    conversation may grow). The other roads to the same ceiling were already
    written and are not repeated in it — the loop watch past its nudge ceiling
    (`handOverLoopingTurn`), an unattended session's wall (`turnwall.go`, asked
    ahead of this ladder) and a believed completion claim disproved by
    `checkpointPrice` more rounds of real work (`checkpointMeter.askAgainAt`).
    No new number anywhere. A turn that reads ten distinct files in forty-eight
    seconds is told twice and moved never, which is the measured failure this
    closes."
  - "A TASK STARTED OUT OF A RUNNING TURN OPENED ON A BRIEF ABOUT THAT TURN.
    It was handed a written brief plus a `CALLS THAT HAVE ALREADY RUN` list of
    pointers at the caller's own results, and it re-read them: measured at 551k
    input tokens, 24% cached, 393 seconds, no item ticked. A promoted worker is
    now a read-only FORK of the caller's transcript — the caller's messages and
    tool results verbatim, with the brief as its opening message — admitted
    through the same door as the tool. The stub-pointer section is not written
    for an inherited worker at all (`admissionContext` still serves every other
    kind), and `quickFromDrawing`'s bare-spec path is gone."
  - "`quick_task` had no way to be handed the conversation that started it. It
    takes `inherit` (a boolean, off by default), described in the four words the
    prefix budget can afford: `Opens on this conversation`. The refusal is a
    result the model reads, it names the window it did not fit, and it says to
    hand the work out with a brief instead. There is one constructor, not two —
    `newQuickSpec` gained a field."
  - "`quick_task`'s DESCRIPTION SAID THE ROAD LAW THAT THE PAGE ALSO SAYS. `If you
    will read the result and carry on, it is quick. If it must be checked and
    merged on its own, or survive the window closing, it is a task.` is
    `beltfacts.go`'s `What must be checked and landed on its own, or must survive
    you, is a task; what you will read and carry on with is quick`, word for word
    in two places and paid for on every request of every turn. The description's
    copy is deleted: the page renders wherever this verb does
    (`Config.mayQuickTask` puts both verbs on a belt or neither), and what is left
    on the tool is what only the tool says — the two roads in one sentence, the
    step that is no hand-off at all, and the grain."
  - "AND WHAT `inherit` DOES IS A VERB LAW, NOT A PAGE LAW. It rode
    `beltfacts.go`'s handoff paragraph for a wave — one sentence naming the
    field — and a page sentence is paid for by every request of every turn
    whether or not the belt carries the tool it is about. It is registered at
    `quick_task` now (`lawregistry_test.go`, class `verb`), which is the class
    the diet's own taxonomy already had for a fact about one field of one tool."
  - "THE `inherit` BOUND IS READ AGAINST THE WORKER'S WINDOW, NOT THE
    CONVERSATION'S. The two are routinely different — a `task.model` pin or the
    crew's worker seat hands tasks to a cheaper model than the person is talking
    to — so the model is settled by the resolver BEFORE the spec is built, and
    `newQuickSpec` takes it and writes it onto the node. The quick door used to
    resolve the model after building the spec and assign it afterwards; that
    order is reversed and the assignment happens once."
  - "A NET THAT FIRES ONCE HAS TO SAY SO. The net measures a condition rather
    than a boundary and a full context stays full, and three of
    `handOverRunningTurn`'s endings LEAVE THE TURN RUNNING — no brief came back,
    the conversation is holding work of its own, the reader says nothing is left.
    Without a latch each of those was followed by a fresh mastermind call and an
    unconditional cut of the next step, every round until the turn ended.
    `checkpointMeter.netFiredAt` records the firing and `netRested` holds the net
    off for `checkpointPrice` rounds of real work — one predicate for all four
    endings, in the unit the meter already used for a disproved claim."
  - "THE BRIEF ROAD SURVIVES ONLY WHERE INHERITANCE CANNOT GO — the rung that
    fired BECAUSE the context is too big to carry. There the brief now carries
    the compiled results themselves, under `WHAT THIS WORK ALREADY FOUND OUT`,
    rather than pointers at calls the worker would have to run again."
  - "`forkSeed` was deleted with #811's 1,368 lines when the fork left the belt.
    It is back in `fork.go`, restored rather than rewritten, and it is what
    `inherit` is built on: there is one read-only fork of a transcript in this
    package and promotion uses it."
  - "THE LINE A MARK USED TO WRITE IS NOT PRINTED ANY MORE. `this has parts ·
    handing it to a task that can take them side by side` was the mark's own
    notice; a mark writes nothing at all now, so the two pages quoting it as a
    live line were describing an event that cannot happen. The two lines a move
    can still write are the ceiling's `this is running long · moving it to a task
    that is watched and can split` and the carry-on's `this is running long ·
    carrying on here, in this folder, with everything already read` — the second
    of which replaces `this has parts · a quick task is taking them here`,
    because what the line has to say now is what the worker took with it."
  - "The manual said an answer that runs long is read and moved at round ten,
    that `markreader` is asked three times, and that the reading decides whether
    the step stops. `chat/tasks.md` now has `An answer that runs long is told, and
    decides for itself` and `When a reply is taken out of your hands`, plus a
    section on keeping what was already read; `how-tasks-run.md` says which tasks
    inherit a transcript and which get a brief; `models-and-cost.md` says
    `markreader` is asked at most once during an answer, on a reply that can no
    longer work where it is; and `screen.md`'s `taking stock` row says the same,
    beside a new section separating that phase word from the `[taking stock]`
    line the model is handed."
---

The decision was being made by the wrong party, on the wrong signal, and the move
threw away the thing that was worth keeping. A summary reader on a small account
cannot know whether a turn is nearly done; a count of rounds says nothing about
whether the work is going anywhere; and a worker that opens on a brief about your
reading starts by doing your reading again.

So the count buys a sentence instead of a decision. The model holding the context
is the only party that knows whether it is two files from an answer, and the note
gives it the three facts it cannot see about itself and the three roads on, with
the cost stated once where the choice is. Nothing about it goes into the prefix:
it arrives through the one ambient door a loop nudge already uses, at the next
step boundary, and it never starts a turn.

What is left of the old ceiling is a net rather than a schedule. It asks whether
this turn has run away — no room left to work in, a loop already nudged, a wall
already spent — and every bound is derived from something the build had already
measured, so there is no new number to tune and no case where a productive turn
meets one.

The prefix comes DOWN. Measured against `dev` at 84016ad09, on a machine with ripgrep:
the lean prefix goes 31,341 → 31,267 bytes and the fixed 39,073 → 39,000, against
budgets of 31,500 and 48,000. The `inherit` property costs 72 bytes and the road law
deleted from `quick_task`'s description gives back 145. On a machine WITHOUT ripgrep —
which is what CI is, and `grep`'s description is longer there — the same pair reads
31,448 → 31,375 and 39,180 → 39,107: the same 73 bytes back, in the environment the
gate measures. The note itself is in neither figure: it rides the event, at the
boundary where it matters, and costs a turn that never reaches a rung nothing at all.
