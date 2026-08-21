package tui3

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// harnessCard is the design thread's one feed object. Live fields are replaced
// by the page when it lands; settled fields remain after the answer.
type harnessCard struct {
	id                         uint64
	goal, phase, hint, thought string
	model                      string
	task                       uint64
	attempt, attempts, bytes   int
	stalled                    bool
	began                      time.Time
	ended                      time.Time
	page                       *subharness.Harness
	state                      string
	buttonRow                  string
}

func (a *app) harnessCardOf(id uint64) (*harnessCard, int) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if c := a.entries[i].harness; c != nil && c.id == id {
			return c, i
		}
	}
	return nil, -1
}

func (a *app) beginHarnessCard(ev session.Event) {
	a.closeLive()
	c := &harnessCard{id: ev.ID, goal: ev.Text, phase: "designing", hint: "thinking", model: ev.Model, began: time.Now()}
	if ev.Task != nil {
		c.task = ev.Task.ID
	}
	a.entries = append(a.entries, entry{kind: entryHarness, turn: a.turn, harness: c})
	a.follow()
	a.touch()
}

func (a *app) progressHarnessCard(ev session.Event) {
	c, i := a.harnessCardOf(ev.ID)
	if c == nil {
		a.beginHarnessCard(ev)
		c, i = a.harnessCardOf(ev.ID)
	}
	c.goal, c.phase, c.hint, c.thought = ev.Goal, ev.Phase, ev.Hint, ev.ThoughtTail
	c.attempt, c.attempts, c.bytes, c.stalled = ev.Attempt, ev.Attempts, ev.Bytes, ev.Stalled
	a.entries[i].stale = true
	a.follow()
	a.touch()
}

// progressHarnessRoom gives a design's own room the same live edge as its feed
// card. The feed card owns the mapping from a design call to its task node; an
// unknown node or a closed room therefore has nowhere honest to draw and is a
// no-op. This row is never appended to entries, so it can never reach a journal.
func (a *app) progressHarnessRoom(ev session.Event) {
	c, _ := a.harnessCardOf(ev.ID)
	if c == nil || c.task == 0 || a.tasks[c.task] == nil || a.room == nil || a.room.id != c.task {
		return
	}
	parts := []string{"harness · " + firstNonEmpty(ev.Phase, "designing")}
	if ev.Attempts > 1 {
		parts = append(parts, fmt.Sprintf("attempt %d/%d", ev.Attempt, ev.Attempts))
	}
	// THE HINT COMES FIRST IN HERE, the opposite of the feed card's order. The
	// room already streams the designer's reasoning in full, so a snippet of
	// ThoughtTail on this row would repeat the prose scrolling right above it —
	// while the hint ("naming it: X", "4 steps so far", "receiving · 12.3 KB")
	// is the JSON half of the stream, which the room deliberately never shows
	// (session's designSeat.says) and this row is the only place to read.
	tail := strings.TrimSpace(ev.Hint)
	if tail == "" {
		tail = firstLineOf(strings.TrimSpace(ev.ThoughtTail))
	}
	if tail != "" {
		parts = append(parts, tail)
	}
	a.room.harnessProgress = strings.Join(parts, " · ")
	a.roomTouched()
}

func (a *app) finishHarnessCard(ev session.Event) {
	c, i := a.harnessCardOf(ev.ID)
	if c == nil {
		a.beginHarnessCard(ev)
		c, i = a.harnessCardOf(ev.ID)
	}
	page := *ev.Harness
	c.page, c.phase, c.hint, c.thought = &page, "", "", ""
	c.ended = time.Now()
	// The room's one-line ticker is stale news the moment the page lands: left
	// alone it kept saying "harness · reviewing · …" under a design that was
	// already awaiting the person's look, because no further progress event was
	// ever coming to replace it.
	if a.room != nil && c.task != 0 && a.room.id == c.task {
		a.room.harnessProgress = ""
	}
	a.entries[i].stale = true
	a.follow()
	a.touch()
}

// withdrawHarnessCard takes a design card back to the LIVE form it wore while
// the page was first being written, because that is what is happening again: the
// person asked for a change, the designer is at work on it, progress is about to
// start arriving, and one finished page follows.
//
// IT IS ONE CARD FOR THE WHOLE DESIGN and never a second one. A design has one
// id and one block in the feed (session's harness_task.go mints one number for
// the node and the question both), so a rewrite is that block going back to
// being live rather than a new card under the old one — which would leave two
// pages on screen, one of them answerable and wrong.
//
// The person's own words go on the row as the hint, because the one thing worth
// reading while a rewrite is in flight is what was asked for.
func (a *app) withdrawHarnessCard(ev session.Event) {
	c, i := a.harnessCardOf(ev.ID)
	if c == nil {
		return
	}
	c.page, c.state, c.thought = nil, "", ""
	c.phase, c.hint = "rewriting", firstLineOf(ev.Text)
	c.attempt, c.attempts, c.bytes, c.stalled = 0, 0, 0, false
	c.began, c.ended = time.Now(), time.Time{}
	a.entries[i].stale = true
	a.follow()
	a.touch()
}

