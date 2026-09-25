package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE SIDE COLUMN: ONE COLUMN, TWO SOURCES ────────────────────────────────
//
// The right of every conversation is one column with one header, and the
// header is two words:
//
//	│ Tasks 14 · Traffic 3 new                  alt+l
//	│ ? @security asks: ship at p99 180ms?          the needs-you band
//	│ ✗ parser bench failed                         a failure, in ink
//	│ ────────────────────────────────────────────
//	│ Running 4
//	│   ⠙ rebase onto dev                   2m
//	│ Queued 3 ▸
//	│ Done 7 ▸
//
// THERE USED TO BE TWO COLUMNS AND A SPECIAL CASE BETWEEN THEM. An ordinary
// chat had the task column; a chat with a team manager in front had the
// Traffic instead, with the manager's own tasks behind a `Traffic · Tasks 2`
// word. Now there is this one component and it has two sources: the
// conversation's own work ([sideTasks], task.go's roster) and what passes in
// its team ([sideTraffic], sidetraffic.go). A chat that is in no team has only
// the first, and its header is the one word.
//
// THE CURRENT WORD IS BOLD INK AND THE OTHER IS DIM, and each is a door with a
// hover ground and a hint. The other word keeps its count, so what arrived in
// the Traffic while the tasks are in front is still on screen (`Traffic 3
// new`). Which word is in front is remembered for the session per kind of chat
// ([sideState.view]): a manager chat opens on the Traffic, every other chat on
// the tasks.
//
// THE BAND IS WHAT NEEDS THE PERSON NOW, from both sources ([app.sideBand]):
// a question or a packet waiting on them, a task whose next step is theirs,
// and a failure nobody has looked at yet. Amber is ONLY for the first kind,
// the items blocked on the person; a failure is a ✗ in ordinary ink. The band
// shows [sideBandCap] rows and then `+N more`, is gone entirely when there is
// nothing in it, and an item in it is not drawn again in the list under it.
// It pushes the list down and never the conversation: the column's width does
// not move for anything drawn in it.
//
// NOTHING HERE MOVES THE CONVERSATION. The column's width is decided by the
// frame's width and the person's own widen answer and by nothing it shows
// ([app.railColumns]), so switching words, folding a group, a row arriving
// and the band appearing all happen inside the column. Nothing here takes the
// keyboard either: alt+t hands it to the column, as it always has, and esc
// gives it back (task.go's [app.railKey]).
//
// THE FRAME DRAWS MEMORY. The Traffic is the cache teamtraffic.go's clock
// keeps, the tasks are the roster's nodes, and the rows the Traffic view lays
// are kept between frames ([sideTrafficCache]) until what they are drawn from
// moves.

// The two sources, as the column's view.
const (
	sideTasks   = 1
	sideTraffic = 2
)

// The three kinds of chat, which is what the view is remembered by.
const (
	sideKindPlain = iota
	sideKindMember
	sideKindManager
	sideKinds
)

// Geometry.
const (
	// The column takes a quarter of the frame, between sideColsMin and
	// sideColsMax, and never leaves the conversation under sideBodyFloor. It
	// is the same at every view and in every kind of chat: at 110 columns it
	// is 28, at 120 it is 30 (the task column's old width), at 160 it is 40.
	// A quarter and not the Traffic's old three tenths, because this is now
	// the column of every chat and the conversation beside it is the page.
	sideColsMin   = 28
	sideColsMax   = 40
	sideBodyFloor = 56
	// sideBandCap is how many rows the band draws before `+N more`.
	sideBandCap = 3
)

// The header's words.
const (
	sideTasksWord   = "Tasks"
	sideTrafficWord = "Traffic"
	sideNewWord     = "new"
	sideWordSep     = " · "
	// sideHideKey is the column's own key, named at the right of its header.
	// ctrl+g ([railStowKey]) is the same key under its older name.
	sideHideKey = trafficKey
)

