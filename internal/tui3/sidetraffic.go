package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TRAFFIC VIEW (sidecol.go says what the column is) ──────────────────
//
// One line a row, newest first, with the time at the right in the muted ink,
// and a thin `new` line under what arrived since the person last looked.
//
// IN A MANAGER'S CHAT A ROW IS A PIECE OF WORK, not a message: a thread
// (internal/teams' thread.go), the manager's message and everything that
// answered it, headed by whom it went to and what it said, with the state its
// answers leave it in and how many messages it holds.
//
//	@security  parser numbers   running · 2 msgs ▸  2m
//	  ↳ 09:58 @review: 2 findings, both minor
//	General                             5 msgs ▸   now
//
// A press on ▸ lays the thread's replies open under it, one line each, and a
// second press folds them. A press on a handle opens that member at the
// message, and a press anywhere else on a row takes the person to the message
// in the conversation in front (teamjump.go). Whatever answers nothing and is
// answered by nothing (a note, a start, a stop, an event) is chatter, and all
// of it is the one `General` thread, where the rows used to be a line each.
//
// IN A MEMBER'S CHAT A ROW IS A MESSAGE, one of those involving that member,
// and a press on it goes to it in the chat in front.
//
// WHAT THE BAND CARRIES IS NOT DRAWN AGAIN HERE: a member's question waiting
// on the person is the band's row, and its own row would say it twice.

// sideTrafficCacheKey is everything the Traffic view's rows are drawn from.
type sideTrafficCacheKey struct {
	team, handle, last, divider, hot, focus, front string
	kind, rows, width, height, hotDoor, moved      int
	ascii, held                                    bool
	minute                                         int64
}

// sideTrafficCache is the Traffic view's rows as last laid.
type sideTrafficCache struct {
	key   sideTrafficCacheKey
	lines []railLine
}

// sideThreadKey is a work thread's name in the column's record of what is
// laid open.
func sideThreadKey(teamID, root string) string { return teamID + "/" + root }

// sideGeneral is the root the chatter thread is keyed by.
const sideGeneral = "general"

// sideTrafficView is the Traffic view's rows, at most height, width the
// column's own. It snapshots where the `new` line stands the first time the
// Traffic comes into view, and records that the person has the newest entry
// in front of them. Frame-safe: memory only.
func (a *app) sideTrafficView(height int) ([]railLine, int) {
	t, kind, handle := a.sideTeam()
	if kind == sideKindPlain || height <= 0 {
		return nil, -1
	}
	if a.side.up != t.ID {
		if a.side.divider == nil {
			a.side.divider = map[string]string{}
		}
		a.side.divider[t.ID] = a.traffic.seen[t.ID]
		a.side.up = t.ID
	}
	width := a.railRoom()
	rows := a.traffic.rows[t.ID]
	last := ""
	if len(rows) > 0 {
		last = rows[len(rows)-1].ID
	}
	hot, hotDoor := "", -1
	if a.hot.kind == hoverSide {
		hot, hotDoor = a.hot.key, a.hot.index
	}
	focus := ""
	if a.railHold {
		focus = a.railWhere.key
	}
	key := sideTrafficCacheKey{
		team: t.ID, handle: handle, last: last, divider: a.side.divider[t.ID], hot: hot, focus: focus, front: a.frontTabKey(),
		kind: kind, rows: len(rows), width: width, height: height, hotDoor: hotDoor, moved: a.side.moved,
		ascii: a.pal.ascii, held: a.railHold, minute: a.now().Unix() / 60,
	}
	a.trafficMarkSeen(t)
	if c := &a.side.traffic; c.lines != nil && c.key == key {
		return c.lines, -1
	}
	s := &sideSheet{a: a, t: t, width: width, hot: hot, hotDoor: hotDoor, divider: key.divider}
	if kind == sideKindManager {
		s.threads(kind, handle)
	} else {
		s.messages(handle)
	}
	lines := s.lines
	if len(lines) > height {
		lines = lines[:height]
	}
	a.side.traffic = sideTrafficCache{key: key, lines: lines}
	return lines, -1
}

