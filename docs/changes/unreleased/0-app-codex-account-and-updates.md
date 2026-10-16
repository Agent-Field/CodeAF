---
kind: changed
title: codeaf app has one Codex sign-in, and a reply is an update only when its own tool call follows
pr: 0
surface: [engine, docs]
invalidates:
  - "The app's ChatGPT (Codex) sign-in had two machineries: providers.sign_in shared OpenRouter's slot and said a failed keep on log, while providers.sign_in_start and _cancel kept an account attempt of their own. There is one now, the account attempt: providers.sign_in, _start and _cancel all reach it, and a failed keep ends sign_in_state failed with sign_in_error."
  - "The Codex row said either kind browser or auth_kind account, depending on which side wrote it. It says both, from one place, beside the sign-in's attempt and state."
  - "PROTOCOL.md said account providers refuse keys.remove. keys.remove signs the Codex account out, as codeaf disconnect codex does: a pending sign-in is cancelled first, one being saved answers busy, and the last sign-in's state is forgotten."
  - "In the app's process the profile guard let Codex's sign-in marker through as an exception. The app's sign-in writes its row with no key, so the exception is gone; a marker the terminal already wrote passes only because it is byte-identical to the row on disk."
  - "The live pump held every reply the engine closed in words alone until the turn's next move, and said all of them as updates if a tool followed. A reply is now an update only when its own tool call comes next, live as in agents.entries (appUpdates): a reply closed at session.EventAssistantDone is said at once as an answer, with its run id."
  - "The manual said the app's day cap was its own, separate from the terminal's daily limit, and kept in app/state.json. It is the profile's daily limit, the one /budget day sets, and app/state.json no longer holds it."
---

The provider catalog, its connect shapes and the checks are the ten-provider
ones; only the Codex sign-in's machinery is the account flow's. A crew
proposal (crews.propose) reads the day cap the way a turn does, and a cap or
ledger that cannot be read ends it in its one quiet line.
