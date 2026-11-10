---
kind: fixed
title: codeaf exec and run no longer leave their turn trace untracked in the person's repository
pr: 1758
surface: [engine]
invalidates:
  - "`codeaf exec` wrote its turn-by-turn recorder and `.obs/` spills into `.codeaf/` beside the work, so a `git add -A` in a clean checkout published the whole transcript. The harness's own files now land in codeaf's state root (one directory per workspace name), never in the workspace; after an `exec`, `git status` shows only what the worker changed."
  - "`codeaf run <program>` had the same defect and dirtied a person's repository the same way; it now moves its scratch the same way."
  - "HEADLESS.md documented 'scratch lands in `--dir`' as deliberate. `CODEAF_SCRATCH` now names a directory to use instead, verbatim, the CI escape hatch the doc's earlier reading assumed."
---

Verified end to end: a deterministic `exec` against a stub worker in a fresh git repository leaves `git status --porcelain` empty; an `exec` that edits one file leaves exactly that file. One `WithScratch` call sweeps every machinery path at once — the trace, `.obs/`, job logs and patches all flow through `Workspace.ScratchPath`.