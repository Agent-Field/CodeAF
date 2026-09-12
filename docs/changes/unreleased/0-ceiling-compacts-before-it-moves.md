---
kind: changed
title: the running-long point compacts and carries on, and only moves an answer that cannot continue
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "The third checkpoint mark was an unconditional hand-over: past it a reply ended and its remaining work moved to a task whatever the sketch said, and the only thing that could stop it was the model declaring itself finished. It is not. The ceiling now compacts the transcript in place and lets the reply carry on whenever what comes out of the fold sits inside the room the runaway net measures (Agent.contextRoomFor — one arithmetic, now read by three rungs); it moves the work only when the fold cannot get it under that line, or when one of the other roads out fires. A reply that ran forty rounds with a free window is no longer taken away from anybody."
  - "The checkpoint meter was a one-way ladder of three rungs and a turn could never be read a fourth time. It can. A compaction puts the ladder back at its foot, standing on the round the room was bought on, so a reply that carries on past the ceiling pays checkpointPrice rounds and then the same doubling again, and meets the ceiling again. Nothing is unbounded: the fold protects the running turn's own working memory, so a reply that keeps working keeps growing what cannot be folded."
  - "A journaled ceiling row was `moved` or one of the `dropped:*` words. There is a third now, `compacted`, and it is the ordinary one — it says the reply bought itself room and carried on, which is a different fact from a hand-over that was considered and declined."
  - "`checkpointCeiling` was named in sidecar_law_test.go's `endingDoors` — the list of roads allowed to wait on a reading in front of the work, because they end the turn. It is not one any more and the by-name exemption is deleted: a turn can now carry on past the ceiling, so a reading awaited on the way to it would be a reading awaited in front of work that is about to continue, which is exactly what that law exists to catch. The ceiling makes no model call of its own."
  - "A fold left one marker line naming a journal path, and everything the model needed to carry on was supposed to come from the post-turn state card. It does not. The fold now writes an account beside the marker, with no model call, out of the messages it is taking away: the places the turn opened and the places it wrote, each with the opening line of the edit that landed there. It shares one transcript walk and one eviction rule with the reader's own digest (foldAccount and checkpointDigest over checkpointLedger and accountSections), and it deliberately carries neither the ledger, the results nor the last thing said — those are the bytes the fold is giving up, and a marker that carried them would hand the transcript back."
  - "The mark reader's digest was the only account anybody got, and it carried no list of the files the turn had read. It does: `PLACES ALREADY OPENED` is deduped out of the same ledger walk, derived from any call carrying a `path` it did not write rather than from a list of reading tools, and it takes its room from the ledger's allowance because it is the ledger deduplicated. The handoff writer is no longer shown 5,000 tokens either — its budget is a share of the window it is writing into (Agent.handoffDigestBytes)."
  - "The digest's written section was a bare list of paths. Each line now carries the opening line of what went into the place — but only once the write came back, because an unanswered or failed write is not evidence of its own content, which is the law the result bodies already kept."
  - "A task's working context carried at most six tool handles (`admissionHandlesKept`). That constant is deleted. The byte budget that was already there is the whole bound, so a turn of sixty-two calls no longer hands a worker six of them and watches it re-read everything."
  - "The handoff contract said nothing in that file ever writes an expectation, because only a divider knows which of its own sentences are load-bearing. That still holds for prose and for every groomed door. A moved reply has no such author, so the ceiling road now writes one expectation per path the turn actually wrote — read off the turn's own tool calls, never mined out of a brief — and the worker preflights them before it spends anything."
  - "A task started by the ceiling ran on the crew's worker seat like any other task. It runs on the conversation's own model: the worker ladder prices work that starts cold from a written brief, and a moved reply is this answer continuing elsewhere. A `task model` row you set still wins."
  - "The crew-only guard in front of the mark reader and the handoff writer checked that a `models.tiers.mastermind` row EXISTED. It now checks that the model on it is fit: a model the shipped crews seat at the working class or below is not a rung, so those two calls are absent rather than answered by a flash model. The rule is derived from internal/config's crew table and never from a list of names, and a model no preset ships is not refused."
---

Measured on `dev` 2026-09-11: a one-paragraph request — "add a hover effect on
the tasks-table column labels" — ran 17m46s, 62 tool calls and 40 rounds, and
then spilled into a fresh worker task that re-read everything the conversation
had already found out.

Five mechanisms had to line up for that, and the first is the one the rest
follow from. The ceiling counted ROUNDS and then spent a CONTEXT, and the two
had never been compared: `checkpointRound` returned true one line before
`maybeCompact` was even reached, so a reply with almost its whole window free
was moved out for having been busy. "Move it to a task" was wearing a context
valve's clothes without being one.

The rule is now one sentence — A TURN LEAVES ONLY WHEN IT CANNOT CONTINUE — and
every road out of the ceiling is a way that can be true: the sketch drew parts
that do not wait on each other, the fold cannot bring the context back under the
line the runaway net already measures, or the detector caught the reply going in
circles. Everything else carries on, and the meter charges it the full ladder
again before asking.

It lands on top of #923's runaway net, and shares its one fact rather than
adding a second. The net fires the ceiling EARLY when a turn cannot hold another
step; this rung stands in front of the MOVE and asks the same question of the
same arithmetic on the other side of a fold. The net's `outOfRoom` latch is
deliberately not cleared by a compaction, which is what keeps a turn that bought
room by folding off the promotion road: the argument for inheriting a transcript
is that the worker holds the RESULTS verbatim, and a folded transcript is a
summary of them.
