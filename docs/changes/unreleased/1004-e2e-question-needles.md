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
  - "`questionChipTail` waited for `question · alt+a`. The status line's chip leads with the question's own head — `? delete the build directory? · alt+a` — and says `1 question` only where there is no head to show, so the needle is the tail: ` · alt+a`."
  - "The tmux scenarios pressed a letter verb (`o`, `c`, `x`, `?`) straight at a question. A letter is the question's only once you have aimed at it (an arrow, `tab`, `enter`, `esc`, or a click); before that it goes in the message box. Six scenarios were opening nothing and waiting out three minutes for it. `aim` is the gesture, spelled once."
  - "The room scenario waited for `nothing chosen yet` on a freshly opened page. Since #789 the page opens where the block's pointer stood, so a page with answers on it always has one to send: the foot says `answering <what enter would send>`, and `nothing chosen yet` belongs to the `something else…` row."
  - "TestQuestionsE2E was 14 of 18 red and that was read as one rot. It was three: the key grammar, the chip, and the aiming rule — and under them three product defects the suite could not reach before, now filed as #1005 (a row too narrow for its answers is drawn as a row and one is cut; `questionRowFits` has no callers), #1006 (an ask-back lets the turn end and the question is withdrawn under the person) and #1007 (`ask` options sent as a JSON string carrying blocks is still refused)."
---

#184 bought this package a gate so a respelled sentence would fail on the pull
request that respelled it rather than in whatever wave next paid for a model.
The gate had a hole in exactly the shape of the thing it was built for: it reads
the table, and half the needles were never in the table. The fix is not nine new
rows — it is that an answer is now COMPOSED from a key and a word rather than
pasted, and that a go/ast law refuses the pasting. A convention nothing enforces
is a convention that lasts until the next wave.

The paid run went 4 of 18 to 16 of 18 on `deepseek/deepseek-v4-flash`, and the
three reds left in it are three product defects (#1005, #1006, #1007) that this
suite could not reach while it was waiting for screens that no longer existed.
