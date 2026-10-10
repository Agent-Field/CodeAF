package tui3

// THE AUTOMATIONS PLACE: /automations, and what it keeps between frames.
//
// The card in the transcript (automation.go) is where a person says yes to one
// automation. It is a good place to agree to something and a poor place to
// remember having agreed to it: the card scrolls away, and what it set up goes
// on running for months. This place is the other half — WHAT IS ON THE CLOCK,
// what each one last came to, and the keys that run, pause, stop, change or
// delete one — which is a LIST, the shape this surface already knows how to
// draw.
//
// This file is the place's state, keys and verbs; what is on the screen is
// automationsplace.go's, a pure reading of what the window already holds.
//
// IT READS NOTHING ITSELF. The list is the watcher's reading of the store, taken
// every two seconds by every window (automationwatch.go), so the place is as
// current as the window is and a frame costs arithmetic. The one thing it asks
// for is a history, when somebody opens one, and that read happens in a command.
//
// EVERY CHANGE IS A GESTURE THROUGH THE SEAM, OFF THE LOOP. Over `--host` the
// store is on another machine and each write is a trip, so a key queues the
// write on the door line (offloop.go) in the order it was pressed, and the
// receipt and a fresh reading come back when it lands.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// placeAutomationsWord is the place's name: the tab, the heading, the command.
const placeAutomationsWord = "automations"

// The receipts and refusals the place says on the message line.
const (
	autoPausedReceipt  = "paused"
	autoResumedReceipt = "going again"
	autoDeletedReceipt = "deleted"
	autoQueuedReceipt  = "asked to run now"
	autoStopReceipt    = "asked to stop"
	autoNoStoreWord    = "this window cannot change automations"
	autoNoOriginWord   = "it was made with a typed command, so there is no conversation behind it"
	autoHereWord       = "you are already in it"
	autoNoTranscript   = "this run kept no transcript"
	// autoDeleteAgain is the second half of delete: the place's own key, named
	// on the foot while it is armed.
	autoDeleteAgain = "d again deletes it and its history · esc keeps it"
)

// The verbs on a row's `→` strip, in the row's own words.
const (
	autoVerbRun     = "run now"
	autoVerbCheck   = "check now"
	autoVerbSay     = "say it now"
	autoVerbStop    = "stop the run"
	autoVerbPause   = "pause"
	autoVerbResume  = "resume"
	autoVerbEdit    = "edit"
	autoVerbDelete  = "delete"
	autoVerbOpen    = "open where it was asked"
	autoHistoryWord = "enter its history"
	autoBackWord    = "esc back to the list"
	autoOpenRunWord = "enter open the run"
)

// automationsPlace is the place's whole state. The zero value is closed, on the
// list, with the cursor at the top.
type automationsPlace struct {
	// cursor is the automation under the cursor, as an index into the list the
	// watcher last read; top is the first one the window shows; shown is how
	// many the last paint had room for, which is how far a page key steps.
	cursor, top, shown int
	// owner maps each line of the last body back to the row that drew it, and
	// -1 for a line that draws none; hover is the line the pointer rests on.
	owner []int
	hover int
	// history is the automation whose runs are open, and "" for the list.
	history string
	runs    []automation.Run
	loaded  bool
	// hcursor and htop are the history's own cursor and window.
	hcursor, htop int
	// armed is the automation a first `d` asked to delete. The second `d` on
	// the same row deletes it; anything else disarms it.
	armed string
}

// automationsRunsMsg is a history, read.
type automationsRunsMsg struct {
	id   string
	runs []automation.Run
	err  error
}

// list is what the place draws: the watcher's last reading.
func (p *automationsPlace) list(a *app) []automation.Automation { return a.watch.list }

// current is the automation under the list's cursor.
func (p *automationsPlace) current(a *app) (automation.Automation, bool) {
	list := p.list(a)
	if p.cursor < 0 || p.cursor >= len(list) {
		return automation.Automation{}, false
	}
	return list[p.cursor], true
}

// opened is the automation whose history is open.
func (p *automationsPlace) opened(a *app) (automation.Automation, bool) {
	if p.history == "" {
		return automation.Automation{}, false
	}
	for _, item := range p.list(a) {
		if item.ID == p.history {
			return item, true
		}
	}
	return automation.Automation{}, false
}

