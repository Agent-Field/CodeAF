package remote

import (
	"encoding/json"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE ENGINE HALF OF THE DELEGATION DOORS, AND THE SURFACE HALF ───────────
//
// wire_delegation.go says what the doors are. This answers them from the
// engine's own profile and asks them, in a file of its own for teams.go's
// reason.

// delegationCall answers the delegation methods, and says whether the method
// was one of them at all.
func (s *server) delegationCall(call Frame) (json.RawMessage, bool, error) {
	switch call.Method {
	case MethodTeamsDefaults, MethodTeamsPackets, MethodTeamsRaise, MethodTeamsDecide,
		MethodTeamsEscalate, MethodTeamsSpend, MethodTeamsDelete,
		MethodTeamsWrapUp, MethodTeamsAcceptClosing:
	default:
		return nil, false, nil
	}
	sess := s.session
	sess.mu.Lock()
	dir := sess.engine.ProfileDir
	sess.mu.Unlock()
	answer, err := delegationAnswer(dir, call)
	if err != nil {
		return nil, true, err
	}
	payload, err := json.Marshal(answer)
	return payload, true, err
}

// delegationAnswer is one delegation method answered from dir.
func delegationAnswer(dir string, call Frame) (any, error) {
	switch call.Method {
	case MethodTeamsDefaults:
		return teamstore.DefaultsAt(dir), nil

	case MethodTeamsPackets:
		args, err := arg[PacketsArgs](call)
		if err != nil {
			return nil, err
		}
		// The stamp is taken before the read, for teamsRead's reason.
		stamp := teamstore.PacketsStamp(dir)
		if args.Stamp != "" && args.Stamp == stamp {
			return PacketsReading{Stamp: stamp, Same: true}, nil
		}
		packets, _, err := teamstore.OpenPackets(dir, args.Scope)
		if err != nil {
			return nil, err
		}
		return PacketsReading{Stamp: stamp, Packets: packets}, nil

	case MethodTeamsRaise:
		args, err := arg[teamstore.Packet](call)
		if err != nil {
			return nil, err
		}
		return teamstore.Raise(dir, args)

	case MethodTeamsDecide:
		args, err := arg[DecideArgs](call)
		if err != nil {
			return nil, err
		}
		return teamstore.Decide(dir, args.ID, args.By, args.Decision, args.Reason)

	case MethodTeamsEscalate:
		args, err := arg[EscalateArgs](call)
		if err != nil {
			return nil, err
		}
		return teamstore.Escalate(dir, args.ID, args.By, args.To, args.Reason)

	case MethodTeamsSpend:
		args, err := arg[SpendArgs](call)
		if err != nil {
			return nil, err
		}
		day := args.Day
		if day == "" {
			day = teamstore.Today()
		}
		stamp := teamstore.TeamSpendStamp(dir, args.Team, day)
		if args.Stamp != "" && args.Stamp == stamp {
			return SpendReading{Stamp: stamp, Same: true}, nil
		}
		spend, err := teamstore.TeamSpend(dir, args.Team, day)
		if err != nil {
			return nil, err
		}
		return SpendReading{Stamp: stamp, Spend: &spend}, nil

	case MethodTeamsWrapUp:
		args, err := arg[WrapUpArgs](call)
		if err != nil {
			return nil, err
		}
		return struct{}{}, teamstore.AppendTraffic(dir, args.Team, teamstore.WrapUpRequest(args.Text))

	case MethodTeamsAcceptClosing:
		args, err := arg[AcceptClosingArgs](call)
		if err != nil {
			return nil, err
		}
		p, err := teamstore.PacketByID(dir, args.ID)
		if err != nil {
			return nil, err
		}
		closed, err := teamstore.AcceptClosing(dir, p)
		if err != nil {
			return nil, err
		}
		return AcceptClosingReply{Closed: closed, Stamp: teamstore.Stamp(dir)}, nil

	default: // MethodTeamsDelete
		args, err := arg[DeleteTeamArgs](call)
		if err != nil {
			return nil, err
		}
		gone, err := teamstore.Delete(dir, args.Team)
		if err != nil {
			return nil, err
		}
		return DeleteTeamReply{Gone: gone, Stamp: teamstore.Stamp(dir)}, nil
	}
}

// ── THE SURFACE HALF ────────────────────────────────────────────────────────

// TeamsDefaults is the engine profile's `teams.` defaults.
func (c *Client) TeamsDefaults() (teamstore.Defaults, error) {
	return delegationAsk[teamstore.Defaults](c, MethodTeamsDefaults, struct{}{})
}

// TeamsPackets is the packets waiting on scope, or Same when the packet files
// are still at stamp.
func (c *Client) TeamsPackets(scope, stamp string) (PacketsReading, error) {
	return delegationAsk[PacketsReading](c, MethodTeamsPackets, PacketsArgs{Scope: scope, Stamp: stamp})
}

// TeamsRaise records p on the engine and answers it as written.
func (c *Client) TeamsRaise(p teamstore.Packet) (teamstore.Packet, error) {
	return delegationAsk[teamstore.Packet](c, MethodTeamsRaise, p)
}

// TeamsDecide records a decision on the engine.
func (c *Client) TeamsDecide(id, by, decision, reason string) (teamstore.Packet, error) {
	return delegationAsk[teamstore.Packet](c, MethodTeamsDecide, DecideArgs{ID: id, By: by, Decision: decision, Reason: reason})
}

// TeamsEscalate sends a packet up on the engine.
func (c *Client) TeamsEscalate(id, by, to, reason string) (teamstore.Packet, error) {
	return delegationAsk[teamstore.Packet](c, MethodTeamsEscalate, EscalateArgs{ID: id, By: by, To: to, Reason: reason})
}

// TeamsSpend is team's spend on day ("" the engine's today), or Same when
// nothing moved since stamp.
func (c *Client) TeamsSpend(team, day, stamp string) (SpendReading, error) {
	return delegationAsk[SpendReading](c, MethodTeamsSpend, SpendArgs{Team: team, Day: day, Stamp: stamp})
}

// TeamsDelete forgets a closed team on the engine.
func (c *Client) TeamsDelete(team string) (DeleteTeamReply, error) {
	return delegationAsk[DeleteTeamReply](c, MethodTeamsDelete, DeleteTeamArgs{Team: team})
}

// TeamsWrapUp asks the engine's manager of team to wrap up.
func (c *Client) TeamsWrapUp(team, text string) error {
	_, err := delegationAsk[struct{}](c, MethodTeamsWrapUp, WrapUpArgs{Team: team, Text: text})
	return err
}

// TeamsAcceptClosing closes the team a decided closing packet reports on,
// on the engine.
func (c *Client) TeamsAcceptClosing(id string) (AcceptClosingReply, error) {
	return delegationAsk[AcceptClosingReply](c, MethodTeamsAcceptClosing, AcceptClosingArgs{ID: id})
}

// delegationAsk is one round trip decoded as T.
func delegationAsk[T any](c *Client, method string, args any) (T, error) {
	var out T
	payload, err := c.call(nil, method, args)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(payload, &out)
	return out, err
}
