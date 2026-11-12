package session

import "github.com/Agent-Field/codeaf/internal/exec"

const contributingReadLimit = exec.ContributingReadLimit

// repositoryRefusesTrailers resolves the checkout before reading the shared
// policy, so nested chat workspaces obey the repository's contribution rules.
func repositoryRefusesTrailers(dir string) bool {
	root, ok := repositoryRoot(dir)
	return ok && exec.RepositoryRefusesTrailers(root)
}