// sideSheet lays the Traffic view's rows one at a time.
type sideSheet struct {
	a       *app
	t       team
	width   int
	hot     string
	hotDoor int
	divider string
	lines   []railLine
	// fresh says the rows laid so far are newer than the divider, and ruled
	// that the `new` line is down.
	fresh, ruled bool
}

// seg is one piece of a row being laid: its words, how they are painted, and
// the door they are (-1 for none).
type sideSeg struct {
	text  string
	paint func(string) string
	door  int
}

// newer draws the `new` line once, above the first row that is not newer
// than the divider, when rows above it were.
func (s *sideSheet) newer(id string) {
	if s.ruled {
		return
	}
	if id > s.divider && s.divider != "" {
		s.fresh = true
		return
	}
	if s.fresh {
		word := " " + sideNewWord + " "
		rule := s.a.linearMark("─", "-")
		left := 2
		right := max(s.width-left-ansi.StringWidth(word), 0)
		s.lines = append(s.lines, railLine{entry: -1,
			text: s.a.pal.dim(strings.Repeat(rule, left)) + s.a.pal.muted(word) + s.a.pal.dim(strings.Repeat(rule, right))})
	}
	s.ruled = true
}

// sideAgeFrom is the narrowest column a row's age is drawn in. Under it the
// age is said on the row's hint line instead, and the double space after who
// a row is with closes to one: at 110 columns the column is 27 cells, and a
// work row kept `@scrape +2  Please … ▸ now`, six letters of what the work
// is, because the time and the air took the cells the words needed.
const sideAgeFrom = 32

// sideHintWith is a row's hint with a fact the row gave up said in it, before
// the click the hint ends on.
func sideHintWith(hint, fact string) string {
	if fact == "" {
		return hint
	}
	if at := strings.LastIndex(hint, hintSegment+"click"); at >= 0 {
		return hint[:at] + hintSegment + fact + hint[at:]
	}
	if hint == "" {
		return fact
	}
	return hint + hintSegment + fact
}

// add lays one row: the segments from the left, cut with an ellipsis where
// they run out of room, and the time flush right. A door keeps the columns
// its words ended up in.
func (s *sideSheet) add(row *sideRow, segs []sideSeg, right []sideSeg, age string) {
	pal := s.a.pal
	room := s.width
	switch {
	case age != "" && s.width >= sideAgeFrom:
		room -= ansi.StringWidth(age) + 1
	case age != "":
		// THE WORDS OUTRANK THE CLOCK in a narrow column: the age goes to the
		// hint line, before the click it names, and the air between who and
		// what closes to one cell.
		row.hint = sideHintWith(row.hint, age)
		age = ""
		fallthrough
	default:
		if s.width < sideAgeFrom {
			for i := range segs {
				if segs[i].text == "  " {
					segs[i].text = " "
				}
			}
		}
	}
	rightW := 0
	for _, seg := range right {
		rightW += ansi.StringWidth(seg.text)
	}
	// THE STATE AT THE RIGHT GIVES WAY BEFORE THE WORDS DO, when the column
	// cannot hold both and a dozen cells of what the row is about (or all of
	// it, when it is shorter). A door on the right, a thread's ▸, is kept: it
	// is the one way to the replies.
	need := 0
	for _, seg := range segs {
		need += ansi.StringWidth(seg.text)
	}
	need = min(need, 20)
	for rightW > 0 && room-rightW < need {
		// The count goes first and the state after it: the last words drawn
		// are the first given up.
		drop := -1
		for i, seg := range right {
			if seg.door < 0 && strings.TrimSpace(seg.text) != "" {
				drop = i
			}
		}
		if drop < 0 {
			break
		}
		rightW -= ansi.StringWidth(right[drop].text)
		right = append(right[:drop:drop], right[drop+1:]...)
	}
	left := room - rightW
	var b strings.Builder
	used := 0
	spans := make([]hudSpan, len(row.doors))
	paint := func(seg sideSeg, text string, w int) {
		painted := text
		switch {
		case seg.door >= 0 && row.key == s.hot && seg.door == s.hotDoor:
			painted = teamLinkHotInk(pal, text)
		case seg.door >= 0 && row.doors[seg.door].act.kind == sideActJump && row.doors[seg.door].act.key != "":
			painted = teamLinkInk(pal, text)
		case seg.paint != nil:
			painted = seg.paint(text)
		}
		if seg.door >= 0 && seg.door < len(spans) {
			spans[seg.door] = hudSpan{from: used, to: used + w}
		}
		b.WriteString(painted)
		used += w
	}
	for _, seg := range segs {
		if used >= left {
			break
		}
		text := seg.text
		w := ansi.StringWidth(text)
		if used+w > left {
			text = ansi.Truncate(text, left-used, s.a.linearMark("…", "~"))
			w = ansi.StringWidth(text)
		}
		paint(seg, text, w)
	}
	if pad := left - used; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
		used += pad
	}
	for _, seg := range right {
		paint(seg, seg.text, ansi.StringWidth(seg.text))
	}
	if age != "" {
		b.WriteString(" " + pal.dim(age))
	}
	for i := range row.doors {
		row.doors[i].span = spans[i]
	}
	s.lines = append(s.lines, railLine{text: b.String(), entry: -1, side: row})
}

