---
kind: changed
title: a reply promising the work is free of a problem nothing checked is sent back to check it or say so
pr: 2848
surface: [engine, chat]
invalidates:
  - "Was believed: the reply check (#2836, #2839) raises only statements the turn's evidence contradicts; a claim nothing covered is never raised. Is true: the reader also raises, as UNSHOWN, a reply's promise that the work is free of a problem (clear of something, no overlap, nothing cut off, no lettering, fits, renders clean) when nothing the turn saw looked at it or any entry reports the problem. The model is sent back to check it, or to leave it out or tell the person plainly it could not check it. Other uncovered claims are still not raised."
  - "Was believed: in the reader's evidence, where two entries disagree the later one wins, and an entry before any possibly-changing step is marked (earlier) and loses. Is true: the page carries one CHANGE line per step that may have changed the work (1.5 KB cap); an earlier entry holds for anything no later entry covers unless such a step changed that very thing; two looks at an unchanged picture that disagree settle nothing; on how the work looks, a look outranks the settings a step wrote."
  - "Was believed: a stale turn (changed after its last look) is read only when an output after the newest look shares words with the reply; otherwise it gets the bare look-again with no reader. Is true: every stale turn with something to compare is read first; the bare look-again is what is left when the reader raises nothing."
  - "Was believed: the reply check's dim lines are 'its reply did not match what it saw this turn' and 'it changed the work after its last look'. Is true: a third, 'its reply said more than it checked this turn · asking it to check or say so', is shown when the reader raised only UNSHOWN lines; like the others it is a carry-on notice the app does not draw."
  - "Was believed: a step that read a skill page is never offered to the reader. Is true for reads by path, and now also for a grep or any call whose arguments name the skill page; before, a grep over SKILL.md reached the reader because only the step line (its pattern) was checked."
---

qloop run24 (deepseek-v4.1-flash): 7 of 14 judged replies described results not
on the canvas, and each passed #2839's check. Replayed on that run's own
transcripts, the old ask raised the broken promise 1 time in 15 tries across the
cup, equal-width and signage turns; this one 12 in 15. Across all 27 replies
that looked at their work, 11-14 raise something and 17-20 are sent back, against
the 17 the run sent back itself. Still one small-tier call and one send-back a
turn.
