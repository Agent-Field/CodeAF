---
kind: fixed
title: a test binary never writes into the state root of whoever ran it
pr: 402
surface: [build, engine]
invalidates:
  - "`home.Dir()` answered `~/.aforge` whenever `AFORGE_HOME` was unset, including under `go test`, so a package that forgot to pin a root of its own wrote into the person's live journals, caches and profile. Under a test binary it now answers a throwaway directory of that process's own when the resolved root is the one the process merely inherited; a root a test chose — `t.Setenv` of `AFORGE_HOME`, or moving `HOME` the way `internal/session` does — is still that root, and outside a test binary nothing changes at all."
  - "`internal/e2e`'s `personConfig` read `home.Dir()/config.json` for the person's provider credentials. That is `home.InheritedDir()/config.json` now, because `Dir()` under test is the throwaway and the credentials this lane copies are the ones the process was started with."
  - "`internal/session`'s TestMain guard counted journal trees under `home.Dir()`. It now watches `home.InheritedDir()`, which is the person's root the gate exists to keep the files out of; watching `Dir()` would have watched the throwaway."
  - "The 488 note that isolation belongs at each package's own resolution seam, and that an empty `AFORGE_HOME` on the suite wrapper is not the fix, still holds for the wrapper: `scripts/one-suite.sh` still does not export one. The gate is inside `Dir()` itself, so tests that pin `HOME` still reach the managed `rtk` they installed under it, and a package that never mentions a home is covered without anybody remembering a TestMain."
---

The class is the same one #352 and #475 closed at the call log and the lane
store: a test binary inherits the state of whoever started it, and a helper in
the packages somebody remembered leaves every package written next month
unprotected. There is exactly one place the state root is resolved. The gate
is there.
