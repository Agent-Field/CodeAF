package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE RAIL'S THREADS (teamrail.go says what the rail is) ─────────────────
//
// The Traffic is drawn as threads (internal/teams' thread.go): a message and
// what answered it, the thread that moved last at the TOP, and inside each one
// in the order it happened.
//
//	◆ manager → @agent @checking @review  do   2m
//	  Please provide a brief status update…
//	├ @checking  ✓ Status update: the lexer…   1m
//	├ @agent  working…
//	└ @review  ✓ finished                      now
//
// ONE QUESTION IS ONE THREAD, NOT TWELVE ROWS. The wake that started a member
// is not a row: it is why the member reads `working…` until it answers. A
// member's finishing is not a row: it is the `✓` on its reply, or its own line
// when it said nothing. A failure is `✗`; a member asking the person is the one
// line in the needs-you amber. A stop, a start with no answers yet, a handle
// changing and every entry written before threads stay one line each.
//
// EVERY HANDLE IS A LINK, inked and grounded exactly as a handle in the chat
// is (teamlink.go): a press opens the member, resumed first when this window
// does not hold it, and the hint line names it. A message's words are a door
// of their own: under the pointer the hint line says them whole, and a press
// lays them out in full under the row, and a second press folds them again.
//
// THE FRAME DRAWS THE CACHE. All of this is worked out when the rail's cache
// key moves ([trafficCacheKey]), off the entries already in memory.

// trafficDoor is one pressable span of a rail row: a member a press opens, or
// an entry whose words a press lays out or folds. span is in the row's own
// columns.
type trafficDoor struct {
	span   hudSpan
	member string
	expand string
	hint   string
	link   bool
	// land is the entry a member's handle opens its conversation at, and here
	// the entry a message's words bring into view in the manager's own
	// conversation (teamjump.go).
	land, here string
}

// trafficLaid is one drawn row of the rail and its doors.
type trafficLaid struct {
	text  string
	doors []trafficDoor
}

// railSeg is one piece of a row being laid: its plain words, how they are
// painted, and the door they are (-1 for none).
type railSeg struct {
	text  string
	paint func(string) string
	door  int
}

// trafficSheet lays a rail's rows one at a time, width wide, lighting the
// door under the pointer as it goes.
type trafficSheet struct {
	a       *app
	t       team
	width   int
	rows    []trafficLaid
	hotRow  int
	hotDoor int
	// first is the row index the sheet's row 0 lands on, for the hover.
	first int
	// land and here are the entry the doors being laid open at (teamjump.go).
	land, here string
}

// add lays one row from segs, with age flush right when there is room for
// it. The last segment that does not fit is cut with an ellipsis and the ones
// after it are dropped; the doors keep the columns the words ended up in.
func (s *trafficSheet) add(segs []railSeg, doors []trafficDoor, age string) {
	pal := s.a.pal
	room := s.width
	if age != "" && s.width >= 24 {
		room = s.width - ansi.StringWidth(age) - 1
	} else {
		age = ""
	}
	hot := len(s.rows)+s.first == s.hotRow
	var b strings.Builder
	used := 0
	spans := make([]hudSpan, len(doors))
	for _, seg := range segs {
		if used >= room {
			break
		}
		text := seg.text
		w := ansi.StringWidth(text)
		if used+w > room {
			text = ansi.Truncate(text, room-used, s.a.linearMark("…", "~"))
			w = ansi.StringWidth(text)
		}
		painted := text
		switch {
		case seg.door >= 0 && hot && seg.door == s.hotDoor && doors[seg.door].link:
			painted = teamLinkHotInk(pal, text)
		case seg.door >= 0 && hot && seg.door == s.hotDoor:
			painted = pal.cursor(pal.ink(text), 0)
		case seg.door >= 0 && doors[seg.door].link:
			painted = teamLinkInk(pal, text)
		case seg.paint != nil:
			painted = seg.paint(text)
		}
		if seg.door >= 0 && seg.door < len(spans) {
			if !spans[seg.door].pressable() {
				spans[seg.door] = hudSpan{from: used, to: used + w}
			} else {
				spans[seg.door].to = used + w
			}
		}
		b.WriteString(painted)
		used += w
	}
	line := b.String() + strings.Repeat(" ", max(room-used, 0))
	if age != "" {
		line += " " + pal.dim(age)
	}
	laid := trafficLaid{text: line}
	for i, d := range doors {
		if spans[i].pressable() {
			d.span = spans[i]
			laid.doors = append(laid.doors, d)
		}
	}
	s.rows = append(s.rows, laid)
}

