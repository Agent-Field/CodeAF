package tui3

// THE TASKS PLACE IS A READING OF THE WORLD, NOT A SECOND TASK STORE.
//
// The world scan has already paid for every project index and settled whether
// a live-looking row is really still running. This layer only groups that
// snapshot into the order a person acts on it, then paints it. Keeping the
// reading pure means a resize, cursor move, or time-window key can never touch
// disk or read a different clock halfway through one frame.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

const taskShown = 6

type tasksSection int

const (
	tasksNeeds tasksSection = iota
	tasksRunning
	tasksToday
	tasksEarlier
)

type tasksItem struct {
	entry   session.TaskIndexEntry
	row     session.SessionRow
	section tasksSection
}

// tasksReading is everything drawing and routing need from one world reading.
// seen is deliberately retained even though state grouping does not use it: it
// is the look stamp paired with this snapshot, and a later place adapter must
// not need to reach back to disk to preserve that boundary.
type tasksReading struct {
	items []tasksItem
	win   session.UsageWindow
	seen  time.Time
	now   time.Time
}

// readTasks walks every session's share of every project index exactly once.
func readTasks(world session.World, win session.UsageWindow, seen time.Time, now time.Time) tasksReading {
	r := tasksReading{win: win.Normalized(), seen: seen, now: now}
	sections := [4][]tasksItem{}
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				at := entry.EndedAt
				if row.Runs(entry) {
					at = now
				}
				if !r.win.Holds(at) {
					continue
				}
				section := tasksEarlier
				switch {
				case entry.Status == string(session.TaskUnverified) || (row.NeedsPerson() && row.Runs(entry)):
					section = tasksNeeds
				case row.Runs(entry):
					section = tasksRunning
				case tasksLandedToday(entry, now):
					section = tasksToday
				}
				sections[section] = append(sections[section], tasksItem{entry: entry, row: row, section: section})
			}
		}
	}
	for _, section := range sections {
		r.items = append(r.items, section...)
	}
	return r
}

func tasksLandedToday(entry session.TaskIndexEntry, now time.Time) bool {
	if entry.Status != string(session.TaskDone) && entry.Status != string(session.TaskFailed) {
		return false
	}
	if entry.EndedAt.IsZero() {
		return false
	}
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return !entry.EndedAt.Before(start) && entry.EndedAt.Before(start.AddDate(0, 0, 1))
}

// rows lays out one stable logical row per heading, task, and fold. The label
// is the elastic column; every other cell either earns its room or disappears
// in the stated order, so no painted row can cross the terminal edge.
func (r tasksReading) rows(width int, pal palette) []string {
	if width <= 0 || len(r.items) == 0 {
		return nil
	}
	var rows []string
	if head := r.header(width, pal); head != "" {
		rows = append(rows, head)
	}
	for _, section := range []tasksSection{tasksNeeds, tasksRunning, tasksToday, tasksEarlier} {
		items := r.section(section)
		if len(items) == 0 {
			continue
		}
		rows = appendPlaceSection(rows, pal.dim(fit(tasksSectionWord(section), width)))
		shown := min(len(items), taskShown)
		for _, item := range items[:shown] {
			rows = append(rows, tasksRow(item, width, r.now, pal))
		}
		if len(items) > shown {
			clause := ""
			if start := tasksWindowStart(r.win); start != "" {
				clause = "back to " + start
			}
			fold := foldLine(len(items)-shown, clause)
			rows = append(rows, pal.dim(fit(fold, width)))
		}
	}
	return rows
}

func (r tasksReading) header(width int, pal palette) string {
	if len(r.items) == 0 {
		return ""
	}
	word := fmt.Sprintf("work aforge ran on its own. %d", len(r.items))
	if start := tasksWindowStart(r.win); start != "" {
		word += " since " + start
	}
	var cost float64
	for _, item := range r.items {
		cost += item.entry.Cost
	}
	if cost > 0 {
		word += ", " + dollars(cost) + " of it"
	}
	return pal.muted(fit(word+".", width))
}

func tasksWindowStart(win session.UsageWindow) string {
	win = win.Normalized()
	if win.From.IsZero() {
		return ""
	}
	return strings.ToLower(win.From.Format("Jan 2"))
}

func (r tasksReading) section(want tasksSection) []tasksItem {
	items := make([]tasksItem, 0)
	for _, item := range r.items {
		if item.section == want {
			items = append(items, item)
		}
	}
	return items
}

func tasksSectionWord(section tasksSection) string {
	switch section {
	case tasksNeeds:
		return "needs your look"
	case tasksRunning:
		return "running"
	case tasksToday:
		return "done today"
	default:
		return "earlier"
	}
}

