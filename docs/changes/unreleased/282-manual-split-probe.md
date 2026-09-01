---
kind: fixed
title: the manual section on splitting work carries the question people ask it with
pr: 282
surface: [docs, chat]
invalidates:
  - "\"what decides whether work gets split\" reached the tasks page FOURTH of four — a tie at the last slot rather than an answer — while the section that answers it, *When a task turns out to be too wide for one worker*, sat sixth. It is first now, because the asker's own words are in that section's `## ` heading, where they count three times. #270 landed a section on a different page and tipped the tie over, which is what a tie at the cut does: BM25 scores against the corpus average length and each term's rarity, so a new section anywhere moves every score a little."
  - "`internal/manual/chat.pack.gz` is a TRACKED file that every manual change has to regenerate, notwithstanding the `.gitignore` line that stopped applying the day it was first committed. #270 edited a page without it, so dev shipped a binary whose manual did not hold the folder-landing section at all — `make embed` on a clean checkout of that commit dirties the pack, which is the proof. `make test-packed-manual` exercises the corpus the shipped binary actually reads."
---

The law this obeys is FIX THE PAGE, NEVER THE TEST. A probe that fails is a
person's question reaching the wrong page, and the repair is always the heading:
they are the search index, and a question that only just reaches its answer is
one section away from not reaching it at all.
