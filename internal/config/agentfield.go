package config

import "strings"

// KeyAgentFieldKey is the profile field the factory's AgentField control
// plane API key is kept in, and KeyAgentFieldNodeSecret the field the node's
// own address secret is kept in (internal/factory/agentfield). Both are kept
// beside [KeyGitHubToken] and [KeyJiraToken] and written the same way: one
// value in one owner-readable file, never in the factory's own folder next to
// the address it belongs to.
//
// IN APP MODE BOTH THE APP AND THE PROFILE KEEP THEM: a door hands each to
// the app to keep (AppCredentials.Keep) and writes it to the profile, so an
// engine the desk starts again reads it back from its own profile when the
// app hands nothing back (live e2e round 9: the key was held in memory alone
// and lost at every desk launch). The reader asks the app's keys first, under
// [AppAgentFieldProvider] and [AppAgentFieldNodeProvider], so a removal the
// app hands back wins over the profile's copy.
//
// KeyAgentFieldInternalToken is the third: the control plane's internal
// token, which it sends as Authorization: Bearer on every call it makes to
// the node. The node refuses a dispatch that does not carry it. The node
// secret is now only READ, for one release, so a control plane that still
// has the old `/n/<secret>` base_url stored reaches the node (legacy).
const (
	KeyAgentFieldKey           = "agentfield_key"
	KeyAgentFieldNodeSecret    = "agentfield_node_secret"
	KeyAgentFieldInternalToken = "agentfield_internal_token"
)

// AppAgentFieldProvider and AppAgentFieldNodeProvider are the names the app's
// keys carry the two AgentField secrets under.
const (
	AppAgentFieldProvider     = "agentfield"
	AppAgentFieldNodeProvider = "agentfield-node"
	// AppAgentFieldTokenProvider carries the internal token.
	AppAgentFieldTokenProvider = "agentfield-internal-token"
)

// AgentFieldInternalTokenAt is the control plane's internal token kept for
// this profile, or "".
func AgentFieldInternalTokenAt(profileDir string) string {
	return agentFieldSecretAt(profileDir, AppAgentFieldTokenProvider, KeyAgentFieldInternalToken)
}

// AgentFieldKeyAt is the control plane API key kept for this profile, or "".
func AgentFieldKeyAt(profileDir string) string {
	return agentFieldSecretAt(profileDir, AppAgentFieldProvider, KeyAgentFieldKey)
}

// AgentFieldNodeSecretAt is the node's address secret kept for this profile,
// or "".
func AgentFieldNodeSecretAt(profileDir string) string {
	return agentFieldSecretAt(profileDir, AppAgentFieldNodeProvider, KeyAgentFieldNodeSecret)
}

// WriteAgentFieldSecrets keeps the API key and the internal token in the
// profile, and forgets any legacy node secret (a fresh connect registers a
// plain base_url, so no control plane needs the old path any more). An empty
// value removes the kept one: a control plane that runs without a key is
// connected with none. THE SECRETS NEVER REACH A LOG.
func WriteAgentFieldSecrets(profileDir, key, internalToken string) error {
	values := map[string]any{KeyAgentFieldNodeSecret: removeProfileKey}
	for field, v := range map[string]string{KeyAgentFieldKey: key, KeyAgentFieldInternalToken: internalToken} {
		if v = strings.TrimSpace(v); v != "" {
			values[field] = v
		} else {
			values[field] = removeProfileKey
		}
	}
	return writeProfileValues(profileDir, values)
}

// ForgetAgentFieldSecrets removes both kept secrets from the profile.
func ForgetAgentFieldSecrets(profileDir string) error {
	return writeProfileValues(profileDir, map[string]any{KeyAgentFieldKey: removeProfileKey, KeyAgentFieldNodeSecret: removeProfileKey, KeyAgentFieldInternalToken: removeProfileKey})
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
