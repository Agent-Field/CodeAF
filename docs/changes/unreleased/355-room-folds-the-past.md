---
kind: changed
title: a task's page folds its settled work into phase chips, and ctrl+e, a click or a scroll up opens them
pr: 355
surface: [chat]
invalidates:
  - "a task's page used to fold NOTHING — a written law in workfold.go and three tests pinning it — and the manual said so in three places. The owner reversed it (issue #252, ruling 1, 2026-09-01): the page now folds settled work by default. It folds by PHASE, not by turn: the work before each paragraph the task wrote goes behind a `▸ worked …· ctrl+e` chip and the paragraph stands. ctrl+e opens the newest chip, a click opens any, and scrolling up when the page is already at its top opens the one nearest the top. What the task is doing right now never folds and keeps a whole screenful of calls; the brief, your corrections, failed calls, questions asked of you and the final report never fold at all."
  - "`ui.work = open` used to change nothing inside a task's page, because there were no chips to open. It now opens a task's chips exactly as it opens the conversation's."
  - "a room's pinned header used to carry a state word, a clock and a spend. It now also carries the call count and the live line (the running call, or `still working` after ten seconds of silence), and it degrades by rank on a narrow frame rather than being cut from the right. Every segment is still dropped when nobody published it."
  - "a task that ran in a folder with no repository used to show the engine's own word for that — `inplace` — on four surfaces: the room's header, the roster's row, and the settled card's tail and expansion (`✓ run these shell · inplace · 55s`). It now says `in your own folder`, in internal/session's own words for that ground (GroundWord), and the two lists in the tasks manual that spelled the landings say so too. All four readers go through one table, task.go's mergeScreenWords, and a structural test reads internal/session's merge-word const block and fails when any word in it has no screen word, so no engine token can leak here again."
  - "a background job a TASK started was invisible to the session: the ambient counts and the quit warning walked only the conversation's own calls, so a task could leave three servers running and `ctrl+c ctrl+c` said nothing about them. A node's finished calls now fold into the same counts, tallied per node as they close so the figure survives closing the task's page, and the header's call count and those counts share one definition of a finished call."
  - "the deck's `clock`, `showsWork` and `toolTail` knobs are gone. A page's whole posture is one `deck.lens` field declared in internal/tui3/lens.go — participantLens, overseerLens, and transcriptLens for a node's transcript inside a run's graph, which still folds nothing."
---

A person goes to a task to steer and to check, not to read a transcript. The
old law optimised the rare visit — the audit — at the cost of the common one,
and a page built on the premise that every call has to be read is the industry's
linear machinery scroll reproduced one level down. Folding by phase rather than
by turn answers the objection the old law was written for: the chip that used to
swallow a whole page (a task is one long turn) now covers one stretch of work at
a time, every paragraph stays standing, and the audit is one keypress away.
