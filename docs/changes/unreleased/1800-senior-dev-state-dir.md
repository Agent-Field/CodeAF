---
kind: fixed
title: senior-dev opens its store by its real path, and takes --state-dir to keep it outside the folder
pr: 1800
surface: [engine, docs]
invalidates:
  - "A run whose `.senior-dev` was a link to a store elsewhere ended on the next model turn after the link went away, with `open <folder>/.senior-dev/projection.lock: no such file or directory`, while the store itself was whole. senior-dev now resolves the store's path once when it opens it, so the database, the records and the lock all name the store itself, and a link that goes away no longer ends the run."
  - "senior-dev's session database and conversation always lived in the folder's `.senior-dev/` and were moved into the task's record when the run ended. `codeaf senior-dev run --state-dir DIR`, or `SENIOR_DEV_STATE_DIR`, keeps them in DIR instead, where they stay; a DIR inside the folder, other than under its own `.senior-dev/`, is refused before the run starts."
---

A CyberGym run on openssl had anchored `/src/openssl/.senior-dev` to
`/home/agent/senior-dev-store` through a link. Sixty-four minutes in the link
stopped resolving, and because the projection lock was reopened through it for
every write, one failed `open()` ended the run unsubmitted after $0.64; the store
was collected intact afterwards. The same ending appeared earlier on arduinojson.

A store that is itself removed still ends the run, now saying `its store <dir>
was removed while the run was working, with the conversation in it`, and it
does so even when something has made the directory again in the meantime — in
the folder's own `.senior-dev/`, the next tool output written there does. The
store remembers which directory it opened and writes into no other: in a fresh
one the next turn would make the session again under the same id and the model
would carry on from a conversation that starts where the removal happened. Runs
in folders without git all share the project `global`, so a rig should give
each run a state directory of its own.
