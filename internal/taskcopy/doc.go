// Package taskcopy carries the working copies of a chat's tasks with the chat.
//
// A task works in a git working tree under the session folder's trees/, which
// sits outside the tree a seal captures. It is a fork with a repository of its
// own where the machine can make one and a linked worktree where it cannot.
// Without this package a task's uncommitted edits stay on the machine that ran
// it, a worktree's registration in the project's own .git arrives on the next
// machine naming the first machine's absolute path, and a fork's commits sit in
// a repository the seal never sees.
//
// The seal side ([Carry.Compose]) writes what each live copy holds beyond its
// last commit into the cell's .cell/trees/<name>/, which the seal already
// captures and the takeover already restores. The take side ([Carry.Restore])
// forgets registrations whose folders are not on this machine, has the [Cutter]
// (the road a task makes its copy by) cut each copy again at this machine's path
// on the branch the task was on, and lays the carried files over it. A copy's
// own commits travel as a bundle beside its record.
package taskcopy
