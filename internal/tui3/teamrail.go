package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE RAIL: WHERE A PERSON READS THE TRAFFIC ─────────────────────────────
//
// While the manager is the conversation in front, the right of the body is the
// team's Traffic, newest at the bottom, under a header that says what it is and
// how to put it away:
//
//	Traffic                  hide alt+l
//	◆ → @web  do  take the scope model     2m
//	@parser → @web  fyi  the lexer is in   now
//	@web  asking: may I run the migration  now   (the needs-you amber)
//
// THE RIGHT COLUMN IS TRAFFIC'S FIRST. The task column and the rail are both
// right-hand panels, and two of them side by side left the conversation ninety
// columns on a wide frame and folded the rail to nothing at 110. So while the
// manager is in front and the rail stands, the task column folds to its own
// edge (task.go's [app.railStowed]), with its whisper of what the work is doing
// still in it, and a press on that edge or ctrl+g brings the tasks back by
// putting the Traffic away. It is one column with two things to show, the way
// an editor's side panel is.
//
// PUT AWAY, OR ON A FRAME TOO NARROW FOR IT, the rail is an edge: the word
// `Traffic` down the right, with a quiet count of what arrived since the person
// last had it in front of them. On a wide frame a press on the edge brings the
// column back; on a narrow one it lays the Traffic over the lower part of the
// body as a card with its own border, `Close esc` in its foot, and the top of
// the conversation still in view above it. [trafficKey] does the same from
// the keyboard, and `esc` closes the card.
//
// EVERY ROW WITH SOMEONE BEHIND IT IS A DOOR to that member; a row about the
// whole team (`◆ → all`) is not, and does not light. Under the pointer a row's
// whole text is said in the hint line, with what a press does.
//
// THE FRAME DRAWS THE CACHE AND NOTHING ELSE, and the column's rows are kept
// between frames ([trafficCache]) until the entries, the width, the pointer or
// the minute move.

const (
	// The column takes three tenths of the frame left after the task column's
	// edge, between trafficColsMin and trafficColsMax, and never leaves the
	// conversation under trafficBodyFloor. At 110 columns that is 32 of 108.
	trafficColsMin   = 28
	trafficColsMax   = 44
	trafficBodyFloor = 56
	// trafficGripCols is the edge's width, the task column's own
	// ([railGripCols]).
	trafficGripCols = 2
	// trafficHandleCap is the most cells one address takes on a row.
	trafficHandleCap = 10
	// trafficKey shows or hides the Traffic, and teamManagerKey goes to the
	// team's manager. Both were free in keys.md and in every key map here.
	trafficKey     = "alt+l"
	teamManagerKey = "alt+m"
	// trafficCardShare is how much of the body's height the narrow frame's card
	// takes, in hundredths.
	trafficCardShare = 60
	// trafficWord is the rail's name, in its header, on its edge and on the card.
	trafficWord = "Traffic"
)

// The rail's three shapes on the last frame.
const (
	trafficNone = iota
	trafficColumn
	trafficEdge
	trafficCard
)

// trafficDrawn is where the last frame put the rail, for the pointer. Columns
// are the frame's; rows count from the body's top.
type trafficDrawn struct {
	mode   int
	x0, x1 int
	y0, y1 int
	// lines is, per body row, the member a press there goes to ("" for none),
	// and hints what the hint line says with the pointer on it.
	lines []string
	hints []string
	// hide is the header's `hide` word, on row hideY, and close the card's
	// `Close esc`, on row closeY.
	hide   hudSpan
	hideY  int
	close  hudSpan
	closeY int
}

// trafficCacheKey is everything the column's rows are drawn from.
type trafficCacheKey struct {
	team, last, seen    string
	rows, width, height int
	hot                 int
	hideHot, ascii      bool
	minute              int64
}

// trafficCache is the column's rows as last drawn.
type trafficCache struct {
	key          trafficCacheKey
	out          []string
	lines, hints []string
	hide         hudSpan
}

// ── WHERE IT STANDS ─────────────────────────────────────────────────────────

// trafficColsFor is the column the rail takes from room columns, 0 when it
// cannot stand there.
func trafficColsFor(room int) int {
	cols := min(max(room*3/10, trafficColsMin), trafficColsMax)
	if room-cols < trafficBodyFloor {
		return 0
	}
	return cols
}

// trafficOn reports whether the frame has a rail in any shape: the manager in
// front, on a window that can read the engine's teams. Frame-safe, allocation
// free: it is asked wherever [app.bodyWidth] is.
func (a *app) trafficOn() bool {
	if a.teamsOff() {
		return false
	}
	_, ok := a.teamFrontManaged()
	return ok
}

// trafficTaskEdge is what the task column costs while the Traffic holds the
// right: its edge, where that column would stand at all.
func (a *app) trafficTaskEdge(width int) int {
	if a.railQuiet() || railColsFor(width) == 0 {
		return 0
	}
	return railGripCols
}

// trafficFits reports whether this frame is wide enough for the column.
func (a *app) trafficFits() bool {
	width, _ := a.size()
	return trafficColsFor(width-a.trafficTaskEdge(width)) > 0
}

// trafficHoldsRail reports whether the Traffic has the right-hand column now,
// which is what folds the task column to its edge.
func (a *app) trafficHoldsRail() bool {
	return a.trafficOn() && !a.trafficHidden() && a.trafficFits()
}

