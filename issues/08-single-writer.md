# 08 · Single writer per file set (chat vs tasks)
Pareto: TRUST + correctness. Evidence: F36 — chat + task2 + task3 wrote the same files;
the user's tree ended inconsistent (SAVE15 uncommitted, task never landed).

Fix: while a task holds a file's worktree, the chat's edit to that file is blocked or
auto-routed into that task. One owner per file at a time.

Test: concurrent chat+task edits to one file leave the user's branch consistent and the
change durably committed.
