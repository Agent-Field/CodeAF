---
kind: fixed
title: the pull-request gate runs for pull requests into santosh/work
pr: 2761
surface: [build]
invalidates:
  - "Was true: a pull request into `santosh/work` got only the licence check from GitHub, so its proof was a local `make pr-ready` serialised behind one lock on spark. Is true now: the PR gate (`ci.yml`) lists `santosh/work` for pull requests and pushes, so those pull requests get the same build, laws and touched-package legs as `dev`."
---
