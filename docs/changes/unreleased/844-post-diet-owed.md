---
kind: changed
title: A settings row for the prompt profile, a smaller `ask` schema, a job footer said once per change
pr: 844
surface: [chat, engine]
invalidates:
  - "The lean prompt profile had two triggers, the pin and a context window under 32,000 tokens. It has three: `AFORGE_PROMPT_PROFILE`, then the `prompt.profile` settings row (`auto`, `lean`, `full`, under `models` in /settings), then the window. `auto` is the default and decides nothing, so a session with the row untouched renders exactly what it rendered before."
  - "`AFORGE_PROMPT_PROFILE` was operator plumbing listed in `config.OperatorEnvPins`. It is a settings row's environment pin now: it wins over the row for one launch and holds the row read-only while it is set, the way every other pinned row in the sheet behaves. It is no longer in `OperatorEnvPins`."
  - "`internal/session/promptprofile.go`'s header comment and `internal/manual/chat/models-and-cost.md` both said there was no settings row for the profile and that a pin was the only way to choose it by hand. Both are rewritten: the row exists, the manual has a `## The prompt profile setting` section naming its three words, and the lean section says lean applies in three cases rather than two."
  - "The job footer rode at the foot of EVERY tool result while any job was out. It now appears only when its text differs from the last footer that turn sent. `internal/manual/chat/what-i-can-do.md` said `on every result` and now says the line is repeated only when it changes; `internal/session/tools_jobs.go`'s comment said the same thing and says the new one."
  - "`ask`'s wire schema was 4,277 bytes of the tool block. It is 3,844. No field was removed and no enum was removed, so anything that called `ask` before calls it identically now; what went was field prose that restated the field's own name and the tails of the two long descriptions."
  - "`leanPrefixBudget` was 32,500. It is 31,500, measured at 30,573 (page 16,825 plus tool block 13,748). The full arm is untouched at 38,742."
---

The row exists because the derivation can be wrong, and a derived state with
nowhere to read it is a state nobody can argue with. The window is the right
fact almost every time; the case it cannot answer is an endpoint reporting a
window its loaded model does not really have, and until now the only way to say
so was a variable in front of the command, which nobody sees in `/settings` and
which lasts one launch. The row and the pin take the same three words on purpose,
so that a word which is not one of them is not an answer at all rather than a
quiet move onto the other arm.
