---
kind: fixed
title: Standalone exec keeps traces and observation spills outside the working directory
pr: 0
surface: [engine, docs]
invalidates:
  - "Standalone exec used to leave harness scratch in --dir; it now retains each run's machinery separately under the codeaf state root."
---

`codeaf exec` keeps `.codeaf/trace`, `.obs` and background job logs in a private,
unique `exec-*` directory under `$CODEAF_HOME/scratch` (`~/.codeaf/scratch` by
default), rather than dirtying a project's Git status. Repeated runs no longer
append to the same recorder. Stderr prints `exec scratch kept at <path>`;
stdout and the JSON work-product file list stay free of harness records.

Scratch is retained after exit. CI can move it with the existing `CODEAF_HOME`
override, pointed outside the checkout. Failure to create scratch stops startup
before the worker runs instead of falling back to the project directory.
