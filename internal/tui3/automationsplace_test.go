package tui3

import (
	"os"
	"path/filepath"
	"regexp"
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
	osNotify = func(title, body, _ string) bool {
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
	// A ONE-CHARACTER START NAMES NOTHING, and delete asks nothing when typed.
	drive(t, a, runCmd(a.automationsCommand("delete "+id[:1]))...)
	if _, err := store.Get(id); err != nil {
		t.Fatal("delete by one character of the id deleted it")
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

// LEAVING NEVER ASKS, A RUN IN HAND OR NOT. Closing codeaf pauses automations
// and a run it cuts off runs again when a window next opens, so ctrl+c at rest
// leaves on its first press with nothing said.
func TestQuitLeavesAtOnceWithARunInHand(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.watch.running = 1
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c at rest with a run in hand did not leave")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c at rest with a run in hand did not quit")
	}
	if notes := homeNotes(a); strings.Contains(notes, "quitting stops") {
		t.Fatalf("leaving still warned: %q", notes)
	}
}

// A RUN'S WHOLE RESULT IS READ IN ITS HISTORY, in the ordinary ink. The row is
// one dim sentence; the run under the cursor — the newest, when the history
// opens — says the rest of its report under it, every line of it.
func TestAHistorySaysTheWholeResultOfTheRunUnderTheCursor(t *testing.T) {
	a, store := automationLab(t, labWork("youtube", "0 9 * * 1"))
	item := a.watch.list[0]
	run, err := store.QueueNow(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Start(run.ID); err != nil {
		t.Fatal(err)
	}
	report := "His latest video is \"Why the war will not end this year\".\nIt went up yesterday and runs for forty minutes, mostly about the spring offensive."
	run.Outcome, run.Line, run.Detail = automation.OutcomeDone, "His latest video is \"Why the war will not end this year\".", report
	if err := store.Finish(run); err != nil {
		t.Fatal(err)
	}
	runCmd(a.openAutomations())
	drive(t, a, key("enter"))
	drive(t, a, runCmd(a.readAutomationRuns(item.ID))...)
	screen := automationsScreen(a)
	for _, want := range []string{"runs for forty minutes", "mostly about the spring offensive"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the history does not carry the whole report (%q):\n%s", want, screen)
		}
	}
	width, height := a.size()
	lines, _, _, _ := a.placeDraw(placeAutomations{}, width, height)
	for _, line := range lines {
		if strings.Contains(plain(line), "spring offensive") && strings.Contains(line, "\x1b[2m") {
			t.Fatalf("the result is drawn faint: %q", line)
		}
	}
}

// A BANNER CARRIES THE RESULT, NOT ONLY ITS FIRST LINE, and does not say the
// title twice: the title is the banner's own head.
func TestABannerCarriesTheResult(t *testing.T) {
	item := labWork("youtube", "0 9 * * 1")
	run := automation.Run{Outcome: automation.OutcomeDone, Line: "Found it.",
		Detail: "Found it.\nHis latest video is \"Why the war will not end this year\", up yesterday."}
	body := automationBannerBody(item, run)
	if !strings.HasPrefix(body, "done · ") || !strings.Contains(body, "Why the war will not end") || strings.Contains(body, "youtube") {
		t.Fatalf("the banner says %q", body)
	}
	watch := automationBannerBody(labWatch("ci on main"), automation.Run{Outcome: automation.OutcomeDone, Line: "ci on main happened",
		Detail: "completed failure tui3: fold the quiet rows ci master push 18734 4m12s"})
	if watch != "done · ci on main happened" {
		t.Fatalf("a watch that spoke says %q, not its own line", watch)
	}
	reminder := automationBannerBody(labReminder("stretch"), automation.Run{Outcome: automation.OutcomeDone, Line: "time to stretch"})
	if reminder != "done · time to stretch" {
		t.Fatalf("a reminder's banner says %q", reminder)
	}
}

// A WINDOW ATTACHED ELSEWHERE WITH NO PROJECT SENDS ~, never this laptop's home:
// the automation is kept and run on the far machine, whose store reads ~ as its
// own.
func TestAHostedWindowsTypedAutomationRunsInTheFarHome(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workspace, a.host = "", "devbox"
	if got := a.automationWorkspace(); got != "~" {
		t.Fatalf("a hosted window with no project sends %q", got)
	}
	a.host = ""
	if got := a.automationWorkspace(); got == "~" || got == "" {
		t.Fatalf("a window on this machine sends %q", got)
	}
}

// THE OWNER'S WALK: a reminder made with `in 2m` says its line and is finished;
// `e` on its row puts its line in the box, the moment is moved to the next
// minute, and the card is answered a beat after that minute came. That save
// used to store the passed moment as the next run, so the clock said the
// reminder at once and the row was finished again. It is refused now, and a
// moment moved properly later wakes the reminder with that moment next.
func TestAFinishedReminderEditedLaterWakesAgainAndALateSaveSaysSo(t *testing.T) {
	a, store := automationLab(t)
	now := time.Date(2026, 10, 14, 20, 31, 13, 0, time.UTC)
	a.clock = func() time.Time { return now }
	store.SetClock(func() time.Time { return now })
	finished := func() automation.Automation {
		t.Helper()
		drive(t, a, runCmd(a.automationsCommand(`add stretch in 2m say "stand up"`))...)
		harnessSettled(t, a)
		drive(t, a, key(session.AutomationSaveKey))
		all, _ := store.List()
		var made automation.Automation
		for _, item := range all {
			if item.Status == automation.StatusActive {
				made = item
			}
		}
		now = made.Schedule.At.Add(3 * time.Second)
		if _, err := store.Take(made, now); err != nil {
			t.Fatal(err)
		}
		readAutomationsNow(t, a)
		got, _ := store.Get(made.ID)
		if got.Status != automation.StatusFinished {
			t.Fatalf("the said reminder is %q", got.Status)
		}
		return got
	}
	// editTo presses `e` on the reminder's row and sends its line with the
	// moment moved to at.
	editTo := func(item automation.Automation, at time.Time) {
		t.Helper()
		runCmd(a.openAutomationsAt(item.ID))
		drive(t, a, key("right"), key("e"))
		box := a.input.String()
		line := regexp.MustCompile(` at [0-9T:.\-]+ `).ReplaceAllString(box, " at "+at.Format("2006-01-02T15:04")+" ")
		if line == box {
			t.Fatalf("the box holds no moment to move: %q", box)
		}
		a.input.reset()
		drive(t, a, runCmd(a.automationsCommand(strings.TrimPrefix(line, "/"+placeAutomationsWord+" ")))...)
		if a.auto == nil || a.auto.settled() || a.auto.head != typedEditHead {
			t.Fatalf("the edit raised no card: %+v (%s)", a.auto, homeNotes(a))
		}
		if !a.auto.notice.Next.Equal(at) {
			t.Fatalf("the card says the next run is %v, want %v", a.auto.notice.Next, at)
		}
		harnessSettled(t, a)
	}

	late := finished()
	moment := late.Schedule.At.Truncate(time.Minute).Add(time.Minute)
	editTo(late, moment)
	now = moment.Add(5 * time.Second)
	drive(t, a, key(session.AutomationSaveKey))
	if due, _ := store.Due(now); len(due) != 0 {
		t.Fatalf("a save answered after its moment left the reminder due at once: %+v", due)
	}
	if got, _ := store.Get(late.ID); got.Status != automation.StatusFinished {
		t.Fatalf("a refused save left the reminder %q", got.Status)
	}
	if notes := homeNotes(a); !strings.Contains(notes, "already passed") {
		t.Fatalf("the refusal does not say why: %q", notes)
	}
	if a.auto.verdict != autoNotSaved {
		t.Fatalf("the late card settled as %q", a.auto.verdict)
	}

	moved := finished()
	moment = now.Add(10 * time.Minute).Truncate(time.Minute)
	editTo(moved, moment)
	drive(t, a, key(session.AutomationSaveKey))
	got, _ := store.Get(moved.ID)
	if got.Status != automation.StatusActive || !got.Next.Equal(moment) {
		t.Fatalf("a finished reminder moved later is %q, next %v, want active at %v", got.Status, got.Next, moment)
	}
}

// AN EDIT'S CARD PROMISES THE NEXT RUN THE SAVE GIVES — the rule the
// conversation's change keeps ([automation.NextAfterChange]): none for a
// finished reminder whose moment stays, none while paused, and the one it has
// for a rhythm the line did not change.
func TestAnEditsCardSaysTheNextRunTheSaveGives(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"), labWatch("ci on main"), labReminder("leave"))
	byTitle := map[string]automation.Automation{}
	for _, item := range a.watch.list {
		byTitle[item.Title] = item
	}
	if _, err := store.SetStatus(byTitle["weekly update"].ID, automation.StatusPaused); err != nil {
		t.Fatal(err)
	}
	reminder := byTitle["leave"]
	if _, err := store.Take(reminder, reminder.Schedule.At.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	readAutomationsNow(t, a)
	for _, item := range a.watch.list {
		byTitle[item.Title] = item
	}
	cases := []struct {
		title string
		want  time.Time
	}{
		{"weekly update", time.Time{}},
		{"leave", time.Time{}},
		{"ci on main", byTitle["ci on main"].Next},
	}
	for _, c := range cases {
		item := byTitle[c.title]
		drive(t, a, runCmd(a.automationsCommand(automation.CommandLine(item)+` title "`+c.title+` renamed"`))...)
		if a.auto == nil || a.auto.settled() {
			t.Fatalf("%s: the edit raised no card (%s)", c.title, homeNotes(a))
		}
		if !a.auto.notice.Next.Equal(c.want) {
			t.Fatalf("%s: the card says the next run is %v, want %v", c.title, a.auto.notice.Next, c.want)
		}
		harnessSettled(t, a)
		drive(t, a, key(session.AutomationSaveKey))
		saved, _ := store.Get(item.ID)
		if saved.Title != c.title+" renamed" {
			t.Fatalf("%s: saving the card stored %+v", c.title, saved)
		}
		if saved.Status == automation.StatusActive && !saved.Next.Equal(c.want) {
			t.Fatalf("%s: the save gave next %v, the card said %v", c.title, saved.Next, c.want)
		}
	}
}
