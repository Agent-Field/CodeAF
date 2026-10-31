---
kind: fixed
title: A deleted codeaf home is never rewritten by an engine that is stopping
pr: 2086
surface: [chat, engine]
invalidates:
  - "An engine still saving after it was asked to stop could find its home deleted and write a brand-new identity.json, device.json, identity-solo and sync ledgers into it, as if it were a first run; codeaf now refuses with \"codeaf home was removed or replaced; restart codeaf to use the current folder\", never re-creates a home it started in, and a stopping engine exits at once when its home is removed. A first run is unchanged."
  - "Naming the process that holds a busy engine store could take many seconds on macOS (one lsof per lock file, 500 ms each); one short budget now covers the whole lookup."
---

Also fixed: tests that failed on macOS (Option chords spelled `opt+`, files that
take their folder's group, bare `mktemp` ignoring `TMPDIR`, a bash 3.2 empty
array in the release workflow) and a test that wrote a lock file into the source
tree. Engine-dependent tests skip on GitHub's full tests because no furrow is
staged there; that is a known follow-up.
