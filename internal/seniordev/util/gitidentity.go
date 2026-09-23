//go:build !windows

package util

// The identity senior-dev's own commits carry.
//
// senior-dev commits as it works: its exact starting tree, every file its
// model writes, each coherent checkpoint and the candidate it submits. A
// working copy on a machine that has never been told who is committing (a
// fresh container, a hermetic HOME) refuses every one of those commits, and a
// refused candidate commit is a submission the run cannot make. So each git
// command that can commit carries an identity of its own, as `-c` overrides,
// which GIT_AUTHOR_* and GIT_COMMITTER_* in the environment still win over.
//
// None of these commits is what a person keeps. When codeaf runs senior-dev on
// a task, the run's commits are squashed into the one commit that lands, and
// that commit carries codeaf's identity rather than this one. The address is
// therefore a local one: it names the program that made a commit and no
// account anywhere.
const (
	CommitterName  = "senior-dev"
	CommitterEmail = "senior-dev@localhost"
)

// GitArgv is a git command line that carries senior-dev's commit identity.
func GitArgv(args ...string) []string {
	argv := []string{"git", "-c", "user.name=" + CommitterName, "-c", "user.email=" + CommitterEmail}
	return append(argv, args...)
}
