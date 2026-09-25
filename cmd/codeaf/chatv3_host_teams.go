package main

import (
	"errors"
	"sync"

	"github.com/Agent-Field/codeaf/internal/remote"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// hostTeams is [tui3.TeamsSeam] over the wire: the ENGINE machine's teams file
// and Traffic logs, which are where the far session's team tools keep them
// (internal/remote's wire_teams.go).
//
// IT IS BUILT LIKE [hostStanding], for the one call the surface makes on its
// loop. [tui3.TeamsSeam.Load] is asked at an opening and must not block, so it
// answers what is held and nothing else; when nothing is held yet it says so,
// and the surface asks [tui3.TeamsSeam.ReadSince] off its loop, once, which
// fills what is held. Every other door here is called from a command and may
// take a round trip: the Traffic clock's turn, and the write an edit queued.
// There is no clock of its own: the surface's Traffic clock is the only thing
// that asks again, and it runs only while a managed team is held.
//
// NOTHING HERE READS THIS LAPTOP'S PROFILE. The teams a window over --host
// draws are the far machine's, or none: an engine without the doors gets no
// seam at all ([hostTeamsSeam]), and the surface turns teams off rather than
// falling back to the file on this disk, which the far session never reads.
type hostTeams struct {
	// read, write and traffic are the three wire doors, as closures for
	// [hostStanding]'s reason: a test hands them a conflict without a pipe.
	read    func(stamp string, reserved []float64) (remote.TeamsReading, error)
	write   func(base string, teams []teamstore.Team) (remote.TeamsReading, error)
	traffic func(team, after string, limit int) (remote.TeamsTraffic, error)

	mu sync.Mutex
	// teams and stamp are the last list the engine answered with and the
	// file's stamp it was at; known says there has been one.
	teams []teamstore.Team
	stamp string
	known bool
	// reserved is the palette's reserved hues as the surface last passed
	// them, for a read this seam makes on its own.
	reserved []float64
}

// hostTeamsTries is how many times a write that met another writer is made
// again from a fresh read before it gives up and says so.
const hostTeamsTries = 3

// errHostTeamsBusy is a write that met another writer on every try.
var errHostTeamsBusy = errors.New("the teams on the far machine kept changing while this was written; try again")

func newHostTeams(far hostFar) *hostTeams {
	return &hostTeams{
		read:    far.client.TeamsRead,
		write:   far.client.TeamsUpdate,
		traffic: far.client.TeamsTraffic,
	}
}

// hostTeamsSeam is the seam the --host door hands the surface: the far
// machine's teams when the engine answers the teams doors, and the zero seam
// when it does not or there is no connection. The zero seam over --host is
// teams off (internal/tui3's [app.teamsOff]), never this laptop's file.
func hostTeamsSeam(far hostFar, welcome remote.Welcome) tui3.TeamsSeam {
	if far.client == nil || !welcome.Teams {
		return tui3.TeamsSeam{}
	}
	return newHostTeams(far).seam()
}

// seam is h as the surface's functions.
func (h *hostTeams) seam() tui3.TeamsSeam {
	return tui3.TeamsSeam{Load: h.load, ReadSince: h.readSince, Update: h.update, Traffic: h.readTraffic}
}

// load is [tui3.TeamsSeam.Load]: what is held, now, and never the wire.
func (h *hostTeams) load(reserved []float64) ([]teamstore.Team, string, bool) {
	return h.held(reserved)
}

// held is the list held, a copy, and remembers the reserved hues asked with.
func (h *hostTeams) held(reserved []float64) ([]teamstore.Team, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if reserved != nil {
		h.reserved = append([]float64(nil), reserved...)
	}
	return cloneTeams(h.teams), h.stamp, h.known
}

// keep holds a list the engine answered with.
func (h *hostTeams) keep(teams []teamstore.Team, stamp string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.teams, h.stamp, h.known = cloneTeams(teams), stamp, true
}

// readSince is [tui3.TeamsSeam.ReadSince]: one round trip, whose answer is a
// few bytes when the file is still at since.
func (h *hostTeams) readSince(since string, reserved []float64) ([]teamstore.Team, string, bool, error) {
	reading, err := h.read(since, reserved)
	if err != nil {
		return nil, "", false, err
	}
	if reading.Same {
		return nil, reading.Stamp, true, nil
	}
	h.keep(reading.Teams, reading.Stamp)
	return reading.Teams, reading.Stamp, false, nil
}

// update is [tui3.TeamsSeam.Update] as a compare-and-swap over the wire.
//
// THE LOCK CANNOT CROSS THE WIRE, SO THE WRITE IS CONDITIONAL. The change is
// made to the list held (or read now, when none is), and the whole list goes
// back with the stamp it was made from; the engine writes it only if its file
// is still at that stamp (internal/teams' ChangeIf). A file that moved in
// between (the far session made a manager, set a handle, started a member)
// answers Stale, and the list is read again and the change made again on top
// of it, up to [hostTeamsTries] times. So what another writer did is kept, as
// the local store's read-modify-write keeps it, and nothing is ever written
// over a list this window did not see.
func (h *hostTeams) update(change func(*teamstore.File) error) ([]teamstore.Team, string, error) {
	teams, stamp, known := h.held(nil)
	for try := 0; try < hostTeamsTries; try++ {
		if !known || try > 0 {
			reserved := h.reservedHues()
			fresh, at, _, err := h.readSince("", reserved)
			if err != nil {
				return nil, "", err
			}
			teams, stamp = fresh, at
		}
		f := &teamstore.File{Version: teamstore.Version, Teams: cloneTeams(teams)}
		if err := change(f); err != nil {
			return nil, "", err
		}
		reply, err := h.write(stamp, f.Teams)
		if err != nil {
			return nil, "", err
		}
		if reply.Stale {
			continue
		}
		h.keep(reply.Teams, reply.Stamp)
		return reply.Teams, reply.Stamp, nil
	}
	return nil, "", errHostTeamsBusy
}

// reservedHues is the palette's reserved hues as last passed.
func (h *hostTeams) reservedHues() []float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]float64(nil), h.reserved...)
}

// readTraffic is [tui3.TeamsSeam.Traffic]: one round trip, answered from a
// stat on the engine when the log has not moved.
func (h *hostTeams) readTraffic(team, after string, limit int) ([]teamstore.Entry, error) {
	got, err := h.traffic(team, after, limit)
	if err != nil {
		return nil, err
	}
	return got.Entries, nil
}

// cloneTeams is a copy of teams that shares nothing with it.
func cloneTeams(teams []teamstore.Team) []teamstore.Team {
	if teams == nil {
		return nil
	}
	out := make([]teamstore.Team, len(teams))
	for i, t := range teams {
		out[i] = t.Clone()
	}
	return out
}
