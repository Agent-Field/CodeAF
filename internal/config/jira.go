package config

import "strings"

// KeyJiraToken is the profile field the factory's Jira API token is kept in,
// beside [KeyGitHubToken] and written the same way, so a token handed to
// factory.jira_connect is one value in one owner-readable file and never sits
// in the factory's own folder next to the site and the query it belongs to.
//
// IN APP MODE THE APP HOLDS IT, exactly as it holds the GitHub token: the
// write is refused with [ErrAppKeys], and the reader asks the app's keys
// first, under the provider name "jira".
const KeyJiraToken = "jira_token"

// AppJiraProvider is the name the app's keys carry the Jira token under.
const AppJiraProvider = "jira"

// JiraTokenAt is the Jira API token kept for this profile, or "". An app
// handoff owns the connection, including a deliberate removal, so an empty
// app entry answers "" rather than falling back to the profile.
func JiraTokenAt(profileDir string) string {
	if key, ok := appKey(AppJiraProvider); ok {
		return key
	}
	value, _ := persistedString(profileDir, KeyJiraToken)
	return strings.TrimSpace(value)
}

// WriteJiraToken keeps a Jira API token in the profile. THE TOKEN NEVER
// REACHES A LOG; a caller says "a token", never the bytes.
func WriteJiraToken(profileDir, token string) error {
	if AppMode() {
		return ErrAppKeys
	}
	return writeProfileValues(profileDir, map[string]any{KeyJiraToken: strings.TrimSpace(token)})
}

// ForgetJiraToken removes the kept Jira API token from the profile.
func ForgetJiraToken(profileDir string) error {
	if AppMode() {
		return ErrAppKeys
	}
	return writeProfileValues(profileDir, map[string]any{KeyJiraToken: removeProfileKey})
}
