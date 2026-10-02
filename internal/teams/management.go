package teams

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ConversationIdentity resolves aliases on the owning engine's filesystem.
// A manager's file and key may carry different symlink spellings.
func ConversationIdentity(key string) string {
	if !filepath.IsAbs(key) {
		return key
	}
	if resolved, err := filepath.EvalSymlinks(key); err == nil {
		return resolved
	}
	return filepath.Clean(key)
}

// UseLocalIdentities enables alias resolution only for records on this engine.
// Hosted windows must leave their copy independent of the local filesystem.
func (f *File) UseLocalIdentities() { f.localIdentities = true }

// conversationIdentity keeps hosted snapshots independent of the window's disk.
func (f *File) conversationIdentity(key string) string {
	if f.localIdentities {
		return ConversationIdentity(key)
	}
	if filepath.IsAbs(key) {
		return filepath.Clean(key)
	}
	return key
}

func (f *File) managerIdentity(t Team) string {
	if m, ok := t.Member(t.Manager); ok && m.File != "" {
		return f.conversationIdentity(m.File)
	}
	return f.conversationIdentity(t.Manager)
}

// ManagedTeams names every active team managed by a conversation, across views.
func (f *File) ManagedTeams(key string) []Team {
	identity := f.conversationIdentity(key)
	var out []Team
	for _, t := range f.Teams {
		if !t.Closed() && t.Manager != "" && (f.conversationIdentity(t.Manager) == identity || f.managerIdentity(t) == identity) {
			out = append(out, t)
		}
	}
	return out
}

// ManagerRemovalMessage tells the person exactly where leadership must change.
func (f *File) ManagerRemovalMessage(key string) string {
	var names []string
	for _, t := range f.ManagedTeams(key) {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ManagerRemovalInstruction
	}
	return "This conversation manages " + strings.Join(names, ", ") + ". Assign another manager before deleting it."
}

type managerConflict struct{ teams []string }

// managementConflicts compares responsibilities rather than ordinary membership.
// One managed anchor may cover descendants; the global manager stays separate.
func (f *File) managementConflicts() map[string]managerConflict {
	groups := map[string][]Team{}
	for _, t := range f.Teams {
		if t.Closed() || t.Manager == "" {
			continue
		}
		key := f.managerIdentity(t)
		groups[key] = append(groups[key], t)
	}
	out := map[string]managerConflict{}
	for key, group := range groups {
		roots := map[string]string{}
		for _, t := range group {
			root := t.ID
			for _, up := range f.Ancestors(t.ID) {
				if !up.Closed() && !up.Root && up.Manager != "" && f.managerIdentity(up) == key {
					root = up.ID
				}
			}
			roots[t.ID] = root
		}
		for i, a := range group {
			for _, b := range group[i+1:] {
				if !a.Root && !b.Root && roots[a.ID] == roots[b.ID] {
					continue
				}
				ids := []string{a.ID, b.ID}
				sort.Strings(ids)
				out[key+"\x00"+strings.Join(ids, "\x00")] = managerConflict{teams: []string{a.Name, b.Name}}
			}
		}
	}
	return out
}

// CheckManagementChange refuses new conflicting responsibilities. Older files
// remain readable and can be repaired without silently reassigning any manager.
func (f *File) CheckManagementChange(previous *File) error {
	before := previous.managementConflicts()
	after := f.managementConflicts()
	keys := make([]string, 0, len(after))
	for pair := range after {
		keys = append(keys, pair)
	}
	sort.Strings(keys)
	for _, pair := range keys {
		if _, already := before[pair]; already {
			continue
		}
		conflict := after[pair]
		names := append([]string(nil), conflict.teams...)
		sort.Strings(names)
		return fmt.Errorf("a conversation can manage one team and its descendants; %s are separate responsibilities. Choose a different manager in Teams", strings.Join(names, " and "))
	}
	return nil
}

// CheckManager checks an appointment without changing membership or leadership.
func (f *File) CheckManager(id, key string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("a manager needs a conversation key")
	}
	next := f.ManagementSnapshot()
	next.Teams[i].Manager = key
	return next.CheckManagementChange(f)
}

// ManagementSnapshot freezes the identities used when a store callback edits
// members in place as well as when it changes managers or moves teams.
func (f *File) ManagementSnapshot() *File {
	next := &File{Version: f.Version, localIdentities: f.localIdentities, Teams: append([]Team(nil), f.Teams...)}
	for i := range next.Teams {
		next.Teams[i].Members = append([]Member(nil), f.Teams[i].Members...)
	}
	return next
}

// ReferencedConversations protects even an unused chat named by retained team
// history. Permanent deletion still owns its explicit transcript tombstone.
func (f *File) ReferencedConversations() map[string]bool {
	out := map[string]bool{}
	for _, t := range f.Teams {
		for _, key := range []string{t.Manager, t.FormerManager} {
			if key != "" {
				out[f.conversationIdentity(key)] = true
			}
		}
		members := append(append([]Member(nil), t.Members...), t.FormerMembers...)
		for _, m := range members {
			for _, key := range []string{m.Key, m.File} {
				if key != "" {
					out[f.conversationIdentity(key)] = true
				}
			}
		}
	}
	return out
}