// fits reports whether segs and age lay out whole in the sheet's width.
func (s *trafficSheet) fits(segs []railSeg, age string) bool {
	room := s.width
	if age != "" && s.width >= 24 {
		room -= ansi.StringWidth(age) + 1
	}
	used := 0
	for _, seg := range segs {
		used += ansi.StringWidth(seg.text)
	}
	return used <= room
}

// trafficReplyAgeCols is the narrowest rail whose answers carry their age: on
// a narrower one the words need the cells more, and the thread's header still
// says when it began.
const trafficReplyAgeCols = 40

// blank lays an empty row.
func (s *trafficSheet) blank() {
	s.rows = append(s.rows, trafficLaid{text: strings.Repeat(" ", s.width)})
}

// full reports whether the sheet holds at least n rows.
func (s *trafficSheet) full(n int) bool { return len(s.rows) >= n }

// handleSeg is a member's handle as a link, and its door, or plain words when
// the handle is nobody's in the team.
func (s *trafficSheet) handleSeg(handle string, doors *[]trafficDoor, paint func(string) string) railSeg {
	word := "@" + strings.TrimPrefix(handle, "@")
	if m, ok := s.t.ByHandle(strings.TrimPrefix(handle, "@")); ok && m.Key != s.a.frontTabKey() {
		hint := s.a.teamMemberHint(m)
		if s.land != "" {
			hint = strings.Replace(hint, " @"+m.Handle, " @"+m.Handle+" at this message", 1)
		}
		*doors = append(*doors, trafficDoor{member: m.Key, link: true, land: s.land, hint: hint})
		return railSeg{text: word, door: len(*doors) - 1}
	}
	return railSeg{text: word, paint: paint, door: -1}
}

// managerSegs is the manager's two words, `◆ manager`: the mark in the accent,
// the word muted.
func (s *trafficSheet) managerSegs() []railSeg {
	pal := s.a.pal
	return []railSeg{
		{text: s.a.teamManagerMark(), paint: pal.accent, door: -1},
		{text: " manager", paint: pal.muted, door: -1},
	}
}

// addressSegs is where an entry was sent, as the rail spells it.
func (s *trafficSheet) addressSegs(e teamstore.Entry, doors *[]trafficDoor) []railSeg {
	pal := s.a.pal
	switch e.To {
	case teamstore.ToManager:
		return s.managerSegs()
	case teamstore.ToEveryone:
		return []railSeg{{text: "everyone", paint: pal.muted, door: -1}}
	case teamstore.ToRoom:
		return []railSeg{{text: "room", paint: pal.muted, door: -1}}
	}
	var out []railSeg
	for i, h := range e.Recipients() {
		if i > 0 {
			out = append(out, railSeg{text: " ", door: -1})
		}
		out = append(out, s.handleSeg(h, doors, pal.muted))
	}
	return out
}

// speakerSegs is who wrote an entry.
func (s *trafficSheet) speakerSegs(from string, doors *[]trafficDoor) []railSeg {
	switch from {
	case teamstore.FromManager:
		return s.managerSegs()
	case teamstore.FromSystem:
		return []railSeg{{text: "codeaf", paint: s.a.pal.dim, door: -1}}
	}
	return []railSeg{s.handleSeg(from, doors, s.a.pal.muted)}
}

// trafficOpenKey is an entry's key in the rail's record of what is laid out
// in full.
func trafficOpenKey(teamID, id string) string { return teamID + "/" + id }

