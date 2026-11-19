---
kind: fixed
title: A canvas's own picture maker can be chosen on an ordinary install
pr: 2853
surface: [engine, app]
invalidates:
  - "pictures.maker.set with scope canvas failed with `mkdir : no such file or directory` whenever CODEAF_PROFILE_DIR was unset, which is every ordinary install, and generate_image never saw a canvas's choice there; only Everywhere worked. The per-canvas store (canvas-pictures.json) now lives beside config.json in the profile config.ProfilePath resolves, the state root's own when the variable is empty, and both reaches work."
---
