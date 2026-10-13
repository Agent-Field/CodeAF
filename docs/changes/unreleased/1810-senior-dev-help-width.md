---
kind: fixed
title: senior-dev help fits eighty columns and is checked before merging
pr: 1810
surface: [engine, build, docs]
invalidates:
  - "The senior-dev run help printed --state-dir, --verify-build and --verify-test beyond eighty columns. Their descriptions now fit, and the state-dir line points to the manual's record-folder and environment defaults."
  - "A change to a carried program's flags could pass the PR gate without running the registry's help-width test. The light gate and make test-quick now run that test for every carried program and command."
---
