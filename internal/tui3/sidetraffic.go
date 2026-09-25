package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TRAFFIC VIEW (sidecol.go says what the column is) ──────────────────
//
// One line a row, newest first, and a thin `new` line under what arrived
// since the person last looked. EVERY ROW READS `from → to  words`: who sent
// it, who it is for, then what it says. The manager is `◆`. Several
// recipients are the first handle and `+N`. In a member's chat that member
// is `you`. The age is never on the row. The hint line says it, at every
// width, so the arrow and the names keep their cells and only the words are
// cut, at a word.
//
//	◆ → @security +2  parser numbers   running ▸
//	  ↳ @review → ◆  ✓ 2 findings, both minor
//	General                             5 msgs ▸
//
// A press on ▸ lays the thread's replies open under it, one line each in the
// same `from → to`, and a second press folds them. A press on a handle opens
// that member at the message, and a press anywhere else on a row takes the
// person to the message in the conversation in front (teamjump.go). Whatever
// answers nothing and is answered by nothing (a note, a start, a stop, an
// event) is chatter, and all of it is the one `General` thread. Laid open,
// its lines read `from → to` too.
//
// IN A MEMBER'S CHAT A ROW IS A MESSAGE, one of those involving that member,
// `◆ → you` for what it was told and `you → ◆` for what it said, and a press
// on it goes to it in the chat in front.
//
// WHAT THE BAND CARRIES IS NOT DRAWN AGAIN HERE: a member's question waiting
// on the person is the band's row, `? @model → ◆  …`, and its own row would
// say it twice.

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
// the door they are (-1 for none). flex is the row's words, the one piece a
// narrow column cuts, and it is cut at a word so the arrow and the names
// stay whole.
type sideSeg struct {
	text  string
	paint func(string) string
	door  int
	flex  bool
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

// sideHintWith is a row's hint with a fact the row gave up said in it, before
// the click the hint ends on. The age is always one of those facts: it used
// to sit at the right of the row, and the arrow had no cells left.
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

// fitAtWord cuts words to width at a word boundary, with tail where they do
// not fit. A first word longer than the room is cut where it lands, because
// there is no boundary to stop at.
func fitAtWord(words string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(words) <= width {
		return words
	}
	tw := ansi.StringWidth(tail)
	if tw >= width {
		return ansi.Truncate(tail, width, "")
	}
	head := ansi.Truncate(words, width-tw, "")
	// A CUT THAT LANDS INSIDE A WORD steps back to the space before it. When
	// the first word itself does not fit, the row says there is more and does
	// not show half a word.
	if len(head) < len(words) && (len(head) == 0 || !strings.HasPrefix(words[len(head):], " ")) {
		if at := strings.LastIndex(head, " "); at > 0 {
			head = strings.TrimRight(head[:at], " ")
		} else {
			return tail
		}
	}
	head = strings.TrimRight(head, " ")
	if head == "" {
		return tail
	}
	return head + tail
}

// add lays one row: the segments from the left, the words cut at a word where
// they run out of room, and nothing for the time. The age goes to the hint
// line at every width, so the arrow and the names keep their cells. A door
// keeps the columns its words ended up in.
func (s *sideSheet) add(row *sideRow, segs []sideSeg, right []sideSeg, age string) {
	pal := s.a.pal
	if age != "" {
		row.hint = sideHintWith(row.hint, age)
	}
	room := s.width
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
	// THE WORDS ARE THE ONLY SEGMENT THAT SHRINKS, and they shrink at a word.
	// The arrow and the names were laid as fixed segments so a narrow column
	// cuts `Please provide a st…` and never `Pleas…` through a name.
	fixed := 0
	for _, seg := range segs {
		if !seg.flex {
			fixed += ansi.StringWidth(seg.text)
		}
	}
	for i := range segs {
		if !segs[i].flex {
			continue
		}
		segs[i].text = fitAtWord(segs[i].text, max(left-fixed, 0), s.a.linearMark("…", "~"))
	}
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

// arrow is the dim ` → ` between who a row is from and who it is for.
func (s *sideSheet) arrow() sideSeg {
	return sideSeg{text: " " + s.a.linearMark("→", "->") + " ", paint: s.a.pal.dim, door: -1}
}

// party is one end of `from → to`: the manager's mark, this member as `you`
// when self is that handle, a word for everyone, or a handle that opens the
// member at entry.
func (s *sideSheet) party(row *sideRow, who, entry, self string) sideSeg {
	if self != "" && (who == self || who == "@"+self) {
		return sideSeg{text: "you", paint: s.a.pal.muted, door: -1}
	}
	switch who {
	case teamstore.FromManager:
		return sideSeg{text: s.a.teamManagerMark(), paint: s.a.pal.accent, door: -1}
	case teamstore.FromSystem:
		return sideSeg{text: "codeaf", paint: s.a.pal.dim, door: -1}
	case teamstore.FromYou:
		return sideSeg{text: "you", paint: s.a.pal.muted, door: -1}
	case teamstore.ToEveryone:
		return sideSeg{text: "all", paint: s.a.pal.muted, door: -1}
	case teamstore.ToRoom:
		return sideSeg{text: "room", paint: s.a.pal.muted, door: -1}
	case "":
		return sideSeg{text: "", door: -1}
	}
	return s.handle(row, who, entry)
}

// route is `from → to` for one entry. named is the member handles it went to,
// and the first of them is drawn with `+N` for the rest. to is the address
// when named is empty (the manager, everyone, one handle). self is this
// member's handle in a member's chat, drawn as `you`.
func (s *sideSheet) route(row *sideRow, from, entry, self string, named []string, to string) []sideSeg {
	segs := []sideSeg{s.party(row, from, entry, self), s.arrow()}
	if len(named) > 0 {
		segs = append(segs, s.party(row, named[0], entry, self))
		if len(named) > 1 {
			segs = append(segs, sideSeg{text: " +" + itoa(len(named)-1), paint: s.a.pal.dim, door: -1})
		}
		return segs
	}
	segs = append(segs, s.party(row, to, entry, self))
	return segs
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
	// EVERY WORK ROW READS `from → to`: the manager's mark to the members it
	// asked, or the member that wrote it back to the manager.
	named := e.Recipients()
	segs := s.route(row, e.From, e.ID, "", named, e.To)
	title := s.says(e)
	if teamstore.IsRuling(e) {
		title = "ruling · " + title
	}
	segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: title, paint: pal.muted, door: -1, flex: true})
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
// into it the way replyLines folds them: `↳ @review → ◆  ✓ 2 findings`, or
// `working…` for a member woken on the thread with nothing said yet. A wake or
// a finishing is never a line of its own. The clock stays on the hint line.
func (s *sideSheet) member(r trafficReply) {
	pal := s.a.pal
	e := r.last
	if r.said {
		e = r.entry
	}
	row := &sideRow{key: "reply/" + e.ID, act: sideAct{kind: sideActJump, entry: e.ID}}
	segs := []sideSeg{{text: "  " + s.a.linearMark("↳", "->") + " ", paint: pal.dim, door: -1}}
	// A MEMBER'S LINE RUNS BACK TO THE MANAGER. A line the manager, the
	// person or codeaf wrote runs the other way, to whoever it names.
	to := teamstore.FromManager
	if r.who == teamstore.FromManager || r.who == teamstore.FromSystem || r.who == teamstore.FromYou {
		to = e.To
	}
	segs = append(segs, s.route(row, r.who, e.ID, "", nil, to)...)
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
	segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: words, paint: pal.muted, door: -1, flex: true})
	row.hint = sideHintWith(words+hintSegment+"click shows it in this chat", s.clock(e))
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

