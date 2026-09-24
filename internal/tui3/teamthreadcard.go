package tui3

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── A THREAD IN THE CONVERSATION ────────────────────────────────────────────
//
// The rail is where a person reads the team at a glance (teamthread.go). The
// manager's own conversation is where it asked, so that is where the question
// keeps its answers: a manager's `team_send` row is a thread card,
//
//	├─▶ team_send ◆ to @agent @checking @review · do          ✓
//	│   │ Please provide a brief status update on your part
//	│   ├ @checking  ✓ Status update: the lexer is in
//	│   └ @review  working…
//
// the message quoted under the call and each member's answer attached under it
// in muted ink as it lands, one line each. A handle is a link, as everywhere in
// a chat (teamlink.go); a press on the words lays them out in full and a second
// folds them, and under the pointer the hint line says them whole. A press on
// the call's own line still opens the call, which is what every tool row does.
//
// THE MEMBER'S CHAT MIRRORS IT. A manager's line delivered to a member is
// already a quoted card (teamcard.go); under it the member's own answers to
// that line are attached the same way, so a person opening the member reads
// the question and what it said back in one place.
//
// AND THE MANAGER IS NOT TOLD IT TWICE. A member's reply reaches the manager's
// model as a delivery note, which replays as a team card of its own. A reply
// already under its question's card is left out of that card, and a card left
// with nothing is one dim line saying who answered and where to read it. The
// model's transcript is unchanged; only the drawing folds.
//
// THE CARDS DRAW MEMORY. Everything is read from the Traffic cache the rail's
// clock keeps (teamtraffic.go), keyed by that cache's version, so a frame reads
// no disk and a card grows the frame after a reply is read.

// threadMemo is what one thread card last drew and every fact that decided it.
type threadMemo struct {
	// args is the call's arguments the target and root were read from.
	args   string
	target string
	to     []string
	text   string
	kind   string
	// team and root name the thread's first message once it is found in the
	// cache; found says it was.
	team, root string
	found      bool
	stamp      threadStamp
	rows       []threadRow
}

// threadStamp is everything a card's rows are drawn from beside the entry.
type threadStamp struct {
	version, opened, room int
	hot                   string
	ascii                 bool
	front                 string
}

// threadRow is one row of a card and the message a press on it opens.
type threadRow struct {
	text string
	open string
}

// threadCardLines is how many lines of a manager's message a folded card
// quotes.
const threadCardLines = 3

// teamSendArgs reads a team_send call's arguments once per change.
func (m *threadMemo) teamSendArgs(args string) {
	if m.args == args && m.args != "" {
		return
	}
	m.args = args
	var parsed struct {
		To   string `json:"to"`
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(args), &parsed)
	m.to = m.to[:0]
	for _, w := range strings.FieldsFunc(strings.ToLower(parsed.To), func(r rune) bool { return r == ' ' || r == ',' || r == ';' }) {
		if w = strings.TrimPrefix(w, "@"); w != "" {
			m.to = append(m.to, w)
		}
	}
	m.text, m.kind = strings.TrimSpace(parsed.Text), parsed.Kind
	m.found, m.target = false, ""
}

// teamSendTarget is a team_send row's sentence: `◆ to @a @b · do`, the handles
// the call named and whether it was a directive. "" when the arguments say
// nothing.
func (a *app) teamSendTarget(e *entry) string {
	if e.detail.Args == "" {
		return ""
	}
	if e.thread == nil {
		e.thread = &threadMemo{}
	}
	m := e.thread
	m.teamSendArgs(e.detail.Args)
	if m.target != "" {
		return m.target
	}
	if len(m.to) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(a.teamManagerMark() + " to")
	for _, h := range m.to {
		if h == teamstore.ToEveryone {
			b.WriteString(" everyone")
			continue
		}
		b.WriteString(" @" + h)
	}
	tag := "fyi"
	if m.kind == teamstore.KindDirective {
		tag = "do"
	}
	b.WriteString(" · " + tag)
	m.target = b.String()
	return m.target
}

// teamSendFind looks for a team_send call's message in the Traffic cache: by
// the number its answer carries, or, for an answer from before numbers, by its
// words. It is asked again only when the cache moved.
func (a *app) teamSendFind(e *entry, m *threadMemo) {
	if m.found {
		return
	}
	id := ""
	if at := strings.LastIndex(e.detail.Output, "(#"); at >= 0 {
		end := strings.IndexByte(e.detail.Output[at:], ')')
		if end > 0 {
			id, _ = teamstore.ThreadID(e.detail.Output[at+1 : at+end])
		}
	}
	for teamID, rows := range a.traffic.rows {
		for i := len(rows) - 1; i >= 0; i-- {
			r := rows[i]
			if r.From != teamstore.FromManager || (r.Kind != teamstore.KindNote && r.Kind != teamstore.KindDirective) {
				continue
			}
			if (id != "" && r.ID == id && strings.TrimSpace(r.Text) == m.text) || (id == "" && strings.TrimSpace(r.Text) == m.text) {
				m.team, m.root, m.found = teamID, r.ID, true
				return
			}
		}
	}
}

