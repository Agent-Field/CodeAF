---
kind: fixed
title: Require factory attestation before claiming a caller signed in with GitHub
pr: 0
surface: [engine]
invalidates:
  - "A non-empty factory credential ID was treated as proof of GitHub sign-in. Script keys and legacy dashboard sessions can also have IDs."
---

The serving factory now explicitly attests a GitHub-verified caller profile.
The app uses that attestation alone; an older factory omitting it produces no
GitHub claim, while the connection keeps its actual authentication state.
The public identity also states whether the caller has a session that can be
signed out: verified legacy access grants do not imply this capability. Private
session identifiers and machine information still stay inside the engine.
