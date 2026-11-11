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
	// TeamID is the id of the team whose log holds the line, for a post up
	// (the one answer that names it); "" for every other answer.
	TeamID string
	// Post is the traffic entry the line was written as and Answers the one it
	// answers, both entry ids; "" when the answer named none (an older build's
	// post up to a parent team's manager was not numbered).
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
	teamPostUpLead = "Posted to the manager of "
	teamPostUpTail = " answers to while it has no manager of its own. It arrives at the start of its next step."
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
// team above: that team by name and id, the entry the line was written as in
// its log and the one it answers (ids or ""), and the team it came from. An
// older build said only the two names.
func teamPostUpSaid(boss, bossID, own, answers, team string) string {
	said := teamPostUpLead + strconv.Quote(boss)
	if bossID != "" {
		said += " (team " + bossID + ")"
	}
	if own != "" {
		said += " as " + teams.ThreadNumber(own)
	}
	if answers != "" {
		said += ", answering " + teams.ThreadNumber(answers)
	}
	return said + ", which " + strconv.Quote(team) + teamPostUpTail
}

// readTeamPostUp is ReadTeamPost for a post up; false for any other answer.
func readTeamPostUp(answer string) (TeamPost, bool) {
	rest, ok := strings.CutPrefix(answer, teamPostUpLead)
	if !ok {
		return TeamPost{}, false
	}
	boss, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return TeamPost{}, false
	}
	post := TeamPost{To: teams.ToManager, Up: true}
	post.Team, _ = strconv.Unquote(boss)
	rest = rest[len(boss):]
	if after, ok := strings.CutPrefix(rest, " (team "); ok {
		id, tail, ok := strings.Cut(after, ")")
		if !ok || id == "" {
			return TeamPost{}, false
		}
		post.TeamID, rest = id, tail
	}
	if after, ok := strings.CutPrefix(rest, " as "); ok {
		number, tail, _ := strings.Cut(after, ",")
		id, ok := teams.ThreadID(number)
		if !ok {
			return TeamPost{}, false
		}
		post.Post, rest = id, ","+tail
	}
	if after, ok := strings.CutPrefix(rest, ", answering "); ok {
		number, tail, _ := strings.Cut(after, ",")
		id, ok := teams.ThreadID(number)
		if !ok {
			return TeamPost{}, false
		}
		post.Answers, rest = id, ","+tail
	}
	from, ok := strings.CutPrefix(rest, ", which ")
	if !ok {
		return TeamPost{}, false
	}
	quoted, err := strconv.QuotedPrefix(from)
	if err != nil {
		return TeamPost{}, false
	}
	post.From, _ = strconv.Unquote(quoted)
	// The sentence is read back only when writing the facts says it again.
	if teamPostUpSaid(post.Team, post.TeamID, post.Post, post.Answers, post.From) != answer {
		return TeamPost{}, false
	}
	return post, true
}

// ReadTeamPost is the post a team_post answer records, and false for any
// other answer (a refusal, a failed write, an answer from an older build).
func ReadTeamPost(answer string) (TeamPost, bool) {
	answer = strings.TrimSpace(answer)
	if post, ok := readTeamPostUp(answer); ok {
		return post, true
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
