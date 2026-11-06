package session

import (
	"fmt"
	"strings"
)

const (
	ScopeProject      = "project"
	ScopeConversation = "conversation"
)

// ConversationScopeAt reads the stored scope. An absent or unreadable identity
// keeps the project's ordinary reading of other conversations' work.
func ConversationScopeAt(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ScopeProject
	}
	meta, err := LoadMeta(dir)
	if err == nil && meta.Scope == ScopeConversation {
		return ScopeConversation
	}
	return ScopeProject
}

// SetConversationScope changes only an existing conversation's scope. Project
// is stored as absence, and a word already kept owes no write.
func SetConversationScope(dir, scope string) (changed bool, err error) {
	if scope != ScopeProject && scope != ScopeConversation {
		return false, fmt.Errorf("scope is project or conversation")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false, fmt.Errorf("set conversation scope: no conversation directory")
	}
	stored := ""
	if scope == ScopeConversation {
		stored = ScopeConversation
	}
	err = withMetaLock(dir, func() error {
		meta, err := LoadMeta(dir)
		if err != nil {
			return err
		}
		if meta.ID == "" {
			return fmt.Errorf("no conversation at %s", dir)
		}
		if meta.Scope == stored {
			return nil
		}
		meta.Scope = stored
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
	return ConversationScopeAt(a.config.Place.Dir) == ScopeConversation
}
