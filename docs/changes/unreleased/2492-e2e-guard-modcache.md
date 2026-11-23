---
kind: fixed
title: The e2e host guard removes a read-only Go module cache
pr: 2492
surface: [build]
invalidates:
  - "Was true: a live factory e2e (TestFactoryManagersE2E) whose job ran `go` under the host guard's throwaway login failed after every assertion passed, because $HOME/go/pkg/mod is read-only and t.TempDir's cleanup could not remove it (\"TempDir RemoveAll cleanup … permission denied\"). Is true: the guard makes its folder writable before removing it, so that red no longer means anything about the factory."
---

The live factory lanes run the engine under a throwaway login; anything a job
downloads there is the guard's to clean up.