// settle keeps the cursor on a row that exists after the list moved under it.
func (p *automationsPlace) settle(a *app) {
	count := len(p.list(a))
	switch {
	case count == 0:
		p.cursor = 0
	case p.cursor >= count:
		p.cursor = count - 1
	case p.cursor < 0:
		p.cursor = 0
	}
	if p.history != "" {
		if _, ok := p.opened(a); !ok {
			p.history, p.runs, p.loaded = "", nil, false
		}
	}
}

// visible is how far a page key steps: what the last paint had room for.
func (p *automationsPlace) visible() int {
	if p.shown > 0 {
		return p.shown
	}
	return 10
}

// move walks the list, or the open history, and stops at the ends.
func (p *automationsPlace) move(a *app, delta int) {
	p.armed = ""
	if p.history != "" {
		p.hcursor = clampIndex(p.hcursor+delta, len(p.runs))
		return
	}
	p.cursor = clampIndex(p.cursor+delta, len(p.list(a)))
}

// readRuns asks for one automation's history, off the loop and off the line:
// it is a read nobody pressed for in order.
func (a *app) readAutomationRuns(id string) tea.Cmd {
	runs := a.autos.Runs
	if runs == nil {
		return nil
	}
	return a.besideLine(func() func(bool) tea.Cmd {
		got, err := runs(id, 100)
		return func(bool) tea.Cmd {
			return func() tea.Msg { return automationsRunsMsg{id: id, runs: got, err: err} }
		}
	})
}

// tookAutomationRuns lands a history.
func (a *app) tookAutomationRuns(msg automationsRunsMsg) {
	p := &a.autoPlace
	if msg.id != p.history {
		return
	}
	p.loaded = true
	if msg.err != nil {
		a.note(msg.err.Error())
		return
	}
	p.runs = msg.runs
	p.hcursor = clampIndex(p.hcursor, len(p.runs))
	a.touch()
}

// ── the verbs ───────────────────────────────────────────────────────────────

// verbs is the `→` strip for the row under the cursor. A verb that cannot work
// on this row is absent rather than broken: a reminder has nothing to stop, a
// finished automation has nothing to pause, and one made with a typed command
// has no conversation to open.
func (p *automationsPlace) verbs(a *app) []verb {
	if p.history != "" {
		return nil
	}
	item, ok := p.current(a)
	if !ok || !a.autos.on() {
		return nil
	}
	var verbs []verb
	_, busy := automationActive(a.watch.active, item.ID)
	if busy && a.autos.StopRun != nil {
		verbs = append(verbs, verb{key: 's', word: autoVerbStop, do: func() tea.Cmd { return a.stopAutomationRun(item) }})
	}
	if !busy && a.autos.RunNow != nil {
		word := autoVerbRun
		switch item.Kind() {
		case automation.KindReminder:
			word = autoVerbSay
		case automation.KindWatch:
			word = autoVerbCheck
		}
		verbs = append(verbs, verb{key: 'r', word: word, do: func() tea.Cmd { return a.runAutomationNow(item) }})
	}
	if a.autos.SetStatus != nil {
		switch item.Status {
		case automation.StatusActive:
			verbs = append(verbs, verb{key: 'p', word: autoVerbPause, do: func() tea.Cmd { return a.setAutomationStatus(item, automation.StatusPaused) }})
		case automation.StatusPaused:
			verbs = append(verbs, verb{key: 'p', word: autoVerbResume, do: func() tea.Cmd { return a.setAutomationStatus(item, automation.StatusActive) }})
		}
	}
	if a.autos.Update != nil {
		verbs = append(verbs, verb{key: 'e', word: autoVerbEdit, do: func() tea.Cmd { return a.editAutomation(item) }})
	}
	if a.autos.Delete != nil {
		verbs = append(verbs, verb{key: 'd', word: autoVerbDelete, do: func() tea.Cmd { return a.armAutomationDelete(item) }})
	}
	if strings.TrimSpace(item.Origin.Transcript) != "" {
		verbs = append(verbs, verb{key: 'o', word: autoVerbOpen, do: func() tea.Cmd { return a.openAutomationOrigin(item) }})
	}
	return verbs
}