// sideState is the column's memory for the session: which word is in front
// per kind of chat, which task groups and work threads the person opened,
// which failures they have looked at, and where the Traffic's `new` line
// stands. It is this window's and in memory only.
type sideState struct {
	view [sideKinds]int
	// open is each foldable task group the person opened.
	open [railGroupCount]bool
	// threads is each work thread laid open, by [sideThreadKey], and moved
	// counts the presses that changed it, which the rows' cache keys on.
	threads map[string]bool
	moved   int
	// acked is every failed task the person has opened since it failed.
	acked map[uint64]bool
	// bandAll says the band draws every item instead of [sideBandCap].
	bandAll bool
	// divider is, per team, the newest entry the person had seen when the
	// Traffic came into view, which is where the `new` line is drawn; up is
	// the team whose Traffic is in view now, so the line holds still while it
	// is being read.
	divider map[string]string
	up      string
	// asks is the band's reading of the Traffic, kept until the log moves.
	asks sideAskCache
	// traffic is the Traffic view's rows as last laid.
	traffic sideTrafficCache
	// last is the band and Traffic rows the last layout drew, for the hint
	// line, and pinned how many rows at its top were the header, the band and
	// the way back to the main chat, which nothing is laid above.
	last   []*sideRow
	pinned int
}

// ── A ROW AND WHAT A PRESS ON IT DOES ───────────────────────────────────────

// The acts a row or a door of the column does.
const (
	sideActNone = iota
	// sideActOpen opens task id.
	sideActOpen
	// sideActJump takes the person to Traffic entry entry, in member key's
	// conversation or, with no key, in the one in front (teamjump.go).
	sideActJump
	// sideActThread lays work thread key open or folds it.
	sideActThread
	// sideActBand shows every band item, or folds them back to three.
	sideActBand
	// sideActView brings word view to the front.
	sideActView
	// sideActHide puts the column away.
	sideActHide
	// sideActTeams opens the teams page, where a packet is answered.
	sideActTeams
)

// sideAct is what a press does.
type sideAct struct {
	kind  int
	id    uint64
	key   string
	entry string
	view  int
}

// sideDoor is a target narrower than its row: a word of the header, a handle,
// a thread's ▸. span is in the row's own columns.
type sideDoor struct {
	span hudSpan
	hint string
	act  sideAct
}

// sideRow is one row of the band or the Traffic view: its identity, which is
// what the hover and the keyboard hold, what the hint line says over it, what
// a press on it does, and its doors.
type sideRow struct {
	key   string
	hint  string
	act   sideAct
	doors []sideDoor
}

// doorAt is the door under column x of the row, -1 for none.
func (r *sideRow) doorAt(x int) int {
	for i, d := range r.doors {
		if d.span.holds(x) {
			return i
		}
	}
	return -1
}

// pressable reports whether a press anywhere on the row does something.
func (r *sideRow) pressable() bool { return r.act.kind != sideActNone }

// ── WHICH CHAT, WHICH WORD, HOW WIDE ────────────────────────────────────────

// sideColsFor is the column the frame lends at width, 0 when the
// conversation would be left too narrow for one.
func sideColsFor(width int) int {
	if width < railSlimFloor {
		return 0
	}
	cols := min(max(width/4, sideColsMin), sideColsMax)
	if width-cols < sideBodyFloor {
		return 0
	}
	return cols
}

// sideTeam is the team the chat in front is in, as the column reads it: the
// team it manages, or the managed team it is a member of with its handle.
// Plain for a chat in no managed team, and on a window that cannot read the
// engine's teams ([app.teamsOff]). Frame-safe and allocation free.
func (a *app) sideTeam() (team, int, string) {
	if a.teamsOff() || !a.wall.loaded || len(a.wall.teams) == 0 {
		return team{}, sideKindPlain, ""
	}
	if t, ok := a.teamFrontManaged(); ok {
		return t, sideKindManager, ""
	}
	front := a.frontTabKey()
	for _, t := range a.wall.teams {
		if t.Manager == "" || t.Manager == front {
			continue
		}
		if m, ok := t.Member(front); ok && m.Handle != "" {
			return t, sideKindMember, m.Handle
		}
	}
	return team{}, sideKindPlain, ""
}

// sideKind is the kind of chat in front.
func (a *app) sideKind() int {
	_, kind, _ := a.sideTeam()
	return kind
}

// sideView is the word in front: the person's own choice for this kind of
// chat, else the Traffic in a manager chat and the tasks everywhere else. A
// chat in no team has only the tasks.
func (a *app) sideView() int {
	kind := a.sideKind()
	if kind == sideKindPlain {
		return sideTasks
	}
	if v := a.side.view[kind]; v != 0 {
		return v
	}
	if kind == sideKindManager {
		return sideTraffic
	}
	return sideTasks
}

