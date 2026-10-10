package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

// raised is every banner a test asked the operating system for, which no test
// ever really raises: a run's news put on the screen of whoever is running the
// suite would be a notification nobody asked for.
var (
	raisedMu sync.Mutex
	raised   []string
)

func init() {
	osNotify = func(title, body string) bool {
		raisedMu.Lock()
		defer raisedMu.Unlock()
		raised = append(raised, title+" — "+body)
		return true
	}
}

// finishRun puts one finished run of an automation in the store, the way the
// clock does: queued, started, finished.
func finishRun(t *testing.T, store *automation.Store, id string, outcome automation.Outcome, line string) automation.Run {
	t.Helper()
	run, err := store.QueueNow(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Start(run.ID); err != nil {
		t.Fatal(err)
	}
	run.Outcome, run.Line = outcome, line
	if err := store.Finish(run); err != nil {
		t.Fatal(err)
	}
	return run
}

// THE PLACE LISTS WHAT IS ON THE CLOCK: every automation's name, its schedule
// in words and when it next runs, with no count of what a person can see.
func TestTheAutomationsPlaceListsEveryAutomationWithItsSchedule(t *testing.T) {
	a, _ := automationLab(t, labWork("weekly update", "0 9 * * 1"), labWatch("ci on main"), labReminder("leave"))
	runCmd(a.openAutomations())
	screen := automationsScreen(a)
	for _, want := range []string{placeAutomationsWord, "weekly update", "ci on main", "leave",
		"every Monday at 09:00", "every 15 minutes", autoKindWatch, autoKindReminder, autoNextWord} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the place does not say %q:\n%s", want, screen)
		}
	}
}

// AN EMPTY MACHINE GETS THE WHISPER, which names what arrives here and the one
// thing that puts it there.
func TestAnEmptyAutomationsPlaceWhispersWhatArrivesThere(t *testing.T) {
	a, _ := automationLab(t)
	runCmd(a.openAutomations())
	if screen := automationsScreen(a); !strings.Contains(screen, "remind me at 6") {
		t.Fatalf("the empty place does not say what arrives there:\n%s", screen)
	}
}

// ENTER IS THE HISTORY, ESC IS THE WAY BACK. A run's outcome, its line and its
// cost are on its row, and esc from the history is the list before it is the
// way out.
func TestEnterOpensAHistoryAndEscWalksBackOut(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	item := a.watch.list[0]
	finishRun(t, store, item.ID, automation.OutcomeDone, "drafted it")
	runCmd(a.openAutomations())
	drive(t, a, key("enter"))
	if a.autoPlace.history != item.ID {
		t.Fatalf("enter opened %q, want the automation's history", a.autoPlace.history)
	}
	// The history is read off the loop; the test lands it as the loop would.
	drive(t, a, runCmd(a.readAutomationRuns(item.ID))...)
	screen := automationsScreen(a)
	for _, want := range []string{"weekly update", "cron 0 9 * * 1", "done", "drafted it"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the history does not say %q:\n%s", want, screen)
		}
	}
	drive(t, a, key("esc"))
	if a.autoPlace.history != "" || !a.at(pageAutomations) {
		t.Fatal("esc from a history did not come back to the list")
	}
	drive(t, a, key("esc"))
	if a.at(pageAutomations) {
		t.Fatal("esc from the list did not leave the place")
	}
}

// THE STRIP'S VERBS ARE THE ROW'S OWN, AND THEY WRITE THROUGH THE SEAM. Pause
// writes, the receipt says so, and the row reads paused on the next reading.
func TestPauseOnTheStripPausesTheAutomation(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	runCmd(a.openAutomations())
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("→ drew no verbs on an automation's row")
	}
	var words []string
	for _, v := range a.strip.verbs {
		words = append(words, string(v.key)+" "+v.word)
	}
	for _, want := range []string{"r " + autoVerbRun, "p " + autoVerbPause, "e " + autoVerbEdit, "d " + autoVerbDelete} {
		if !strings.Contains(strings.Join(words, " · "), want) {
			t.Fatalf("the strip offers %q, want %q among them", words, want)
		}
	}
	drive(t, a, key("p"))
	got, err := store.Get(a.watch.list[0].ID)
	if err != nil || got.Status != automation.StatusPaused {
		t.Fatalf("pause left the automation %q (%v)", got.Status, err)
	}
	if notes := homeNotes(a); !strings.Contains(notes, autoPausedReceipt+" · weekly update") {
		t.Fatalf("pause said nothing about itself: %q", notes)
	}
	if a.watch.list[0].Status != automation.StatusPaused {
		t.Fatal("the row was not read again after the pause")
	}
}

