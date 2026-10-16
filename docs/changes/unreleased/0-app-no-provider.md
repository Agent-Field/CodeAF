---
kind: changed
title: codeaf app with no provider connected lists no models and says why a turn could not start
pr: 0
surface: [engine]
invalidates:
  - "With no provider connected, `models.list` answered the `engine` error `OPENROUTER_API_KEY is required` with the terminal's advice to export it. It now answers `{models: []}`, by the same rule `providers.list` answers `signed_in` with."
  - "Once the last provider was removed from a process half that had opened, `models.list` still offered the default model alone, although nothing could run it. It now answers `{models: []}`."
  - "A turn that could not start for want of any provider ended with `export OPENROUTER_API_KEY and run it again.` in its error. The app is now told `no model provider is connected`, and a crew member waiting on its key still reads that refusal as waiting, not failing."
---

Setup can finish with no provider, so an app with none connected is an ordinary
state rather than a broken one, so nothing the engine says to the app in it names a variable a
person in the app cannot set.