// sideSetView brings view to the front and remembers it for this kind of
// chat. It moves nothing but what the column draws: the keyboard stays where
// it was, and a cursor on a row the other view does not have goes to the
// view's first row.
func (a *app) sideSetView(view int) {
	kind := a.sideKind()
	if kind == sideKindPlain || a.sideView() == view {
		return
	}
	a.side.view[kind] = view
	a.side.up = ""
	a.dropHover()
	if a.railHold {
		a.railWhere = railSpot{}
		if spots := a.railSpots(); len(spots) > 0 {
			a.railWhere = spots[0]
		}
	}
	a.touch()
}

// sideStep switches to the other word, which is what ← and → do while the
// column holds the keyboard.
func (a *app) sideStep() {
	if a.sideView() == sideTasks {
		a.sideSetView(sideTraffic)
		return
	}
	a.sideSetView(sideTasks)
}

// sideAway reports whether the person has put the column away: the saved
// answer, and on the teams page the page's own, which starts folded because
// the page has a rail of its own on the left (teamspagehost.go).
func (a *app) sideAway() bool {
	if a.teamsHosting() {
		return !a.tp.traffic
	}
	return a.railAway
}

// sideToggle is alt+l: the column goes away or comes back where the frame
// lends it one, and on a frame too narrow for one it is laid over the body
// and taken off again ([app.railFull]).
func (a *app) sideToggle() bool {
	if a.railShowing() || a.railStowed() {
		a.railStow(a.railShowing())
		return true
	}
	if a.railFull() {
		a.railTake(false)
		return true
	}
	if a.railAvail() && !a.railQuiet() {
		a.railTake(true)
		return a.railHold
	}
	return false
}

// ── THE HEADER ──────────────────────────────────────────────────────────────

// sideHeadRow is the column's first line, width wide, and its doors: the two
// words, and the key that puts the column away at the right.
func (a *app) sideHeadRow(width int) (string, *sideRow) {
	t, kind, handle := a.sideTeam()
	view := a.sideView()
	row := &sideRow{key: sideHeadKey}
	hot := -1
	if a.hot.kind == hoverSide && a.hot.key == sideHeadKey {
		hot = a.hot.index
	}
	var b strings.Builder
	used := 0
	// word paints one word of the header: its label bold ink when it is in
	// front and dim when it is not, its count the same except that a zero is
	// always dim and a count of new rows (fresh) is always ink, so what arrived
	// behind the other word is still read.
	word := func(label, count string, fresh bool, current bool, act sideAct, hint string) {
		text := label + " " + count
		if fresh {
			text += " " + sideNewWord
		}
		var painted string
		switch {
		case current:
			painted = a.pal.bold(a.pal.ink(label + " "))
		default:
			painted = a.pal.dim(label + " ")
		}
		switch {
		case count == "0":
			painted += a.pal.dim(count)
		case fresh:
			painted += a.pal.ink(count + " " + sideNewWord)
		case current:
			painted += a.pal.bold(a.pal.ink(count))
		default:
			painted += a.pal.dim(count)
		}
		w := ansi.StringWidth(text)
		if act.kind != sideActNone {
			if hot == len(row.doors) {
				painted = a.pal.cursor(painted, 0)
			}
			row.doors = append(row.doors, sideDoor{span: hudSpan{from: used, to: used + w}, hint: hint, act: act})
		}
		b.WriteString(painted)
		used += w
	}
	team := kind != sideKindPlain
	tasksAct, trafficAct := sideAct{}, sideAct{}
	if team {
		tasksAct = sideAct{kind: sideActView, view: sideTasks}
		trafficAct = sideAct{kind: sideActView, view: sideTraffic}
	}
	word(sideTasksWord, itoa(len(a.taskOrder)), false, view == sideTasks, tasksAct, "Show this chat's tasks"+hintSegment+"click")
	if team {
		n, fresh := a.sideTrafficCount(t, kind, handle)
		// WITH THE TRAFFIC IN FRONT NOTHING IN IT IS NEW TO THE WORD: the `new`
		// line in the view says where the unread rows end.
		if view == sideTraffic {
			fresh = 0
		}
		count, isNew := itoa(n), fresh > 0
		if isNew {
			count = itoa(fresh)
		}
		// A NARROW COLUMN LOSES THE WORD `new` FIRST, and keeps the count.
		full := ansi.StringWidth(sideWordSep + sideTrafficWord + " " + count + " " + sideNewWord)
		if isNew && used+full > width {
			isNew = false
		}
		b.WriteString(a.pal.dim(sideWordSep))
		used += ansi.StringWidth(sideWordSep)
		hint := "Show the team's traffic"
		if kind == sideKindMember {
			hint = "Show the traffic with @" + handle
		}
		if fresh > 0 {
			hint += hintSegment + itoa(fresh) + " new"
		}
		word(sideTrafficWord, count, isNew, view == sideTraffic, trafficAct, hint+hintSegment+"click")
	}
	// THE WAY OUT IS THE LAST WORD, the key itself, and it is a door.
	// Spelled the way this keyboard's caps say it: `opt+l` on a Mac.
	key := a.chords.say(sideHideKey)
	// It keeps one cell of air from the words, which is what lets `Tasks 14 ·
	// Traffic 8 alt+l` stand in the twenty-eight columns of a 110 frame.
	if gap := width - used - ansi.StringWidth(key); gap >= 1 {
		b.WriteString(strings.Repeat(" ", gap))
		used += gap
		hint := "Hide this column" + hintSegment + key
		if a.railFull() {
			hint = "Close this column" + hintSegment + key
		}
		door := len(row.doors)
		painted := a.pal.dim(key)
		if hot == door {
			painted = a.pal.cursor(a.pal.ink(key), 0)
		}
		row.doors = append(row.doors, sideDoor{span: hudSpan{from: used, to: used + ansi.StringWidth(key)}, hint: hint, act: sideAct{kind: sideActHide}})
		b.WriteString(painted)
	}
	return fit(b.String(), width), row
}

