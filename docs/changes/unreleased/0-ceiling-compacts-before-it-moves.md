---
kind: changed
title: a turn is folded before its weight is priced, and the handover it then writes carries the work
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "A running turn's weight was priced BEFORE this build folded it: `checkpointRound` stood one statement above `foldTurnOutputs` and `maybeCompact` in the step boundary. The order is now the other way round. The runaway net reads what a fold could not get rid of, so an answer whose last round pushed it over the line is folded back under and carries on instead of being moved to a cold worker. Nothing new decides this — no second fold, no carry-on rung, no extra journal word — it is the pass that was already there, run one statement earlier."
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

#923 took the count out of the decision: the ladder's rungs climb no further
than the two notes, and what moves a running turn now is the runaway net — this
turn can no longer work where it is — rather than a third rung at round forty.
That left one thing standing, and it was an ORDER rather than a mechanism. The
net reads a quantity this loop reduces on its own, every step, for free; and the
reading stood one statement ABOVE the reduction. So a turn whose last round
pushed it over the line was moved out to a cold worker immediately before the
fold that would have brought it back under.

Two statements moved. Everything else here is about the handover that does
happen: the fold leaves an account of what it took, the reader and the writer
are shown where the turn has already been, the worker's context is bounded by
bytes rather than by a count of six, the brief carries the paths the turn wrote
as things to preflight, the work continues on the model that was doing it, and
the two crew-only calls refuse a model this codebase ships as a worker instead
of letting a flash model decide whether your answer is taken away from you.
