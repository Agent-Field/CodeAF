---
kind: fixed
title: A rule about how it writes lives with the rules about how it codes
pr: 974
surface: [chat, docs]
invalidates:
  - "`I want it to always write in british english` reached `standing orders` only at fourth place, on body text alone, and #933's rewrite of `questions.md` took that fourth place away — `TestHeldOutQuestionsReachTheirPage` scored 16 of 22 against its floor of 17 on `dev` `23f5da1eb` ITSELF, so the light gate, the touched packages and `check` were red on every open pull request for a reason none of them caused. The asker's own sentence is on `standing-orders.md`'s `Always do it this way` heading and prose now, and the question reaches its page FIRST rather than fourth."
  - "`Always do it this way` read as a section about coding style alone — `We use tabs here`, `Prefer small commits`. It holds rules about how aforge WRITES too: spelling, language and tone are the same kind of standing order as a convention about code, they never fire, and they ride into the work the same way."
---

A held-out question that passes at rank four is not really passing: it is sitting
on the margin of some other page's silence, and any page that later learns to
speak takes the slot. That is what happened here, and it is why the fix is on the
page that deserved the question rather than on the page that beat it — the asker
really is asking about standing orders, and the test says in so many words to fix
the scoring rather than the questions. The words go into an EXISTING `## `
heading, because a new heading hijacks the scoring and breaks unrelated probes.

Found by lane R while gating its own pull request, and independently reproduced
by lanes A, F, T and P before it was written.
