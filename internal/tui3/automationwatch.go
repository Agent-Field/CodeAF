package tui3

// automationwatch.go is how a window learns what automations have been doing.
//
// THE STORE IS THE TRUTH, AND EVERY WINDOW READS IT. The clock that runs
// automations is its own process and holds no conversation, so it tells no
// window anything; every window keeps a cursor on the store's change counter
// and reads what moved, every [watchEvery]. That is what makes a run's news
// reach the window somebody is sitting in whichever process ran it — the one
// thing the feature this replaces got wrong.
//
// From what it reads a window draws the run's one line in the conversation that
// made the automation, if it has that conversation open; raises a desktop
// notification for news, once per run per machine; refreshes the list; and
// keeps the snapshot the quit question is asked from. Every read is a seam
// call, so all of it runs in a command and none of it on the update loop.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

// watchEvery is how often a window reads the store.
const watchEvery = 2 * time.Second

// AutomationsSeam is what a window reads and changes automations through. Every
// function may be slow — over `--host` each one is a call to another machine —
// so the surface calls them from commands and never from the update loop. A nil
// function is that ability absent.
type AutomationsSeam struct {
	// List is every automation.
	List func() ([]automation.Automation, error)
	// Runs is one automation's history, newest first, at most limit long.
	Runs func(id string, limit int) ([]automation.Run, error)
	// Changes is every run that changed after the cursor, and the cursor to
	// ask from next time; Cursor is the counter now.
	Changes func(cursor int64) ([]automation.Run, int64, error)
	Cursor  func() (int64, error)
	// Active is every run queued or in hand.
	Active func() ([]automation.Run, error)
	// Windows is how many codeaf windows are open on the machine the
	// automations run on, this one included.
	Windows func() (int, error)
	// Create, Update, SetStatus and Delete are the person's changes; RunNow
	// asks for a run outside the schedule and StopRun ends one in hand.
	Create    func(automation.Automation) (automation.Automation, error)
	Update    func(automation.Automation) (automation.Automation, error)
	SetStatus func(id string, status automation.Status) (automation.Automation, error)
	Delete    func(id string) error
	RunNow    func(id string) error
	StopRun   func(id int64) error
	// Claim reports whether THIS window is the one to raise the desktop
	// notification for a run: the first window on the machine to ask, for
	// each run, and nobody after it.
	Claim func(runID int64) bool
	// Zone is the zone a typed automation's rhythm is read in.
	Zone string
}

// on reports whether the seam can be read at all.
func (s AutomationsSeam) on() bool { return s.List != nil && s.Changes != nil }

// automationsWatch is what this window last read.
type automationsWatch struct {
	started bool
	cursor  int64
	list    []automation.Automation
	// active is every run queued or in hand, which the place reads to offer a
	// stop and to mark a row as running.
	active []automation.Run
	// running is how many runs are in hand, and others how many windows are
	// open besides this one. known is false until the first reading lands, and
	// the quit question is not asked from an unknown.
	running, others int
	known           bool
	// awayFor is the conversation whose missed news this window last gathered.
	// A conversation that comes to the front is asked about once, on the next
	// reading, whether it is the first one this window opened or one it
	// switched to.
	awayFor string
	// drawn is every run whose line this window has drawn, so a conversation
	// switched away from and back to is not told the same news twice.
	drawn map[int64]bool
}

// drawnMost bounds [automationsWatch.drawn]. A window open for weeks would
// otherwise remember every run it ever drew; forgetting them all at once costs
// at most a line drawn again in a conversation switched back to.
const drawnMost = 4096

// automationsReadMsg is one reading.
type automationsReadMsg struct {
	first   bool
	cursor  int64
	runs    []automation.Run
	away    []automation.Run
	// awayFor is the conversation away was gathered for, and empty when this
	// reading gathered nothing.
	awayFor string
	list    []automation.Automation
	listed  bool
	active  []automation.Run
	running int
	windows int
	known   bool
}

// automationsBeatMsg is the watcher's beat: time to read the store again.
type automationsBeatMsg struct{}

// beatAutomations asks for the next reading after [watchEvery]. THE TICK NAMES
// A MESSAGE AND DOES NOTHING ELSE; the reading is its own command, asked for
// when the message lands (harnessdriver_test.go's law about tick callbacks).
func (a *app) beatAutomations() tea.Cmd {
	if !a.autos.on() {
		return nil
	}
	return surfaceTick(watchEvery, func(time.Time) tea.Msg { return automationsBeatMsg{} })
}

// readAutomations reads the store once, in a command: the runs that changed
// since the cursor, the list, how many runs are in hand and how many windows
// are open. The first reading starts the cursor at now. AND WHENEVER A
// CONVERSATION COMES TO THE FRONT — the first one, or one switched to — the
// reading also collects what it missed while it was not in front.
func (a *app) readAutomations() tea.Cmd {
	seam := a.autos
	if !seam.on() {
		return nil
	}
	cursor := a.watch.cursor
	started := a.watch.started
	file := a.file
	gather := strings.TrimSpace(file) != "" && !sameTranscript(file, a.watch.awayFor)
	return func() tea.Msg {
		msg := automationsReadMsg{first: !started, cursor: cursor}
		if !started {
			if seam.Cursor != nil {
				if now, err := seam.Cursor(); err == nil {
					msg.cursor = now
				}
			}
		} else if runs, next, err := seam.Changes(cursor); err == nil {
			msg.runs, msg.cursor = runs, next
		}
		if list, err := seam.List(); err == nil {
			msg.list, msg.listed = list, true
		}
		if gather {
			msg.away, msg.awayFor = automationsAway(seam, msg.list, file), file
		}
		if seam.Active != nil {
			if active, err := seam.Active(); err == nil {
				msg.active = active
				for _, run := range active {
					if run.Phase == automation.PhaseRunning {
						msg.running++
					}
				}
				msg.known = true
			}
		}
		if seam.Windows != nil {
			if windows, err := seam.Windows(); err == nil {
				msg.windows = windows
			} else {
				msg.known = false
			}
		}
		return msg
	}
}

