// Package taskcopy carries the working copies of a chat's tasks with the chat.
//
// A task works in a git worktree under the session folder's trees/, which sits
// outside the tree a seal captures. Without this package a task's uncommitted
// edits stay on the machine that ran it, and the project's own .git arrives on
// the next machine still registering the worktree at the first machine's
// absolute path.
//
// The seal side ([Carry.Compose]) writes what each live copy holds beyond its
// last commit into the cell's .cell/trees/<name>/, which the seal already
// captures and the takeover already restores. The take side ([Carry.Restore])
// forgets registrations whose folders are not on this machine, cuts each
// copy's worktree again at this machine's path on the branch the task was on,
// and lays the carried files over it. The commits themselves need no carrying:
// they are in the project's .git, which the seal captures with the project.
package taskcopy
