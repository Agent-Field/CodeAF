---
kind: removed
title: releases come from one repository, and a 404 there is final
pr: 1832
surface: [build]
invalidates:
  - "After a 404 from Agent-Field/codeaf, the updater (selecting a release, looking up a tag's publish moment, downloading) and scripts/install.sh asked the repository's former name as a second source. They no longer do: a 404 from Agent-Field/codeaf ends the selection or install, and no other repository is asked."
  - "The installer used whatever repository a caller wrote on the release. It now always downloads from Agent-Field/codeaf, so no caller can point an install at a second origin."
---

The former name is only GitHub's redirect to the renamed repository (the two
report one repository id), so it never held a release the real one lacked; the
fallback could only ever have reached a repository created under that name
later. The former ASSET name is still tried inside the same release, because
v0.1.0, the one stable published before the rename, carries it.