// threadOf is the thread in rows that begins with root: root and everything
// that answers it, directly or through another answer.
func threadOf(rows []teamstore.Entry, root string) (teamstore.Thread, bool) {
	var th teamstore.Thread
	in := map[string]bool{}
	for _, r := range rows {
		switch {
		case r.ID == root:
			th.Root, th.Latest = r, r.ID
			in[r.ID] = true
		case r.Answers != "" && in[r.Answers] && trafficShown(r):
			th.Replies = append(th.Replies, r)
			th.Latest = r.ID
			in[r.ID] = true
		}
	}
	return th, th.Root.ID != ""
}

// teamSendCard is the card a team_send row hangs, room wide, and false for a
// row that hangs none. i is the row's entry, for the pointer.
func (a *app) teamSendCard(e *entry, i, room int) ([]threadRow, bool) {
	if e.tool != "team_send" || e.status == toolForming || e.detail.Args == "" {
		return nil, false
	}
	if e.thread == nil {
		e.thread = &threadMemo{}
	}
	m := e.thread
	m.teamSendArgs(e.detail.Args)
	if m.text == "" {
		return nil, false
	}
	stamp := a.threadStampOf(i, room)
	if m.rows != nil && m.stamp == stamp {
		return m.rows, true
	}
	a.teamSendFind(e, m)
	pal := a.pal
	bar := a.linearMark("│", "|") + " "
	key := ""
	if m.found {
		key = trafficOpenKey(m.team, m.root)
	}
	hot := func(open string) bool {
		return open != "" && a.hot.kind == hoverThread && a.hot.entry == i && a.hot.key == open
	}
	var rows []threadRow
	words := wrap(m.text, max(room-2, 8))
	if !a.traffic.open[key] && len(words) > threadCardLines {
		words = append(words[:threadCardLines-1:threadCardLines-1], ansi.Truncate(words[threadCardLines-1], max(room-3, 4), "")+a.linearMark("…", "..."))
	}
	for _, w := range words {
		text := pal.ink(w)
		if hot(key) {
			text = pal.cursor(pal.ink(w), 0)
		}
		rows = append(rows, threadRow{text: pal.dim(bar) + text, open: key})
	}
	if m.found {
		if th, ok := threadOf(a.traffic.rows[m.team], m.root); ok {
			rows = append(rows, a.threadReplyRows(m.team, th, "", room, hot)...)
		}
	}
	m.rows, m.stamp = rows, stamp
	return rows, true
}

// threadStampOf is the facts beside the entry a card at i, room wide, is drawn
// from.
func (a *app) threadStampOf(i, room int) threadStamp {
	s := threadStamp{version: a.traffic.version, opened: a.traffic.opened, room: room, ascii: a.pal.ascii, front: a.frontTabKey()}
	if a.hot.kind == hoverThread && a.hot.entry == i {
		s.hot = a.hot.key
	}
	return s
}

