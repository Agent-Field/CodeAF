---
kind: changed
title: the approval question is the question block now, answered by digits, with esc meaning later
pr: 740
surface: [chat]
invalidates:
  - "internal/tui3/consent.go was a 1388-line block of its own — its own layout loop, its own key set, its own countdown, its own pointer targets, its own phone sheet. It is 470 lines and draws nothing. The question block (internal/tui3/question.go) draws every approval question, and what is left in consent.go is the three things that are the LANE'S: the transcript row the question is about, the widening yes written into the person's settings, and how long the reading clock runs."
  - "The approval question was answered with `y` allow, `n` deny, `a` always, and silently with `t` and `d`. It is answered with the option's own digits now — `[1] allow once · [2] always, this command · [3] deny` — which is the ONE KEY GRAMMAR every question on this surface takes. `y`, `n`, `a`, `t` and `d` are not keys on it and type themselves into the box."
  - "`esc` on the approval question denied the call. It is LATER: the question folds to the chip `? 1 question · alt+a`, the call stays blocked, the turn stays paused on it, and nothing is answered. Nothing is decided by making something go away."
  - "The approval question SUSPENDED THE KEYBOARD — every key that was not an answer was swallowed and the draft was untouchable. It is not modal. A key the question does not draw belongs to the message box, `[c] change` answers in words, and the question is still open and still answerable the moment the box is clear."
  - "A key pressed on the frame the approval question arrived on answered it. A key that arrives before the question has been on screen for 250ms is DROPPED, never applied late — the settle guard, which the block already applied to every other lane."
  - "The approval question's countdown lived in app.askAt/askPaused/askWait and was ticked by app.tickAsk/refocusAsk. It is the block's reading clock — questionShown.clockAt/clockFor/clockHeld, app.tickQuestion, app.holdQuestionClocks, app.refocusQuestions — and every law it carried is unchanged: expiry HOLDS rather than answering (F41), any key the block reads stops it for good, and it does not run on a window that has not got the keyboard."
  - "The line of words was the approval question's only wide shape and it truncated when it would not fit — which cut `deny` off the end at eighty columns. The answers row now gives things up in a fixed order (verbs, then the clock, then a parenthetical inside an answer's own word) and then PROMOTES to the card, one row per answer. No answer is ever cut."
  - "consentSheeted/consentSheet were the approval question's own phone tier. The narrow sheet is the block's (internal/tui3/questionsheet.go) and every question can have one: title, the subject wrapped rather than cut, the reason, a full-width band per answer, and the queue count and clock on the foot."
  - "The hint under the box spelled the approval keys as a literal, `y allow · n deny · a always`. It is derived from the question in front of the person now (app.questionHint), so a key drawn on the row and a key named under the box cannot come apart again."
  - "Answering the approval question left nothing above the box but the annotation on the call's row. It goes through Agent.ResolveQuestion now, so it also writes decisions.jsonl and leaves the block's receipt — `decided needs your ok to run bash → allow once · you · 14:02 · c change`."
  - "session.Answer had no way to say that a SURFACE had already written a permission down, so an always on a shell shape would have had the engine write a coarse tool-wide memo beside it — silence for every bash command from a person who read `git status*`. session.AnswerBanked is the comment key that carries the banked rule, and session.ConsentScopeOf turns it into ConsentRule, which is the scope the engine writes nothing beside."
  - "`[d] you decide` was offered on any question that was not a confirmation and not irreversible, which included the approval gate. It is refused on a permission whose stakes are not reversible: handing an approval back to the thing the gate was put in front of is the gate answering itself with one keystroke."
  - "app.questionDrawnHere covered three lanes with no older block. It covers session.QuestionConsent as well; the approval question is drawn in exactly one place."
---

The approval gate is the flagship of the migration and the reason the wave
exists: every law worth having was written into `consent.go` and nowhere else, so
eight other blocks went without. Moving it moves them — the settle guard, the
receipt, the chip, the never-modal keyboard and the typed answer are the block's
now, and the gate keeps every law it had.

Two of those laws were worth the whole port on their own. F41's countdown — the
hidden ten-second timer that recorded "denied" and killed work nobody refused —
is now a clock the block holds rather than a clock one lane holds, with its focus
gate intact. And `esc` stops meaning deny: it was the honest reading of "get this
off my screen" when the only alternative was a block a person could not leave,
and there is a way out now.
