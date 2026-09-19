---
kind: fixed
title: a check is one command as the shell reads its quotes, and its refusal says what passes
pr: 1215
surface: [chat, engine]
invalidates:
  - "A declared check holding a bar, a dollar, a brace or a parenthesis anywhere in its text was refused as shell composition, quoted or not, so a search pattern of three words joined by bars could not be a check. Inside single quotes those characters are now text and the check reaches the checker byte for byte. Inside double quotes a dollar, a backtick and a backslash still refuse it, because the shell still expands them there."
  - "The refusal read `checks must each be ONE command with no shell composition` and ended `… is not`. It now names the character and says what passes: `\"|\" joins, redirects or expands commands in \"<the check>\". Such a character may stand only inside a single-quoted argument, where it is text`."
  - "Three `propose_task` calls made side by side in one reply and refused for one reason earned a `[stuck]` note saying the call had been repeated three times. Calls made side by side in one reply are one attempt, and a failure they share counts once."
---

The owner proposed three tasks in one message on 2026-09-18 and nine calls
were refused over three rounds before three started. Each round sent three
whole briefs again. The briefs were good: every refusal was about the shape of
a check, the second round's was wrong (a quoted bar is an argument), and none
of them said what would pass.

The rule is stated by what the shell would do with a character, never by which
program the check names. Nothing the shape law stopped before starts running.
