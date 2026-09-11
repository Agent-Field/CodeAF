---
kind: fixed
title: an answer is drawn the moment it is pressed, and a question takes keys only where it is drawn
pr: 919
surface: [chat]
invalidates:
  - "A question used to freeze the window for about ten seconds per keystroke, and the tree said it was a busy machine. It was a deadlock: the wire's reader goroutine handed news to the update loop with a blocking p.Send while the loop sat inside Update waiting for the answer's own result frame, and only remote.callDeadline broke it. No door on the agent may now be called from Update — every one is asked inside an app.offLoop literal, and TestNoEngineDoorIsAskedFromTheUpdateLoop fails the build on a call that is not."
  - "A question was answerable from behind anything that was drawn over it: pressing ctrl+t and typing `hello there` on the start page granted a waiting tool on the `t` (#677). A question takes keys only where it is drawn now, and app.questionOffFrame governs both the keys and the drawing, for the batch sheet as well as the block."
  - "Every key a question drew acted the moment it was pressed, so the first letter of a sentence typed into an empty box answered: `d` handed the call back to the asker, `c` opened the change prompt. A VERB on the key table now belongs to the box until somebody aims at the question with a key that could never be text — an arrow, tab, enter, esc — or a click. The answers' own keys (1-9, and a letter a lane prints on an option) are unchanged and act straight away."
  - "app.answerQuestion was the only answering door and took one answer, and the batch sheet, the full page and home's band each had a hand-rolled copy of the road beside it. app.answerQuestions takes a list and sends a whole frame's answers through one command, off the loop, on one goroutine, in order; all four now go through it and the copies are gone."
  - "Every door was asked on the command's own goroutine, so two answers could reach the engine in the order two goroutines happened to be scheduled — `D` writes a rule AND answers in one keystroke, and the answer could resume the turn before the rule existed. Doors are queued on the update loop under the keystroke that made them and walked by one goroutine, so wire order is keystroke order."
  - "A refused answer put its question back unconditionally, which could delete a later answer's receipt and re-raise a question that had been settled. A refusal now re-raises only an answer that actually resolved something, never past a decision the questions lane has confirmed, and a refusal that arrived after the person switched conversation is kept and said when the question comes back instead of being dropped."
---

The freeze was measured before anything was changed, on a deterministic reproduction with
no model in it: 10.000362601s, which is `remote.callDeadline` to the microsecond. The same
reproduction now measures 181.056µs. A live drive of the real binary answered twenty
blocking questions with no `did not answer in time` and the receipt in the first frame
captured after the key.
