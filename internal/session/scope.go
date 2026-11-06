package session

import (
	"fmt"
	"strings"
)

const (
	WorkScopeProject      = "project"
	WorkScopeConversation = "conversation"
)

// ConversationScopeAt reads the stored scope. An absent or unreadable identity
// keeps the project's ordinary reading of other conversations' work.
func ConversationScopeAt(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return WorkScopeProject
	}
	meta, err := LoadMeta(dir)
	if err == nil && meta.Scope == WorkScopeConversation {
		return WorkScopeConversation
	}
	return WorkScopeProject
}

// SetConversationScope changes only an existing conversation's scope. A word
// already kept, or project when no scope is stored, owes no write.
func SetConversationScope(dir, scope string) (changed bool, err error) {
	if scope != WorkScopeProject && scope != WorkScopeConversation {
		return false, fmt.Errorf("scope is project or conversation")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false, fmt.Errorf("set conversation scope: no conversation directory")
	}
	err = withMetaLock(dir, func() error {
		meta, err := LoadMeta(dir)
		if err != nil {
			return err
		}
		if meta.ID == "" {
			return fmt.Errorf("no conversation at %s", dir)
		}
		if meta.Scope == scope || (meta.Scope == "" && scope == WorkScopeProject) {
			return nil
		}
		meta.Scope = scope
		if err := saveMetaLocked(dir, meta); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// ownWorkOnly is read beside the task index, so a stored choice reaches a live
// conversation without another connection or a read on the request path.
func (a *Agent) ownWorkOnly() bool {
	return ConversationScopeAt(a.config.Place.Dir) == WorkScopeConversation
}