// DELETE IS TWO PRESSES, AND THE SECOND ONE IS NAMED BEFORE IT IS PRESSED. The
// first `d` deletes nothing and the foot says what the second does; anything
// else takes the offer back.
func TestDeleteAsksForASecondPress(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	id := a.watch.list[0].ID
	runCmd(a.openAutomations())
	drive(t, a, key("right"), key("d"))
	if _, err := store.Get(id); err != nil {
		t.Fatal("the first d deleted the automation")
	}
	if hint := (placeAutomations{}).hint(a); hint != autoDeleteAgain {
		t.Fatalf("the foot over an armed delete reads %q, want %q", hint, autoDeleteAgain)
	}
	drive(t, a, key("esc"))
	if _, err := store.Get(id); err != nil || a.autoPlace.armed != "" {
		t.Fatal("esc did not take the delete back")
	}
	drive(t, a, key("right"), key("d"), key("d"))
	if _, err := store.Get(id); err == nil {
		t.Fatal("the second d did not delete the automation")
	}
}

// `e` PUTS THE AUTOMATION'S OWN LINE IN THE BOX: the exact command that makes
// it what it is, to be changed and sent.
func TestEditPutsTheAutomationsOwnLineInTheBox(t *testing.T) {
	a, _ := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	item := a.watch.list[0]
	runCmd(a.openAutomations())
	drive(t, a, key("right"), key("e"))
	if a.pageShowing() {
		t.Fatal("edit did not leave the place for the box")
	}
	if got, want := a.input.String(), "/automations "+automation.CommandLine(item); got != want {
		t.Fatalf("the box holds %q, want %q", got, want)
	}
}

// A TYPED ADD ENDS ON A CARD AND SAVES NOTHING UNTIL IT IS ANSWERED.
func TestATypedAddRaisesACardAndOneSavesIt(t *testing.T) {
	a, store := automationLab(t)
	drive(t, a, runCmd(a.automationsCommand(`add stretch in 2h say "stand up and stretch"`))...)
	if all, _ := store.List(); len(all) != 0 {
		t.Fatalf("a typed add saved before it was answered: %+v", all)
	}
	if a.auto == nil || a.auto.head != typedAddHead || a.auto.settled() {
		t.Fatalf("the typed add raised no card: %+v", a.auto)
	}
	harnessSettled(t, a)
	drive(t, a, key(session.AutomationSaveKey))
	all, _ := store.List()
	if len(all) != 1 || all[0].Title != "stretch" || all[0].Action.Say != "stand up and stretch" {
		t.Fatalf("saving the card stored %+v", all)
	}
	if all[0].Origin.Transcript != a.file {
		t.Fatalf("a typed automation's runs report to %q, want this conversation", all[0].Origin.Transcript)
	}
	if !a.auto.settled() || a.auto.verdict != autoSavedWord {
		t.Fatalf("the card settled as %q", a.auto.verdict)
	}
}

// AND A NO SAVES NOTHING.
func TestATypedAddDeclinedSavesNothing(t *testing.T) {
	a, store := automationLab(t)
	drive(t, a, runCmd(a.automationsCommand(`add stretch in 2h say stretch`))...)
	harnessSettled(t, a)
	drive(t, a, key(session.AutomationNoKey))
	if all, _ := store.List(); len(all) != 0 {
		t.Fatalf("a declined card saved %+v", all)
	}
	if a.auto.verdict != autoNotSaved {
		t.Fatalf("the declined card settled as %q", a.auto.verdict)
	}
}

// A TYPED LINE THAT CANNOT BE READ SAYS WHY, and raises nothing.
func TestATypedLineThatCannotBeReadSaysWhyAndRaisesNothing(t *testing.T) {
	a, _ := automationLab(t)
	a.automationsCommand("add")
	if a.auto != nil {
		t.Fatal("an unreadable line raised a card")
	}
	if notes := homeNotes(a); !strings.Contains(notes, "needs a title") {
		t.Fatalf("the refusal does not say why: %q", notes)
	}
}

// PAUSE, RESUME AND DELETE TYPED OUT WITH AN ID DO WHAT THEY SAY, by the start
// of the id as well as the whole of it.
func TestTypedVerbsActOnTheAutomationNamed(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	id := a.watch.list[0].ID
	drive(t, a, runCmd(a.automationsCommand("pause "+id[:6]))...)
	if got, _ := store.Get(id); got.Status != automation.StatusPaused {
		t.Fatalf("pause by the start of the id left it %q", got.Status)
	}
	drive(t, a, runCmd(a.automationsCommand("resume "+id))...)
	if got, _ := store.Get(id); got.Status != automation.StatusActive {
		t.Fatalf("resume left it %q", got.Status)
	}
	drive(t, a, runCmd(a.automationsCommand("delete "+id))...)
	if _, err := store.Get(id); err == nil {
		t.Fatal("delete left it in the store")
	}
	a.automationsCommand("pause nothing-like-this")
	if notes := homeNotes(a); !strings.Contains(notes, typedNoAutoWord+"nothing-like-this") {
		t.Fatalf("an unknown id is not said: %q", notes)
	}
}

