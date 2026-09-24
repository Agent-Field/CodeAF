package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAMS SEAM: WHERE THE TEAMS FILE AND THE TRAFFIC ARE ─────────────────
//
// A team's file and its Traffic logs belong to the machine the SESSION runs
// on, because the team tools a model calls (internal/session) read and write
// them in that machine's profile. On a local launch that is this machine's
// profile; over --host it is the far machine's, and this window's own profile
// is somewhere the manager never looks. So the interface does not open the
// store itself. It asks through [TeamsSeam]: the local one wraps
// internal/teams at the profile directory ([localTeams]), and the --host door
// hands one that asks the engine over the wire (cmd/codeaf's hostTeams).
//
// EVERY CALL THAT MAY WAIT IS MADE OFF THE LOOP. An edit is made to what the
// window holds at once, so the strip and the wall show it on the keystroke,
// and queued; the queue is written by one command beside the loop's own
// ([app.teamsWrite]), on the ordered door line, so two edits reach the file in
// the order they were made. The Traffic clock's reads are made by its own
// command ([app.trafficNext]). The one call made on the loop is
// [TeamsSeam.Load], at an opening, and it must not block: locally it is one
// small file (the fourth law's `open`), and over a connection it answers what
// is held and asks the engine behind itself.
//
// A WINDOW THAT CANNOT REACH THE SESSION'S TEAMS HAS NONE, IT DOES NOT BORROW
// THIS MACHINE'S. Over --host with an engine that predates the teams doors the
// door hands no seam, and [app.teamsOff] turns the team writes and the manager
// off with [teamHostedWord] (host.go's honesty table).

// TeamsSeam is the teams file and the Traffic logs of the machine the session
// runs on, as functions (the reason [StandingSeam] gives). The zero value is
// this machine's own profile, which is every local launch.
type TeamsSeam struct {
	// Load is the teams as held, coloured around reserved, and the file's stamp
	// (internal/teams' stamp.go). It is asked on the loop, at an opening, and
	// must NOT block. known false says nothing is held yet: the window asks
	// [TeamsSeam.ReadSince] off the loop and holds no teams until it answers.
	Load func(reserved []float64) (teams []teamstore.Team, stamp string, known bool)
	// ReadSince is the file when its stamp is not since, and same when it is; ""
	// is a window that has never read it and is always answered with the file.
	// It may block.
	ReadSince func(since string, reserved []float64) (teams []teamstore.Team, stamp string, same bool, err error)
	// Update is the store's read-modify-write: change is made to the file as
	// it is, and what was written and its stamp come back. change may be made
	// more than once (a write over a wire that met another writer reads and
	// makes it again), so it depends only on the file it is handed. It may
	// block.
	Update func(change func(*teamstore.File) error) (teams []teamstore.Team, stamp string, err error)
	// Traffic is team's log after the cursor after ("" the tail), at most
	// limit entries, oldest first. A log that has not moved since the last
	// ask from the same cursor is answered with nothing from a stat. It may
	// block.
	Traffic func(team, after string, limit int) ([]teamstore.Entry, error)
}

// present reports whether the seam was handed at all.
func (s TeamsSeam) present() bool { return s.Load != nil && s.Update != nil }

// localTeams is the seam onto internal/teams in dir, the profile of this
// machine. watch is the window's stat-before-read memory of the logs.
func localTeams(dir string, watch *teamstore.Watch) TeamsSeam {
	return TeamsSeam{
		Load: func(reserved []float64) ([]teamstore.Team, string, bool) {
			stamp := teamstore.Stamp(dir)
			f, err := teamstore.LoadHued(dir, reserved)
			if err != nil {
				// AN UNREADABLE FILE IS MOVED ASIDE, NOT OVERWRITTEN. Starting
				// empty is the only way the person can go on making teams, but
				// the next write would then replace a file that may hold every
				// team they made; renamed to teams.json.unreadable-<nanos> it
				// survives for a person to recover.
				_, _ = teamstore.SetAside(dir)
				return nil, teamstore.Stamp(dir), true
			}
			return f.Teams, stamp, true
		},
		ReadSince: func(since string, reserved []float64) ([]teamstore.Team, string, bool, error) {
			stamp := teamstore.Stamp(dir)
			if since != "" && since == stamp {
				return nil, stamp, true, nil
			}
			f, err := teamstore.LoadHued(dir, reserved)
			if err != nil {
				return nil, stamp, false, err
			}
			return f.Teams, stamp, false, nil
		},
		Update: func(change func(*teamstore.File) error) ([]teamstore.Team, string, error) {
			f, stamp, err := teamstore.Change(dir, change)
			if err != nil {
				return nil, "", err
			}
			return f.Teams, stamp, nil
		},
		Traffic: func(team, after string, limit int) ([]teamstore.Entry, error) {
			entries, _, err := watch.Traffic(dir, team, after, limit)
			return entries, err
		},
	}
}

// teamsDisk is the window's side of the seam: the door, the queue of edits
// not yet written, and the read asked for when nothing was held.
type teamsDisk struct {
	// door is [Options.Teams]; its zero value is the local seam.
	door TeamsSeam
	// watch is the local seam's stat-before-read memory of the logs.
	watch teamstore.Watch
	// queue is every edit made to what this window holds and not yet handed
	// to the store, in the order they were made.
	queue []func(*teamstore.File) error
	// fetch says an opening found nothing held ([TeamsSeam.Load]'s known
	// false) and a read is wanted; fetching says it is out.
	fetch, fetching bool
}

