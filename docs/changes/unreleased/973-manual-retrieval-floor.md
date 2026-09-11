---
kind: fixed
title: a cold question about how it writes, and about what to do after installing, reaches its page again
pr: 973
surface: [chat, docs]
invalidates:
  - "`internal/manual/chat/standing-orders.md` said `## Always do it this way — coding style rules, conventions and preferences`, and every example under that heading was code: the tests, the public API, tabs, small commits. It read as though the kind that **holds** were for code alone. It never was — the sentence is kept word for word and rides into a new conversation and into a task's brief under `Standing orders` whatever it is about — so the heading now also names `how I want it to write` and carries three examples of that shape (\"always write in full sentences, never bullet lists\", \"use our spelling, not the American one\", \"keep replies short\"). Nothing about the machinery changed; the page had simply been describing a narrower thing than the one that exists."
  - "`internal/manual/chat/getting-started.md` — the page a person meets the moment they finish installing — did not use the word *install* anywhere, so \"what is the first thing I should do after installing\" was answered by `aforge doctor` on the *running from the terminal* page and by a task's `node_modules`. It now opens with its own section, `## I just installed it — what is the first thing to do after installing aforge`: run `aforge`, the setup opens by itself, and the install line itself stays where it lives, on *running from the terminal*."
  - "`TestHeldOutQuestionsReachTheirPage` in `internal/manual` was RED on a clean `dev` from 23f5da1eb (#933) until this, at 16 of 22 against a floor of 17 — it was not a flake and not a local tree. Anybody who saw it red on their branch was seeing the trunk. It is 18 of 22 now, with 10 first places against a floor of 7."
---

#933 rewrote `questions.md` and `permissions.md`, and the table the test logs says
exactly one question moved: *"I want it to always write in british english"* fell
out of fourth place by three thousandths of a point, to the `Pick several` section
of `questions.md`, which picked up the words *wants*, *writes* and *always* in one
true sentence about the tick's own cell. That section is not wrong, so the fix is on
the two pages that should have been reached and were not, and both of them were
saying something narrower than the truth.

The install paragraph is a `## ` section of its own rather than a few lines added to
the section below it, because the first attempt padded that section and cost *"how
do I get started"* its first place. BM25 normalises by length: a section that grows
loses the probes it already had, which is the reason the page law asks for a split
at the sub-topic rather than a longer section. Held out went 8 → 10 first and
16 → 18 within the four sections handed to the model; the plain twenty-five held at
21 and 25. No test, no floor and no question in `internal/manual/asked/` was touched.
