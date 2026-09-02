---
kind: fixed
title: a conversation seats a crew older than the work seat too, and says so once
pr: 314
surface: [chat, engine, docs]
invalidates:
  - "A conversation's task nodes could run on the build's own worker model while `/crew` showed a crew the person had picked, with no receipt anywhere. Both surfaces now read one ladder, and the one that inherits says so."
  - "`TierModelAt` had two answers: the row somebody wrote, and this build's choice. It has three — a key that was NEVER HELD asks the row it was split out of first, so the settings sheet, `CrewAt` and the chat's role map all read the model a task ACTUALLY runs on. A row cleared on purpose and a profile with no tier keys are unchanged."
  - "`CrewAt` read a pre-#278 profile as though its worker row held this build's default. It reads the inherited value, so the preset word a profile of that vintage derives to may change — and it now agrees with the model the work runs on."
  - "The inheritance receipt was a headless thing (#311). It is also one line in the thread, said once per session at the first task start, and one line on the `/crew` sheet: `your work seat is inherited from small work — picking one writes it`."
---

#311 fixed the headless doors and left the surface where most tasks are started
saying nothing. The rung is not widened twice: the three cases a tier key can be
in — a row written, a row cleared, a key never held — are decided once in
`crewRow`, and both climbers ask it, so only the bottom rung differs between a
run and a conversation, because only it can. The receipt is `Seat.Notice`, the
same string on both surfaces, said where a person is already looking and never
per call — and picking any crew ends it, because `ApplyCrew` writes all five
rows.
