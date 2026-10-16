package session

// WHAT A TEAM POST SAID IT DID, AS FACTS.
//
// team_post answers its model with one sentence (tools_team.go, team_nest.go):
// where the line went, in which team, the number it was written as and the
// number it answers. That sentence is the only record a conversation keeps of
// the post, so a surface that wants to say "reported to the manager" from the
// fact rather than from the agent's own prose reads it back here, next to the
// two writers that compose it, which are the only code that knows the shape.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// TeamPost is one delivered team_post as its answer recorded it.
type TeamPost struct {
	// Team is the name of the team whose log the line was written to.
	Team string
	// To is "manager", "room" or a teammate's handle (no @).
	To string
	// Post is the traffic entry the line was written as and Answers the one it
	// answers, both entry ids; "" when the answer named none (a post up to a
	// parent team's manager is not numbered).
	Post    string
	Answers string
	// Up is a post from a team with no manager of its own to the manager of
	// the team above; Team is then that team, From the team it came from.
	Up   bool
	From string
}

const (
	teamPostLead   = "Posted to "
	teamPostArrive = ". It arrives at the start of their next step."
	teamPostUpFmt  = "Posted to the manager of %q, which %q answers to while it has no manager of its own. It arrives at the start of its next step."
)

// teamPostSaid is a delivered post's answer: where is "the room", "the
// manager" or "@handle"; own and answers are entry ids or "".
func teamPostSaid(where, team, own, answers string) string {
	said := teamPostLead + where + " in " + strconv.Quote(team)
	if own != "" {
		said += " as " + teams.ThreadNumber(own)
	}
	if answers != "" {
		said += ", answering " + teams.ThreadNumber(answers)
	}
	return said + teamPostArrive
}

// teamPostUpSaid is a post's answer when it went up to the manager of the
// team above.
func teamPostUpSaid(boss, team string) string {
	return fmt.Sprintf(teamPostUpFmt, boss, team)
}

// ReadTeamPost is the post a team_post answer records, and false for any
// other answer (a refusal, a failed write, an answer from an older build).
func ReadTeamPost(answer string) (TeamPost, bool) {
	answer = strings.TrimSpace(answer)
	var boss, from string
	if n, err := fmt.Sscanf(answer, "Posted to the manager of %q, which %q answers", &boss, &from); err == nil && n == 2 {
		if teamPostUpSaid(boss, from) == answer {
			return TeamPost{Team: boss, To: teams.ToManager, Up: true, From: from}, true
		}
	}
	rest, ok := strings.CutPrefix(answer, teamPostLead)
	if !ok {
		return TeamPost{}, false
	}
	rest, ok = strings.CutSuffix(rest, teamPostArrive)
	if !ok {
		return TeamPost{}, false
	}
	where, rest, ok := strings.Cut(rest, " in ")
	if !ok {
		return TeamPost{}, false
	}
	post := TeamPost{}
	switch {
	case where == "the room":
		post.To = teams.ToRoom
	case where == "the manager":
		post.To = teams.ToManager
	case strings.HasPrefix(where, "@") && len(where) > 1 && !strings.Contains(where, " "):
		post.To = where[1:]
	default:
		return TeamPost{}, false
	}
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return TeamPost{}, false
	}
	post.Team, _ = strconv.Unquote(quoted)
	rest = rest[len(quoted):]
	if after, ok := strings.CutPrefix(rest, " as "); ok {
		number, tail, _ := strings.Cut(after, ",")
		id, ok := teams.ThreadID(number)
		if !ok {
			return TeamPost{}, false
		}
		post.Post, rest = id, ""
		if tail != "" {
			rest = "," + tail
		}
	}
	if after, ok := strings.CutPrefix(rest, ", answering "); ok {
		id, ok := teams.ThreadID(after)
		if !ok {
			return TeamPost{}, false
		}
		post.Answers, rest = id, ""
	}
	if rest != "" {
		return TeamPost{}, false
	}
	// The sentence is read back only when writing the facts says it again.
	where = "the room"
	switch post.To {
	case teams.ToManager:
		where = "the manager"
	case teams.ToRoom:
	default:
		where = "@" + post.To
	}
	if teamPostSaid(where, post.Team, post.Post, post.Answers) != answer {
		return TeamPost{}, false
	}
	return post, true
}
