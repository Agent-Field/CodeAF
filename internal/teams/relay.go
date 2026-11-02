package teams

import (
	"sort"
	"strings"
)

// RemoteKey is the chat list's existing machine row identity. A synced member
// without a local transcript takes this identity so opening it asks to continue.
func RemoteKey(id string) string { return "machine:" + id }

// RemoteCell reads only the machine row identity, never another computer's path.
func RemoteCell(key string) (string, bool) {
	id, ok := strings.CutPrefix(key, "machine:")
	return id, ok && id != ""
}

// OtherComputerKey identifies a conversation that has no transferable cell.
// Its title and membership travel, but this identity never opens a local path.
func OtherComputerKey(id string) string { return "other-computer:" + id }

// OnOtherComputer identifies a member whose chat cannot be continued here yet.
func OnOtherComputer(key string) bool { return strings.HasPrefix(key, "other-computer:") }

// RepairMerged restores file laws in a stable identity order before Update's
// ordinary tidy runs. The member order is restored after handle repair because
// collision ownership depends on join time rather than a concurrent reorder.
func (f *File) RepairMerged() {
	sort.Slice(f.Teams, func(i, j int) bool {
		if !f.Teams[i].Made.Equal(f.Teams[j].Made) {
			return f.Teams[i].Made.Before(f.Teams[j].Made)
		}
		return f.Teams[i].ID < f.Teams[j].ID
	})
	index := map[string]int{}
	for i, t := range f.Teams {
		index[t.ID] = i
	}
	for i := range f.Teams {
		if _, ok := index[f.Teams[i].Parent]; !ok {
			f.Teams[i].Parent = ""
		}
	}
	for _, t := range f.Teams {
		path := []string{}
		seen := map[string]int{}
		for id := t.ID; id != ""; {
			if start, ok := seen[id]; ok {
				loop := append([]string(nil), path[start:]...)
				sort.Strings(loop)
				f.Teams[index[loop[0]]].Parent = ""
				break
			}
			seen[id] = len(path)
			path = append(path, id)
			id = f.Teams[index[id]].Parent
		}
	}
	for i := range f.Teams {
		t := &f.Teams[i]
		order := map[string]int{}
		for j, m := range t.Members {
			order[m.Key] = j
		}
		sort.SliceStable(t.Members, func(i, j int) bool {
			a, b := t.Members[i], t.Members[j]
			if !a.JoinedAt.Equal(b.JoinedAt) {
				return a.JoinedAt.Before(b.JoinedAt)
			}
			return memberSyncIdentity(a) < memberSyncIdentity(b)
		})
		used := map[string]bool{}
		for j := range t.Members {
			m := &t.Members[j]
			if m.Handle != "" && !used[m.Handle] && ValidHandle(m.Handle) == nil {
				used[m.Handle] = true
				continue
			}
			m.Handle = ""
		}
		assignHandles(t)
		sort.SliceStable(t.Members, func(i, j int) bool { return order[t.Members[i].Key] < order[t.Members[j].Key] })
		if !t.Holds(t.Manager) {
			t.Manager = ""
		}
	}
	tidy(f.Teams)
	// Two individually legal appointments can merge into separate duties for
	// one manager. Keep the smaller team id so Update can accept the merge.
	for {
		conflicts := f.managementConflicts()
		if len(conflicts) == 0 {
			break
		}
		pairs := make([]string, 0, len(conflicts))
		for pair := range conflicts {
			pairs = append(pairs, pair)
		}
		sort.Strings(pairs)
		parts := strings.Split(pairs[0], "\x00")
		f.Teams[index[parts[len(parts)-1]]].Manager = ""
	}
	assignHomes(f.Teams)
}

func memberSyncIdentity(m Member) string {
	if m.SyncID != "" {
		return m.SyncID
	}
	return m.Key
}

// DeliveryRefusal is shared by the tools and surface, so a refused send never
// sounds like a receipt for work that only another computer could receive.
func DeliveryRefusal(member Member, computer string) string {
	who := member.Word
	if member.Handle != "" {
		who = "@" + member.Handle
	}
	if who == "" {
		who = "This chat"
	}
	return who + " is on " + computer + "; the message would not reach it, so continue the chat here first."
}
