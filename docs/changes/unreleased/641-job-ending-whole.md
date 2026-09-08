---
kind: fixed
title: a job's ending reaches the model whole, and the general trim is gone
pr: 641
surface: [chat, engine]
invalidates:
  - "A background job's ending was reduced to its headline on the way to the model, and #574 said only an OWED ending — a command a task is parked on — travelled whole while `Every other job note is byte-for-byte what it was`. That is no longer true: every bash job's ending now arrives as `jobRegistry.settleExit` composed it, with the exit line, the last `jobExitTailLines` lines and the path to the whole log."
  - "`jobNote`, `Agent.enqueueJobNote` and `jobRegistry.notify` took a `whole bool` saying whether an ending was reduced. They do not: the argument went with the trim, exactly as `jobNote`'s own comment said it would, and `newJobRegistry`'s notify parameter is now `func(string)`."
  - "`jobNote`'s comment said repeating a job's tail in every later request would turn a notification into a second copy of the log, and `jobs.go`'s `settleExit` said in the same package that the output comes with the ending. Two comments stated two opposite laws; there is one law now, and it is `settleExit`'s."
  - "`internal/manual/chat/what-i-can-do.md` said a job's ending arrives as its exit code and last non-empty output line, and that the rest stays behind `jobs output` so a long build tail does not drag the model away from its work. Both sentences were the trim's justification; the page now shows the headline, the tail and the log path as the note actually carries them, and `jobs output` is for lines OLDER than that tail."
  - "`jobs_test.go` asserted that a completion note contains no newline (`THE NOTE IS ONLY THE HEADLINE`) and `wake_test.go` asserted that the long output did NOT reach the next step. Both pinned the defect and both now assert the opposite; a test that wants a one-line job note is testing something this engine stopped doing."
  - "A person watching a conversation saw a job's ending as one `while you worked: job 3 exited 0: …` line. They now see that line followed by the command's last lines and the bracket naming its full log, because the note the model reads is the note the transcript keeps."
---

What was already right was the composition: the registry built the whole ending
and stated the law for it — a job's ending is the moment its output finally
means something, and a note that withheld it would be an invitation to make one
more call for what the note was already about. `jobNote` then took `firstLine`
to it, so the model read the headline and spent a `jobs output` call learning
what the note was already about. `payOwed` still runs before the note is queued
and the registry with no lane to report into still releases its own park; what
is gone is a shape decision made twice, in two places, in opposite directions.