// trafficWidth is what the rail costs the conversation, in columns: its column
// where it holds the right, the edge where it is away or cannot stand, and
// nothing without the manager in front.
func (a *app) trafficWidth() int {
	if !a.trafficOn() {
		return 0
	}
	if !a.trafficHidden() {
		width, _ := a.size()
		if cols := trafficColsFor(width - a.trafficTaskEdge(width)); cols > 0 {
			return cols
		}
	}
	return trafficGripCols
}

// trafficOverShowing reports whether the card is laid over the body.
func (a *app) trafficOverShowing() bool {
	return a.traffic.over && a.trafficOn() && !a.trafficFits()
}

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

// trafficFull is an address the rail cut, spelled whole again: a handle is
// `@` and the raw name, and every other address was never cut.
func trafficFull(cut, raw string) string {
	if strings.HasPrefix(cut, "@") {
		return "@" + raw
	}
	return cut
}

// trafficShown reports whether an entry is drawn at all. The person's own
// words are in the manager's conversation already, where they said them.
func trafficShown(e teamstore.Entry) bool {
	return e.Kind != teamstore.KindYou && e.From != teamstore.FromYou
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

// trafficLine is one entry as one painted row width cells wide, the member a
// press on it goes to, and what the hint line says over it.
//
// A DIRECTIVE AND A NOTE DIFFER BY A WORD, dim after the addresses: `do` for
// the one the member is to act on, `fyi` for the one it is to know. Colour
// would say it only to a person who can see colour and who knows the key.
func (a *app) trafficLine(t team, e teamstore.Entry, width int) (string, string, string) {
	pal := a.pal
	arrow := a.linearMark("→", "->")
	text := strings.Join(strings.Fields(e.Text), " ")
	var head, plainHead, tag, target string
	switch e.Kind {
	case teamstore.KindStop, teamstore.KindStart:
		word := "stopped"
		if e.Kind == teamstore.KindStart {
			word = "started"
		}
		plainHead = a.trafficAddr(e.From) + " " + word + " " + a.trafficAddr(e.To)
		head = pal.muted(plainHead)
		target = trafficMemberKey(t, e.To, e.Member)
	case teamstore.KindEvent:
		if text == "" {
			text = e.State
		}
		plainHead = a.trafficAddr(e.From)
		head = pal.ink(plainHead)
		target = trafficMemberKey(t, e.From, e.Member)
	default:
		plainHead = a.trafficAddr(e.From) + " " + arrow + " " + a.trafficAddr(e.To)
		head = pal.ink(a.trafficAddr(e.From)) + pal.dim(" "+arrow+" ") + pal.ink(a.trafficAddr(e.To))
		tag = "fyi"
		if e.Kind == teamstore.KindDirective {
			tag = "do"
		}
		target = trafficMemberKey(t, e.From, "")
		if target == "" {
			target = trafficMemberKey(t, e.To, e.Member)
		}
	}
	asking := trafficAsking(e)
	line := head
	if tag != "" {
		line += "  " + pal.dim(tag)
	}
	if text != "" {
		switch {
		case asking:
			line += "  " + pal.ask(text)
		case e.Kind == teamstore.KindEvent:
			line += "  " + pal.dim(text)
		default:
			line += "  " + pal.muted(text)
		}
	}
	if asking {
		line = pal.ask(plainHead) + strings.TrimPrefix(line, head)
	}
	age := a.trafficAge(e)
	room := width
	if age != "" && width >= 24 {
		room = width - ansi.StringWidth(age) - 1
	} else {
		age = ""
	}
	line = fit(line, room)
	line += strings.Repeat(" ", max(room-ansi.StringWidth(line), 0))
	if age != "" {
		line += " " + pal.dim(age)
	}
	hint := ""
	if target != "" {
		// The hint line has the room the row did not, so it names everyone
		// whole.
		hint = strings.ReplaceAll(plainHead, a.trafficAddr(e.From), trafficFull(a.trafficAddr(e.From), e.From))
		if e.To != "" {
			hint = strings.ReplaceAll(hint, a.trafficAddr(e.To), trafficFull(a.trafficAddr(e.To), e.To))
		}
		if tag != "" {
			hint += " " + tag
		}
		if text != "" {
			hint += ": " + text
		}
		if m, ok := t.Member(target); ok && m.Handle != "" {
			hint += hintSegment + "click opens @" + m.Handle
		} else {
			hint += hintSegment + "click opens it"
		}
	}
	return line, target, hint
}

// trafficVisible is the newest room entries of team t that are drawn, oldest
// first. It allocates only the slice it returns.
func (a *app) trafficVisible(t team, room int) []teamstore.Entry {
	rows := a.traffic.rows[t.ID]
	n := 0
	from := len(rows)
	for from > 0 && n < room {
		from--
		if trafficShown(rows[from]) {
			n++
		}
	}
	out := make([]teamstore.Entry, 0, n)
	for _, e := range rows[from:] {
		if trafficShown(e) {
			out = append(out, e)
		}
	}
	return out
}

// trafficUnseen is how many drawn entries of team t arrived after the newest
// the person had in front of them.
func (a *app) trafficUnseen(t team) int {
	seen := a.traffic.seen[t.ID]
	rows := a.traffic.rows[t.ID]
	n := 0
	for i := len(rows) - 1; i >= 0 && rows[i].ID > seen; i-- {
		if trafficShown(rows[i]) {
			n++
		}
	}
	return n
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