// handle is a member's handle as a door that opens it at entry, or plain
// muted words when the handle is nobody's in the team or is the chat in
// front.
func (s *sideSheet) handle(row *sideRow, h, entry string) sideSeg {
	word := "@" + strings.TrimPrefix(h, "@")
	if ansi.StringWidth(word) > trafficHandleCap {
		word = ansi.Truncate(word, trafficHandleCap, s.a.linearMark("…", "~"))
	}
	if m, ok := s.t.ByHandle(strings.TrimPrefix(h, "@")); ok && m.Key != s.a.frontTabKey() {
		hint := strings.Replace(s.a.teamMemberHint(m), " @"+m.Handle, " @"+m.Handle+" at this message", 1)
		row.doors = append(row.doors, sideDoor{hint: hint, act: sideAct{kind: sideActJump, key: m.Key, entry: entry}})
		return sideSeg{text: word, door: len(row.doors) - 1}
	}
	return sideSeg{text: word, paint: s.a.pal.muted, door: -1}
}

// who is who an entry is from, as a row's lead: the manager's mark, a
// member's handle as a door, or the person.
func (s *sideSheet) who(row *sideRow, e teamstore.Entry) sideSeg {
	switch e.From {
	case teamstore.FromManager:
		return sideSeg{text: s.a.teamManagerMark(), paint: s.a.pal.accent, door: -1}
	case teamstore.FromSystem:
		return sideSeg{text: "codeaf", paint: s.a.pal.dim, door: -1}
	case teamstore.FromYou:
		return sideSeg{text: "you", paint: s.a.pal.muted, door: -1}
	}
	return s.handle(row, e.From, e.ID)
}

// words is an entry's words on one line, what a row says about it.
func sideWords(e teamstore.Entry) string {
	text := strings.Join(strings.Fields(e.Text), " ")
	if teamstore.IsRuling(e) {
		text = trafficRulingText(text)
	}
	if text == "" {
		text = e.State
	}
	return text
}

// says is what a row says about an entry: its words, and for a start or a
// stop what was done to whom first, `started @lexer to run orbit · rewrite
// the lexer`, `stopped @web · stuck in a retry loop`, because a start's words
// are its brief and a stop's are its reason and neither says the act.
func (s *sideSheet) says(e teamstore.Entry) string {
	words := sideWords(e)
	var act string
	switch e.Kind {
	case teamstore.KindStart:
		act = "started " + s.a.trafficAddr(e.To)
		// A START THAT NAMES A TEAM made a sub-team for the started
		// conversation to run (DESIGN.md 8.10).
		if e.Team != "" {
			name := e.Team
			if sub, ok := s.a.teamByID(e.Team); ok {
				name = sub.Name
			}
			act += " to run " + name
		}
	case teamstore.KindStop:
		act = "stopped " + s.a.trafficAddr(e.To)
	default:
		return words
	}
	if words == "" || words == e.State {
		return act
	}
	return act + " · " + words
}

