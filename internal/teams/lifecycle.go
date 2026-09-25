package teams

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── A TEAM IS OPEN OR CLOSED, AND ONLY A CLOSED ONE CAN BE DELETED ──────────
//
// The ruling (c-9): a team the work is done with is CLOSED, not deleted. It
// keeps its members, its Traffic, its packets and its closing report, it is
// drawn folded under `Closed · N`, and it can be reopened. Deleting forgets
// the grouping, its Traffic and its packets, and is offered only from Closed,
// so nothing that is running is ever one keystroke from gone. The
// conversations are never deleted; they stay in history.
//
// WHAT CLOSED MEANS TO THE REST OF THIS PACKAGE. A closed team is outside
// every walk: it is never anybody's home and its manager is found by no walk
// (home.go), it is never the team that decides between parties ([File.LCA]),
// its overrides are skipped by a team under it ([File.Effective]) and its own
// effective cap is none, because a closed team spends nothing. tidy re-runs
// the home rule on the same write that closes a team, so a conversation whose
// home closed reports to its next manager, or to nobody, from that write on.
//
// CLOSING CASCADES DOWN AND ONLY DOWN. Closing a team closes every open team
// under it and records which close closed them ([Team.ClosedWith]), so
// reopening the team reopens exactly those and not a sub-team the person had
// closed on its own before. A sub-team may close alone; its parent stays open.
// A team under a closed parent cannot be reopened until the parent is.
//
// THE SESSION'S WRAP-UP IS NOT HERE. What a manager does before a close (tell
// members to finish, answer what it can, write the closing report as a
// [KindClosing] packet) is internal/session's; stopping member turns and
// closing tabs is the interface's. This file is the record those two write.

// Team states.
const (
	TeamOpen   = "open"
	TeamClosed = "closed"
)

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

// Close closes team id at at, with report the id of its closing report packet
// ("" for none), and every open team under it. A team already closed is left
// as it was, and so is a sub-team already closed on its own.
func (f *File) Close(id string, at time.Time, report string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if f.Teams[i].Root {
		return ErrRoot
	}
	if f.Teams[i].Closed() {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	t := &f.Teams[i]
	t.State, t.ClosedAt, t.ClosedWith, t.Report = TeamClosed, at, id, report
	// A closed team has no wrap-up left to resume.
	t.Wrap = nil
	for _, d := range f.Descendants(id) {
		j := Index(f.Teams, d.ID)
		if f.Teams[j].Closed() {
			continue
		}
		c := &f.Teams[j]
		c.State, c.ClosedAt, c.ClosedWith = TeamClosed, at, id
		c.Wrap = nil
	}
	return nil
}

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

// Delete forgets closed team id and every team under it (closed with it, by
// the cascade): their entries in the teams file, then their Traffic and packet
// files. It answers the ids it deleted. A team that is open is [ErrOpen] and
// nothing is changed; so is one with an open team under it, which only a
// hand-edited file can have. The conversations are not touched.
//
// THE FILE IS WRITTEN FIRST. A crash between the two leaves directories no
// team names, which cost a few kilobytes and are harmless; the other order
// could leave a team whose Traffic was gone.
func Delete(profileDir, id string) ([]string, error) {
	var gone []string
	err := Update(profileDir, func(f *File) error {
		t, ok := f.Team(id)
		if !ok {
			return fmt.Errorf("no team %s", id)
		}
		if !t.Closed() {
			return ErrOpen
		}
		gone = []string{id}
		for _, d := range f.Descendants(id) {
			if !d.Closed() {
				return ErrOpen
			}
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
			_ = os.RemoveAll(TeamDir(profileDir, g))
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
