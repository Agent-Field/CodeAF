---
kind: fixed
title: a tasks section heading says what its fold hides, and stops stating a total the foot disagrees with
pr: 739
surface: [chat]
invalidates:
  - "A tasks section heading over a fold read `your call · 2 of 10 shown` — a total for the section in the same words the foot uses. It now reads `your call · 8 folded away`, counting only what the folds are holding back, and is still the section word alone when they hold nothing. Nothing on the page says `N of M shown` any more."
  - "`tasksSectionHead`'s comment said the heading was where the foot and the rows met, and that opening the fold made the numbers agree by themselves. Neither was true: the sections file a conversation and all its work under where the CONVERSATION stands ([tasksTreeOf], [tasksTree.filed]) while the foot counts what each piece of work IS, so the two are different partitions of the same rows and no line reconciles them. The comments now say that."
  - "The foot is UNCHANGED and its basis is deliberate. `tasksReading.tally` still counts each piece of work by its own state, which is why it can read `3 running` on a page that draws no `running` heading at all — the three rows are on the page, under the conversation that owns them, each wearing its own mark. A change that made the foot count the filing instead would re-create `4 your call` on a machine with three workers out."
  - "`internal/manual/chat/tasks.md` documented the page's opening heading as `tasks · 148 pieces of work since aug 11 · $34.10` only. That branch is taken solely when the page holds NO conversation; a page with conversations on it opens `tasks · 15 chats · 13 subtasks · $2.98`, and the manual now quotes both. Two tests ask the corpus for the exact strings `tasksReading.head` builds, so neither can drift again unseen."
  - "The same page explained the foot against the rows as a fold difference alone — `not the rows drawn, which is why it can read a larger number than you can count on the screen when a family is folded. The section heading is where that difference is said.` It is replaced by a section headed *Why the count at the bottom does not match the rows or headings*, which says the filing is the larger half of it."
  - "The ALL-CAPS law above `app.roomKinWord` read \"AND THAT ONE WORD IS `parked` AND NOT `queued`\". `railGroupWords` has been {needs you, running, queued, waiting, done} since #653, so the law named a word the table no longer has and forbade the word that is now its neighbour's. The word is `waiting` (`railParked` — admitted and BLOCKED) and the forbidden one is `queued` (`railIdle` — nothing in its way but a slot); the code never changed."
  - "Four manual passages still taught `idle` and `parked` as the roster's own group words, and the roster footer was quoted as `1 need you · 3 running` / `148 parked · 12 done`. The column builds that line from `railGroupWords` in `railFootOrder`, which leads with what is happening: `3 running · 1 needs you` / `148 waiting · 12 done`. A test now asks the corpus for it in the table's own words."
---

The heading between the foot and the rows was the one line on the frame whose job
was to settle the question of why the two disagreed, and it was the line
contradicting them: its `M` was the work FILED in that section, which is not the
number the foot says for that word whenever a conversation holds work in more
than one state. Nothing can reconcile two partitions on one line, so the heading
stops trying and says the one thing it does know — what its own folds are
hiding — and the manual takes over the explaining.