func (a *app) harnessFeedRows(c *harnessCard, width int, selected bool) []string {
	if c == nil {
		return nil
	}
	if c.page == nil {
		head := "⠿ harness · " + c.phase
		switch {
		case c.model != "" && c.goal != "":
			head += " with " + c.model + " · " + c.goal
		case c.model != "":
			head += " with " + c.model
		case c.goal != "":
			head += " " + c.goal
		}
		if c.task != 0 {
			head += fmt.Sprintf(" — task %d", c.task)
		}
		if c.attempts > 1 {
			head += fmt.Sprintf(" · attempt %d/%d", c.attempt, c.attempts)
		}
		if c.stalled {
			head += fmt.Sprintf(" · thinking · %ds — reasoning models answer in one burst at the end", int(time.Since(c.began).Seconds()))
		}
		out := []string{a.pal.dim(fit(head, width))}
		if tail := strings.TrimSpace(c.thought); tail != "" {
			lines := wrap(tail, width-2)
			if len(lines) > 3 {
				lines = lines[len(lines)-3:]
			}
			for _, line := range lines {
				out = append(out, a.pal.dim("  "+line))
			}
		}
		if c.hint != "" {
			out = append(out, a.pal.dim(fit("  "+c.hint, width)))
		}
		return out
	}
	inner := width - 4
	if inner < 12 {
		inner = 12
	}
	name := c.page.Id.Name
	version := c.page.Id.Version
	if version == 0 {
		version = 1
	}
	head := "┌─ harness designed " + strings.Repeat("─", max(1, inner-24)) + fmt.Sprintf(" v%d ─┐", version)
	rows := []string{fit(head, width), boxLine(name, inner), boxLine(c.page.Id.Desc, inner), boxLine("", inner)}
	for _, line := range harnessDiagram(*c.page, inner, layoutTier(width) == tierPhone) {
		rows = append(rows, boxLine(line, inner))
	}
	rows = append(rows, boxLine("", inner))
	for _, line := range harnessPoints(*c.page) {
		for _, part := range wrap(line, inner) {
			rows = append(rows, boxLine(part, inner))
		}
	}
	if !c.began.IsZero() {
		end := c.ended
		if end.IsZero() {
			end = time.Now()
		}
		rows = append(rows, boxLine("thought for "+taskSpanWord(end.Sub(c.began)), inner))
	}
	actions := harnessCardActions
	if c.state != "" {
		actions = c.state
	}
	c.buttonRow = actions
	rows = append(rows, boxLine(actions, inner), "└"+strings.Repeat("─", inner+2)+"┘")
	if selected {
		rows[0] = a.pal.accent(rows[0])
	}
	return rows
}

func boxLine(s string, width int) string {
	return "│ " + fit(s, width) + strings.Repeat(" ", max(0, width-ansi.StringWidth(fit(s, width)))) + " │"
}

func harnessPoints(h subharness.Harness) []string {
	out := make([]string, 0, len(h.Program.Nodes)+2)
	for _, n := range h.Program.Nodes {
		detail := firstNonEmpty(n.Fields.Get("brief"), n.Fields.Get("check"), n.Fields.Get("tool"), n.Kind)
		out = append(out, "• "+n.Id+": "+firstLineOf(detail))
	}
	if h.Verify.Ladder != "" {
		out = append(out, "verify: "+h.Verify.Ladder)
	}
	if len(h.Whitelist) > 0 {
		out = append(out, "tools: "+strings.Join(h.Whitelist, " · "))
	}
	return out
}

// harnessDiagram lays nodes by dependency depth. A single path uses the compact
// horizontal form; branches use stable stacked rows; phones always rotate the
// same order vertically.
func harnessDiagram(h subharness.Harness, width int, phone bool) []string {
	ids := make([]string, 0, len(h.Program.Nodes))
	for _, n := range h.Program.Nodes {
		ids = append(ids, n.Id)
	}
	if len(ids) == 0 {
		return nil
	}
	linear := len(h.Program.Edges) == len(ids)-1
	if phone {
		out := []string{}
		for i, id := range ids {
			out = append(out, "┌"+strings.Repeat("─", min(14, max(4, len(id))))+"┐", "│ "+fit(id, min(12, max(2, width-4)))+" │", "└"+strings.Repeat("─", min(14, max(4, len(id))))+"┘")
			if i < len(ids)-1 {
				out = append(out, "▼")
			}
		}
		return out
	}
	if !linear {
		pred, succ := map[string]int{}, map[string][]string{}
		for _, edge := range h.Program.Edges {
			succ[edge.From()] = append(succ[edge.From()], edge.To())
			pred[edge.To()]++
		}
		for _, id := range ids {
			if len(succ[id]) < 2 {
				continue
			}
			out := []string{"[" + fit(id, 12) + "]"}
			for i, branch := range succ[id] {
				arm := "├──▶ "
				if i == len(succ[id])-1 {
					arm = "└──▶ "
				}
				out = append(out, arm+"["+fit(branch, 12)+"]")
			}
			for _, join := range ids {
				if pred[join] > 1 {
					out = append(out, "      ▼", "    ["+fit(join, 12)+"]")
					break
				}
			}
			return out
		}
	}
	boxes := make([]string, len(ids))
	for i, id := range ids {
		boxes[i] = "[" + fit(id, 12) + "]"
	}
	return []string{strings.Join(boxes, "──▶")}
}