// drawAutomationRun draws one run's line in the conversation in front, once per
// window: a run already drawn here is not drawn again when its conversation is
// switched back to.
func (a *app) drawAutomationRun(item automation.Automation, run automation.Run) {
	if a.watch.drawn[run.ID] {
		return
	}
	if a.watch.drawn == nil || len(a.watch.drawn) >= drawnMost {
		a.watch.drawn = map[int64]bool{}
	}
	a.watch.drawn[run.ID] = true
	a.automationRan(item, run)
}

// automationsAway is the news a conversation missed while it was not in front:
// runs of automations it made that ended after the person was last in it.
func automationsAway(seam AutomationsSeam, list []automation.Automation, file string) []automation.Run {
	if seam.Runs == nil || strings.TrimSpace(file) == "" {
		return nil
	}
	since := time.Time{}
	if meta, err := session.LoadMeta(filepath.Dir(file)); err == nil {
		since = meta.LastUserAt
	}
	if since.IsZero() {
		if info, err := os.Stat(file); err == nil {
			since = info.ModTime()
		}
	}
	if since.IsZero() {
		return nil
	}
	var away []automation.Run
	for _, item := range list {
		if !sameTranscript(item.Origin.Transcript, file) {
			continue
		}
		runs, err := seam.Runs(item.ID, 10)
		if err != nil {
			continue
		}
		for i := len(runs) - 1; i >= 0; i-- {
			run := runs[i]
			if run.Phase == automation.PhaseOver && run.Outcome.Delivered() && run.Finished.After(since) {
				away = append(away, run)
			}
		}
	}
	return away
}

// automationsRead folds one reading in: the lines, the notifications, the
// snapshot, and the next reading.
func (a *app) automationsRead(msg automationsReadMsg) tea.Cmd {
	a.watch.started = true
	a.watch.cursor = msg.cursor
	// AN EMPTY STORE IS A READING TOO. A list is replaced whenever the store
	// answered, so the last automation deleted leaves the window with none
	// rather than with the list from before; a reading that failed keeps what
	// the window had.
	if msg.listed {
		a.watch.list = msg.list
	}
	a.watch.running = msg.running
	if msg.known {
		a.watch.active = msg.active
	}
	if msg.windows > 0 {
		a.watch.others = msg.windows - 1
	}
	a.watch.known = msg.known && msg.windows > 0
	byID := make(map[string]automation.Automation, len(a.watch.list))
	for _, item := range a.watch.list {
		byID[item.ID] = item
	}
	var cmds []tea.Cmd
	// THE MISSED NEWS IS DRAWN ONLY WHERE IT WAS GATHERED FOR. A switch that
	// landed while the reading was out leaves the news for the conversation it
	// was about; the next reading asks about the one now in front.
	if msg.awayFor != "" {
		a.watch.awayFor = msg.awayFor
		if sameTranscript(msg.awayFor, a.file) {
			for _, run := range msg.away {
				if item, ok := byID[run.AutomationID]; ok {
					a.drawAutomationRun(item, run)
				}
			}
		} else {
			a.watch.awayFor = ""
		}
	}
	for _, run := range msg.runs {
		if run.Phase != automation.PhaseOver || !run.Outcome.Delivered() {
			continue
		}
		item, ok := byID[run.AutomationID]
		if !ok {
			continue
		}
		if sameTranscript(item.Origin.Transcript, a.file) {
			a.drawAutomationRun(item, run)
		}
		if automationNotifies(item, run) {
			cmds = append(cmds, a.notifyAutomation(item, run))
		}
	}
	if len(msg.runs) > 0 || msg.first {
		a.touch()
	}
	cmds = append(cmds, a.beatAutomations())
	return tea.Batch(cmds...)
}

// automationNotifies reports whether a run is worth a desktop notification:
// news the person did not just make themselves.
func automationNotifies(item automation.Automation, run automation.Run) bool {
	switch run.Outcome {
	case automation.OutcomeStopped, automation.OutcomeQuiet, "":
		return false
	}
	return true
}

// notifyAutomation raises the desktop notification for one run, in the one
// window on the machine that claims it.
func (a *app) notifyAutomation(item automation.Automation, run automation.Run) tea.Cmd {
	seam := a.autos
	if seam.Claim == nil {
		return nil
	}
	// A WINDOW THAT IS KNOWN TO HAVE THE KEYBOARD RAISES NO BANNER: the
	// person is looking at codeaf, and the line is already in front of them.
	// A terminal that never reports focus is NOT taken as focused here, as a
	// turn's banner takes it (notify.go) — an automation's news is for somebody
	// who is elsewhere, and a missed reminder costs more than a redundant one.
	if a.seenFocus && a.focused {
		return nil
	}
	title := "codeaf · " + strings.TrimSpace(item.Title)
	body := automationRunWords(item, run)
	return func() tea.Msg {
		if !seam.Claim(run.ID) {
			return nil
		}
		if osNotify(title, body) {
			return nil
		}
		return automationNotifyMsg{title: title, body: body}
	}
}

// sameTranscript reports whether two paths name the same transcript.
func sameTranscript(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// automationRunningLine is the quit question's sentence.
func automationRunningLine(running int) string {
	if running == 1 {
		return "1 automation is running — quitting stops it"
	}
	return strconv.Itoa(running) + " automations are running — quitting stops them"
}
