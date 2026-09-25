package session

// WHAT A SURFACE DRAWS FOR A TEAM DELIVERY.
//
// A delivery ([Agent.teamBoundary]) is journaled as one of the session's own
// notes, so a replayed page gets it as an "aside" ([DisplayEntry]) and never as
// the person's words. A dim one-line aside is right for a task landing and wrong
// for a manager's brief, which is the whole assignment a new member was started
// with and deserves to be read as a quoted card. So the aside carries the lines
// it delivered, taken apart here, next to the composer that put them together
// ([teamLine], [teamNewsGroup]), which is the only code that knows the shape.
// A surface draws [DisplayEntry.Team] when it is there and the aside's text
// when it is not.

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// TeamLine is one line of team traffic as a conversation was handed it.
type TeamLine struct {
	// Team is the team's name as the delivery named it.
	Team string
	// From is "manager", a member's handle (no @), "you" or "system".
	From string
	// To is where the line was aimed: "room", "everyone", "manager" or a
	// member's handle, and "" for the conversation reading it.
	To string
	// Kind is [teams.KindNote], [teams.KindDirective], or [teams.KindStart] for
	// the brief a manager started this conversation with.
	Kind string
	// Text is the line's words, whole lines kept.
	Text string
	// Thread is the line's own entry id when the delivery numbered it ("#42",
	// which a member is told so its reply can name it), "" when it did not.
	Thread string
}

// teamNewsLead opens every group of a delivery ([teamNewsGroup]).
const teamNewsLead = "Team traffic in "

// teamNewsLines is a delivery's lines, nil for text that is not one. A note
// that woke a turn opens with a sentence of its own and carries the delivery
// under it (team_wakewatch.go), so a delivery is found at the start of any
// line, not only the first.
func teamNewsLines(text string) []TeamLine {
	if !strings.HasPrefix(text, teamNewsLead) && !strings.Contains(text, "\n"+teamNewsLead) {
		return nil
	}
	var (
		out  []TeamLine
		team string
		open bool
	)
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, teamNewsLead):
			quoted, err := strconv.QuotedPrefix(strings.TrimPrefix(line, teamNewsLead))
			if err != nil {
				open = false
				continue
			}
			team, _ = strconv.Unquote(quoted)
			open = true
		case !open || line == "":
		case strings.HasPrefix(line, "("):
			// The group's closing sentence, the authority law.
			open = false
		case strings.HasPrefix(line, "    ") && len(out) > 0:
			out[len(out)-1].Text += "\n" + strings.TrimPrefix(line, "    ")
		default:
			if parsed, ok := teamLineParts(line); ok {
				parsed.Team = team
				out = append(out, parsed)
			}
		}
	}
	return out
}

// teamLineParts takes one delivered line apart: who, where, what kind, and
// the words. The head before the first ": " holds no colon, because a handle
// is letters and digits.
func teamLineParts(line string) (TeamLine, bool) {
	head, text, ok := strings.Cut(line, ": ")
	if !ok {
		return TeamLine{}, false
	}
	parsed := TeamLine{Kind: teams.KindNote, Text: text}
	switch {
	case strings.HasPrefix(head, teamBriefWord):
		parsed.From, parsed.Kind, head = teams.FromManager, teams.KindStart, strings.TrimPrefix(head, teamBriefWord)
	case strings.HasPrefix(head, "◆ directive from manager"):
		parsed.From, parsed.Kind, head = teams.FromManager, teams.KindDirective, strings.TrimPrefix(head, "◆ directive from manager")
	case strings.HasPrefix(head, "◆ from manager"):
		parsed.From, head = teams.FromManager, strings.TrimPrefix(head, "◆ from manager")
	case strings.HasPrefix(head, "from the person"):
		parsed.From, head = teams.FromYou, strings.TrimPrefix(head, "from the person")
	case strings.HasPrefix(head, "from codeaf"):
		parsed.From, head = teams.FromSystem, strings.TrimPrefix(head, "from codeaf")
	case strings.HasPrefix(head, "from @"):
		rest := strings.TrimPrefix(head, "from @")
		handle, after, _ := strings.Cut(rest, " ")
		if handle == "" {
			return TeamLine{}, false
		}
		parsed.From, head = handle, ""
		if after != "" {
			head = " " + after
		}
	default:
		return TeamLine{}, false
	}
	// A line delivered to a member ends its head with the line's number.
	if at := strings.LastIndex(head, " #"); at >= 0 {
		if id, ok := teams.ThreadID(head[at+1:]); ok {
			parsed.Thread, head = id, head[:at]
		}
	}
	switch head {
	case "":
	case " to the room":
		parsed.To = teams.ToRoom
	case " to everyone":
		parsed.To = teams.ToEveryone
	case " to the manager":
		parsed.To = teams.ToManager
	default:
		handle, ok := strings.CutPrefix(head, " to @")
		if !ok || handle == "" || strings.Contains(handle, " ") {
			return TeamLine{}, false
		}
		parsed.To = handle
	}
	return parsed, true
}
