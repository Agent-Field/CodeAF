package session

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
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

// WHAT THE MODEL IS TOLD ABOUT OTHER WINDOWS FOLLOWS THE SCOPE. Under
// conversation scope no `<elsewhere>` block arrives and the tasks tool lists
// only this conversation's work, so a page still promising other windows'
// work would let the model read an empty answer as "nothing else is running"
// and say so. elsewhereFactProject is prompts/system.md's own line, swapped at
// render time, so the project page stays byte for byte what it was.
const (
	elsewhereFactProject = "- OTHER codeaf WINDOWS ON THIS PROJECT ARE VISIBLE TO YOU: the final `<elsewhere>` note lists LANDED files and RUNNING work. Read it before editing files another window touched."
	elsewhereFactOwnWork = "- THIS CONVERSATION IS TOLD ONLY ABOUT ITS OWN WORK: other codeaf windows' landed files and running work are hidden from you, so never conclude that nothing else is running or changing on this project."
)

// readsOwnWorkOnly reads the stored scope for the page and the tasks
// description. A task node is never told about other windows
// ([Agent.tellsElsewhere]), so its page is left as it was.
func (c Config) readsOwnWorkOnly() bool {
	if c.InTask || c.taskID != 0 {
		return false
	}
	return ConversationScopeAt(c.Place.Dir) == WorkScopeConversation
}

func (c Config) scopedElsewhereFact(page string) string {
	if !c.ownWork {
		return page
	}
	return strings.Replace(page, elsewhereFactProject, elsewhereFactOwnWork, 1)
}

// followWorkScopeLocked brings the page and the tasks description to the
// stored scope at a turn's start, the moment the scope takes effect, and does
// nothing while the scope is unchanged. A changed scope costs one cold prefix,
// as a changed placement does ([Agent.refreshClockLocked]).
//
// The caller holds a.mu; the belt is rewritten under armMu, in the order an
// anchoring conversation already takes them.
func (a *Agent) followWorkScopeLocked(now time.Time) {
	own := a.config.readsOwnWorkOnly()
	if own == a.ownWorkTold.Load() {
		return
	}
	a.ownWorkTold.Store(own)
	a.retellTasksTool()
	a.rerenderSystemLocked(now)
}

// retellTasksTool replaces the tasks tool where the belt or the shelf holds
// it, in fresh arrays, so a request walking the old snapshot is undisturbed.
func (a *Agent) retellTasksTool() {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	retold := a.tasksTool()
	for i, tool := range a.tools {
		if tool.Name != retold.Name {
			continue
		}
		tools := slices.Clone(a.tools)
		tools[i] = retold
		if len(a.definitions) == len(a.tools) {
			definition, err := toolDefinitions([]bare.Tool{retold})
			if err != nil {
				return
			}
			definitions := slices.Clone(a.definitions)
			definitions[i] = definition[0]
			a.definitions = definitions
		}
		a.tools = tools
	}
	for name, group := range a.shelf {
		for i, tool := range group {
			if tool.Name != retold.Name {
				continue
			}
			shelf := maps.Clone(a.shelf)
			shelf[name] = slices.Clone(group)
			shelf[name][i] = retold
			a.shelf = shelf
			return
		}
	}
}
