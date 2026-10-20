---
kind: changed
title: Dropping a factory job deletes its local branch and folder, and asks first when there is work
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a dropped job kept its branch for seven days (`DroppedBranchRetention`, `branch_delete_at` on the job); then a factory served by a window or `codeaf factory` deleted the branch ON GITHUB through the API and the local worktree and branch, while the app's own factory deleted nothing. Is true now: a drop deletes, at once and on the factory's own machine only, the job's worktree folder (`<factory folder>/work/<repo>-<number>`) with `git worktree remove --force` and its local branch (`factory/<number>-<slug>`) with `git branch -D`, in the repository that worktree belongs to. Nothing on GitHub is touched: no remote branch is deleted and no pull request closed. A pull request's job keeps its branch; only its folder goes. A running job is stopped first, and the delete waits for this machine's worker to let go of it."
  - "Was true: Drop never refused for the work it would lose. Is true now: when the job's local branch has commits beyond the commit its run started from, or its folder holds uncommitted or untracked changes, or its branch could not be read, `POST /v1/jobs/{id}/drop` is `409` with `reason` `has_work` and an error that names the work (`codeaf#1844's branch has 3 commits and uncommitted changes.`), and nothing is stopped or deleted. The same drop with `\"discard\": true` drops and deletes. The app receives the refusal as appwire code `has_work` with the factory's sentence as its message and a `FactoryRefusal` detail."
  - "Was true: Drop took optional `words`, on the HTTP body and as `factory.job_drop` `words?`, kept on the drop's receipt. Is true now: Drop takes no words; its one argument is `discard` (`factory.job_drop {factory, job, discard?, expect}`), and a body or params that send `words` are refused (`422`, `bad_params`). The receipt's detail is empty, or `discarded: <the sentence>` when work was thrown away."
  - "Was true: a job on the wire carried `branch_delete_at` and `branch_deleted_at`, and the factory appended `branch_deleted` receipts. Is true now: both fields are gone, the factory schedules no deletion, and nothing appends `branch_deleted`; older receipts of that kind still read as `deleted branch`."
  - "Was true: a delete that failed was retried by the retention sweep. Is true now: a delete that fails does not fail the drop; the job is dropped, and its activity says `dropped · the local branch and folder were kept: <why>`."
---

The owner's decision: Stop keeps the work, Drop throws the job's local work
away, Bring back starts it fresh, and nothing on GitHub is ever touched by
either. A drop never loses work silently, so the factory reads the branch and
the folder first and asks.

A team factory's job that ran on another runner keeps its folder and branch on
that runner: the factory reads and deletes only its own machine's, and has no
signal yet that tells a runner which job's work to discard.