// automationChanged folds one write's answer back in: the receipt, and a fresh
// reading so the row says what the store now says.
func (a *app) automationChanged(item automation.Automation, receipt string, err error) tea.Cmd {
	if err != nil {
		a.note(err.Error())
		return nil
	}
	a.note(receipt + " · " + strings.TrimSpace(item.Title))
	a.touch()
	return a.readAutomations()
}

func (a *app) runAutomationNow(item automation.Automation) tea.Cmd {
	door := a.autos.RunNow
	if door == nil {
		a.note(autoNoStoreWord)
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(item.ID)
		return func(bool) tea.Cmd { return a.automationChanged(item, autoQueuedReceipt, err) }
	})
}

func (a *app) stopAutomationRun(item automation.Automation) tea.Cmd {
	door := a.autos.StopRun
	run, ok := automationActive(a.watch.active, item.ID)
	if door == nil || !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(run.ID)
		return func(bool) tea.Cmd { return a.automationChanged(item, autoStopReceipt, err) }
	})
}

func (a *app) setAutomationStatus(item automation.Automation, status automation.Status) tea.Cmd {
	door := a.autos.SetStatus
	if door == nil {
		a.note(autoNoStoreWord)
		return nil
	}
	receipt := autoPausedReceipt
	if status == automation.StatusActive {
		receipt = autoResumedReceipt
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		_, err := door(item.ID, status)
		return func(bool) tea.Cmd { return a.automationChanged(item, receipt, err) }
	})
}

// editAutomation leaves the place with the automation's own `edit` line in the
// box: the line that makes it what it is, to be changed and sent, which raises
// the same card any typed change does (automationscmd.go).
func (a *app) editAutomation(item automation.Automation) tea.Cmd {
	a.leavePlace()
	a.input.setText("/" + placeAutomationsWord + " " + automation.CommandLine(item))
	a.touch()
	return nil
}

// armAutomationDelete is the first `d`: nothing is deleted, and the foot says
// what the second one does.
func (a *app) armAutomationDelete(item automation.Automation) tea.Cmd {
	a.autoPlace.armed = item.ID
	a.touch()
	return nil
}

func (a *app) deleteAutomation(item automation.Automation) tea.Cmd {
	door := a.autos.Delete
	a.autoPlace.armed = ""
	if door == nil {
		a.note(autoNoStoreWord)
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(item.ID)
		return func(bool) tea.Cmd { return a.automationChanged(item, autoDeletedReceipt, err) }
	})
}

// openAutomationOrigin opens the conversation an automation was asked in, which
// is what "why did I get this?" means.
func (a *app) openAutomationOrigin(item automation.Automation) tea.Cmd {
	transcript := strings.TrimSpace(item.Origin.Transcript)
	switch {
	case transcript == "":
		a.note(autoNoOriginWord)
		return nil
	case sameTranscript(transcript, a.file):
		a.leavePlace()
		a.note(autoHereWord)
		return nil
	}
	cmd, refusal := a.openSession(Session{File: transcript})
	if refusal != "" {
		a.note(refusal)
		return nil
	}
	a.leavePlace()
	return cmd
}

// openAutomationRun opens one run's own transcript — the work a run did, read
// the way any conversation is read.
func (a *app) openAutomationRun(run automation.Run) tea.Cmd {
	transcript := strings.TrimSpace(run.Transcript)
	if transcript == "" {
		a.note(autoNoTranscript)
		return nil
	}
	cmd, refusal := a.openSession(Session{File: transcript})
	if refusal != "" {
		a.note(refusal)
		return nil
	}
	a.leavePlace()
	return cmd
}

// ── the keys ────────────────────────────────────────────────────────────────

