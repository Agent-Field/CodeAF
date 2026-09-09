---
kind: changed
title: Keep streaming prose compact until the response confirms an answer
pr: 653
surface: [chat, engine]
invalidates:
  - "A streaming paragraph appeared as a full answer until a later tool call proved it was progress. Unclassified prose now stays in the compact work display until its response ends without tool calls."
  - "Full answers streamed at full size immediately. In the compact view they now open at the confirmed response boundary, before later completion checks; expanded work remains available while they stream."
---

The provider's response boundary supplies the distinction rather than wording
heuristics or a second model call. Tool preambles remain short step headings;
valid tool-free replies and questions open in full before turn completion checks.
The shared reducer applies the same rule to chat and task rooms. A task joining
after the response was journaled does not replay the same prose a second time.

Empty, failed and truncated responses do not signal a confirmed answer. Saved
history keeps the existing structural answer hierarchy. No answer text is removed;
explicit disclosure still makes the streaming source available.

Interleaved content sections become one complete answer at confirmation, matching
the saved transcript; private reasoning stays inside work rather than interrupting
the answer. Joining a task during a retry replays only the replacement attempt.

An answer still being assembled remains above a queued user message or surface
notice when it is confirmed. Confirmation follows that active response instead of
misreading the newer row as its endpoint. A late boundary after interruption does
not restore live emphasis or activity to the stopped turn.

Both queue timings preserve the response: before first reasoning and during it.
A finished private-work tail below the queued line has its own closed disclosure;
only reasoning belonging to that confirmed response is eligible for it.

Completion receipts remain outside the private-work disclosure. Retrying also
withdraws private reasoning attached to the discarded response, so a live task
and the same task reopened after the retry show the same work.