// sideHeadKey is the header row's identity for the hover.
const sideHeadKey = "head"

// sideTrafficCount is the Traffic word's count, and how many of those rows
// arrived since the person last had the Traffic in front of them: every drawn
// message of team t, or in a member's chat the ones involving that member.
func (a *app) sideTrafficCount(t team, kind int, handle string) (int, int) {
	rows := a.traffic.rows[t.ID]
	seen := a.traffic.seen[t.ID]
	n, fresh := 0, 0
	for i := range rows {
		e := &rows[i]
		if !trafficShown(*e) || e.Wake() {
			continue
		}
		if kind == sideKindMember && !sideInvolves(*e, handle) {
			continue
		}
		n++
		if e.ID > seen {
			fresh++
		}
	}
	return n, fresh
}

// sideInvolves reports whether an entry is to or from a member's handle, or
// to the whole team.
func sideInvolves(e teamstore.Entry, handle string) bool {
	if e.From == handle || e.To == teamstore.ToEveryone {
		return true
	}
	if e.To == handle {
		return true
	}
	if e.To == teamstore.ToSeveral {
		for _, h := range e.Handles {
			if h == handle {
				return true
			}
		}
	}
	return false
}

// ── THE BAND ────────────────────────────────────────────────────────────────

// sideBandItem is one thing that needs the person now.
type sideBandItem struct {
	key string
	// ask says the item is blocked on the person, which is what the amber is
	// for; every other item is a failure. mark is its one cell: a task's own
	// state glyph, and the needs-a-person `?` for a question from the team.
	ask   bool
	mark  string
	words string
	age   string
	act   sideAct
	hint  string
}

// sideBandFails reports whether a task is a failure the band carries: work
// this window watched fail on its own, that the person has not opened since.
// A stop the person made is not a failure waiting on them, and a failure a
// reopened conversation replayed is history, not news.
func (a *app) sideBandFails(node *taskNode) bool {
	return node != nil && node.state == session.TaskFailed && !node.stopped && !node.restored && !a.side.acked[node.id]
}

