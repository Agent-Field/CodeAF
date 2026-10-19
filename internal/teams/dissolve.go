package teams

import (
	"errors"
	"fmt"
	"os"
)

// ── DISSOLVING A TEAM THAT NEVER HELD ANYONE ─────────────────────────────────
//
// A conversation the person turns into a team (the chat's `/helpers`) is made
// its manager at once, with nobody under it, and is asked to propose helpers.
// The person may decline them all. What they are declining is the team itself,
// so the manager is given one way to take it back while there is still nothing
// to take back: a team that has never held anyone but its manager, has no team
// under it and has said nothing in its Traffic is forgotten whole, as if it had
// never been made. The conversation is untouched and goes on as an ordinary one.
//
// IT IS NOT DISBANDING. Disbanding (lifecycle.go) is the person's, from Teams,
// and keeps the team as read-only history, because a team that ran has a record
// worth keeping. A team that never ran has none, so there is nothing to keep and
// no history row to leave behind. The moment the team has held a second
// conversation, under any route, this door is shut for good, and the team ends
// the way every other team does.
//
// NOTHING HERE KNOWS WHICH DOOR MADE THE TEAM. A team made on the Teams page
// and given a manager there, and then never used, is dissolvable by the same
// rule: one rule for every team, whichever way it was made.

// ErrNotDissolvable is [Dissolve] asked to forget a team that has held more than
// its manager, has a team under it, or has Traffic.
var ErrNotDissolvable = errors.New("teams: this team has already held members or exchanged messages; the person disbands it from Teams")

// Dissolvable reports whether team id has never held anyone but its manager: it
// is open, it is not the root, it has a manager, that manager is its one member,
// it has no former member or former manager, and no team sits under it. It reads
// the file only; [Dissolve] also asks the Traffic.
func (f *File) Dissolvable(id string) bool {
	t, ok := f.Team(id)
	if !ok || t.Closed() || t.Root || t.Manager == "" {
		return false
	}
	if len(t.Members) != 1 || t.Members[0].Key != t.Manager {
		return false
	}
	if len(t.FormerMembers) > 0 || t.FormerManager != "" {
		return false
	}
	return len(f.Children(id)) == 0
}

// Dissolve forgets team id when it is [File.Dissolvable], manager is still its
// manager, and its Traffic is empty. It is checked and written under the
// store's lock, so a member added since the caller looked is seen. The team's
// directory goes with it. No conversation is touched.
func Dissolve(profileDir, id, manager string) error {
	err := Update(profileDir, func(f *File) error {
		t, ok := f.Team(id)
		if !ok {
			return fmt.Errorf("no team %s", id)
		}
		if t.Manager != manager {
			return errors.New("this conversation no longer manages that team")
		}
		if !f.Dissolvable(id) || trafficSaid(profileDir, id) {
			return ErrNotDissolvable
		}
		kept := f.Teams[:0:0]
		for _, other := range f.Teams {
			if other.ID != id {
				kept = append(kept, other)
			}
		}
		f.Teams = kept
		return nil
	})
	if err != nil {
		return err
	}
	if safeTeamID(id) == nil {
		if err := os.RemoveAll(TeamDir(profileDir, id)); err != nil {
			return err
		}
	}
	forgetPackets(profileDir)
	return nil
}

// trafficSaid reports whether team id's Traffic has anything in it, either
// file of it. An unreadable log counts as said, so a doubt keeps the team.
func trafficSaid(profileDir, id string) bool {
	path := TrafficPath(profileDir, id)
	for _, file := range []string{path, trafficRotated(path)} {
		info, err := os.Stat(file)
		if err == nil && info.Size() > 0 {
			return true
		}
		if err != nil && !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