// teamsSeam is the seam this window reads and writes teams through, bound now,
// on the loop, so a command that carries it off never reads the window.
func (a *app) teamsSeam() TeamsSeam {
	if a.teamsDisk.door.present() {
		return a.teamsDisk.door
	}
	return localTeams(a.profileDir, &a.teamsDisk.watch)
}

// teamsOff reports whether this window has no teams it can keep: over --host,
// facing an engine that does not answer the teams doors. The window's own
// profile is never the answer there, so the writes and the manager say
// [teamHostedWord] and the Traffic clock does not run.
func (a *app) teamsOff() bool { return a.hosted() && !a.teamsDisk.door.present() }

// teamTabWords is each conversation's current tab name, by key, for the edits
// written off the loop: [app.teamRefreshWords] reads the tabs, and a command
// may not.
func (a *app) teamTabWords() map[string]string {
	words := map[string]string{}
	for _, tab := range a.chatTabs {
		if tab.key != "" && strings.TrimSpace(tab.word) != "" && !tab.start && !tab.work {
			if _, ok := words[tab.key]; !ok {
				words[tab.key] = tab.word
			}
		}
	}
	return words
}

// teamApplyWords gives each member of teams its tab's name from words.
func teamApplyWords(teams []team, words map[string]string) {
	for i := range teams {
		for j, m := range teams[i].Members {
			if w, ok := words[m.Key]; ok {
				teams[i].Members[j].Word = w
			}
		}
	}
}

// teamsWrote is what one write, or one read asked for at an opening, came back
// with, folded on the loop.
type teamsWrote struct {
	teams []team
	stamp string
	err   error
	// covers is [trafficState.edits] when the write left, and read says this
	// was the opening's read rather than a write.
	covers int
	read   bool
}

// teamsWrite hands the queued edits to the store in one command on the door
// line, and the opening's read when one is wanted. It is asked after every
// message ([app.Update]) and at [app.Init], and answers nil when there is
// nothing to do, which is almost always.
//
// THE EDITS ARE WRITTEN TOGETHER AND KEPT APART. One command makes every
// queued edit to the file as the store has it now, each on a copy, and an edit
// the file refuses (a team another process deleted) is left out rather than
// taking the others down with it; the first refusal is said. What was written
// is what the window holds afterwards, unless the person has made another edit
// since, whose own write answers for it.
func (a *app) teamsWrite() tea.Cmd {
	var cmds []tea.Cmd
	if a.teamsDisk.fetch && !a.teamsDisk.fetching {
		a.teamsDisk.fetch, a.teamsDisk.fetching = false, true
		seam, reserved, covers := a.teamsSeam(), teamReservedHues(a.pal), a.traffic.edits
		cmds = append(cmds, a.besideLine(func() func(bool) tea.Cmd {
			teams, stamp, _, err := seam.ReadSince("", reserved)
			return func(bool) tea.Cmd {
				a.teamsTake(teamsWrote{teams: teams, stamp: stamp, err: err, covers: covers, read: true})
				return nil
			}
		}))
	}
	if len(a.teamsDisk.queue) > 0 {
		changes := a.teamsDisk.queue
		a.teamsDisk.queue = nil
		seam, words, reserved, covers := a.teamsSeam(), a.teamTabWords(), teamReservedHues(a.pal), a.traffic.edits
		cmds = append(cmds, a.offLoop(func() func(bool) tea.Cmd {
			var refused error
			teams, stamp, err := seam.Update(func(f *teamstore.File) error {
				refused = nil
				for _, change := range changes {
					mine := &teamstore.File{Version: f.Version, Teams: teamsClone(f.Teams)}
					if err := change(mine); err != nil {
						if refused == nil {
							refused = err
						}
						continue
					}
					f.Teams = mine.Teams
				}
				teamApplyWords(f.Teams, words)
				f.Colour(reserved)
				return nil
			})
			if err == nil {
				err = refused
			}
			return func(bool) tea.Cmd {
				a.teamsTake(teamsWrote{teams: teams, stamp: stamp, err: err, covers: covers})
				return nil
			}
		}))
	}
	return tea.Batch(cmds...)
}

// teamsTake folds one write or the opening's read in, on the loop.
//
// A LIST FROM THE STORE REPLACES WHAT THE WINDOW HOLDS ONLY WHEN IT IS THE
// NEWEST THING THE WINDOW KNOWS. An edit made since the command left is in
// memory and not in the list, and its own write will answer; taking this list
// would draw that edit undone for the beat between. A refused write is said
// once, and the window keeps what it holds, which is the person's change; an
// edit the file refused is left out of what was written and said the same way.
func (a *app) teamsTake(w teamsWrote) {
	if w.read {
		a.teamsDisk.fetching = false
	} else {
		a.traffic.wrote = w.covers
	}
	if w.err != nil {
		if !w.read {
			a.note("the teams are kept for this window, but " + w.err.Error())
		}
		return
	}
	if w.covers != a.traffic.edits || w.read && a.wall.loaded {
		return
	}
	if w.read {
		a.wall.loaded, a.wall.activeID = true, ""
	}
	if w.stamp != "" {
		a.teamAdopt(teamsClone(w.teams))
		// AND THE FILE AS THIS LEFT IT IS THE ONE THE TRAFFIC CLOCK HAS SEEN,
		// so its next turn does not read back what this window just wrote.
		a.traffic.stamp = w.stamp
	}
	a.touch()
}
