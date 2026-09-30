package tui3

import (
	"strings"
	"time"

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

// trafficAge is how long ago an entry was written. Through a day it is the
// same few cells a task row and a home session use ([sinceAt]): `now`, `2m`,
// `3h`, `1d`. Past that a Traffic row keeps a compact age ([trafficAgeAt]),
// because [sinceAt] prints a calendar date and a date in this margin is a
// second clock.
func (a *app) trafficAge(e teamstore.Entry) string {
	return trafficAgeAt(e.At, a.now())
}

// trafficAgeAt is [sinceAt] for the spans where both say a count, and a
// compact count after that.
//
// A TASK ROW AND A HOME SESSION PRINT A DATE PAST THIRTY DAYS, and that is
// the right answer on a list of sessions a person is picking by the month.
// A Traffic row is a few cells beside a message. `2 Jan` there does not say
// how stale the line is, so the row stays on the count: days through the
// forty-first (`41d`, which includes the thirtieth day [sinceAt] already
// gives up), weeks from six weeks (`6w`, `12w`) until a year, then years
// (`1y`). Six weeks is the first week reading, so the number does not step
// backwards from `41d` to `4w`. The count truncates, so a row never claims
// to be older than it is.
func trafficAgeAt(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	// A missing latch is the wall clock, the same fallback [sinceAt] takes,
	// and the reading is still this ladder rather than a date.
	if now.IsZero() {
		now = time.Now()
	}
	d := now.Sub(at)
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < day:
		return itoa(int(d/time.Hour)) + "h"
	case d < 42*day:
		return itoa(int(d/day)) + "d"
	case d < 365*day:
		return itoa(int(d/(7*day))) + "w"
	default:
		return itoa(int(d/(365*day))) + "y"
	}
}

// trafficBelongsTo is the conversation a Traffic entry was written in. A
// message the sender wrote belongs in the sender's chat: the manager's mark
// is the manager, a handle is that member. A message put to the person
// belongs in the manager's chat, which is where the person reads it.
func (a *app) trafficBelongsTo(t team, e teamstore.Entry) string {
	if e.To == teamstore.ToYou {
		return t.Manager
	}
	switch e.From {
	case teamstore.FromManager, teamstore.FromYou, teamstore.FromSystem, "":
		return t.Manager
	}
	if m, ok := t.ByHandle(e.From); ok {
		return m.Key
	}
	return t.Manager
}

// trafficSenderWord is who a row says the message is from, the same word the
// row draws: the manager's mark, `you` for this member, `codeaf`, or `@handle`.
func (a *app) trafficSenderWord(from, self string) string {
	if self != "" && (from == self || from == "@"+self) {
		return "you"
	}
	switch from {
	case teamstore.FromManager:
		return a.teamManagerMark()
	case teamstore.FromYou:
		return "you"
	case teamstore.FromSystem:
		return "codeaf"
	case "":
		return "it"
	}
	return "@" + strings.TrimPrefix(from, "@")
}

// trafficOpenHint is the hint over a row that opens a message: who wrote it,
// how long ago, then whatever else the row gave up, then the click.
func trafficOpenHint(sender, age, extra string) string {
	head := "Open " + sender + "'s message"
	if sender == "you" {
		head = "Open your message"
	}
	out := head
	switch age {
	case "":
	case "now":
		out += hintSegment + "now"
	default:
		out += hintSegment + age + " ago"
	}
	if extra != "" {
		out += hintSegment + extra
	}
	return out + hintSegment + "click"
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
