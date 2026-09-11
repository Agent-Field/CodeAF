---
kind: fixed
title: an answer is drawn the moment it is pressed, and a question takes keys only where it is drawn
pr: 919
surface: [chat]
invalidates:
  - "A question used to freeze the window for about ten seconds per keystroke, and the tree said it was a busy machine. It was a deadlock: the wire's reader goroutine handed news to the update loop with a blocking p.Send while the loop sat inside Update waiting for the answer's own result frame, and only remote.callDeadline broke it. No door on the agent may now be called from Update — every one is asked inside an app.offLoop literal, and TestNoEngineDoorIsAskedFromTheUpdateLoop fails the build on a call that is not."
  - "A question was answerable from behind anything that was drawn over it: pressing ctrl+t and typing `hello there` on the start page granted a waiting tool on the `t` (#677). A question takes keys only where it is drawn now, and app.questionOffFrame governs both the keys and the drawing, for the batch sheet as well as the block."
  - "Every key a question drew acted the moment it was pressed, so the first letter of a sentence typed into an empty box answered: `d` handed the call back to the asker, `c` opened the change prompt. A VERB on the key table now belongs to the box until somebody aims at the question with a key that could never be text — an arrow, tab, enter, esc — or a click. The answers' own keys (1-9, and a letter a lane prints on an option) are unchanged and act straight away."
  - "app.answerQuestion was the only answering door and took one answer. app.answerQuestions takes a list and sends a whole frame's answers through one command, off the loop, on one goroutine; answerQuestion is now the one-item call of it."
---

The freeze was measured before anything was changed, on a deterministic reproduction with
no model in it: 10.000362601s, which is `remote.callDeadline` to the microsecond. The same
reproduction now measures 181.056µs. A live drive of the real binary answered twenty
blocking questions with no `did not answer in time` and the receipt in the first frame
captured after the key.
