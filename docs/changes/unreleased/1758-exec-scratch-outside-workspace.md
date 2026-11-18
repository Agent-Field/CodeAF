---
kind: fixed
title: codeaf exec roots its scratch outside the workspace it was pointed at
pr: 1758
surface: [engine, docs]
invalidates:
  - "codeaf exec and codeaf run wrote the harness's own files — the turn recorder .codeaf/trace/task-1.trace.log, its stream sidecar, patch files and .obs spills — into the --dir the person named, as untracked files a `git add -A` would publish. Both doors now move scratch to <state root>/scratch/<workspace name>, keyed by the workspace's name the way `codeaf plan run` keys its own, so later runs in the same directory append to the recorders they already wrote and the working directory holds only work product: after an exec, `git status --porcelain` shows only what the worker changed. HEADLESS.md documented the old behavior; it now says where scratch lands and that CODEAF_HOME moves it."
---
