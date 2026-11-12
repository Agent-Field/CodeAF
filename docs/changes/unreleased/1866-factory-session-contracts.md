---
kind: fixed
title: Keep factory progress small and refuse recipe writes in held folders
pr: 1866
surface: [engine, chat]
invalidates: []
---

Factory progress preserves its recorded identity without increasing every transcript entry's memory cost. Recipe banking checks the actual file before writing, including when Git is unavailable or a repository has no commits. A held checkout stays untouched; a separate unheld change worktree and unrelated folders remain available. The chat manual explains how to continue after a refusal.

Factory cost estimates and budgets still accept fractional dollars. Session memory and prompt limits are unchanged.