// reply lays one chatter line under General: `↳ ◆ → @price  an older aside`.
// The clock stays on the hint line, as a work row's age does.
func (s *sideSheet) reply(e teamstore.Entry) {
	pal := s.a.pal
	row := &sideRow{key: "reply/" + e.ID, act: sideAct{kind: sideActJump, entry: e.ID}}
	segs := []sideSeg{{text: "  " + s.a.linearMark("↳", "->") + " ", paint: pal.dim, door: -1}}
	segs = append(segs, s.route(row, e.From, e.ID, "", e.Recipients(), e.To)...)
	words := s.says(e)
	segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: words, paint: pal.muted, door: -1, flex: true})
	row.hint = sideHintWith(words+hintSegment+"click shows it in this chat", s.clock(e))
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
	rows := s.a.traffic.rows[s.t.ID]
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
		// THIS MEMBER IS `you`. What it was told reads `◆ → you`, what it
		// said reads `you → ◆` or `you → @other`.
		segs := s.route(row, e.From, e.ID, handle, e.Recipients(), e.To)
		words := s.says(e)
		switch {
		case e.State == teamstore.StateFinished:
			words = s.a.linearMark("✓", "ok") + " " + words
		case e.State == teamstore.StateFailed:
			words = s.a.linearMark("✗", "x") + " " + words
		}
		segs = append(segs, sideSeg{text: "  ", door: -1}, sideSeg{text: words, paint: s.a.pal.muted, door: -1, flex: true})
		row.hint = words + hintSegment + "click shows it in this chat"
		s.add(row, segs, nil, s.a.trafficAge(e))
	}
}
