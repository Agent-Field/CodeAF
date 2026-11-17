---
kind: added
title: A Personal job can work on a folder on this computer
surface: [engine, app, factory]
invalidates:
  - "A factory job ran only on a repository the factory watches on GitHub, and every job that changed code ended waiting for an approval that opened a pull request. In Personal a job can now be pointed at a folder on this computer that holds a git repository (`folder` on create, in place of `repo`; the job's repository is the folder's full path). It runs in its own worktree off that folder and finishes on a local branch with its proof, with nothing pushed and no approval to wait on. `factory.job_open_pr` (can.open_pr) pushes that branch and opens a draft pull request, offered only when the folder's origin is on GitHub and GitHub is connected. A Personal factory lists `local_folder` in /v1/features; an organization's never does and refuses a folder. The runner on this computer still starts only once GitHub is connected, so a folder job waits in the queue until then."
---
