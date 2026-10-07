---
kind: added
title: senior-dev checks CMake projects itself and takes --verify-build and --verify-test
pr: 1793
surface: [engine, docs]
invalidates:
  - "A CMake project with no recognized CI, script or README command failed senior-dev's checks with `no build entrypoint could be discovered`. senior-dev now builds it into `.senior-dev/cmake-build` and runs `ctest` there."
  - "There was no way to tell senior-dev which command checks a project; `submission_evidence` was the only channel, and nothing read it. `codeaf senior-dev run` takes `--verify-build CMD` and `--verify-test CMD`, which senior-dev runs itself on the submitted tree in place of discovery."
---

The shape is a header-only library or a fuzz target whose harness lives beside
the checkout: CyberGym's arvo_24633 was fixed, verified externally and graded
4/4, yet senior-dev called it a failed change. The flags come only from the
command line, never from the working model, so a check it could choose is not a
check it could choose to pass; naming one kind never excuses the other.