// sideBand is every item that needs the person now, blocked items first:
// the team's questions waiting on them, packets put to them, tasks whose next
// step is theirs, then failures nobody has looked at. Newest first in each.
func (a *app) sideBand() []sideBandItem {
	var out []sideBandItem
	t, kind, handle := a.sideTeam()
	if kind != sideKindPlain {
		for _, e := range a.sideAsks(t, kind, handle) {
			// THE BAND READS `from → to` TOO: the member that asked, the
			// manager's mark, then the question. In this member's own chat
			// the member is `you`. The whole row stays amber, because it is
			// the one thing that needs the person.
			from := a.trafficAddr(e.From)
			if kind == sideKindMember && e.From == handle {
				from = "you"
			}
			q := strings.TrimSpace(strings.TrimPrefix(strings.Join(strings.Fields(e.Text), " "), "asks:"))
			words := from + " " + a.linearMark("→", "->") + " " + a.teamManagerMark() + "  " + q
			act := sideAct{kind: sideActJump, entry: e.ID}
			hint := words
			if m, ok := t.ByHandle(e.From); ok && m.Key != a.frontTabKey() {
				act.key = m.Key
				hint += hintSegment + "click opens @" + m.Handle + " at the question"
			} else {
				hint += hintSegment + "click shows the question"
			}
			out = append(out, sideBandItem{key: "ask/" + t.ID + "/" + e.ID, ask: true, mark: homeAskGlyph, words: words, age: a.trafficAge(e), act: act, hint: hint})
		}
		if kind == sideKindManager {
			for _, p := range a.tp.packets {
				if !p.Waiting() || p.Team != teamstore.Person || p.Origin != t.ID {
					continue
				}
				words := "decide: " + strings.Join(strings.Fields(p.Question), " ")
				out = append(out, sideBandItem{key: "packet/" + p.ID, ask: true, mark: homeAskGlyph, words: words,
					act: sideAct{kind: sideActTeams}, hint: words + hintSegment + "click opens the teams page to decide"})
			}
		}
	}
	members := a.railMembers()
	for _, node := range members[railAttention] {
		status := a.taskStatus(node)
		words := node.title
		if word := status.RowWord(); word != "" {
			words += railSep + word
		}
		// A RUN HELD AT ITS GATE WEARS THE GATE'S MARK here as in the list
		// ([app.railTreeGlyph]).
		mark := a.taskStateMark(node)
		if node.Paused() {
			mark = ansi.Strip(a.stripPausedGlyph())
		}
		out = append(out, sideBandItem{key: "task/" + itoa(int(node.id)), ask: true, mark: mark, words: words,
			act: sideAct{kind: sideActOpen, id: node.id}})
	}
	for _, node := range members[railDone] {
		if !a.sideBandFails(node) {
			continue
		}
		words := node.title
		if word := a.taskStatus(node).Word; word != "" {
			words += " " + word
		}
		out = append(out, sideBandItem{key: "fail/" + itoa(int(node.id)), mark: a.taskStateMark(node), words: words,
			act: sideAct{kind: sideActOpen, id: node.id}})
	}
	return out
}

// sideTaskOf is the task a band row's key names, 0 for a row that is not a
// task's.
func sideTaskOf(key string) uint64 {
	for _, lead := range []string{"task/", "fail/"} {
		if rest, ok := strings.CutPrefix(key, lead); ok {
			id := uint64(0)
			for _, c := range rest {
				if c < '0' || c > '9' {
					return 0
				}
				id = id*10 + uint64(c-'0')
			}
			return id
		}
	}
	return 0
}

// sideBandRows lays the band width wide: at most [sideBandCap] items and a
// `+N more` row, or every item when the person asked for them, and a rule
// under them. Nothing at all when nothing needs the person.
// fitClauses is words cut to width with the ellipsis where they run out, and
// A CUT THAT LEAVES A CLAUSE ITS SEPARATOR AND NOTHING ELSE ends at the clause
// before it instead: `Ship the price table…`, never `Ship the price table · …`,
// which spends three cells on saying there was more.
func fitClauses(words string, width int, tail string) (string, int) {
	if ansi.StringWidth(words) <= width {
		return words, ansi.StringWidth(words)
	}
	cut := ansi.Truncate(words, max(width, 0), tail)
	body := strings.TrimRight(strings.TrimSuffix(cut, tail), " ·")
	if at := strings.LastIndex(body, " · "); at > 0 && ansi.StringWidth(body[at+len(" · "):]) < 4 {
		body = strings.TrimRight(body[:at], " ·")
	}
	if body == "" {
		return cut, ansi.StringWidth(cut)
	}
	cut = body + tail
	return cut, ansi.StringWidth(cut)
}

