---
kind: changed
title: senior-dev's coder checks its work the way the request uses it before it submits
pr: 2914
surface: [engine]
invalidates: []
---
The coder prompt now tells it to run the project's tests and use the deliverable before `submit`, to write its own check when there are no tests, to write a first version early, to install where the system finds it, to add no unrequested access paths, to keep cleanup to what its own work created, to take a check's expected values from the request rather than its own output, to solve the request itself without looking up its answer or tests, and to reproduce given code, models and formats exactly.
