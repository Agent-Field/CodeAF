---
kind: changed
title: task commits are authored by the agentfield-bot account; the old local address is legacy
surface: [chat, engine]
invalidates:
  - "Task commits were authored `codeaf <codeaf@localhost>` (`codeafGitEmail` in `internal/session/task_branch_protection.go`, and the session-opening commit in `cmd/codeaf/chatv3_place.go`). They are authored `codeaf <agentfield-bot@users.noreply.github.com>`, the same identity the Co-Authored-By trailer names; `codeaf@localhost` is never written, and `taskCommitIdentity` still recognises it (with `aforge <aforge@localhost>`) as the task system's own work."
---
