---
kind: fixed
title: A command quoted in the person's ask names no check, in either spelling
pr: 630
surface: [chat, engine]
invalidates:
  - "A span in backticks named a check in either account, which is the one exception #599 left standing when it took the `$ ` prompt line out of the person's pasted words. The backtick loop in `declaredChecks` ran unconditionally, above the source gate and outside it, so an ask that pasted an issue reading \"Repro: `chmod 000 tox.ini`, then watch it fail\" handed that command to the audit door as a declared check; `runnableHere` admitted it because `chmod` is on `$PATH`, and the checker ran it against the deliverable tree. `rm -rf build` came in the same way. Whose account the text came from now decides whether it names a check AT ALL: neither convention is read out of the person's pasted words, and both stay live in the work's own half of the brief and in a done-condition somebody wrote."
  - "The gate was a comparison inside the function, with one loop in front of it and one behind. It is now the first statement of `declaredChecks` and the only one — the inner `from == checksFromWork` around the prompt-line loop is gone, because two spellings of one law drift — and it is written `from != checksFromWork`, so a third `checkSource` added later reads as the person's account without anyone remembering to say so. `TestNoSpanIsHarvestedBeforeTheAccountIsAsked` is that shape as a law and refuses any loop in the function that begins before the guard ends, which is exactly the defect this closes."
  - "An ask that names a check in backticks no longer reaches the door on the strength of its own punctuation. It reaches it because the planner writes it into the work's own account first, which is the point: somebody authored it as a check. Nothing was added to refuse a list of dangerous words — a denylist is a guess at which commands hurt and the third one nobody thought of is the one that runs, while provenance is a fact."
  - "The task page said a backticked span \"names a check in either half of the brief\", and its section answering why a command out of a pasted issue was run closed by saying a backticked command still counts; the unattended page said the session's `done when` sentence naming something in backticks is run, and that sentence can BE the person's pasted words when nobody could write one. All three now say a check is a command the work names in its own half of the brief or in a `done when` sentence somebody wrote, in both spellings, and in your quoted words in neither."
---

#599 asked whose account a text came from and then exempted the one punctuation
nearly every bug report uses. The exception was the same hole wearing different
marks, and it was reachable by any ask that quoted a shell command — which is how
a person writes down what they saw.
