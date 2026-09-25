package remote

import (
	"encoding/json"
	"errors"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE ENGINE HALF OF THE TEAMS DOORS, AND THE SURFACE HALF ────────────────
//
// wire_teams.go says what the three doors are and why they are shaped for a
// clock. This is what answers them, from the engine's own profile, and what
// asks them. They are in a file of their own for places.go's reason: server.go
// and client.go are where every lane meets.

// teamsPage is the most one Traffic answer carries, whatever was asked: a
// window pages forward from its cursor, so a longer backlog is the next call's.
const teamsPage = 500

// teamsWatch is the engine's stat-before-read memory of the Traffic logs it
// has been asked about, shared by every connection to this process: two windows
// on one team ask about the same log, and one stat answers both.
var teamsWatch teamstore.Watch

// teamsCall answers the three teams methods, and says whether the method was
// one of them at all. A false hands the call on to the refusal an engine from
// before these doors answers.
func (s *server) teamsCall(call Frame) (json.RawMessage, bool, error) {
	switch call.Method {
	case MethodTeamsRead, MethodTeamsUpdate, MethodTeamsTraffic:
	default:
		return nil, false, nil
	}
	sess := s.session
	sess.mu.Lock()
	dir := sess.engine.ProfileDir
	sess.mu.Unlock()

	switch call.Method {
	case MethodTeamsRead:
		args, err := arg[TeamsReadArgs](call)
		if err != nil {
			return nil, true, err
		}
		reading, err := teamsRead(dir, args)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(reading)
		return payload, true, err

	case MethodTeamsUpdate:
		args, err := arg[TeamsUpdateArgs](call)
		if err != nil {
			return nil, true, err
		}
		reading, err := teamsUpdate(dir, args)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(reading)
		return payload, true, err

	default:
		args, err := arg[TeamsTrafficArgs](call)
		if err != nil {
			return nil, true, err
		}
		limit := args.Limit
		if limit <= 0 || limit > teamsPage {
			limit = teamsPage
		}
		entries, stamp, err := teamsWatch.Traffic(dir, args.Team, args.After, limit)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(TeamsTraffic{Entries: entries, Stamp: stamp})
		return payload, true, err
	}
}

// teamsRead is [MethodTeamsRead]: a stat, and a read only when the stamp is
// not the one the window holds.
//
// THE STAMP ANSWERED IS THE ONE TAKEN BEFORE THE READ. A write that lands
// between the stat and the read is then in the list with an older stamp, and
// the window's next question reads the file once more; the other order would
// hand back an older list with a newer stamp, and the window would be told
// "same" about a change it never saw. A colour repair the load writes back
// moves the stamp the same way, and costs the same one extra read.
func teamsRead(dir string, args TeamsReadArgs) (TeamsReading, error) {
	stamp := teamstore.Stamp(dir)
	if args.Stamp != "" && args.Stamp == stamp {
		return TeamsReading{Stamp: stamp, Same: true}, nil
	}
	f, err := teamstore.LoadHued(dir, args.Reserved)
	if err != nil {
		// AN UNREADABLE FILE IS MOVED ASIDE, NOT OVERWRITTEN, exactly as the
		// local seam does (internal/tui3's localTeams). Every ordinary launch
		// reads teams through this door, so without it a bad file refused every
		// team edit for good; renamed to teams.json.unreadable-<nanos> it
		// survives for a person to recover, and the window starts empty.
		if _, aside := teamstore.SetAside(dir); aside != nil {
			return TeamsReading{}, err
		}
		return TeamsReading{Stamp: teamstore.Stamp(dir)}, nil
	}
	return TeamsReading{Stamp: stamp, Teams: f.Teams}, nil
}

// teamsUpdate is [MethodTeamsUpdate]: the list written whole, under the store's
// lock, only while the file is at the window's base.
func teamsUpdate(dir string, args TeamsUpdateArgs) (TeamsReading, error) {
	f, stamp, err := teamstore.ChangeIf(dir, args.Base, func(f *teamstore.File) error {
		f.Teams = args.Teams
		return nil
	})
	if errors.Is(err, teamstore.ErrStale) {
		return TeamsReading{Stamp: teamstore.Stamp(dir), Stale: true}, nil
	}
	if err != nil {
		return TeamsReading{}, err
	}
	return TeamsReading{Stamp: stamp, Teams: f.Teams}, nil
}

// TeamsRead asks for the engine machine's teams file, telling it the stamp this
// window holds ("" for none); a reading with Same set carries no teams.
func (c *Client) TeamsRead(stamp string, reserved []float64) (TeamsReading, error) {
	payload, err := c.call(nil, MethodTeamsRead, TeamsReadArgs{Stamp: stamp, Reserved: reserved})
	if err != nil {
		return TeamsReading{}, err
	}
	var out TeamsReading
	err = json.Unmarshal(payload, &out)
	return out, err
}

// TeamsUpdate writes teams as the engine machine's whole teams file while it is
// still at stamp base. A reading with Stale set wrote nothing.
func (c *Client) TeamsUpdate(base string, teams []teamstore.Team) (TeamsReading, error) {
	if teams == nil {
		teams = []teamstore.Team{}
	}
	payload, err := c.call(nil, MethodTeamsUpdate, TeamsUpdateArgs{Base: base, Teams: teams})
	if err != nil {
		return TeamsReading{}, err
	}
	var out TeamsReading
	err = json.Unmarshal(payload, &out)
	return out, err
}

// TeamsTraffic reads one team's log on the engine machine after a cursor.
func (c *Client) TeamsTraffic(team, after string, limit int) (TeamsTraffic, error) {
	payload, err := c.call(nil, MethodTeamsTraffic, TeamsTrafficArgs{Team: team, After: after, Limit: limit})
	if err != nil {
		return TeamsTraffic{}, err
	}
	var out TeamsTraffic
	err = json.Unmarshal(payload, &out)
	return out, err
}
