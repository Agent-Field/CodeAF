---
kind: changed
title: The app and terminal share one home and one factory runner
pr: 1800
surface: [build, engine, chat, docs]
invalidates:
  - "app serve ignored CODEAF_HOME and defaulted to an app-data engine folder. It now resolves the TUI's ~/.codeaf home, respecting CODEAF_HOME and an explicit --home."
  - "Every process started its own factory worker slots. One process now holds the home's worker lock; another waits and reuses the private shared credentials when it takes over."
---

The owner decision supersedes API section 6's separate-home assumption.
Document flocks, conversation locks and presence continue coordinating shared
state. Worker credential updates now also take a cross-process file lock.
App stdin secrets remain unsaved; the shared home grants no permission to
persist Keychain or Secret Service keys.
