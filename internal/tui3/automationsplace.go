package tui3

// automationsplace.go is what the automations place DRAWS: the list, and one
// automation's history, as pure readings of what the window already holds
// (place_automations.go is the state and the keys).
//
// NOTHING HERE READS THE DISK. The list is the watcher's last reading of the
// store (automationwatch.go) and a history is what the place asked for when it
// was opened, so a frame drawn on every keystroke costs arithmetic and nothing
// else (ARCHITECTURE.md's three layers).
//
// ONE ROW SAYS ONE AUTOMATION, ONCE. Its mark says what state it is in, its
// title is the person's own name for it, and one dim sentence says when it runs
// and how it last went — in the schedule's own words (internal/automation's
// words.go) and the task-state words a run's outcome is spelled in
// (docs/design/task-states/DESIGN.md).

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The words the place says. Each is quoted in the manual exactly as it is
// spelled here (internal/manual/chat/automations.md).
const (
	// autoKindReminder, autoKindWork and autoKindWatch are the three kinds as a
	// row names them.
	autoKindReminder = "reminder"
	autoKindWork     = "scheduled work"
	autoKindWatch    = "watch"
	// autoRunningWord is a row with a run in hand right now.
	autoRunningWord = "running now"
	// autoQueuedWord is a row with a run asked for and not yet started.
	autoQueuedWord = "about to run"
	// autoPausedWord and autoFinishedWord are the two states that wake for
	// nothing.
	autoPausedWord   = "paused"
	autoFinishedWord = "finished"
	// autoNextWord and autoLastWord lead the two clocks a row can carry.
	autoNextWord = "next "
	autoLastWord = "last "
	// autoLateWord leads how late a run started after its slot.
	autoLateWord = "late "
	// autoNoRunsWord is a history with nothing in it yet.
	autoNoRunsWord = "it has not run yet"
	// autoReadingWord is a history being read.
	autoReadingWord = "reading its history"
)

// automationKindWord is the kind as a row names it.
func automationKindWord(item automation.Automation) string {
	switch item.Kind() {
	case automation.KindReminder:
		return autoKindReminder
	case automation.KindWatch:
		return autoKindWatch
	}
	return autoKindWork
}

// automationActive is the run in hand or waiting for one automation, if any.
func automationActive(active []automation.Run, id string) (automation.Run, bool) {
	for _, run := range active {
		if run.AutomationID == id {
			return run, true
		}
	}
	return automation.Run{}, false
}

// automationRowGlyph is the mark one row wears: the state it is in now, and
// when it is merely waiting for its time, what its last run came to.
func automationRowGlyph(item automation.Automation, active []automation.Run) tokens.GlyphID {
	if run, ok := automationActive(active, item.ID); ok {
		if run.Phase == automation.PhaseRunning {
			return tokens.GWorking
		}
		return tokens.GQueued
	}
	switch item.Status {
	case automation.StatusPaused:
		return tokens.GPaused
	case automation.StatusFinished:
		return tokens.GSettled
	}
	if last := item.Last; last != nil {
		switch last.Outcome {
		case automation.OutcomeYourCall, automation.OutcomeUnchecked:
			return tokens.GNeedsHuman
		case automation.OutcomeIncomplete:
			return tokens.GFailed
		}
	}
	return tokens.GQueued
}

