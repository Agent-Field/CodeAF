package config

import "strings"

// KeyGitHubToken and KeyGitHubVia are the profile fields the factory's GitHub
// connection is kept in, beside [KeyAPIKey] and written the same way, so a
// token typed on the floor is one value in one owner-readable file.
//
// KeyGitHubToken is a token the person handed over. KeyGitHubVia is `gh` when
// the person said yes to using gh's own login instead: CODEAF READS GH'S TOKEN
// ONLY AFTER THAT YES, because a program that helps itself to another
// program's credentials is a program nobody can audit.
const (
	KeyGitHubToken = "github_token"
	KeyGitHubVia   = "github_via"
)

// GitHubTokenAt is the token kept in the profile, or "".
func GitHubTokenAt(profileDir string) string {
	value, _ := persistedString(profileDir, KeyGitHubToken)
	return strings.TrimSpace(value)
}

// GitHubViaAt is `gh` when the person consented to gh's own login, or "".
func GitHubViaAt(profileDir string) string {
	value, _ := persistedString(profileDir, KeyGitHubVia)
	return strings.TrimSpace(value)
}

// WriteGitHubToken keeps a token in the profile and forgets any yes to gh,
// in one write, so the two can never both be the answer. THE TOKEN NEVER
// REACHES A LOG; a caller says "a token", never the bytes.
func WriteGitHubToken(profileDir, token string) error {
	return writeProfileValues(profileDir, map[string]any{
		KeyGitHubToken: strings.TrimSpace(token),
		KeyGitHubVia:   removeProfileKey,
	})
}

// WriteGitHubViaGH keeps the person's yes to gh's own login and forgets any
// kept token, in one write.
func WriteGitHubViaGH(profileDir string) error {
	return writeProfileValues(profileDir, map[string]any{
		KeyGitHubVia:   "gh",
		KeyGitHubToken: removeProfileKey,
	})
}