// words lays an entry's words: one row cut to the width, or, when the person
// opened it, every line of it. lead is what stands before the words on the
// first row and under it on the rest; paint inks the words.
func (s *trafficSheet) words(e teamstore.Entry, lead []railSeg, under string, paint func(string) string, age string, extra []trafficDoor) {
	text := strings.Join(strings.Fields(e.Text), " ")
	key := trafficOpenKey(s.t.ID, e.ID)
	open := s.a.traffic.open[key]
	doors := append([]trafficDoor(nil), extra...)
	hint := text + hintSegment + "click shows it all"
	if open {
		hint = "click folds it" + hintSegment + trafficKey + " hides the traffic"
	}
	door := len(doors)
	doors = append(doors, trafficDoor{expand: key, here: s.here, hint: hint})
	if !open {
		s.add(append(lead, railSeg{text: text, paint: paint, door: door}), doors, age)
		return
	}
	leadW := 0
	for _, seg := range lead {
		leadW += ansi.StringWidth(seg.text)
	}
	lines := wrap(strings.TrimSpace(e.Text), max(s.width-leadW, 8))
	for i, line := range lines {
		if i == 0 {
			s.add(append(lead, railSeg{text: line, paint: paint, door: door}), doors, age)
			continue
		}
		s.add([]railSeg{{text: under, paint: s.a.pal.dim, door: -1}, {text: line, paint: paint, door: 0}},
			[]trafficDoor{{expand: key, here: s.here, hint: hint}}, "")
	}
}

// thread lays one thread: a line of its own, or its message under a header
// and its answers under that as a tree.
func (s *trafficSheet) thread(th teamstore.Thread) {
	pal := s.a.pal
	e := th.Root
	arrow := " " + s.a.linearMark("→", "->") + " "
	var doors []trafficDoor
	// A HANDLE ON THE HEADER OPENS ITS MEMBER AT THIS MESSAGE, and the
	// message's words bring its card into view here (teamjump.go).
	defer func() { s.land, s.here = "", "" }()
	switch e.Kind {
	case teamstore.KindStop:
		segs := append(s.speakerSegs(e.From, &doors), railSeg{text: " stopped ", paint: pal.muted, door: -1})
		segs = append(segs, s.handleSeg(e.To, &doors, pal.muted))
		if reason := strings.TrimSpace(e.Text); reason != "" {
			segs = append(segs, railSeg{text: " · " + strings.Join(strings.Fields(reason), " "), paint: pal.dim, door: -1})
		}
		s.add(segs, doors, s.a.trafficAge(e))
		return
	case teamstore.KindEvent:
		s.event(e)
		return
	case teamstore.KindStart:
		segs := append(s.speakerSegs(e.From, &doors), railSeg{text: " started ", paint: pal.muted, door: -1})
		segs = append(segs, s.handleSeg(e.To, &doors, pal.muted))
		s.add(segs, doors, s.a.trafficAge(e))
	default:
		s.land, s.here = e.ID, e.ID
		tag := "fyi"
		if e.Kind == teamstore.KindDirective {
			tag = "do"
		}
		// A HEADER THAT CANNOT NAME EVERYONE NAMES WHOM IT CAN AND COUNTS THE
		// REST, `@agent @checking +1  do`, so the tag is never the part cut.
		named := e.Recipients()
		for keep := len(named); ; keep-- {
			doors = doors[:0]
			segs := append(s.speakerSegs(e.From, &doors), railSeg{text: arrow, paint: pal.dim, door: -1})
			if keep == len(named) {
				segs = append(segs, s.addressSegs(e, &doors)...)
			} else {
				for i, h := range named[:keep] {
					if i > 0 {
						segs = append(segs, railSeg{text: " ", door: -1})
					}
					segs = append(segs, s.handleSeg(h, &doors, pal.muted))
				}
				segs = append(segs, railSeg{text: " +" + itoa(len(named)-keep), paint: pal.dim, door: -1})
			}
			segs = append(segs, railSeg{text: "  " + tag, paint: pal.dim, door: -1})
			if keep <= 1 || s.fits(segs, s.a.trafficAge(e)) {
				s.add(segs, doors, s.a.trafficAge(e))
				break
			}
		}
	}
	if strings.TrimSpace(e.Text) != "" {
		s.words(e, []railSeg{{text: "  ", door: -1}}, "  ", pal.dim, "", nil)
	}
	s.replies(th)
}

