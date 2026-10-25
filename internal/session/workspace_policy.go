package session

import "fmt"

// WorkspacePolicy records a conversation's task placement contract. Its absence
// preserves legacy placement; new conversation creation explicitly selects v1.
type WorkspacePolicy struct {
	Version int    `json:"version"`
	Mode    string `json:"mode"`
}

// DefaultWorkspacePolicy selects private delegated task copies. It does not
// relocate foreground editing, grant landing consent, or contain shell programs.
func DefaultWorkspacePolicy() *WorkspacePolicy {
	return &WorkspacePolicy{Version: 1, Mode: "private-tasks"}
}

func validateWorkspacePolicy(policy *WorkspacePolicy) error {
	if policy != nil && (policy.Version != 1 || policy.Mode != "private-tasks") {
		return fmt.Errorf("unsupported conversation workspace policy")
	}
	return nil
}

// WorkspaceOwnershipEnabled reports this conversation's task placement policy.
func WorkspaceOwnershipEnabled(place Place) bool {
	if place.Dir != "" {
		meta, err := LoadMeta(place.Dir)
		if err != nil || validateWorkspacePolicy(meta.WorkspacePolicy) != nil {
			return false
		}
		if meta.WorkspacePolicy != nil || meta.ChatURI != "" {
			return true
		}
	}
	return WorkspaceOwnershipPrototypeEnabled()
}