func (a *app) harnessCardKey(msg tea.KeyPressMsg) bool {
	if a.sel < 0 || a.sel >= len(a.entries) {
		return false
	}
	c := a.entries[a.sel].harness
	if c == nil || c.page == nil || c.state != "" {
		return false
	}
	switch msg.String() {
	case "enter":
		a.resolveHarnessCard(c, "save")
	case "e":
		a.resolveHarnessCard(c, "improve")
	case "esc":
		a.resolveHarnessCard(c, "drop")
	default:
		return false
	}
	return true
}

// resolveHarnessCard is the one place a design is answered from, whichever door
// the answer came through — this card's keys, its clickable columns, or the
// approval row pinned in the design's own room (roomapproval.go).
//
// ── WHAT `e` USED TO DO, AND WHY IT DOES NOT ANY MORE ──
//
// `e` was labelled "improve" and it DROPPED THE DESIGN. It called ResolveHarness
// with false — the same call the discard key makes — and then put "Improve
// harness X: " in the message box, so that asking for a change quietly destroyed
// the page you were asking about and started a second design from scratch,
// minutes of model work and a name that would land as a second version. Nothing
// on screen said so. A person who pressed it and then changed their mind had
// nothing left to go back to.
//
// It is a DOOR now. The design stays exactly where it is, still waiting, and the
// key walks into its room — which is the place a change is actually made:
// saying what is wrong there hands the page back to the designer and it is
// rewritten and put in front of you again (session's revise_design). The state
// is not set, because nothing was resolved.
func (a *app) resolveHarnessCard(c *harnessCard, action string) {
	if action == "save" {
		if a.agent != nil {
			a.agent.ResolveHarness(c.id, true, "")
		}
		c.state = "saved as " + c.page.Id.Name + " v1"
	}
	if action == "drop" {
		if a.agent != nil {
			a.agent.ResolveHarness(c.id, false, "")
		}
		c.state = "dropped"
	}
	if action == "improve" {
		a.openHarnessRoom(c)
	}
	a.touch()
}

// openHarnessRoom walks into the design's own room and points the box at it.
//
// THE ROOM IS THE ONLY PLACE THIS CAN GO. A change to a page is a sentence the
// design's THREAD has to read — it is the thing holding the page, and the only
// thing on this surface that can hand it back to the designer — and the message
// box talks to a thread only from inside its room. Prefilling the main box would
// send the sentence to the conversation, which would answer it with prose about
// a design it is not part of.
//
// A DESIGN WITH NO NODE HAS NO ROOM, and that is a real case rather than a
// defensive one: a surface talking to an older engine gets a card with no task
// on it (harness.go's [designTaskWord] says the same). It says so in one dim line
// and leaves the card exactly as it was, because the card is still answerable.
func (a *app) openHarnessRoom(c *harnessCard) {
	if c.task == 0 || a.tasks[c.task] == nil {
		a.note(harnessNoRoomWord)
		return
	}
	title := ""
	if node := a.tasks[c.task]; node != nil {
		title = node.label
	}
	a.openRoom(c.task, title)
}

// harnessNoRoomWord is what `e` says when there is no room to open: the honest
// version of "this build cannot do that", naming the door that still works.
const harnessNoRoomWord = "this design has no room to open — answer the card here"

// The card's action row, in three pieces, so that the columns a press is
// resolved against are MEASURED off the row that was drawn rather than guessed
// at beside it. They were two hardcoded numbers, and they were already a cell or
// two out of step with the words they were meant to sit under.
//
// THE MIDDLE ONE READS "change it" AND NOT "improve", because that is what it
// does now: it opens the design's room, where saying what is wrong hands the
// page back to the designer ([app.resolveHarnessCard] tells the whole story of
// what that key used to do instead).
const (
	harnessCardSave   = "[enter] save"
	harnessCardChange = "[e] change it"
	harnessCardDrop   = "[esc] drop"
	harnessCardGap    = "   "
)

const harnessCardActions = harnessCardSave + harnessCardGap + harnessCardChange + harnessCardGap + harnessCardDrop

func (a *app) harnessCardPress(i, x int) {
	if i < 0 || i >= len(a.entries) {
		return
	}
	c := a.entries[i].harness
	if c == nil || c.page == nil || c.state != "" {
		return
	}
	// Each answer owns its own words and the gap that follows them, which is the
	// offer row's rule (harness.go's recordHarnessTaps): a press just past a word
	// is a person aiming at it.
	switch changeAt := len(harnessCardSave + harnessCardGap); {
	case x < changeAt:
		a.resolveHarnessCard(c, "save")
	case x < changeAt+len(harnessCardChange+harnessCardGap):
		a.resolveHarnessCard(c, "improve")
	default:
		a.resolveHarnessCard(c, "drop")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