// trafficReply is one member's line under a thread: its words, or the state
// its turn is in.
type trafficReply struct {
	who   string
	entry teamstore.Entry // the reply whose words the line carries, if any
	said  bool
	state string // finished, failed, asking, working, or "" for none
	note  string // what the state says, when it says anything
	last  teamstore.Entry
}

// replyLines folds a thread's answers into one line per reply, with the
// events folded into them (see the file's comment).
func replyLines(th teamstore.Thread) []trafficReply {
	var lines []trafficReply
	latest := map[string]int{}
	lineOf := func(who string) (*trafficReply, bool) {
		if i, ok := latest[who]; ok {
			return &lines[i], true
		}
		return nil, false
	}
	push := func(r trafficReply) {
		latest[r.who] = len(lines)
		lines = append(lines, r)
	}
	for _, e := range th.Replies {
		switch {
		case e.Kind == teamstore.KindYou || e.From == teamstore.FromYou:
			continue
		case e.Wake():
			who := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(e.Text), "woke "), "@")
			if e.From != teamstore.FromManager {
				continue // a member waking the manager is the manager's business
			}
			if l, ok := lineOf(who); ok && l.state == "working" {
				l.last = e
				continue
			}
			push(trafficReply{who: who, state: "working", last: e})
		case e.Kind == teamstore.KindEvent:
			who := e.From
			if who == teamstore.FromSystem || who == teamstore.FromManager {
				who = e.To
			}
			state := e.State
			note := strings.Join(strings.Fields(e.Text), " ")
			switch state {
			case teamstore.StateFinished, teamstore.StateFailed, teamstore.StateAsking:
			default:
				state = "note"
			}
			if l, ok := lineOf(who); ok && (l.state == "working" || (l.said && l.state == "")) {
				l.state, l.note, l.last = state, note, e
				continue
			}
			push(trafficReply{who: who, state: state, note: note, last: e})
		case e.Kind == teamstore.KindNote || e.Kind == teamstore.KindDirective:
			if l, ok := lineOf(e.From); ok && l.state == "working" && !l.said {
				l.entry, l.said, l.state, l.last = e, true, "", e
				continue
			}
			push(trafficReply{who: e.From, entry: e, said: true, last: e})
		}
	}
	return lines
}

// replies lays a thread's answers as a tree under it.
func (s *trafficSheet) replies(th teamstore.Thread) {
	pal := s.a.pal
	lines := replyLines(th)
	for i, r := range lines {
		lastOne := i == len(lines)-1
		glyph, under := s.a.linearMark("├ ", "|-"), s.a.linearMark("│ ", "| ")
		if lastOne {
			glyph, under = s.a.linearMark("└ ", "`-"), "  "
		}
		var doors []trafficDoor
		// AN ANSWER'S HANDLE OPENS ITS MEMBER AT ITS OWN POST, and a line with
		// nothing said yet at the message it answers.
		s.land, s.here = th.Root.ID, th.Root.ID
		if r.said {
			s.land = r.entry.ID
		}
		lead := []railSeg{{text: glyph, paint: pal.dim, door: -1}}
		if r.who == teamstore.FromManager {
			lead = append(lead, s.managerSegs()...)
		} else {
			lead = append(lead, s.handleSeg(r.who, &doors, pal.muted))
		}
		lead = append(lead, railSeg{text: "  ", door: -1})
		age := ""
		if s.width >= trafficReplyAgeCols {
			age = s.a.trafficAge(r.last)
		}
		mark := ""
		switch r.state {
		case teamstore.StateFinished:
			mark = s.a.linearMark(s.a.icon(tokens.GSettled), "ok") + " "
		case teamstore.StateFailed:
			mark = s.a.linearMark(s.a.icon(tokens.GFailed), "x") + " "
		}
		switch {
		case r.state == teamstore.StateAsking:
			// THE ONE LINE IN THE NEEDS-YOU AMBER: a member waiting on the person.
			quote := r.last
			quote.Text = "asking: " + strings.TrimPrefix(r.note, "asks: ")
			s.words(quote, lead, under+"    ", pal.ask, age, doors)
		case r.said:
			if mark != "" {
				lead = append(lead, railSeg{text: mark, paint: pal.muted, door: -1})
			}
			s.words(r.entry, lead, under+"    ", pal.muted, age, doors)
		case r.state == "working":
			s.add(append(lead, railSeg{text: "working" + s.a.linearMark("…", "..."), paint: pal.dim, door: -1}), doors, age)
		default:
			word := r.state
			if r.state == teamstore.StateFailed && r.note != "" {
				word = ""
			}
			segs := append(lead, railSeg{text: mark + word, paint: pal.muted, door: -1})
			if r.note != "" && r.note != r.state {
				sep := " · "
				if word == "" {
					sep = ""
				}
				segs = append(segs, railSeg{text: sep + r.note, paint: pal.dim, door: -1})
			}
			s.add(segs, doors, age)
		}
	}
}

