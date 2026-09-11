---
kind: fixed
title: Asking back gets an answer with the question still open, and the chip counts only what waits on you
pr: 956
surface: [chat]
invalidates:
  - "The chat manual said an ask-back reaches the model only once you have answered whenever the work is parked on the question. That is no longer true of a question the model itself asked: the sentence goes back as the result of the call the model is parked on, carrying a note that the question is still on your screen, so the reply comes back while you are still deciding. It is still true of a permission, a task proposal and a standing card, which are not asked by a parked call — the manual now says which is which."
  - "`?` on a question no longer sends the sentence as an ordinary new message on every lane. Where the lane has the seam it goes through ResolveQuestion with session.Answer.AskedBack; nothing else about it moved, and the reply is still found in the transcript where the page was already looking."
  - "The question chip on the status row counted every open question. It now counts only the ones something is waiting on (session.AskKind.Waits), so a standing ratify no longer puts `? 1 question · alt+a` on every page and sends somebody to a screen with nothing for them to decide. The ratify is still drawn on the block and still answerable there."
---

#910 built the engine's half of both and #919 landed the surface's answer road;
these are the three seams the two of them handed back. The ask-back door
(`session.Answer.AskedBack`, returning the parked `ask` call with the person's
words) had no caller on the surface, and the chip was counting rows where the
engine had just been given one reading of what "waiting" means.

Which lanes take the door is asked of `session.AnswerResolves` rather than
listed, because that function is already the one reading of "does this answer
END the question" at both ends of the wire. It matters here: an answer carrying
nothing but words asked back RESOLVES a consent — approving it with no key — so
a version that routed every lane through the door would have turned a question
into an approval.
