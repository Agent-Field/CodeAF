---
kind: fixed
title: Factory repair checks reconcile successful reruns and unchanged published repairs resume monitoring
pr: 1899
surface: [engine, chat]
invalidates:
  - "A failed shell command capturing a check's exit and printing its log could permanently block fixed even after an equivalent measured check passed. Supported output presentation now defers to the latest measured check in the same directory and target context."
  - "A GUI-launched factory could wrap cargo without finding its installed executable. Factory commands now discover CARGO_HOME/bin or the default home installation before creating the wrapper, preserving explicit PATH selection."
  - "Repeated rejected completion reports could consume the stage budget indefinitely. Three unchanged verification rejections, or five minutes after the first rejected completion, stop the stage with retained work and a concrete retry reason."
  - "A recheck of a successfully published repair could report fixed with no further edits, then pause demanding a new commit. A complete successful exact-head review on a clean published checkout is reconciled to no_change and returns to monitoring."
---

Unknown commands and skipped compound branches remain blocking. A passing subset,
another directory or target does not erase a failed check; current checkout
fingerprints and fresh remote CI/readiness gates retain their existing authority.
The verification recovery bound starts after a rejected completion, not during
the initial test run. No installation, profile sourcing, global PATH mutation,
automatic merge or publication replay is introduced.
