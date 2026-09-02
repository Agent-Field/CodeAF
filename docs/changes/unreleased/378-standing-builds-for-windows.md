---
kind: fixed
title: the standing package builds for windows again, through the lock and pid packages the tree has
pr: 378
surface: [build, chat]
invalidates:
  - "internal/standing spelled its locks with unix.Flock and its pid check with unix.Kill, and the binary did not cross-build for windows at that package; it now asks internal/filelock and internal/processgroup.ProcessAlive, which each carry a windows file, and the package builds and vets clean under GOOS=windows."
  - "GOOS=windows go build ./... stopped in internal/standing; it now stops in internal/session (sessionfile.go's flock, jobs.go's Setsid, Getpgid and Kill), which is the same class in a package this change did not touch."
---

The nightly full check's windows cross-build had been red on this since the calls landed,
and the nightly's other redness hid it (#372). The timer itself already refuses windows
from `NewWatch`, so nothing else on that platform changes: the store works, a pass runs
from a window, and there is no timer to install.
