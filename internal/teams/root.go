package teams

import (
	"errors"
	"time"
)

// ── THE OPTIONAL GLOBAL MANAGER IS THE MANAGER OF A REAL ROOT TEAM ──────────
//
// The ruling's goal is a person who talks to the top manager and steps away.
// With several top-level teams there is no top manager until the person makes
// one, on the teams page's `All teams` row. That manager could have been a
// special case beside the tree (a field on the file, a pseudo-team every walk
// would have to know about); it is instead the manager of an ordinary team
// marked Root that every other top-level team is moved under. So every rule
// already written holds without a special case: two parties in different trees
// meet at the root ([File.LCA]) and its manager decides instead of the person;
// a top-level team's manager reports to it ([File.Home]); an override on the
// root is what `· from All teams` means ([File.Effective]); and its spend is
// the whole machine's teams ([TeamSpend]).
//
// THE ROOT IS NOT A LEVEL. [File.Depth] does not count it, so a depth limit
// of three still means three levels of the person's own teams.
//
// tidy keeps it true: at most one root (the first), at the top, open, and
// every other top-level team under it, so a team made later at the top level
// lands under the root on the same write. [File.DissolveRoot] undoes it: the
// root team is removed and its children are top-level again; the global
// manager's conversation stays a conversation.
//
// Until the person asks, there is no root team, and the `All teams` row is a
// row the interface draws over the top level with nothing stored behind it.
//
// THE GLOBAL MANAGER'S MEMBERS ARE THE TOP-LEVEL MANAGERS (ruling c-5: orders go
// one level down, and a sub-team's manager is a member of the team above). So
// while the root has a manager, tidy makes every open top-level team's manager
// a member of the root, with the handle it has in its own team when that is
// free there. Its directives then reach them by the ordinary road, its digest
// lists them, and each reports to it by the ordinary home rule. It is only ever
// added: a membership the person removes comes back while that conversation
// still manages a top-level team, and one left behind by a manager who stopped
// being one stays until the person removes it (the session's view of the root
// shows only the current managers).

// RootName is the root team's name.
const RootName = "All teams"

// ErrRoot is a change the root cannot take: closing it (dissolve it instead)
// or putting it under another team.
var ErrRoot = errors.New("teams: All teams holds every team; remove its manager or dissolve it instead")

// Root is the root team, false when there is none.
func (f *File) Root() (Team, bool) {
	for _, t := range f.Teams {
		if t.Root {
			return t, true
		}
	}
	return Team{}, false
}

// MakeRoot makes the root team, when there is none, and moves every other
// top-level team under it. It answers the root's id. The caller then makes
// the global manager with [File.AddMember] and [File.SetManager], in the same
// write.
func (f *File) MakeRoot(at time.Time) string {
	if r, ok := f.Root(); ok {
		return r.ID
	}
	if at.IsZero() {
		at = time.Now()
	}
	root := Team{ID: NewID(), Name: RootName, Made: at, Root: true}
	f.Teams = append([]Team{root}, f.Teams...)
	tidyRoot(f.Teams)
	return root.ID
}

// DissolveRoot removes the root team and puts every team under it back at the
// top level. A file with no root is left as it is.
func (f *File) DissolveRoot() {
	r, ok := f.Root()
	if !ok {
		return
	}
	kept := f.Teams[:0:0]
	for _, t := range f.Teams {
		if t.ID == r.ID {
			continue
		}
		if t.Parent == r.ID {
			t.Parent = ""
		}
		kept = append(kept, t)
	}
	f.Teams = kept
}

// tidyRoot keeps one open root at the top with every other top-level team
// under it, and reports whether it changed anything.
func tidyRoot(teams []Team) bool {
	changed := false
	rootID := ""
	for i := range teams {
		t := &teams[i]
		if !t.Root {
			continue
		}
		if rootID != "" {
			t.Root, changed = false, true
			continue
		}
		rootID = t.ID
		if t.Parent != "" {
			t.Parent, changed = "", true
		}
		if t.Closed() {
			t.State, t.ClosedAt, t.ClosedWith, changed = "", time.Time{}, "", true
		}
	}
	if rootID == "" {
		return changed
	}
	for i := range teams {
		if teams[i].ID != rootID && teams[i].Parent == "" {
			teams[i].Parent, changed = rootID, true
		}
	}
	if seatTopManagers(teams, rootID) {
		changed = true
	}
	return changed
}

// seatTopManagers makes every open top-level team's manager a member of the
// root rootID while the root has a manager, and reports whether it added one.
func seatTopManagers(teams []Team, rootID string) bool {
	r := Index(teams, rootID)
	if r < 0 || teams[r].Manager == "" {
		return false
	}
	changed := false
	for _, t := range teams {
		if t.Parent != rootID || t.Closed() || t.Manager == "" || teams[r].Holds(t.Manager) {
			continue
		}
		m, ok := t.Member(t.Manager)
		if !ok {
			continue
		}
		m.Home, m.Started = false, false
		if m.Handle != "" && handleProblem(teams[r], m.Key, m.Handle) != nil {
			m.Handle = ""
		}
		teams[r].Members = append(teams[r].Members, m)
		assignHandles(&teams[r])
		changed = true
	}
	return changed
}

// TopManagers is the members of the root who manage an open top-level team
// right now, in the root's member order: the global manager's own members.
// Anything else the root holds (a manager who stopped being one, a
// conversation the person put there) is not one of them.
func (f *File) TopManagers() []Member {
	r, ok := f.Root()
	if !ok {
		return nil
	}
	managing := map[string]bool{}
	for _, t := range f.Teams {
		if t.Parent == r.ID && !t.Closed() && t.Manager != "" {
			managing[t.Manager] = true
		}
	}
	var out []Member
	for _, m := range r.Members {
		if managing[m.Key] && m.Key != r.Manager {
			out = append(out, m)
		}
	}
	return out
}