// A FINISHED RUN WRITES ONE DIM LINE IN THE CONVERSATION THAT MADE IT, and a
// quiet look writes nothing.
func TestARunThatEndedLeavesOneLineInItsConversation(t *testing.T) {
	a, store := automationLab(t, labWatch("ci on main"), labWork("weekly update", "0 9 * * 1"))
	watch, work := a.watch.list[0], a.watch.list[1]
	if watch.Kind() != automation.KindWatch {
		watch, work = work, watch
	}
	before := len(a.entries)
	finishRun(t, store, watch.ID, automation.OutcomeQuiet, "")
	finishRun(t, store, work.ID, automation.OutcomeDone, "drafted it")
	readAutomationsNow(t, a)
	var lines []string
	for _, e := range a.entries[before:] {
		if e.kind == entryAutomation && e.auto != nil && e.auto.news() {
			lines = append(lines, e.auto.line)
		}
	}
	if len(lines) != 1 || lines[0] != "weekly update · done · drafted it" {
		t.Fatalf("the conversation got %q, want the one finished run and nothing for the quiet look", lines)
	}
}

// A CONVERSATION SWITCHED TO IS TOLD WHAT IT MISSED, ONCE. Its run ended while
// another conversation was in front; when it comes forward the next reading
// draws the line there, and coming back to it again does not draw it twice.
func TestAConversationSwitchedToIsToldWhatItMissedOnce(t *testing.T) {
	a, store := automationLab(t)
	here := a.file
	other := filepath.Join(t.TempDir(), "other", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(other), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The person last had anything to do with it an hour ago.
	hourAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(other, hourAgo, hourAgo); err != nil {
		t.Fatal(err)
	}
	item := labWork("weekly update", "0 9 * * 1")
	item.Workspace, item.Origin.Transcript = t.TempDir(), other
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	readAutomationsNow(t, a)
	finishRun(t, store, made.ID, automation.OutcomeDone, "drafted it")

	said := func() int {
		n := 0
		for _, e := range a.entries {
			if e.kind == entryAutomation && e.auto != nil && e.auto.line == "weekly update · done · drafted it" {
				n++
			}
		}
		return n
	}
	readAutomationsNow(t, a)
	if got := said(); got != 0 {
		t.Fatalf("the line was drawn in a conversation that did not make it (%d)", got)
	}
	a.file = other
	readAutomationsNow(t, a)
	if got := said(); got != 1 {
		t.Fatalf("the conversation switched to was told %d times, want once", got)
	}
	a.file = here
	readAutomationsNow(t, a)
	a.file = other
	readAutomationsNow(t, a)
	if got := said(); got != 1 {
		t.Fatalf("coming back to it told it %d times, want once", got)
	}
}

// ONE WINDOW TELLS THE PERSON. Two windows read the same finished run; the
// banner is raised once, by whichever claimed it first.
func TestARunsBannerIsRaisedOnceAcrossWindows(t *testing.T) {
	first, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	second := newTestApp(&fakeAgent{model: "m"})
	second.autos = automationsSeamOf(store)
	readAutomationsNow(t, second)
	raisedMu.Lock()
	raised = nil
	raisedMu.Unlock()
	finishRun(t, store, first.watch.list[0].ID, automation.OutcomeDone, "drafted it")
	for _, a := range []*app{first, second} {
		cmd := a.readAutomations()
		msg := cmd().(automationsReadMsg)
		runCmd(a.automationsRead(msg))
	}
	raisedMu.Lock()
	defer raisedMu.Unlock()
	if len(raised) != 1 || !strings.Contains(raised[0], "weekly update") {
		t.Fatalf("the banner was raised %d times: %q", len(raised), raised)
	}
}

// LEAVING SAYS WHAT IT WOULD STOP, ONCE. The last window open with a run in
// hand warns on ctrl+c, and the same key again leaves.
func TestQuitWarnsOnceWhenItWouldStopARun(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.watch.known, a.watch.running, a.watch.others = true, 1, 0
	if cmd := a.requestQuit(quitAgainChord); cmd != nil {
		t.Fatal("the first ctrl+c left with a run in hand")
	}
	if notes := homeNotes(a); !strings.Contains(notes, "1 automation is running — quitting stops it"+quitAgainChord) {
		t.Fatalf("the warning reads %q", notes)
	}
	cmd := a.requestQuit(quitAgainChord)
	if cmd == nil {
		t.Fatal("the second ctrl+c did not leave")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the second ctrl+c did not quit")
	}
}

// AND IT DOES NOT WARN WHEN NOTHING WOULD STOP: another window keeps the clock
// going, and a reading that has not landed is not grounds for a warning.
func TestQuitDoesNotWarnWhenNothingWouldStop(t *testing.T) {
	for name, set := range map[string]func(a *app){
		"another window open": func(a *app) { a.watch.known, a.watch.running, a.watch.others = true, 1, 1 },
		"nothing running":     func(a *app) { a.watch.known, a.watch.running = true, 0 },
		"no reading yet":      func(a *app) { a.watch.running = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			set(a)
			if cmd := a.requestQuit(quitAgainChord); cmd == nil {
				t.Fatal("a quit that would stop nothing warned")
			}
		})
	}
}

// AND A WARNING GOES STALE: the same key ten seconds later warns again rather
// than leaving on a press somebody made for another reason.
func TestAQuitWarningGoesStale(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.watch.known, a.watch.running = true, 1
	a.requestQuit(quitAgainChord)
	now = now.Add(quitArmedFor + time.Second)
	if cmd := a.requestQuit(quitAgainChord); cmd != nil {
		t.Fatal("a stale warning still let the key leave")
	}
}
