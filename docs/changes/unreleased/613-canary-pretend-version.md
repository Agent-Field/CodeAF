---
kind: fixed
title: a canary cell builds the version its base was measured at, so VCS-versioned anchors install
pr: 613
surface: [build]
invalidates:
  - "Every pypa/virtualenv row on every canary table read `venv: pip install --group dev failed`, and it looked like pip weather. It was the rig: a cell's work tree has no reachable tag, so hatch-vcs stamped the editable install as `0.1.dev1` and the dev group's pre-commit-uv could not be satisfied. `pick.py` now records the measured `version` on the pool entry and `cell.sh` pretends it around the pip installs; virtualenv carries `20.39.1.dev3+g1d4a33811`, the other anchors gain theirs on their next remeasure."
---
