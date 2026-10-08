package teams

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// Disbanding ends coordination recursively and preserves history. The persisted
// closed state remains compatible with earlier builds; it is read-only history
// in the current surface. Conversations and their current work survive.

// Team states.
const (
	TeamOpen   = "open"
	TeamClosed = "closed"
)

// ErrClosed refuses new coordination in a team's preserved history.
var ErrClosed = errors.New("teams: this team has been disbanded; its history is read-only")

// ErrOpen is [Delete] asked to delete a team that is not closed.
var ErrOpen = errors.New("teams: only a closed team can be deleted; close it first")

// ErrParentClosed is [File.Reopen] asked to reopen a team whose parent is
// closed.
var ErrParentClosed = errors.New("teams: its parent team is closed; reopen that first")

// Closed reports whether the team is closed.
func (t Team) Closed() bool { return t.State == TeamClosed }

// Descendants is every team under id, at any depth, parents before children.
func (f *File) Descendants(id string) []Team {
	var out []Team
	frontier := []string{id}
	seen := map[string]bool{id: true}
	for len(frontier) > 0 && len(out) < len(f.Teams) {
		next := frontier[0]
		frontier = frontier[1:]
		for _, t := range f.Children(next) {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			out = append(out, t)
			frontier = append(frontier, t.ID)
		}
	}
	return out
}

// Disband releases the selected team's coordination and every descendant's.
// Historical rosters stay with their records, while no closed membership can
// confer authority. Losing a reporting manager never assigns another by itself.
func (f *File) Disband(id string, at time.Time, report string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if f.Teams[i].Root {
		return ErrRoot
	}
	if at.IsZero() {
		at = time.Now()
	}
	ids := map[string]bool{id: true}
	for _, d := range f.Descendants(id) {
		ids[d.ID] = true
	}
	independent := map[string]bool{}
	for _, t := range f.Teams {
		for _, m := range t.Members {
			if home, ok := f.Home(m.Key); ok && (ids[home.Via] || ids[home.Team]) {
				independent[m.Key] = true
			}
		}
	}
	for j := range f.Teams {
		t := &f.Teams[j]
		if ids[t.ID] && !t.Closed() {
			t.State, t.ClosedAt, t.ClosedWith, t.Wrap = TeamClosed, at, id, nil
			if t.ID == id {
				t.Report = report
			}
		}
		for k := range t.Members {
			if independent[t.Members[k].Key] {
				t.Members[k].Home, t.Members[k].Independent = false, true
			}
		}
	}
	return nil
}

// Close preserves the old store door for closing reports and older callers.
// Its behavior follows disbanding: no session is stopped or implicitly reassigned.
func (f *File) Close(id string, at time.Time, report string) error { return f.Disband(id, at, report) }

// Reopen opens team id again, and every team under it that its own close
// closed. Its closing report stays recorded; a team reopened and closed again
// gets the new one. A team whose parent is closed is [ErrParentClosed].
func (f *File) Reopen(id string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if !f.Teams[i].Closed() {
		return nil
	}
	if p := f.Teams[i].Parent; p != "" {
		if parent, ok := f.Team(p); ok && parent.Closed() {
			return ErrParentClosed
		}
	}
	reopen := func(j int) {
		t := &f.Teams[j]
		t.State, t.ClosedAt, t.ClosedWith = "", time.Time{}, ""
	}
	for _, d := range f.Descendants(id) {
		if d.Closed() && d.ClosedWith == id {
			reopen(Index(f.Teams, d.ID))
		}
	}
	reopen(i)
	return nil
}

// Open is the teams that are open, in stored order.
func (f *File) Open() []Team {
	var out []Team
	for _, t := range f.Teams {
		if !t.Closed() {
			out = append(out, t)
		}
	}
	return out
}

