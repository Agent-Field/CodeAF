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
	if key, ok := appKey("github"); ok {
		return key
	}
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
//
// IN APP MODE IT IS KEPT HERE TOO, beside the copy handed to the app
// (AppCredentials.Keep): an engine the desk starts again reads it back from
// its own profile, owner-readable only (0600), when the app hands nothing
// back at the next launch. Before 2026-11-08 an app-run engine held a pasted
// token in memory alone, and a desk whose keychain did not keep it lost
// GitHub at every launch (live e2e round 9). [GitHubTokenAt] still asks the
// app first, so a removal the app hands back wins over this copy.
func WriteGitHubToken(profileDir, token string) error {
	return writeProfileValues(profileDir, map[string]any{
		KeyGitHubToken: strings.TrimSpace(token),
		KeyGitHubVia:   removeProfileKey,
	})
}

// WriteGitHubViaGH keeps the person's yes to gh's own login and forgets any
// kept token, in one write.
//
// IN APP MODE IT KEEPS THE YES ALONE. The yes is consent, not a key: gh keeps
// its own token, and the yes is what lets the app's factory read GitHub again
// after a relaunch. A token the person pasted is the app's to keep, in its
// keychain, so no token row is touched here.
func WriteGitHubViaGH(profileDir string) error {
	if AppMode() {
		return writeProfileValues(profileDir, map[string]any{KeyGitHubVia: "gh"})
	}
	return writeProfileValues(profileDir, map[string]any{
		KeyGitHubVia:   "gh",
		KeyGitHubToken: removeProfileKey,
	})
}

// ForgetGitHubViaGH takes back the yes to gh's own login, for the app's
// factory when the person connects a pasted token instead: the token the app
// keeps is then the one answer, and a keychain that loses it must not quietly
// fall back to gh.
func ForgetGitHubViaGH(profileDir string) error {
	return writeProfileValues(profileDir, map[string]any{KeyGitHubVia: removeProfileKey})
}