// event lays an event that answers nothing: one line.
func (s *trafficSheet) event(e teamstore.Entry) {
	pal := s.a.pal
	var doors []trafficDoor
	text := strings.Join(strings.Fields(e.Text), " ")
	segs := append(s.speakerSegs(e.From, &doors), railSeg{text: "  ", door: -1})
	switch {
	case trafficAsking(e):
		quote := e
		quote.Text = "asking: " + strings.TrimPrefix(text, "asks: ")
		s.words(quote, segs, "    ", pal.ask, s.a.trafficAge(e), doors)
		return
	case e.State == teamstore.StateFinished:
		segs = append(segs, railSeg{text: s.a.linearMark(s.a.icon(tokens.GSettled), "ok") + " finished", paint: pal.muted, door: -1})
		if text != "" && text != e.State {
			segs = append(segs, railSeg{text: " · " + text, paint: pal.dim, door: -1})
		}
	case e.State == teamstore.StateFailed:
		if text == "" {
			text = "failed"
		}
		segs = append(segs, railSeg{text: s.a.linearMark(s.a.icon(tokens.GFailed), "x") + " " + text, paint: pal.muted, door: -1})
	default:
		if text == "" {
			text = e.State
		}
		segs = append(segs, railSeg{text: text, paint: pal.dim, door: -1})
	}
	s.add(segs, doors, s.a.trafficAge(e))
}

// trafficThreads is team t's cached entries that the rail draws, as threads,
// newest activity first.
func (a *app) trafficThreads(t team) []teamstore.Thread {
	rows := a.traffic.rows[t.ID]
	shown := make([]teamstore.Entry, 0, len(rows))
	for _, e := range rows {
		if !trafficShown(e) {
			continue
		}
		shown = append(shown, e)
	}
	return teamstore.Threads(shown)
}

// trafficSheetOf lays team t's threads into height rows width wide, newest
// thread first, a blank row between threads. first is the body row the
// sheet's first row lands on, which is what the pointer holds.
func (a *app) trafficSheetOf(t team, height, width, first int) []trafficLaid {
	s := &trafficSheet{a: a, t: t, width: width, hotRow: -1, first: first}
	if a.hot.kind == hoverTraffic {
		s.hotRow, s.hotDoor = a.hot.index, a.hot.entry
	}
	for i, th := range a.trafficThreads(t) {
		if s.full(height) {
			break
		}
		if i > 0 {
			s.blank()
		}
		s.thread(th)
	}
	if len(s.rows) > height {
		s.rows = s.rows[:height]
	}
	return s.rows
}

// teamMemberHint is what the hint line says with the pointer on a member's
// handle: `Open @web · web frontend · click`, or Resume when this window does
// not hold it.
func (a *app) teamMemberHint(m teamMember) string {
	verb := "Resume"
	if tabsHold(a.tabList(), m.Key) || a.teamHeldOpen(m.Key) {
		verb = "Open"
	}
	words := verb + " @" + m.Handle
	if title := strings.TrimSpace(m.Word); title != "" {
		if ansi.StringWidth(title) > teamLinkHintTitle {
			title = strings.TrimRight(ansi.Truncate(title, teamLinkHintTitle-1, ""), " ") + a.linearMark("…", "...")
		}
		words += hintSegment + title
	}
	return words + hintSegment + "click"
}