// clock is the time an entry was written, as the replies under a thread say
// it: the hour and minute.
func (s *sideSheet) clock(e teamstore.Entry) string {
	if e.At.IsZero() {
		return ""
	}
	return e.At.In(s.a.now().Location()).Format("15:04")
}

// ── A MANAGER'S WORK ────────────────────────────────────────────────────────

// sideWork is a thread as the manager's Traffic draws it, and chatter the
// entries that are a thread of their own and nobody's work.
type sideWork struct {
	thread teamstore.Thread
	last   teamstore.Entry
}

// sideChatter reports whether a thread is chatter: one entry, answering
// nothing and answered by nothing, that is not a message somebody was asked
// to act on.
func sideChatter(th teamstore.Thread) bool {
	if len(th.Replies) > 0 {
		return false
	}
	switch th.Root.Kind {
	case teamstore.KindDirective, teamstore.KindQuestion:
		return false
	}
	return !teamstore.IsRuling(th.Root)
}

// sideLast is the newest entry of a thread.
func sideLast(th teamstore.Thread) teamstore.Entry {
	last := th.Root
	for _, r := range th.Replies {
		if r.ID > last.ID {
			last = r
		}
	}
	return last
}

// threads lays a manager's Traffic: its work threads, newest activity first,
// and one General thread for the chatter, where its newest entry falls among
// them.
func (s *sideSheet) threads(kind int, handle string) {
	all := s.a.trafficThreads(s.t)
	var chatter []teamstore.Entry
	var newest teamstore.Entry
	for _, th := range all {
		if !sideChatter(th) || s.a.sideAsked(s.t, kind, handle, th.Root.ID) {
			continue
		}
		chatter = append(chatter, th.Root)
		if th.Root.ID > newest.ID {
			newest = th.Root
		}
	}
	laid := len(chatter) == 0
	for _, th := range all {
		if sideChatter(th) {
			continue
		}
		last := sideLast(th)
		if !laid && newest.ID > last.ID {
			s.general(chatter, newest)
			laid = true
		}
		s.work(th, last)
	}
	if !laid {
		s.general(chatter, newest)
	}
}

// work lays one work thread: its row, and its replies under it when laid
// open.
func (s *sideSheet) work(th teamstore.Thread, last teamstore.Entry) {
	pal := s.a.pal
	e := th.Root
	s.newer(last.ID)
	key := sideThreadKey(s.t.ID, e.ID)
	open := s.a.side.threads[key]
	lines := replyLines(th)
	row := &sideRow{key: "thread/" + e.ID,
		act: sideAct{kind: sideActJump, entry: e.ID}}
	// WHO THE WORK IS WITH LEADS: whom the manager's message went to, or the
	// member that wrote it.
	var segs []sideSeg
	named := e.Recipients()
	if e.From != teamstore.FromManager || len(named) == 0 {
		segs = append(segs, s.who(row, e))
	} else {
		segs = append(segs, s.handle(row, named[0], e.ID))
		if len(named) > 1 {
			segs = append(segs, sideSeg{text: " +" + itoa(len(named)-1), paint: pal.dim, door: -1})
		}
	}
	title := s.says(e)
	if teamstore.IsRuling(e) {
		title = "ruling · " + title
	}
	segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: title, paint: pal.muted, door: -1})
	// THE STATE ITS ANSWERS LEAVE IT IN, and how many messages it holds.
	state := sideThreadState(lines)
	// A MESSAGE IS WORDS SOMEBODY WROTE: a wake or a finishing is an event
	// the state already says, not a message.
	msgs := 1
	for _, r := range lines {
		if r.said {
			msgs++
		}
	}
	var right []sideSeg
	words := state
	if state != "" {
		right = append(right, sideSeg{text: "  " + state, paint: pal.dim, door: -1})
	}
	if msgs > 1 {
		count := itoa(msgs) + " msgs"
		sep := "  "
		if words != "" {
			words += " · "
			sep = " · "
		}
		words += count
		right = append(right, sideSeg{text: sep + count, paint: pal.dim, door: -1})
	}
	if len(th.Replies) > 0 {
		glyph := s.a.linearMark(glyphShut, glyphShutASCII)
		hint := "Show the replies" + hintSegment + "click"
		if open {
			glyph = s.a.linearMark(glyphOpen, glyphOpenASCII)
			hint = "Fold the replies" + hintSegment + "click"
		}
		row.doors = append(row.doors, sideDoor{hint: hint, act: sideAct{kind: sideActThread, key: key}})
		right = append(right, sideSeg{text: " ", door: -1}, sideSeg{text: glyph, paint: pal.muted, door: len(row.doors) - 1})
	}
	// THE HINT SAYS THE STATE TOO, because the row gives it up first when
	// the column is narrow.
	row.hint = title
	if words != "" {
		row.hint += hintSegment + words
	}
	// A RULING'S HINT NAMES THE CONFLICT IT DECIDED (DESIGN.md 8.10).
	if teamstore.IsRuling(e) && e.Packet != "" {
		row.hint += hintSegment + "the ruling on conflict " + e.Packet
	}
	row.hint += hintSegment + "click shows it in this chat"
	s.add(row, segs, right, s.a.trafficAge(last))
	if !open {
		return
	}
	for _, r := range lines {
		s.member(r)
	}
}