// ClosedTeams is the teams that are closed, most recently closed first, for
// the folded `Closed · N` section.
func (f *File) ClosedTeams() []Team {
	var out []Team
	for _, t := range f.Teams {
		if t.Closed() {
			out = append(out, t)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ClosedAt.After(out[j-1].ClosedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TeamDir is team id's own directory, <profile>/teams/<id>, which holds its
// Traffic and its decision packets.
func TeamDir(profileDir, teamID string) string {
	return config.ProfilePath(profileDir, filepath.Join("teams", teamID))
}

// Delete disbands and forgets a selected team and its descendants. The reviewed
// scope is checked under the store lock before any mutation. Conversations survive.
// Records are removed before their history directories so a cleanup failure cannot
// leave an active team whose exchanges have vanished.
func Delete(profileDir, id string, expected ...[]string) ([]string, error) {
	var gone []string
	err := Update(profileDir, func(f *File) error {
		t, ok := f.Team(id)
		if !ok {
			return fmt.Errorf("no team %s", id)
		}
		if t.Root {
			return ErrRoot
		}
		if len(expected) > 0 {
			if err := f.CheckAffected(id, expected[0]); err != nil {
				return err
			}
		}
		if err := f.Disband(id, time.Now(), ""); err != nil {
			return err
		}
		gone = []string{id}
		for _, d := range f.Descendants(id) {
			gone = append(gone, d.ID)
		}
		drop := map[string]bool{}
		for _, g := range gone {
			drop[g] = true
		}
		kept := f.Teams[:0:0]
		for _, t := range f.Teams {
			if !drop[t.ID] {
				kept = append(kept, t)
			}
		}
		f.Teams = kept
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, g := range gone {
		if safeTeamID(g) == nil {
			if err := os.RemoveAll(TeamDir(profileDir, g)); err != nil {
				return gone, err
			}
		}
	}
	forgetPackets(profileDir)
	return gone, nil
}

// QuietAfter is how long a team goes without activity before Organize may
// propose closing it (ruling c-9's "about seven days").
const QuietAfter = 7 * 24 * time.Hour

// Quiet is every open team in f, the root aside, that Organize may propose
// closing at now: nothing in its Traffic, its packets or its members'
// transcripts for idle, and no packet waiting on it or raised from it. It is
// a proposal's input and closes nothing. It reads one Traffic line and one
// packet fold per team and stats each member's transcript, so it is asked off
// the loop, when Organize is.
func Quiet(profileDir string, f *File, now time.Time, idle time.Duration) ([]string, error) {
	waiting := map[string]bool{}
	open, _, err := OpenPackets(profileDir, ScopeAll)
	if err != nil {
		return nil, err
	}
	for _, p := range open {
		waiting[p.Team], waiting[p.Origin] = true, true
	}
	cut := now.Add(-idle)
	var out []string
	for _, t := range f.Teams {
		if t.Closed() || t.Root || waiting[t.ID] {
			continue
		}
		if last := lastActivity(profileDir, t); last.After(cut) {
			continue
		}
		out = append(out, t.ID)
	}
	return out, nil
}

// lastActivity is the latest of team t's last Traffic line, its last packet
// change and its members' transcripts' modification times; a team with none
// of these is as old as it was made.
func lastActivity(profileDir string, t Team) time.Time {
	last := t.Made
	later := func(at time.Time) {
		if at.After(last) {
			last = at
		}
	}
	if tail, err := ReadTraffic(profileDir, t.ID, "", 1); err == nil && len(tail) == 1 {
		later(tail[0].At)
	}
	if packets, err := Packets(profileDir, t.ID); err == nil {
		for _, p := range packets {
			later(p.At)
		}
	}
	for _, m := range t.Members {
		later(modTime(m.Key))
	}
	return last
}

// CheckAffected keeps a confirmation about exactly the teams it named, even
// when another writer adds or moves descendants while the card is open.
func (f *File) CheckAffected(id string, expected []string) error {
	actual := map[string]bool{id: true}
	for _, t := range f.Descendants(id) {
		actual[t.ID] = true
	}
	if len(actual) != len(expected) {
		return errors.New("the affected teams changed; review the confirmation again")
	}
	for _, id := range expected {
		if !actual[id] {
			return errors.New("the affected teams changed; review the confirmation again")
		}
	}
	return nil
}
