---
kind: internal
title: Join command test writers and select touched packages from the branch point
pr: 99999
surface: [build]
invalidates:
  - "Touched-package checks compared the current base tree with HEAD; they now compare the merge-base with HEAD, so advancing trunk does not select another lane's packages or module changes."
---

Cell and local factory fixtures stop and join their saving helpers before
TempDir removes their homes. Local check fixtures retain touch and sleep on
PATH even when Git comes from Homebrew. The memory and plan-file fixtures close factory
writers and both process lifetimes before restoring their environment.
CI and local checks use the same branch point for failure attribution, and
script fixtures cover a base advancing in a Go package or module file.