// member lays one member's line under an open thread, with its events folded
// into it the way replyLines folds them: `↳ 09:58 @review: ✓ 2 findings`, or
// `working…` for a member woken on the thread with nothing said yet. A wake or
// a finishing is never a line of its own.
func (s *sideSheet) member(r trafficReply) {
	pal := s.a.pal
	e := r.last
	if r.said {
		e = r.entry
	}
	row := &sideRow{key: "reply/" + e.ID, act: sideAct{kind: sideActJump, entry: e.ID}}
	segs := []sideSeg{{text: "  " + s.a.linearMark("↳", "->") + " ", paint: pal.dim, door: -1}}
	clock := s.clock(e)
	if clock != "" && s.width >= sideAgeFrom {
		segs = append(segs, sideSeg{text: clock + " ", paint: pal.dim, door: -1})
		clock = ""
	}
	switch r.who {
	case teamstore.FromManager, teamstore.FromSystem, teamstore.FromYou:
		segs = append(segs, sideSeg{text: s.a.trafficAddr(r.who), paint: pal.muted, door: -1})
	default:
		segs = append(segs, s.handle(row, r.who, e.ID))
	}
	segs = append(segs, sideSeg{text: ": ", paint: pal.dim, door: -1})
	words := r.note
	if r.said {
		words = s.says(r.entry)
	}
	switch r.state {
	case teamstore.StateFinished:
		words = s.a.linearMark("✓", "ok") + " " + words
	case teamstore.StateFailed:
		words = s.a.linearMark("✗", "x") + " " + words
	case teamstore.StateAsking:
		words = "asking: " + strings.TrimSpace(strings.TrimPrefix(words, "asks:"))
	case "working":
		words = "working" + s.a.linearMark("…", "...")
	}
	segs = append(segs, sideSeg{text: words, paint: pal.muted, door: -1})
	// A NARROW COLUMN SAYS THE TIME ON THE HINT LINE, as it does a row's age.
	row.hint = sideHintWith(words+hintSegment+"click shows it in this chat", clock)
	s.add(row, segs, nil, "")
}

// sideThreadState is the one word a thread's answers leave it in: someone
// asking, someone still working, a failure, all finished, or answered.
func sideThreadState(lines []trafficReply) string {
	if len(lines) == 0 {
		return "sent"
	}
	// EACH MEMBER'S LAST LINE IS WHERE IT STANDS. replyLines starts a new line
	// for a member when an event follows a finished one, so a member that asked
	// and then failed has both lines, and only the later one is still true.
	last := make(map[string]int, len(lines))
	for i, r := range lines {
		last[r.who] = i
	}
	working, failed, finished, members := false, false, 0, 0
	for i, r := range lines {
		if last[r.who] != i {
			continue
		}
		members++
		switch r.state {
		case teamstore.StateAsking:
			return "asking"
		case "working":
			working = true
		case teamstore.StateFailed:
			failed = true
		case teamstore.StateFinished:
			finished++
		}
	}
	switch {
	case working:
		return "running"
	case failed:
		return "failed"
	case finished == members:
		return "done"
	}
	return "answered"
}

