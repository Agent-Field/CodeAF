package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── WHAT A TEAM SAID TO A CONVERSATION, AS A QUOTED CARD ────────────────────
//
// A line the manager or a teammate addressed to a conversation reaches it
// through the Traffic, at a step boundary, as ONE note the session wrote
// (internal/session's team.go): a sentence saying these are the team's words
// and not the person's, the lines, and the authority rule under them. The
// journal keeps it as the session's own line, and a page opened on the
// conversation draws it here instead of in the dim lane every other session
// line takes: as the card it is, headed by who said it to whom,
//
//	◆ manager → @lexer  do
//	│ rewrite the lexer so string escapes are handled in one pass
//
// and never with the person's `›`, because the person did not say it. A member
// the manager started opens on this card: the brief is its first thing, and
// the card is how a person opening that tab sees why it is working.
//
// The shape parsed is the session's, and only the lines are drawn: the
// sentence above them and the rule under them are for the model.

// teamAsideLead is how the session's team note begins.
const teamAsideLead = "Team traffic in "

// teamCard is one line of a team note: who said it, to whom, whether it was a
// directive, and what was said.
type teamCard struct {
	from, to, tag, text string
	// who is the raw speaker (a handle, or manager), and thread the line's own
	// entry id when the delivery numbered it; both empty on a card read from
	// the note's text alone.
	who, thread string
}

// teamAsideCards reads a session's team note into its lines, and reports
// whether text was one.
func teamAsideCards(text string, mark string) ([]teamCard, bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], teamAsideLead) {
		return nil, false
	}
	self := mark + " manager"
	if at := strings.Index(lines[0], "(@"); at >= 0 {
		if end := strings.Index(lines[0][at:], ")"); end > 0 {
			self = lines[0][at+1 : at+end]
		}
	}
	var cards []teamCard
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "(") {
			break
		}
		if strings.HasPrefix(line, " ") && len(cards) > 0 {
			cards[len(cards)-1].text += "\n" + strings.TrimSpace(line)
			continue
		}
		speaker, said, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		// A line delivered to a member ends its head with its number, "#42".
		if at := strings.LastIndex(speaker, " #"); at >= 0 {
			if _, numbered := teamstore.ThreadID(speaker[at+1:]); numbered {
				speaker = speaker[:at]
			}
		}
		card := teamCard{to: self, text: strings.TrimSpace(said)}
		if who, aim, aimed := strings.Cut(speaker, " to "); aimed {
			speaker = who
			switch aim {
			case "the room":
				card.to = "room"
			case "everyone":
				card.to = "all"
			case "the manager":
				card.to = mark + " manager"
			default:
				card.to = aim
			}
		}
		speaker = strings.TrimSpace(strings.TrimPrefix(speaker, "◆"))
		switch {
		case strings.HasPrefix(speaker, "directive from manager"):
			card.from, card.tag = mark+" manager", "do"
		case strings.HasPrefix(speaker, "from manager"):
			card.from = mark + " manager"
		case strings.HasPrefix(speaker, "from "):
			card.from = strings.TrimPrefix(speaker, "from ")
		default:
			card.from = speaker
		}
		cards = append(cards, card)
	}
	return cards, len(cards) > 0
}

// teamLineCards is the lines the session took apart for a delivery
// ([session.TeamLine]) as cards; the aside's text still says who they were for.
func teamLineCards(lines []session.TeamLine, text, mark string) ([]teamCard, bool) {
	if len(lines) == 0 {
		return nil, false
	}
	self := mark + " manager"
	if at := strings.Index(text, "(@"); at >= 0 {
		if end := strings.Index(text[at:], ")"); end > 0 {
			self = text[at+1 : at+end]
		}
	}
	addr := func(s string) string {
		switch s {
		case "":
			return self
		case teamstore.FromManager:
			return mark + " manager"
		case teamstore.FromYou:
			return "you"
		case teamstore.FromSystem:
			return "codeaf"
		case teamstore.ToRoom:
			return "room"
		case teamstore.ToEveryone:
			return "all"
		}
		return "@" + strings.TrimPrefix(s, "@")
	}
	cards := make([]teamCard, 0, len(lines))
	for _, l := range lines {
		c := teamCard{from: addr(l.From), to: addr(l.To), text: strings.TrimSpace(l.Text), who: l.From, thread: l.Thread}
		if l.Kind == teamstore.KindDirective {
			c.tag = "do"
		}
		cards = append(cards, c)
	}
	return cards, true
}