func (a *app) sideBandRows(width int) []railLine {
	items := a.sideBand()
	if len(items) == 0 || width < 8 {
		return nil
	}
	shown := items
	more := 0
	if !a.side.bandAll && len(items) > sideBandCap+1 {
		shown, more = items[:sideBandCap], len(items)-sideBandCap
	}
	out := make([]railLine, 0, len(shown)+2)
	for _, item := range shown {
		row := &sideRow{key: item.key, hint: item.hint, act: item.act}
		// THE MARK IS THE ITEM'S OWN, and amber only for what is blocked on
		// the person: a failure is its ✗ in ordinary ink.
		mark, paint := item.mark, a.pal.ask
		lead := a.pal.askBold(mark)
		if !item.ask {
			paint = a.pal.ink
			lead = a.pal.ink(mark)
		}
		room := width - ansi.StringWidth(mark) - 1
		// THE AGE IS ON THE HINT LINE AT EVERY WIDTH, as the Traffic's rows
		// do: the arrow and the names need the cells the clock used to take.
		if item.age != "" {
			row.hint = sideHintWith(row.hint, item.age)
		}
		// A TRAFFIC ASK KEEPS `from → to` AND CUTS THE QUESTION AT A WORD. A task
		// row has no arrow, and still cuts at a clause.
		words := item.words
		tail := a.linearMark("…", "~")
		if at := strings.Index(item.words, "  "); at > 0 && (strings.Contains(item.words[:at], "→") || strings.Contains(item.words[:at], "->")) {
			prefix := item.words[:at+2]
			words = prefix + fitAtWord(item.words[at+2:], max(room-ansi.StringWidth(prefix), 0), tail)
			words = strings.TrimRight(words, " ")
		} else {
			words, _ = fitClauses(item.words, room, tail)
		}
		w := ansi.StringWidth(words)
		line := lead + " " + paint(words) + strings.Repeat(" ", max(room-w, 0))
		out = append(out, railLine{text: line, entry: -1, side: row})
	}
	if more > 0 || (a.side.bandAll && len(items) > sideBandCap+1) {
		word := "+" + itoa(more) + " more"
		hint := "Show all " + itoa(len(items)) + hintSegment + "click"
		if more == 0 {
			word, hint = "fewer", "Show three"+hintSegment+"click"
		}
		out = append(out, railLine{text: a.pal.dim(fit(word, width)), entry: -1,
			side: &sideRow{key: "band/more", hint: hint, act: sideAct{kind: sideActBand}}})
	}
	out = append(out, railLine{text: a.pal.dim(strings.Repeat(a.linearMark("─", "-"), width)), entry: -1})
	return out
}

// ── THE ASKS IN THE TRAFFIC ─────────────────────────────────────────────────

// sideAskCache is the Traffic's questions waiting on the person, as last read.
type sideAskCache struct {
	team, handle, last string
	rows, kind         int
	asks               []teamstore.Entry
}

// sideAsks is every member of team t that asked the person something and has
// not been answered, newest first: its latest asking event, until anything
// later from it, or from the person to it, says it moved on. In a member's
// chat, that member's only. Read from memory and kept until the log moves.
func (a *app) sideAsks(t team, kind int, handle string) []teamstore.Entry {
	rows := a.traffic.rows[t.ID]
	last := ""
	if len(rows) > 0 {
		last = rows[len(rows)-1].ID
	}
	c := &a.side.asks
	if c.team == t.ID && c.handle == handle && c.last == last && c.rows == len(rows) && c.kind == kind {
		return c.asks
	}
	var pending map[string]int
	for i := range rows {
		e := &rows[i]
		switch {
		case e.Kind == teamstore.KindEvent && trafficAsking(*e) && e.From != teamstore.FromManager && e.From != teamstore.FromSystem:
			if pending == nil {
				pending = map[string]int{}
			}
			pending[e.From] = i
		case e.Kind == teamstore.KindYou || e.From == teamstore.FromYou || teamstore.IsRuling(*e):
			for _, h := range e.Recipients() {
				delete(pending, h)
			}
			if e.To == teamstore.ToEveryone {
				pending = nil
			}
		case e.Wake():
			// A member woken again on its question is running once more.
			delete(pending, strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(e.Text), "woke "), "@"))
		default:
			delete(pending, e.From)
		}
	}
	var asks []teamstore.Entry
	for i := len(rows) - 1; i >= 0; i-- {
		e := rows[i]
		if at, ok := pending[e.From]; !ok || at != i {
			continue
		}
		if kind == sideKindMember && e.From != handle {
			continue
		}
		asks = append(asks, e)
	}
	*c = sideAskCache{team: t.ID, handle: handle, last: last, rows: len(rows), kind: kind, asks: asks}
	return asks
}