// automationsKey is the place's own keys, after the router's classes.
func (a *app) automationsKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.autoPlace
	var cmd tea.Cmd
	key := msg.String()
	if p.armed != "" {
		armed := p.armed
		p.armed = ""
		if key == "d" {
			if item, ok := p.current(a); ok && item.ID == armed {
				return a.deleteAutomation(item)
			}
		}
		if key == "esc" {
			a.touch()
			return nil
		}
	}
	switch key {
	case "esc":
		if p.history != "" {
			p.history, p.runs, p.loaded = "", nil, false
			break
		}
		a.leavePlace()
		return nil
	case "up", "ctrl+p":
		p.move(a, -1)
	case "down", "ctrl+n":
		p.move(a, 1)
	case "pgup":
		p.move(a, -p.visible())
	case "pgdown":
		p.move(a, p.visible())
	case "enter":
		cmd = p.enter(a)
	}
	a.touch()
	return cmd
}

// enter opens the row under the cursor: an automation's history from the list,
// and a run's own transcript from a history.
func (p *automationsPlace) enter(a *app) tea.Cmd {
	p.armed = ""
	if p.history != "" {
		if p.hcursor >= 0 && p.hcursor < len(p.runs) {
			return a.openAutomationRun(p.runs[p.hcursor])
		}
		return nil
	}
	item, ok := p.current(a)
	if !ok {
		return nil
	}
	p.history, p.runs, p.loaded, p.hcursor, p.htop = item.ID, nil, false, 0, 0
	return a.readAutomationRuns(item.ID)
}

// hint is the foot: what enter does here, what `→` reaches, and the way out —
// derived from the row under the cursor so nothing is named that is not bound.
func (p *automationsPlace) hint(a *app) string {
	if p.armed != "" {
		return autoDeleteAgain
	}
	if p.history != "" {
		if len(p.runs) > 0 {
			return autoOpenRunWord + " · " + autoBackWord
		}
		return autoBackWord
	}
	if _, ok := p.current(a); !ok {
		return "esc"
	}
	line := autoHistoryWord
	verbs := p.verbs(a)
	words := make([]string, 0, len(verbs))
	for _, v := range verbs {
		words = append(words, v.word)
	}
	if len(words) > 0 {
		line += " · " + homeStripWord(words...)
	}
	return line + " · esc"
}

// ── the handle the registry files ───────────────────────────────────────────

type placeAutomations struct{ placeBase }

func init() { registerPlace(placeAutomations{}) }

func (placeAutomations) id() page      { return pageAutomations }
func (placeAutomations) word() string  { return placeAutomationsWord }
func (placeAutomations) counted() bool { return true }
func (placeAutomations) about() string { return "what codeaf does on a clock while it is open" }

// open lays the place out on the list and asks for a fresh reading, so a person
// arriving from a long conversation sees the store as it is now.
func (placeAutomations) open(a *app) tea.Cmd {
	a.autoPlace = automationsPlace{hover: -1, cursor: a.autoPlace.cursor}
	a.autoPlace.settle(a)
	a.closeLists()
	a.dismissWelcome()
	// The person found the place, so the tips that teach it retire (notice.go).
	a.noticeEvent(eventAutomationsOpened)
	return tea.Batch(a.armPlaceClock(), a.readAutomations())
}

func (placeAutomations) close(a *app) {
	a.leavePage(pageAutomations)
	cursor := a.autoPlace.cursor
	a.autoPlace = automationsPlace{cursor: cursor}
}

// tick is the beat: the watcher keeps the list current on its own, so the beat
// only settles the cursor over whatever moved and asks an open history again.
func (placeAutomations) tick(a *app, now time.Time) (bool, tea.Cmd) {
	p := &a.autoPlace
	p.settle(a)
	if p.history != "" {
		return true, a.readAutomationRuns(p.history)
	}
	return true, nil
}