// teamCardRows draws one team note's lines as quoted cards, width wide.
func (a *app) teamCardRows(e entry, width int) []string {
	cards, ok := teamLineCards(e.team, e.text, a.teamManagerMark())
	if !ok {
		cards, ok = teamAsideCards(e.text, a.teamManagerMark())
	}
	if !ok {
		return []string{a.pal.dim("· " + firstLine(e.text))}
	}
	name := ""
	if len(e.team) > 0 {
		name = e.team[0].Team
	}
	// THE MANAGER IS NOT SHOWN AN ANSWER TWICE: one already under its
	// question's card in this conversation is left out here, and a note left
	// with nothing is one dim line (teamthreadcard.go).
	if t, managed := a.teamFrontManaged(); managed && (name == "" || t.Name == name) {
		kept := cards[:0:0]
		var answered []string
		for _, c := range cards {
			if c.who != "" && c.who != teamstore.FromManager && c.who != teamstore.FromYou && c.who != teamstore.FromSystem &&
				a.teamNoteReplyShown(t, c.who, c.text) {
				if !strings.Contains(strings.Join(answered, " "), "@"+c.who) {
					answered = append(answered, "@"+c.who)
				}
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) == 0 {
			return []string{a.pal.dim("· " + strings.Join(answered, " ") + " answered" + hintSegment + "in the thread above")}
		}
		cards = kept
	}
	mirror, self := a.teamNoteSelf(name)
	pal := a.pal
	arrow := a.linearMark("→", "->")
	bar := a.linearMark("│", "|")
	var out []string
	for i, c := range cards {
		if i > 0 {
			out = append(out, "")
		}
		head := pal.muted(c.from) + pal.dim(" "+arrow+" ") + pal.muted(c.to)
		if strings.HasPrefix(c.from, a.teamManagerMark()) {
			head = pal.accent(a.teamManagerMark()) + pal.muted(strings.TrimPrefix(c.from, a.teamManagerMark())) +
				pal.dim(" "+arrow+" ") + pal.muted(c.to)
		}
		if c.tag != "" {
			head += "  " + pal.dim(c.tag)
		}
		out = append(out, fit(head, width))
		for _, para := range strings.Split(c.text, "\n") {
			for _, line := range wrap(para, max(width-2, 8)) {
				out = append(out, pal.dim(bar+" ")+pal.ink(line))
			}
		}
		// THE MEMBER'S OWN ANSWERS HANG UNDER THE MANAGER'S LINE, muted, as the
		// manager's card has them (teamthreadcard.go).
		if self != "" && c.who == teamstore.FromManager && c.thread != "" {
			if th, found := threadOf(a.traffic.rows[mirror.ID], c.thread); found {
				for _, r := range a.threadReplyRows(mirror.ID, th, self, width, func(string) bool { return false }) {
					out = append(out, r.text)
				}
			}
		}
	}
	return out
}

// asideShape is how a surface draws one session aside, decided once for the
// reopened conversation (replay.go) and the wall's tiles (wallmini.go,
// walltail.go), so a tile shows what the conversation shows.
type asideShape uint8

const (
	// asideLine is every other aside: its first line, in the session's lane.
	asideLine asideShape = iota
	// asideTeam is a team delivery: the lines as cards, never the sentence
	// the session put above them for the model.
	asideTeam
	// asideHidden is a team wake with nothing delivered in it: the note that
	// started a turn nobody typed, which the live conversation never draws
	// (followup.go), so no surface draws it.
	asideHidden
)

// asideShapeOf is [asideShape] for one aside's entry.
func asideShapeOf(e session.DisplayEntry) asideShape {
	text := strings.TrimSpace(e.Text)
	switch {
	case len(e.Team) > 0 || strings.HasPrefix(text, teamAsideLead):
		return asideTeam
	case session.TeamWakeNote(text):
		return asideHidden
	}
	return asideLine
}

// teamCardsOf is a team delivery's lines as cards: the session's own parse
// when the entry carries it, the note's text read back otherwise.
func teamCardsOf(e session.DisplayEntry, mark string) []teamCard {
	cards, ok := teamLineCards(e.Team, e.Text, mark)
	if !ok {
		cards, _ = teamAsideCards(e.Text, mark)
	}
	return cards
}