// sideAsked reports whether entry id is one of the band's questions.
func (a *app) sideAsked(t team, kind int, handle, id string) bool {
	for _, e := range a.sideAsks(t, kind, handle) {
		if e.ID == id {
			return true
		}
	}
	return false
}

// ── THE HEAD OF THE COLUMN, AS THE ROSTER LAYS IT ───────────────────────────

// sideHead is the column's pinned rows: the header, and the band under it.
// They stand at the top of every view and scroll with nothing.
func (a *app) sideHead(width int) []railLine {
	text, row := a.sideHeadRow(width)
	out := []railLine{{text: text, entry: -1, side: row}}
	return append(out, a.sideBandRows(width)...)
}

// sideRemember keeps the rows a layout drew, for the hint line.
func (a *app) sideRemember(lines []railLine) {
	a.side.last = a.side.last[:0]
	for _, l := range lines {
		if l.side != nil {
			a.side.last = append(a.side.last, l.side)
		}
	}
}

// sideRowOf is the last drawn row with identity key, nil for none.
func (a *app) sideRowOf(key string) *sideRow {
	for _, r := range a.side.last {
		if r.key == key {
			return r
		}
	}
	return nil
}

// ── UNDER THE HAND ──────────────────────────────────────────────────────────

// sideLineCol is the column's own column under screen column x.
func (a *app) sideLineCol(x int) int {
	return x - a.railLeft() - ansi.StringWidth(railSeam)
}

// sideHoverAt is the hover the column's own rows answer with: a door of the
// header, a row of the band or of the Traffic, or a door on one. A row with
// nothing to press answers with nothing, so it does not light.
func (a *app) sideHoverAt(x, y int) (hoverAt, bool) {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return hoverAt{}, false
	}
	line, ok := a.railLineAt(y)
	if !ok {
		return hoverAt{}, false
	}
	if line.side == nil {
		if e, ok := a.railEntryAt(y); ok && e.head && e.group != railRunning {
			return hoverAt{kind: hoverRailGroup, index: int(e.group)}, true
		}
		return hoverAt{}, false
	}
	if door := line.side.doorAt(a.sideLineCol(x)); door >= 0 {
		return hoverAt{kind: hoverSide, key: line.side.key, index: door}, true
	}
	if line.side.pressable() {
		return hoverAt{kind: hoverSide, key: line.side.key, index: -1}, true
	}
	return hoverAt{}, false
}

// sideHovering reports whether the pointer is on row key as a whole row.
func (a *app) sideHovering(key string) bool {
	return key != "" && a.hot.kind == hoverSide && a.hot.key == key && a.hot.index < 0
}

// sidePress answers a press on one of the column's own rows at the column's
// column x.
func (a *app) sidePress(row *sideRow, x int) (tea.Cmd, bool) {
	a.railWhere = railSpot{key: row.key}
	if door := row.doorAt(x); door >= 0 {
		return a.sideDo(row.doors[door].act), true
	}
	return a.sideDo(row.act), true
}

// sideDo does one act. Nothing here takes the keyboard.
func (a *app) sideDo(act sideAct) tea.Cmd {
	switch act.kind {
	case sideActOpen:
		node := a.tasks[act.id]
		if node == nil {
			return nil
		}
		a.sideAck(node)
		return a.openRailRoom(node)
	case sideActJump:
		a.railTake(false)
		return a.trafficJump(act.key, act.entry)
	case sideActThread:
		a.sideToggleThread(act.key)
	case sideActBand:
		a.side.bandAll = !a.side.bandAll
		a.dropHover()
		a.touch()
	case sideActView:
		a.sideSetView(act.view)
	case sideActHide:
		a.sideToggle()
	case sideActTeams:
		a.railTake(false)
		return a.showPage(pageTeams)
	}
	return nil
}

// sideAck records that the person opened a failed task, which takes it out
// of the band.
func (a *app) sideAck(node *taskNode) {
	if node == nil || node.state != session.TaskFailed {
		return
	}
	if a.side.acked == nil {
		a.side.acked = map[uint64]bool{}
	}
	a.side.acked[node.id] = true
}

// sideGroupShut reports whether a task group is folded to its heading. The
// running work is always open; the rest open when the person opens them, and
// stay that way for the session.
func (a *app) sideGroupShut(g railGroup) bool {
	return g != railRunning && !a.side.open[g]
}

