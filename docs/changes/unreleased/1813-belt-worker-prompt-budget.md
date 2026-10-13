---
kind: fixed
title: the belt worker page fits its budget and is checked before merging
pr: 1813
surface: [engine, build, docs]
invalidates:
  - "The belt worker page exceeded its unchanged sixteen-KiB bound after the task-check guidance grew. Shorter wording preserves its rules and restores room below that bound."
  - "Embedded Markdown prompt changes could pass the Go-only touched-package gate without running the composed-page size and belt assertions. The light gate and make test-quick now run that existing test before merging."
---