func tasksRow(item tasksItem, width int, now time.Time, pal palette) string {
	entry := item.entry
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = strings.TrimSpace(entry.Title)
	}
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyphInk(glyph) + " "
	source := strings.TrimSpace(item.row.Title)
	if source == "" {
		source = strings.TrimSpace(item.row.Project)
	}
	parts := []tasksCell{
		{text: source, ink: pal.dim, drop: tasksDropSource},
		{text: tasksMiddle(entry), ink: pal.dim, drop: tasksDropMiddle},
		{text: session.TaskKindWord(entry.Kind), ink: pal.dim, drop: tasksDropKind},
	}
	if entry.Cost > 0 {
		parts = append(parts, tasksCell{text: dollars(entry.Cost), ink: placeMoneyInk(pal), drop: tasksDropCost})
	}
	parts = append(parts, tasksCell{text: sinceAt(tasksEntryAt(entry, now), now), ink: pal.dim, drop: tasksDropAge})
	if width < 80 {
		parts = tasksOmit(parts, tasksDropMiddle)
	}
	for tasksFixedWidth(parts)+ansi.StringWidth(lead)+8 > width {
		before := len(parts)
		for _, drop := range []tasksDrop{tasksDropMiddle, tasksDropCost, tasksDropSource, tasksDropKind} {
			parts = tasksOmit(parts, drop)
			if len(parts) < before {
				break
			}
		}
		if len(parts) == before {
			break
		}
	}
	fixed := tasksFixedWidth(parts)
	labelRoom := width - ansi.StringWidth(lead) - fixed
	if labelRoom < 0 {
		labelRoom = 0
	}
	left := lead + pal.ink(fit(label, labelRoom))
	used := ansi.StringWidth(left)
	pad := width - used - fixed
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	b.WriteString(left)
	b.WriteString(strings.Repeat(" ", pad))
	for _, part := range parts {
		if part.text != "" {
			b.WriteByte(' ')
			b.WriteString(part.ink(part.text))
		}
	}
	return fit(b.String(), width)
}

type tasksDrop int

const (
	tasksDropMiddle tasksDrop = iota
	tasksDropCost
	tasksDropSource
	tasksDropKind
	tasksDropAge
)

type tasksCell struct {
	text string
	ink  func(string) string
	drop tasksDrop
}

func tasksOmit(parts []tasksCell, drop tasksDrop) []tasksCell {
	for i, part := range parts {
		if part.drop == drop && part.text != "" {
			return append(parts[:i:i], parts[i+1:]...)
		}
	}
	return parts
}

func tasksFixedWidth(parts []tasksCell) int {
	width := 0
	for _, part := range parts {
		if part.text != "" {
			width += 1 + ansi.StringWidth(part.text)
		}
	}
	return width
}

func tasksEntryAt(entry session.TaskIndexEntry, now time.Time) time.Time {
	if entry.Live() || entry.EndedAt.IsZero() {
		return now
	}
	return entry.EndedAt
}

func tasksMiddle(entry session.TaskIndexEntry) string {
	if activity := strings.TrimSpace(entry.Activity); activity != "" {
		return activity
	}
	if entry.Status == string(session.TaskFailed) && strings.TrimSpace(entry.Outcome) != "" {
		return "gave up, said why"
	}
	var parts []string
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if outcome := strings.TrimSpace(entry.Outcome); outcome != "" {
		parts = append(parts, outcome)
	}
	return strings.Join(parts, " · ")
}

func tasksGlyph(item tasksItem, pal palette) (string, func(string) string) {
	if item.section == tasksNeeds {
		return tokens.GlyphNeedsHuman, pal.warn
	}
	switch item.entry.Status {
	case string(session.TaskRunning):
		return tokens.GlyphWorking, pal.live
	case string(session.TaskQueued):
		return tokens.GlyphQueued, pal.dim
	case string(session.TaskDone):
		return tokens.GlyphSettled, pal.muted
	case string(session.TaskFailed):
		return tokens.GlyphFailed, pal.bad
	default:
		return tokens.GlyphQueued, pal.dim
	}
}

// at maps a painted page row back to its task. Headings, air, and the fold are
// deliberately holes so a cursor can move over the page without opening the
// wrong room.
func (r tasksReading) at(i int) (session.TaskIndexEntry, session.SessionRow, bool) {
	if i < 0 || len(r.items) == 0 {
		return session.TaskIndexEntry{}, session.SessionRow{}, false
	}
	at := 1 // The non-empty reading always begins with its header sentence.
	for _, section := range []tasksSection{tasksNeeds, tasksRunning, tasksToday, tasksEarlier} {
		items := r.section(section)
		if len(items) == 0 {
			continue
		}
		at += 2 // Blank air and the section word are not stops.
		shown := min(len(items), taskShown)
		for _, item := range items[:shown] {
			if at == i {
				return item.entry, item.row, true
			}
			at++
		}
		if len(items) > shown {
			at++
		}
	}
	return session.TaskIndexEntry{}, session.SessionRow{}, false
}

// step keeps the four time keys in one grammar shared with spend.
func (r tasksReading) step(win session.UsageWindow, key string) session.UsageWindow {
	switch key {
	case "shift+left":
		return win.Step(-1)
	case "shift+right":
		return win.Step(1)
	case "shift+up":
		return win.Coarser()
	case "shift+down":
		return win.Finer()
	default:
		return win
	}
}

// tasksTeach spends an empty page on explaining the place rather than drawing
// headings for lists that do not exist.
func tasksTeach(pal palette) []string {
	return []string{
		pal.dim("tasks is the history of work this machine has run."),
		pal.dim("it lists work aforge ran on its own, across every project."),
		pal.dim("enter opens a task's room when there is one here."),
	}
}
