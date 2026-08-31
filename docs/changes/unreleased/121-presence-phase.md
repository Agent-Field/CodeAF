---
kind: fixed
title: another window's running task says which life it is in, and a task may not read your login
pr: 121
surface: [chat, engine, docs]
invalidates:
  - "A home row or switcher row about a task running in ANOTHER window said a bare `running` for the whole of it. It says the phase too — `running · checking what it left`, `running · closing gaps` — because the presence file carries it now (`PresenceTask.Phase`). The round numbers stay on the window running the work, so another window reads `closing gaps` with no numbers after it, and a window whose presence has gone stale draws exactly the row it always drew."
  - "`gh auth token` was on a task's belt. It is refused inside a task, in every spelling — `GH_TOKEN=$(gh auth token)`, a pipe, a redirection, a substitution inside another command — and `gh auth status --show-token` with it. The conversation is unchanged and still runs it. `gh auth status` on its own, `gh auth setup-git` and every `gh` read are untouched."
  - "The manual said `TOKEN=$(gh auth token)` was how a command uses a credential and that nothing on the belt was worse off for redaction. True in a conversation, false in a task."
  - "`taskSegments` let a double quote swallow a command substitution, so `curl -d \"$(gh auth token)\" …` read as one word with no command in it. A substitution inside double quotes is now read as the command it is, which the path law and the road-home refusals see as well."
---

Two follow-ups from the 2026-08-31 waves. A node's state is `running` from its
worker's first call to its landing, and for whole minutes in the middle of that
the worker is not the one working — the window holding the graph learned to say
so, and every other window did not, because the phase was on the node and the
node was in another process. The beat writes it now, working written as nothing
at all, so a file from an older build decodes and draws as it always did.

And the redactor, which took the SHAPE of a token out of every result, was never
the whole of that worry: a token never has to be displayed to be spent, so `curl
-d "$(gh auth token)"` handed the person's login to a stranger with no character
of it passing a result. The read itself is refused inside a task now, and the
redactor gained the task-level pin it never had — a real worker, and then every
file the run wrote, read for the token's own characters.
