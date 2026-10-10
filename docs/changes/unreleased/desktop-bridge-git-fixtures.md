---
kind: internal
title: Desktop bridge repository fixtures initialize Git metadata
surface: [desktop]
invalidates:
  - "Desktop bridge fixtures treated an empty .git folder as a repository. They now initialize real Git repositories, matching the detector's requirement for HEAD metadata."
---

The places wire contract, folder arrival, symlink source and permitted-root tests
use a shared Git initializer. A route regression keeps empty ancestor markers
from claiming plain folders as repositories.
