---
kind: fixed
title: Windows vet passes again, so the light gate stops failing pull requests that touch cmd/codeaf
pr: 2648
surface: [build]
invalidates:
  - "Was true: on app/v3, make vet (which runs GOOS=windows go vet ./...) failed on cmd/codeaf/app_one_engine_test.go because it called syscall.Kill. Every pull request into app/v3 that touched cmd/codeaf showed a red light gate it had not caused, and #2570 and #2645 merged red on it. Is true now: that test kills its child with os.Process.Kill, and Windows vet passes for the whole tree."
---
