package teams

import (
	"fmt"
	"time"
)

// RemoveConversation removes every membership only after each managed team's
// replacement or disbanding choice has been checked against the current file.
// An empty replacement means recursive disbanding; the optional global manager
// is removed without disbanding the root or its other teams.
func (f *File) RemoveConversation(key string, choices map[string]string, at time.Time, affected ...map[string][]string) error {
	for _, t := range f.Teams {
		if t.Closed() || t.Manager != key {
			continue
		}
		replacement, chosen := choices[t.ID]
		if !chosen {
			return fmt.Errorf("choose a replacement or disband %s before deleting its manager", t.Name)
		}
		if replacement == "" && !t.Root && len(affected) > 0 {
			if err := f.CheckAffected(t.ID, affected[0][t.ID]); err != nil {
				return err
			}
		}
		if replacement != "" && (replacement == key || !t.Holds(replacement)) {
			return fmt.Errorf("the replacement for %s is no longer an eligible member", t.Name)
		}
	}
	for id := range choices {
		t, ok := f.Team(id)
		if !ok || t.Closed() || t.Manager != key {
			return fmt.Errorf("the managed teams changed; review the deletion again")
		}
	}
	// Removing the optional global manager cannot silently assign its reports
	// to another team's manager.
	for _, t := range f.Teams {
		if !t.Root || t.Manager != key || choices[t.ID] != "" {
			continue
		}
		for _, other := range f.Teams {
			for _, m := range other.Members {
				if home, ok := f.Home(m.Key); ok && home.Team == t.ID {
					for i := range f.Teams {
						for j := range f.Teams[i].Members {
							member := &f.Teams[i].Members[j]
							if member.Key == m.Key {
								member.Home, member.Independent = false, true
							}
						}
					}
				}
			}
		}
	}
	for _, t := range append([]Team(nil), f.Teams...) {
		if current, ok := f.Team(t.ID); !ok || current.Closed() || current.Manager != key {
			continue
		}
		if replacement := choices[t.ID]; replacement != "" {
			if err := f.SetManager(t.ID, replacement); err != nil {
				return err
			}
		} else if t.Root {
			if err := f.ClearManager(t.ID); err != nil {
				return err
			}
		} else if err := f.Disband(t.ID, at, ""); err != nil {
			return err
		}
	}
	for _, t := range append([]Team(nil), f.Teams...) {
		for current, ok := f.Team(t.ID); ok && current.Holds(key); current, ok = f.Team(t.ID) {
			if err := f.RemoveMember(t.ID, key); err != nil {
				return err
			}
		}
	}
	return nil
}