// reply lays one reply under an open thread: `↳ 09:58 @review: 2 findings`.
func (s *sideSheet) reply(e teamstore.Entry) {
	pal := s.a.pal
	row := &sideRow{key: "reply/" + e.ID, act: sideAct{kind: sideActJump, entry: e.ID}}
	segs := []sideSeg{{text: "  " + s.a.linearMark("↳", "->") + " ", paint: pal.dim, door: -1}}
	clock := s.clock(e)
	if clock != "" && s.width >= sideAgeFrom {
		segs = append(segs, sideSeg{text: clock + " ", paint: pal.dim, door: -1})
		clock = ""
	}
	segs = append(segs, s.who(row, e), sideSeg{text: ": ", paint: pal.dim, door: -1})
	words := s.says(e)
	segs = append(segs, sideSeg{text: words, paint: pal.muted, door: -1})
	row.hint = sideHintWith(words+hintSegment+"click shows it in this chat", clock)
	s.add(row, segs, nil, "")
}

// general lays the chatter as one thread, `General`, and its entries under
// it when laid open, newest first.
func (s *sideSheet) general(chatter []teamstore.Entry, newest teamstore.Entry) {
	pal := s.a.pal
	s.newer(newest.ID)
	key := sideThreadKey(s.t.ID, sideGeneral)
	open := s.a.side.threads[key]
	row := &sideRow{key: key, act: sideAct{kind: sideActThread, key: key},
		hint: "Everything that is nobody's work" + hintSegment + "click shows it"}
	if open {
		row.hint = "Everything that is nobody's work" + hintSegment + "click folds it"
	}
	glyph := s.a.linearMark(glyphShut, glyphShutASCII)
	if open {
		glyph = s.a.linearMark(glyphOpen, glyphOpenASCII)
	}
	row.doors = append(row.doors, sideDoor{hint: row.hint, act: row.act})
	right := []sideSeg{{text: "  " + itoa(len(chatter)) + " " + plural("msg", len(chatter)) + " ", paint: pal.dim, door: -1}, {text: glyph, paint: pal.muted, door: 0}}
	s.add(row, []sideSeg{{text: "General", paint: pal.muted, door: -1}}, right, s.a.trafficAge(newest))
	if !open {
		return
	}
	for _, e := range chatter {
		s.reply(e)
	}
}

// ── A MEMBER'S MESSAGES ─────────────────────────────────────────────────────

// messages lays a member's Traffic: every drawn message involving it, newest
// first, one line each.
func (s *sideSheet) messages(handle string) {
	pal := s.a.pal
	rows := s.a.traffic.rows[s.t.ID]
	arrow := " " + s.a.linearMark("→", "->") + " "
	for i := len(rows) - 1; i >= 0; i-- {
		e := rows[i]
		if !trafficShown(e) || e.Wake() || !sideInvolves(e, handle) {
			continue
		}
		if s.a.sideAsked(s.t, sideKindMember, handle, e.ID) {
			continue
		}
		s.newer(e.ID)
		row := &sideRow{key: "msg/" + e.ID, act: sideAct{kind: sideActJump, entry: e.ID}}
		var segs []sideSeg
		// WHAT THIS MEMBER SAID IS `→ whom`; WHAT IT WAS TOLD IS WHO TOLD IT.
		if e.From == handle {
			to := s.a.trafficAddr(e.To)
			if e.To == teamstore.ToManager {
				to = s.a.teamManagerMark()
			}
			segs = append(segs, sideSeg{text: strings.TrimLeft(arrow, " "), paint: pal.dim, door: -1}, sideSeg{text: to, paint: pal.muted, door: -1})
		} else {
			segs = append(segs, s.who(row, e))
		}
		words := s.says(e)
		switch {
		case e.State == teamstore.StateFinished:
			words = s.a.linearMark("✓", "ok") + " " + words
		case e.State == teamstore.StateFailed:
			words = s.a.linearMark("✗", "x") + " " + words
		}
		segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: words, paint: pal.muted, door: -1})
		row.hint = words + hintSegment + "click shows it in this chat"
		s.add(row, segs, nil, s.a.trafficAge(e))
	}
}