func (placeAutomations) body(a *app, width, room int) []placeRow {
	p := &a.autoPlace
	p.settle(a)
	list := p.list(a)
	var lines []string
	if p.history != "" {
		item, _ := p.opened(a)
		lines, p.owner, p.htop, p.shown = automationHistoryLines(a.pal, a.icon, item, p.runs, p.loaded, p.hcursor, p.htop, width, room, a.now())
	} else if len(list) == 0 {
		p.owner, p.shown = nil, 0
		return placeWhisperRows(pageAutomations, width, room, a.pal)
	} else {
		lines, p.owner, p.top, p.shown = automationsListLines(a.pal, a.icon, list, a.watch.active, p.cursor, p.top, width, room, a.now())
	}
	rows := make([]placeRow, 0, room)
	for i, text := range lines {
		at := -1
		if i < len(p.owner) {
			at = p.owner[i]
		}
		rows = append(rows, placeRow{text: text, hit: at})
	}
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

// stops is every row the cursor may rest on, in the place's own numbers —
// the list's index, or the history's — which is what the hit map answers in
// too. EVERY ROW AND NOT ONLY THE DRAWN ONES: `↑` off the first stop leaves for
// the tab bar ([app.barReach]), and a scrolled list's first drawn row is not
// its first row.
func (placeAutomations) stops(a *app) []int {
	p := &a.autoPlace
	count := len(p.list(a))
	if p.history != "" {
		count = len(p.runs)
	}
	out := make([]int, count)
	for i := range out {
		out[i] = i
	}
	return out
}

// cursorAt is the row the cursor stands on, in [placeAutomations.stops]'
// numbers.
func (placeAutomations) cursorAt(a *app) int {
	if p := &a.autoPlace; p.history != "" {
		return p.hcursor
	}
	return a.autoPlace.cursor
}

func (placeAutomations) cursorRow(a *app, rows []placeRow) int {
	p := &a.autoPlace
	want := p.cursor
	if p.history != "" {
		want = p.hcursor
	}
	return placeRowAtLine(rows, want)
}

// rowID names the row under the cursor by the automation's own id, which a
// re-read cannot hand to a different row.
func (placeAutomations) rowID(a *app) string {
	p := &a.autoPlace
	if p.history != "" {
		return ""
	}
	if item, ok := p.current(a); ok {
		return item.ID
	}
	return ""
}

func (placeAutomations) enter(a *app) tea.Cmd { return a.autoPlace.enter(a) }
func (placeAutomations) verbs(a *app) []verb  { return a.autoPlace.verbs(a) }
func (placeAutomations) hint(a *app) string   { return a.autoPlace.hint(a) }

func (placeAutomations) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.automationsKey(msg) }

// changed is the tab's count: how many runs that are news ended since the
// person last looked at this place.
func (placeAutomations) changed(a *app, since time.Time) int {
	n := 0
	for _, item := range a.watch.list {
		if last := item.Last; last != nil && last.Outcome.Delivered() && last.Finished.After(since) {
			n++
		}
	}
	return n
}

// summary is what is behind the place, for home's typed drop-up.
func (placeAutomations) summary(a *app) string {
	active := 0
	for _, item := range a.watch.list {
		if item.Status == automation.StatusActive {
			active++
		}
	}
	switch active {
	case 0:
		return ""
	case 1:
		return "1 automation on the clock"
	}
	return itoa(active) + " automations on the clock"
}

// press is enter on the row pressed.
func (placeAutomations) press(a *app, y int) (tea.Cmd, bool) {
	p := &a.autoPlace
	line := y - placeHeadRows
	if line < 0 || line >= len(p.owner) || p.owner[line] < 0 {
		return nil, true
	}
	if p.history != "" {
		p.hcursor = p.owner[line]
	} else {
		p.cursor = p.owner[line]
	}
	a.touch()
	return p.enter(a), true
}

func (placeAutomations) hover(a *app, y int) bool {
	p := &a.autoPlace
	next, row := -1, -1
	if line := y - placeHeadRows; line >= 0 && line < len(p.owner) && p.owner[line] >= 0 {
		next, row = line, p.owner[line]
	}
	if p.history != "" {
		return placeHoverMoved(&p.hover, &p.hcursor, next, row, a)
	}
	return placeHoverMoved(&p.hover, &p.cursor, next, row, a)
}

func (placeAutomations) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.autoPlace.move(a, delta)
	a.touch()
	return nil, true
}

// openAutomations is the door every road into this place takes.
func (a *app) openAutomations() tea.Cmd { return a.openAutomationsAt("") }

// openAutomationsAt opens the place with the cursor on one automation, and
// leaves it where it was when nothing on the list is that one any more.
func (a *app) openAutomationsAt(id string) tea.Cmd {
	cmd := a.showPage(pageAutomations)
	if id != "" {
		for at, item := range a.watch.list {
			if item.ID == id {
				a.autoPlace.cursor = at
				break
			}
		}
	}
	a.touch()
	return cmd
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
