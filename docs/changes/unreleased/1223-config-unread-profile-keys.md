---
kind: fixed
title: unread profile config keys are reported once at load
pr: 1223
surface: [engine]
invalidates:
  - "A profile config.json key that no reader consumed was silently ignored. Each unread top-level key is now named once through the load diagnostic channel."
---

Profile loading compares the file's top-level keys with the settings registry and
its non-setting profile fields. Unknown keys, including an object used where a
dotted key was intended, are reported through the process logger once per load.
Correctly shaped flat keys keep their existing resolution.
