---
kind: fixed
title: --one-model now reaches the mark reader and the brief writer, which had no model at all
pr: 449
surface: [chat, engine]
invalidates:
  - "A --one-model run had a mark reader and a brief writer. It had neither: the flag withheld the roles ladder, and the two crew-only callers hand callRole an empty floor on purpose, so those rungs had no pin, no tier and no floor and roles.Ladder answered ErrNoModel. Every measured --one-model chat run that handed off carried the ask only. It now settles both on the conversation's model, like every other text call."
  - "applyV3Governance's comment said --one-model needs to set nothing because every rung below already ends at the session model when nothing answers. That was true of three rows and false of the roles ladder. The flag is now carried into the engine as session.Config.OneModel and answered in callRole."
  - "\"the second model could not be reached\" covered every failure that was not a deadline, including a role nobody configured. A role with no model, and a session with no client, now read \"no second model is set\"; the journal still keeps the exact error, roles: no model for role \"handoff\"."
---

The two calls that decide whether a long answer is moved and write the brief the
task opens on are crew-only by design — with no mastermind they are skipped
rather than handed to the model that just wrote the answer, because that is the
author being asked to edit itself. `--one-model` says the conversation's model IS
the crew, so the refusal has no one to protect, and it was leaving the flag's own
runs with no second mind at all. It is decided once at the seam every errand
passes through, so a third crew-only caller is right without knowing the flag
exists.
