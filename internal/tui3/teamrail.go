package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TRAFFIC'S WORDS ─────────────────────────────────────────────────────
//
// The Traffic is read on the side column, as its second word (sidecol.go and
// sidetraffic.go draw it). This file is the vocabulary both it and the thread
// cards in the conversation spell an entry with: an address, whether an entry
// is drawn at all, whether it is a member asking the person, how old it is,
// and what the person has already had in front of them.
//
// EVERY HANDLE IS A DOOR to its member, and a message's words are a door to
// the message; a word about the whole team (`everyone`) is not, and does not
// light. Under the pointer the hint line says what a press does, and over a
// message its whole text.

const (
	// trafficHandleCap is the most cells one address takes on a row.
	trafficHandleCap = 10
	// trafficKey shows or hides the side column, and teamManagerKey goes to the
	// team's manager. Both were free in keys.md and in every key map here.
	trafficKey     = "alt+l"
	teamManagerKey = "alt+m"
)

// ── ONE ROW ─────────────────────────────────────────────────────────────────

// trafficAddr is one address of an entry as the rail spells it: `@handle`, the
// manager's mark, or the word for everyone, cut to [trafficHandleCap].
func (a *app) trafficAddr(s string) string {
	switch s {
	case teamstore.FromManager:
		return a.teamManagerMark()
	case teamstore.FromSystem:
		return "codeaf"
	case teamstore.ToEveryone:
		return "all"
	case teamstore.ToRoom:
		return "room"
	case "":
		return ""
	}
	word := "@" + s
	if ansi.StringWidth(word) > trafficHandleCap {
		word = ansi.Truncate(word, trafficHandleCap, a.linearMark("…", "~"))
	}
	return word
}

// trafficShown reports whether an entry is drawn at all. The person's own
// words are in the manager's conversation already, where they said them. A
// wake is drawn only as the `working…` of the thread it answers
// (teamthread.go), so one that answers nothing, and a member waking the
// manager, which the manager's own turn already shows, are not drawn.
func trafficShown(e teamstore.Entry) bool {
	// A RULING THE PERSON MADE is still a ruling every party's log carries,
	// so it is drawn, as `you ruling → …` (teamthread.go).
	if teamstore.IsRuling(e) {
		return true
	}
	if e.Kind == teamstore.KindYou || e.From == teamstore.FromYou {
		return false
	}
	if e.Wake() {
		return e.Answers != "" && e.From == teamstore.FromManager
	}
	return true
}

// trafficAsking reports whether an event is a member waiting on the person,
// the one row on the rail in the needs-you amber.
func trafficAsking(e teamstore.Entry) bool {
	if e.Kind != teamstore.KindEvent {
		return false
	}
	if e.State != "" {
		return e.State == teamstore.StateAsking
	}
	text := strings.ToLower(strings.TrimSpace(e.Text))
	return strings.HasPrefix(text, "ask") || strings.HasPrefix(text, "needs you")
}

// trafficAge is how long ago an entry was written, in the fewest cells.
func (a *app) trafficAge(e teamstore.Entry) string {
	if e.At.IsZero() {
		return ""
	}
	d := a.now().Sub(e.At)
	switch {
	case d < 60e9:
		return "now"
	case d < 3600e9:
		return itoa(int(d/60e9)) + "m"
	case d < 86400e9:
		return itoa(int(d/3600e9)) + "h"
	}
	return itoa(int(d/86400e9)) + "d"
}

// trafficMarkSeen records that the person has team t's newest entry in front of
// them. It is memory, written by the frame that showed it.
func (a *app) trafficMarkSeen(t team) {
	rows := a.traffic.rows[t.ID]
	if len(rows) == 0 {
		return
	}
	if a.traffic.seen == nil {
		a.traffic.seen = map[string]string{}
	}
	a.traffic.seen[t.ID] = rows[len(rows)-1].ID
}

// trafficRulingText is a ruling's words without the lead the store writes
// (`ruling on the conflict p…, `), which the row's head already says.
func trafficRulingText(text string) string {
	const lead = "ruling on the conflict "
	if !strings.HasPrefix(text, lead) {
		return text
	}
	rest := text[len(lead):]
	if at := strings.Index(rest, ", "); at >= 0 && at < 40 {
		return rest[at+2:]
	}
	return rest
}
