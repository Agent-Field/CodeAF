package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TRAFFIC'S THREADS (teamrail.go says what the Traffic is) ──────────
//
// The Traffic is threads (internal/teams' thread.go): a message and what
// answered it, the thread that moved last first, and inside each one in the
// order it happened. The side column draws each as one row of work
// (sidetraffic.go) and the thread cards in the manager's conversation draw
// each answer under its question (teamthreadcard.go); both read the answers
// through [replyLines].
//
// ONE QUESTION IS ONE THREAD, NOT TWELVE ROWS. The wake that started a member
// is not a line: it is why the member reads `working…` until it answers. A
// member's finishing is not a line: it is the `✓` on its reply, or its own line
// when it said nothing. A failure is `✗`, and a member asking the person is
// the state `asking`.

// trafficOpenKey is an entry's key in the thread cards' record of what is laid
// out in full.
func trafficOpenKey(teamID, id string) string { return teamID + "/" + id }

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
		case e.Kind == teamstore.KindNote || e.Kind == teamstore.KindDirective || e.Kind == teamstore.KindAnswer || e.Kind == teamstore.KindQuestion:
			if l, ok := lineOf(e.From); ok && l.state == "working" && !l.said {
				l.entry, l.said, l.state, l.last = e, true, "", e
				continue
			}
			push(trafficReply{who: e.From, entry: e, said: true, last: e})
		}
	}
	return lines
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
		// A MANAGER'S ANSWER NAMES THE QUESTION IT ANSWERS in Reply, the
		// field it was written with before threads; it threads under it.
		if e.Kind == teamstore.KindAnswer && e.Answers == "" {
			e.Answers = e.Reply
		}
		shown = append(shown, e)
	}
	return teamstore.Threads(shown)
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
