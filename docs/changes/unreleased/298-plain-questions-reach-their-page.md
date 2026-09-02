---
kind: fixed
title: a page's own title is a search term, so a plain question reaches the page it names
pr: 298
surface: [chat, docs]
invalidates:
  - "The chat manual's retrieval was believed exact because chat_test.go's 700+ probes were
    green. It was exact on the questions the pages were written to answer and not on the
    questions people bring: twenty-five plain questions, each paired with the page whose NAME
    is the topic, landed their page first 11 times of 25 and reached it at all 17 times of 25.
    The floor is now 21 and 25, held by internal/manual/plainquestions_test.go."
  - "Section length was believed to be why plain questions miss. It is not: sweeping bm25B
    over 0.75, 0.5, 0.3 and 0 makes the numbers worse at every step, and saturating an
    over-long section's length costs probes without moving either question set. An over-long
    section is a page that needs splitting at its own sub-topics; the scorer is not the seam."
---

A page states what it is about on its `# ` line, and that line was a search term almost
nowhere: `split` hands it to the preamble section, and a page whose `# ` is followed straight
by a `## ` has no preamble — which is every page in `chat/`. Counted, 0 of the corpus's 1029
sections carried their page's title, so somebody who named the topic ("the screen is blank",
"who can see my files") could only be matched by whichever section happened to repeat the
word, and a long page has more sections in which to repeat it by accident. The title now
counts once, like a body word, in every section of its page.

That one change is the part that generalises, and it is measured as such: it moves the
twenty-five plain questions from 11 first / 17 within four to 14 / 20, and a held-out
twenty-two written cold before the work began from 6 / 15 to 7 / 17. Headings written in the
asker's own words carry the twenty-five the rest of the way to 21 / 25 and bought the
held-out set nothing at all, because a heading written for one question only ever answers
that question. Both sets are now floors that fail CI; the held-out one is documented as never
to be tuned against, and its low number is the honest size of the gap between a question the
pages were prepared for and a question they were not.
