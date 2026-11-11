---
kind: added
title: Team traffic records which turn caused each line
surface: [engine]
invalidates:
  - "A team's traffic carried no link to the turn that wrote a line, so anything that grouped traffic under the person's message that caused it had to guess from time, order or wording. Each line a model turn writes (a send, stop, start, post, decision, escalation, a sub-team start, and the member events a turn raises, including ones written after the turn ended) now carries turn (the durable id of the person's message that OPENED the turn, the same string the journal keeps on that message's line), run (the turn's run id) and run_by (the conversation that ran it). The crew log wire projects them as turn, run and run_agent. A steer does not become the opener; a turn a wake or a report opened has a run and no turn; a wake that answers exactly one drained person line names that line; legacy lines, watch-loop notes, the person's own lifecycle actions and any line outside a turn stay empty, which means unknown and never hidden. A door's client id is never a turn. A person message that was recorded now has its id minted before it is copied into the journal, so the id a receipt returns is the id the journal holds."
---