// sideToggleGroup opens a folded group or folds it again.
func (a *app) sideToggleGroup(g railGroup) {
	if g == railRunning || g >= railGroupCount {
		return
	}
	a.side.open[g] = !a.side.open[g]
	a.touch()
}

// sideToggleThread lays a work thread's replies open under it, or folds them.
func (a *app) sideToggleThread(key string) {
	if a.side.threads == nil {
		a.side.threads = map[string]bool{}
	}
	if a.side.threads[key] {
		delete(a.side.threads, key)
	} else {
		a.side.threads[key] = true
	}
	a.side.moved++
	a.touch()
}

// sideRowAct is what enter does on the column's row key: a work thread's
// replies are laid open or folded, which is the one thing a keyboard cannot
// otherwise reach on it, and every other row does what a press on it does.
func (a *app) sideRowAct(key string) tea.Cmd {
	row := a.sideRowOf(key)
	if row == nil {
		return nil
	}
	for _, d := range row.doors {
		if d.act.kind == sideActThread {
			return a.sideDo(d.act)
		}
	}
	return a.sideDo(row.act)
}

// sideSpots is every one of the column's own rows the keyboard walks, in the
// order they are drawn: the band, then the Traffic's rows while it is in
// front.
func (a *app) sideSpots() []railSpot {
	var out []railSpot
	for _, l := range a.sideHead(a.railRoom()) {
		if l.side != nil && l.side.pressable() {
			out = append(out, railSpot{key: l.side.key})
		}
	}
	if a.sideView() == sideTraffic {
		lines, _ := a.sideTrafficView(a.viewHeight())
		for _, l := range lines {
			if l.side != nil && l.side.pressable() {
				out = append(out, railSpot{key: l.side.key})
			}
		}
	}
	return out
}

// sideHoverWords is what the hint line says with the pointer on the column,
// "" anywhere else.
func (a *app) sideHoverWords() string {
	switch a.hot.kind {
	case hoverSide:
		if a.hot.key == sideHeadKey {
			width := a.railRoom()
			_, row := a.sideHeadRow(width)
			if i := a.hot.index; i >= 0 && i < len(row.doors) {
				return row.doors[i].hint
			}
			return ""
		}
		row := a.sideRowOf(a.hot.key)
		if row == nil {
			return ""
		}
		if i := a.hot.index; i >= 0 && i < len(row.doors) {
			return row.doors[i].hint
		}
		// A TASK'S ROW IN THE BAND SAYS WHAT ITS ROW IN THE LIST SAYS, read
		// when the pointer is on it and not for every frame.
		if row.hint == "" && row.act.kind == sideActOpen {
			return a.sideTaskHint(a.tasks[row.act.id])
		}
		return row.hint
	case hoverRailGroup:
		g := railGroup(a.hot.index)
		if g >= railGroupCount {
			return ""
		}
		if a.sideGroupShut(g) {
			return "Show what is " + railGroupWords[g] + hintSegment + "click"
		}
		return "Fold what is " + railGroupWords[g] + hintSegment + "click"
	case hoverRail:
		return a.sideTaskHint(a.tasks[a.hot.id])
	case hoverRailGrip:
		words := "Show this column" + hintSegment + a.chords.say(sideHideKey)
		if t, kind, handle := a.sideTeam(); kind != sideKindPlain {
			if _, fresh := a.sideTrafficCount(t, kind, handle); fresh > 0 {
				words += hintSegment + itoa(fresh) + " new in the traffic"
			}
		}
		return words
	}
	return ""
}

// sideTaskHint is the hint line over a task's row, in the list or in the
// band: its whole title, then what the row had no room for (what it is doing,
// what it spent), then what a press does.
func (a *app) sideTaskHint(node *taskNode) string {
	if node == nil {
		return ""
	}
	words := strings.TrimSpace(node.label)
	if words == "" {
		words = node.title
	}
	for _, under := range a.railUnder(node, 400) {
		if s := strings.TrimSpace(ansi.Strip(under)); s != "" {
			words += hintSegment + s
		}
	}
	return words + hintSegment + "click opens it"
}

// sideBackHint is what the legend's hint slot says while the column is away:
// the key, and what it brings back.
func (a *app) sideBackHint() string {
	if a.sideKind() == sideKindManager {
		return a.chords.say(sideHideKey) + " traffic"
	}
	return a.chords.say(sideHideKey) + " tasks"
}
