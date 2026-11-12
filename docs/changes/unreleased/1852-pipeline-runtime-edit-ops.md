---
kind: added
title: Guarded sidecars preview when effort and fanout edits on chat steps
pr: 1852
surface: [engine]
invalidates:
  - "PRs 1846 and 1850 described prompt, until and max as the only structured step edits. A sidecar announcing factory.pipeline_edit_runtime now also previews when, effort and fanout, including clearing them, for an unlocked chat step whose exact source the factory attested. Step kind still changes only in the file. Desktop controls are delivered separately."
---

Preview changes retain the runtime values before and after the edit. Legacy JSON omits empty runtime fields, and a source or caller change requires a fresh read.