// automationRowSaid is the row's one sentence: the kind, when it runs, where it
// is now, and what its last run came to. Every clause is said only when there
// is something to say (the emptiness law).
func automationRowSaid(item automation.Automation, active []automation.Run, now time.Time) string {
	clauses := []string{automationKindWord(item)}
	if item.Schedule.Repeats() || item.Status != automation.StatusFinished {
		clauses = append(clauses, automation.Describe(item.Schedule, now))
	}
	if run, ok := automationActive(active, item.ID); ok {
		if run.Phase == automation.PhaseRunning {
			clauses = append(clauses, autoRunningWord)
		} else {
			clauses = append(clauses, autoQueuedWord)
		}
	} else {
		switch item.Status {
		case automation.StatusPaused:
			clauses = append(clauses, autoPausedWord)
		case automation.StatusFinished:
			clauses = append(clauses, autoFinishedWord)
		default:
			if !item.Next.IsZero() && item.Schedule.Repeats() {
				clauses = append(clauses, autoNextWord+automation.Moment(item.Next, now))
			}
		}
	}
	if last := item.Last; last != nil && last.Outcome != "" {
		clauses = append(clauses, autoLastWord+last.Outcome.Word())
	}
	out := ""
	for _, clause := range clauses {
		out = joinDot(out, strings.TrimSpace(clause))
	}
	return out
}

// automationsListLines is the list as lines of the body, with the map from
// each line back to the automation it draws (-1 for a line that draws none),
// the window's top, and how many automations were on screen.
func automationsListLines(a *app, list []automation.Automation, active []automation.Run, cursor, top, width, room int, now time.Time) (lines []string, owner []int, newTop, shown int) {
	if room <= 0 || width < 8 {
		return nil, nil, 0, 0
	}
	// THE HEADING NAMES THE PAGE AND COUNTS NOTHING: a tally of what a person can
	// already see is the emptiness law broken from the other end.
	lines = append(lines, placeLead+placeHeading(fit(placeAutomationsWord, width-len(placeLead)), a.pal), "")
	owner = append(owner, -1, -1)
	visible := room - len(lines)
	if visible < 1 {
		return lines[:room], owner[:room], 0, 0
	}
	newTop = listTop(cursor, top, len(list), visible)
	titleRoom := automationsTitleRoom(list, width)
	for i := newTop; i < len(list) && len(lines) < room; i++ {
		item := list[i]
		mark := a.icon(automationRowGlyph(item, active))
		title := fit(strings.TrimSpace(item.Title), titleRoom)
		row := placeLead + mark + " " + a.pal.ink(title)
		pad := titleRoom - ansi.StringWidth(title)
		said := automationRowSaid(item, active, now)
		if left := width - ansi.StringWidth(placeLead) - ansi.StringWidth(mark) - 1 - titleRoom - 2; left > 4 && said != "" {
			row += strings.Repeat(" ", max(0, pad)) + "  " + a.pal.dim(fit(said, left))
		}
		if i == cursor {
			row = placeBand(row, width, a.pal)
		}
		lines = append(lines, row)
		owner = append(owner, i)
		shown++
	}
	return lines, owner, newTop, shown
}

// automationsTitleRoom is the title column: as wide as the longest title, and
// never more than a third of the row, so the sentence beside it has room.
func automationsTitleRoom(list []automation.Automation, width int) int {
	widest := 0
	for _, item := range list {
		widest = max(widest, ansi.StringWidth(strings.TrimSpace(item.Title)))
	}
	return max(rowWordsFloor/2, min(widest, width/3))
}

// automationHistoryHead is the top of one automation's history: its name and
// schedule exactly, and what it does — the facts a person checks a history
// against.
func automationHistoryHead(item automation.Automation, now time.Time) []string {
	head := []string{joinDot(strings.TrimSpace(item.Title), automation.Describe(item.Schedule, now))}
	if exact := automation.Exact(item.Schedule); exact != "" && item.Schedule.Repeats() {
		head = append(head, exact)
	}
	if look := item.Look; look != nil {
		switch {
		case strings.TrimSpace(look.Command) != "":
			head = append(head, "looks · "+look.Command)
		case strings.TrimSpace(look.Files) != "":
			head = append(head, "looks · the files matching "+look.Files)
		case strings.TrimSpace(look.Tool) != "":
			head = append(head, "looks · "+look.Tool)
		}
		head = append(head, "until · "+look.Condition)
	}
	if say := strings.TrimSpace(item.Action.Say); say != "" {
		head = append(head, "says · "+say)
	}
	if do := strings.TrimSpace(item.Action.Do); do != "" {
		head = append(head, "does · "+do)
	}
	return head
}