// threadReplyRows is a thread's answers as a card draws them, in muted ink,
// one line each: every member's, or only self's when self is a handle.
func (a *app) threadReplyRows(teamID string, th teamstore.Thread, self string, room int, hot func(string) bool) []threadRow {
	pal := a.pal
	lines := replyLines(th)
	if self != "" {
		kept := lines[:0:0]
		for _, r := range lines {
			if r.who == self {
				kept = append(kept, r)
			}
		}
		lines = kept
	}
	var rows []threadRow
	for n, r := range lines {
		glyph, under := a.linearMark("├ ", "|-"), a.linearMark("│ ", "| ")
		if n == len(lines)-1 {
			glyph, under = a.linearMark("└ ", "`-"), "  "
		}
		who := "@" + r.who
		if r.who == teamstore.FromManager {
			who = a.teamManagerMark() + " manager"
		}
		lead := pal.dim(glyph) + pal.muted(who) + "  "
		leadW := ansi.StringWidth(glyph + who + "  ")
		mark := ""
		switch r.state {
		case teamstore.StateFinished:
			mark = a.linearMark("✓", "ok") + " "
		case teamstore.StateFailed:
			mark = a.linearMark("✗", "x") + " "
		}
		var text, open string
		paint := pal.muted
		switch {
		case r.state == teamstore.StateAsking:
			text, paint = "asking: "+strings.TrimPrefix(r.note, "asks: "), pal.ask
			open = trafficOpenKey(teamID, r.last.ID)
		case r.said:
			text = strings.TrimSpace(r.entry.Text)
			open = trafficOpenKey(teamID, r.entry.ID)
		case r.state == "working":
			text, paint = "working"+a.linearMark("…", "..."), pal.dim
		default:
			text = r.state
			if r.note != "" && r.note != r.state {
				text += " · " + r.note
			}
		}
		room := max(room-leadW-ansi.StringWidth(mark), 8)
		paintHot := func(s string) string {
			if hot(open) {
				return pal.cursor(paint(s), 0)
			}
			return paint(s)
		}
		if open != "" && a.traffic.open[open] {
			for k, w := range wrap(text, room) {
				if k == 0 {
					rows = append(rows, threadRow{text: lead + pal.muted(mark) + paintHot(w), open: open})
					continue
				}
				rows = append(rows, threadRow{text: pal.dim(under) + strings.Repeat(" ", max(leadW-ansi.StringWidth(under), 0)) + paintHot(w), open: open})
			}
			continue
		}
		flat := strings.Join(strings.Fields(text), " ")
		if ansi.StringWidth(flat) > room {
			flat = ansi.Truncate(flat, room-1, "") + a.linearMark("…", "~")
		}
		rows = append(rows, threadRow{text: lead + pal.muted(mark) + paintHot(flat), open: open})
	}
	return rows
}

// threadHoverWords is what the hint line says with the pointer on a thread
// card's words: the message whole, and what a press does. "" anywhere else.
func (a *app) threadHoverWords() string {
	if a.hot.kind != hoverThread || a.hot.key == "" {
		return ""
	}
	teamID, id, _ := strings.Cut(a.hot.key, "/")
	for _, r := range a.traffic.rows[teamID] {
		if r.ID != id {
			continue
		}
		text := strings.Join(strings.Fields(r.Text), " ")
		if a.traffic.open[a.hot.key] {
			return "click folds it"
		}
		return text + hintSegment + "click shows it all"
	}
	return ""
}

// ── the team note's side ────────────────────────────────────────────────────

// teamNoteStamp reports whether a team note's cached rows are stale because
// the Traffic under it moved: its thread lines draw answers from the cache.
func (a *app) teamNoteStale(e *entry, width int) bool {
	if e.kind != entryTeam || len(e.team) == 0 || !a.wall.loaded {
		return false
	}
	stamp := threadStamp{version: a.traffic.version, opened: a.traffic.opened, room: width, ascii: a.pal.ascii, front: a.frontTabKey()}
	if e.thread == nil {
		e.thread = &threadMemo{}
	}
	if e.thread.stamp == stamp {
		return false
	}
	e.thread.stamp = stamp
	return true
}

// teamNoteTeam is the managed team this conversation is in that a note's team
// name names, false for none.
func (a *app) teamNoteTeam(name string) (team, bool) {
	front := a.frontTabKey()
	for _, t := range a.wall.teams {
		if t.Manager != "" && teamHolds(t, front) && (name == "" || t.Name == name) {
			return t, true
		}
	}
	return team{}, false
}

// teamNoteReplyShown reports whether a member's line delivered to the manager
// is an answer already drawn under its question's card in the manager's own
// conversation: the manager's message it answers is in the cache.
func (a *app) teamNoteReplyShown(t team, from, text string) bool {
	rows := a.traffic.rows[t.ID]
	text = strings.TrimSpace(text)
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.From != from || r.Kind != teamstore.KindNote || r.Answers == "" {
			continue
		}
		said := strings.TrimSpace(r.Text)
		if said != text && !(strings.HasSuffix(text, "…") && strings.HasPrefix(said, strings.TrimSuffix(text, "…"))) {
			continue
		}
		up := r.Answers
		for hop := 0; hop < 16 && up != ""; hop++ {
			found := false
			for j := i - 1; j >= 0; j-- {
				if rows[j].ID == up {
					if rows[j].From == teamstore.FromManager {
						return true
					}
					up, found = rows[j].Answers, true
					break
				}
			}
			if !found {
				return false
			}
		}
		return false
	}
	return false
}

// teamNoteSelf is this conversation's handle in the team a note came from, ""
// when it has none there or is its manager.
func (a *app) teamNoteSelf(name string) (team, string) {
	front := a.frontTabKey()
	for _, t := range a.wall.teams {
		if t.Manager == "" || t.Manager == front || (name != "" && t.Name != name) {
			continue
		}
		if m, ok := t.Member(front); ok && m.Handle != "" {
			return t, m.Handle
		}
	}
	return team{}, ""
}
