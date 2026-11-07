package config

import "strings"

// KeyAgentFieldKey is the profile field the factory's AgentField control
// plane API key is kept in, and KeyAgentFieldNodeSecret the field the node's
// own address secret is kept in (internal/factory/agentfield). Both are kept
// beside [KeyGitHubToken] and [KeyJiraToken] and written the same way: one
// value in one owner-readable file, never in the factory's own folder next to
// the address it belongs to.
//
// IN APP MODE THE APP HOLDS THEM, exactly as it holds the GitHub and Jira
// tokens: the write is refused with [ErrAppKeys], and the reader asks the
// app's keys first, under [AppAgentFieldProvider] and
// [AppAgentFieldNodeProvider].
const (
	KeyAgentFieldKey        = "agentfield_key"
	KeyAgentFieldNodeSecret = "agentfield_node_secret"
)

// AppAgentFieldProvider and AppAgentFieldNodeProvider are the names the app's
// keys carry the two AgentField secrets under.
const (
	AppAgentFieldProvider     = "agentfield"
	AppAgentFieldNodeProvider = "agentfield-node"
)

// AgentFieldKeyAt is the control plane API key kept for this profile, or "".
func AgentFieldKeyAt(profileDir string) string {
	return agentFieldSecretAt(profileDir, AppAgentFieldProvider, KeyAgentFieldKey)
}

// AgentFieldNodeSecretAt is the node's address secret kept for this profile,
// or "".
func AgentFieldNodeSecretAt(profileDir string) string {
	return agentFieldSecretAt(profileDir, AppAgentFieldNodeProvider, KeyAgentFieldNodeSecret)
}

// WriteAgentFieldSecrets keeps the API key and the node secret in the
// profile. An empty key removes the kept one: a control plane that runs
// without a key is connected with none. THE SECRETS NEVER REACH A LOG.
func WriteAgentFieldSecrets(profileDir, key, nodeSecret string) error {
	if AppMode() {
		return ErrAppKeys
	}
	values := map[string]any{KeyAgentFieldNodeSecret: strings.TrimSpace(nodeSecret)}
	if key = strings.TrimSpace(key); key != "" {
		values[KeyAgentFieldKey] = key
	} else {
		values[KeyAgentFieldKey] = removeProfileKey
	}
	return writeProfileValues(profileDir, values)
}

// ForgetAgentFieldSecrets removes both kept secrets from the profile.
func ForgetAgentFieldSecrets(profileDir string) error {
	if AppMode() {
		return ErrAppKeys
	}
	return writeProfileValues(profileDir, map[string]any{KeyAgentFieldKey: removeProfileKey, KeyAgentFieldNodeSecret: removeProfileKey})
}

// agentFieldSecretAt reads one of the two. An app handoff owns the
// connection, including a deliberate removal, so an app entry that is there
// answers itself rather than falling back to the profile.
func agentFieldSecretAt(profileDir, provider, field string) string {
	if key, ok := appKey(provider); ok {
		return key
	}
	value, _ := persistedString(profileDir, field)
	return strings.TrimSpace(value)
}
