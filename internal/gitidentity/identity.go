// Package gitidentity holds the signature shared by codeaf's own commits and
// commands a program runs on its behalf.
package gitidentity

const (
	Name  = "codeaf"
	Email = "agentfield-bot@users.noreply.github.com"
)

// Environment makes Git attribute a model-written commit to the run even
// when the repository carries the person's user.name and user.email.
func Environment() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + Name,
		"GIT_AUTHOR_EMAIL=" + Email,
		"GIT_COMMITTER_NAME=" + Name,
		"GIT_COMMITTER_EMAIL=" + Email,
	}
}
