---
kind: fixed
title: the questions e2e suite waits for the keys the surface actually draws, and a law says it must
pr: 1004
surface: [chat, docs]
invalidates:
  - "A green `go test ./internal/e2e/` was read as proof the tmux suites were waiting for real screens. Between #933 (2026-09-11) and this fix it proved nothing about TestQuestionsE2E, which was 14 of 18 red the whole time: the rotted needles were inline literals and table `screen` fields, and the gate read neither."
  - "The needle table's gate was described as covering every string the tmux suite waits for. It only ever covered the `source` field, so a row could spell `[c] change` on screen while its source said `change` and pass. A row offered on a key now names the key and `keyedWord` spells the gap once."
  - "The tmux suite could type a screen string straight into `awaitQuestion`/`screenSays`/`waitFor`. A literal in one of those positions that brackets a key, or pastes a numbered answer onto its word, now fails `TestNoNeedleSpellsAKeyTheSurfaceStoppedSpelling` in twenty milliseconds. Sentences the model wrote are still literals and always will be."
  - "`internal/manual/chat/questions.md` said a pressable key is drawn `[1] allow once`. It is drawn `1 allow once` — the key, one space, the word, and no brackets anywhere on this surface since #933."
  - "`settleAccept`, `settleNotRight`, `settleTellIt` and `settleConflictNo` were `[a] accept`, `[n] not right`, `[s] tell it` and `[n] drop it`. The landing row draws `a accept`, `n not right`, `s tell it` and `n drop it` — one space, as internal/tui3's relanding and papercuts tests have pinned since #933."
---

#184 bought this package a gate so a respelled sentence would fail on the pull
request that respelled it rather than in whatever wave next paid for a model.
The gate had a hole in exactly the shape of the thing it was built for: it reads
the table, and half the needles were never in the table. The fix is not nine new
rows — it is that an answer is now COMPOSED from a key and a word rather than
pasted, and that a go/ast law refuses the pasting. A convention nothing enforces
is a convention that lasts until the next wave.