// automationRunRowSaid is one run as a history row says it: what it came to,
// how late it was, its one sentence and what it cost.
func automationRunRowSaid(run automation.Run) string {
	out := ""
	switch run.Phase {
	case automation.PhaseRunning:
		out = autoRunningWord
	case automation.PhaseQueued:
		out = autoQueuedWord
	default:
		out = run.Outcome.Word()
	}
	if late := run.Late(); late > 0 {
		out = joinDot(out, autoLateWord+automationLate(late))
	}
	out = joinDot(out, strings.TrimSpace(firstLine(run.Line)))
	if run.USD >= 0.005 {
		out = joinDot(out, fmt.Sprintf("$%.2f", run.USD))
	}
	return out
}

// automationRunWhen is when a run happened: when it started, or when it was due
// if it never did.
func automationRunWhen(run automation.Run) time.Time {
	if !run.Started.IsZero() {
		return run.Started
	}
	return run.Due
}

// automationHistoryLines is one automation's history as lines of the body, with
// the same owner map the list writes (a run's index, -1 for the head).
func automationHistoryLines(a *app, item automation.Automation, runs []automation.Run, loaded bool, cursor, top, width, room int, now time.Time) (lines []string, owner []int, newTop, shown int) {
	if room <= 0 || width < 8 {
		return nil, nil, 0, 0
	}
	for i, head := range automationHistoryHead(item, now) {
		text := fit(head, width-len(placeLead))
		if i == 0 {
			text = placeHeading(text, a.pal)
		} else {
			text = a.pal.dim(text)
		}
		lines = append(lines, placeLead+text)
		owner = append(owner, -1)
	}
	lines = append(lines, "")
	owner = append(owner, -1)
	if len(lines) >= room {
		return lines[:room], owner[:room], 0, 0
	}
	if !loaded || len(runs) == 0 {
		word := autoNoRunsWord
		if !loaded {
			word = autoReadingWord
		}
		lines = append(lines, placeLead+a.pal.dim(word))
		owner = append(owner, -1)
		return lines, owner, 0, 0
	}
	visible := room - len(lines)
	newTop = listTop(cursor, top, len(runs), visible)
	for i := newTop; i < len(runs) && len(lines) < room; i++ {
		run := runs[i]
		mark := a.icon(automationRunGlyph(run.Outcome))
		if run.Phase == automation.PhaseRunning {
			mark = a.icon(tokens.GWorking)
		}
		when := fit(automation.Moment(automationRunWhen(run), now), 24)
		row := placeLead + mark + " " + a.pal.ink(when) + strings.Repeat(" ", max(0, 24-ansi.StringWidth(when))) + "  "
		if left := width - ansi.StringWidth(row); left > 4 {
			row += a.pal.dim(fit(automationRunRowSaid(run), left))
		}
		if i == cursor {
			row = placeBand(row, width, a.pal)
		}
		lines = append(lines, row)
		owner = append(owner, i)
		shown++
	}
	return lines, owner, newTop, shown
}

// automationsStatusWord is what /status and the phone's sheet say about the
// automations: how many will run again, which is next and when, and how many
// are running now — or nothing at all when none are on the clock.
func (a *app) automationsStatusWord() (string, bool) {
	upcoming := nextAutomations(a.watch.list)
	if len(upcoming) == 0 && a.watch.running == 0 {
		return "", false
	}
	word := ""
	if len(upcoming) > 0 {
		next := upcoming[0]
		word = itoa(len(upcoming)) + " on the clock · next " + strings.TrimSpace(next.Title) + " " + automation.Moment(next.Next, a.now())
	}
	if a.watch.running > 0 {
		word = joinDot(word, itoa(a.watch.running)+" running")
	}
	return word, true
}
