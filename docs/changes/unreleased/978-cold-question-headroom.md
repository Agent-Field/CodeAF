---
kind: fixed
title: Two cold questions reach their page first instead of fourth, so the retrieval floor has margin
pr: 978
surface: [chat, docs]
invalidates:
  - "The held-out retrieval floor in internal/manual/plainquestions_test.go had NO margin anywhere, not one thin probe: on a green tree 8 of the 22 cold questions reached their page first, 9 reached it only within the top four, and the floor is 17 — so all nine of those were simultaneously load-bearing and any one slipping out of fourth place reddened every open pull request. Two of the three fourth-place probes are now first, so it is 12 first of 22 rather than 10 and the floor has room."
  - "The folklore that a new `## ` heading in internal/manual/chat always hijacks BM25 and must never be added is too strong. It is a hazard, not a prohibition: #973's install section proved a new heading can be the right instrument, and the short section added here to choosing-a-folder.md took its question from fourth place to first with nothing else losing a place. What is forbidden is adding one WITHOUT measuring the whole table."
  - "CLAUDE.md's `keep each section under ~2000 characters and self-contained` has been readable as a style preference about how much prose somebody wants to read at once. It is not: THE LENGTH LAW AND THE RETRIEVAL LAW ARE THE SAME LAW. BM25 normalises by length, so a section past the cap is already hard to reach whatever its heading says — measured here, the `/folder` section is 3825 characters and would not move to first place however many of the asker's words its heading carried, while a 600-character section on the same page took the same question to first. choosing-a-folder.md has nine sections over the cap, up to 5079."
---

Lane R and lane P spent an evening between them on a red that was one question
slipping by three thousandths of a point, and the reason it cost that much is
that the gate reports one aggregate with no per-question ownership.

Both changes are measured rather than reasoned. `attaching-files` took the
asker's words into its existing heading; `choosing-a-folder` did not move that
way — its `/folder` section is 3825 characters and loses on length normalisation
however many of the asker's words the heading carries — so it got a short section
of its own instead, which is the form #973 proved works when the whole table is
measured with it.

**So there is a cheap test before choosing between the two forms, and it is not
judgement:** `wc -c` the section. Under CLAUDE.md's ~2000 character cap, put the
asker's words in its heading. Over the cap, the section is already unreachable —
split it, which fixes the length law and the retrieval together, or write a short
new one beside it. Splitting is the better instrument where there is room,
because a section past the cap is a defect on its own terms and not only when a
probe happens to find it. This change does not do that for choosing-a-folder.md's
nine over-cap sections; that is its own piece of work.
