package tui3

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The notices' own tests (notice.go, notice_ledger.go): the table check, the
// ledger's promises, the arbitration as a pure thing, and then the whole road
// through the app — a hint arming on its moment, drawing at the lowest rung,
// retiring on its gesture, and staying retired across a restart.

// ── the table ───────────────────────────────────────────────────────────────

func TestTheNoticeTableIsWhole(t *testing.T) {
	if err := checkNotices(notices); err != nil {
		t.Fatal(err)
	}
	if len(notices) == 0 {
		t.Fatal("the table is empty")
	}
}

// Each of these is a mistake somebody will make in the table one day, and the
// check has to name it rather than let the notice silently never fire.
func TestTheNoticeTableCheckRefusesEachMistake(t *testing.T) {
	sound := notice{id: "a-fine-one", armed: func(*app) bool { return true }, text: "x does y"}
	broken := []struct {
		name string
		list []notice
		want string
	}{
		{"duplicate id", []notice{sound, sound}, "twice"},
		{"empty text", []notice{{id: "quiet", armed: sound.armed}}, "nothing to say"},
		{"unknown retire event", []notice{{id: "lost", armed: sound.armed, text: "t", retire: "no-such-thing"}}, "nothing fires"},
		{"machinery word", []notice{{id: "loud", armed: sound.armed, text: "the auditor says"}}, "machinery"},
		{"bad id", []notice{{id: "Not Kebab", armed: sound.armed, text: "t"}}, "kebab"},
		{"no slot", []notice{{id: "nowhere", slot: noticeSlots, armed: sound.armed, text: "t"}}, "slot"},
		{"no arming rule", []notice{{id: "unarmed", text: "t"}}, "arming"},
	}
	for _, tc := range broken {
		err := checkNotices(tc.list)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: the check said %v, want a message with %q", tc.name, err, tc.want)
		}
	}
}

// EVERY EVENT IS FIRED FROM A SEAM. A retire rule naming an event nobody fires
// is a hint that never goes away, and the table check cannot see that — it only
// knows the event is spelled right. So the package's own source is read: every
// event constant in notice.go appears in [noticeEvents], and every one of them
// is passed to [app.noticeEvent] somewhere outside notice.go.
func TestEveryNoticeEventIsFiredSomewhere(t *testing.T) {
	source, err := os.ReadFile("notice.go")
	if err != nil {
		t.Fatal(err)
	}
	// eventFoo = "foo" → foo: eventFoo
	decl := regexp.MustCompile(`(?m)^\s*(event[A-Z]\w*)\s*=\s*"([^"]+)"`)
	names := map[string]string{}
	for _, m := range decl.FindAllStringSubmatch(string(source), -1) {
		names[m[2]] = m[1]
	}
	listed := map[string]bool{}
	for _, name := range noticeEvents {
		listed[name] = true
	}
	for value, ident := range names {
		if !listed[value] {
			t.Errorf("%s is declared but missing from noticeEvents", ident)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var rest strings.Builder
	for _, file := range files {
		if file == "notice.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rest.Write(raw)
	}
	for value, ident := range names {
		if !strings.Contains(rest.String(), "noticeEvent("+ident+")") {
			t.Errorf("%s (%q) is never fired from a seam", ident, value)
		}
	}
	for _, n := range notices {
		if n.retire != "" && names[n.retire] == "" {
			t.Errorf("notice %q retires on %q, which is not a declared constant", n.id, n.retire)
		}
	}
}

// ── the ledger ──────────────────────────────────────────────────────────────

func TestTheNoticeLedgerRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", noticeLedgerName)
	var ledger noticeLedger
	ledger.Build = "abc123"
	ledger.show("one")
	ledger.show("one")
	ledger.retire("two")
	if err := ledger.write(path); err != nil {
		t.Fatal(err)
	}
	back := loadNoticeLedger(path)
	if back.Build != "abc123" || back.shown("one") != 2 || !back.retired("two") || back.retired("one") {
		t.Fatalf("the ledger came back as %+v", back)
	}
	// And the file is the profile's own kind of file: nobody else may read it.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the ledger is mode %o, want 0600", info.Mode().Perm())
	}
}

func TestAMissingOrCorruptNoticeLedgerIsAnEmptyOne(t *testing.T) {
	dir := t.TempDir()
	if got := loadNoticeLedger(filepath.Join(dir, "absent.json")); got.Build != "" || len(got.Seen) != 0 {
		t.Fatalf("a missing ledger read as %+v", got)
	}
	path := filepath.Join(dir, noticeLedgerName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadNoticeLedger(path); got.Build != "" || len(got.Seen) != 0 {
		t.Fatalf("a corrupt ledger read as %+v", got)
	}
	if got := loadNoticeLedger(""); got.Build != "" || len(got.Seen) != 0 {
		t.Fatalf("no path read as %+v", got)
	}
}

// The write goes to a temporary name and is renamed over the old file, so the
// directory holds exactly one ledger afterwards and the old one is intact
// until the new one is whole.
func TestTheNoticeLedgerWriteLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, noticeLedgerName)
	var ledger noticeLedger
	ledger.retire("first")
	if err := ledger.write(path); err != nil {
		t.Fatal(err)
	}
	ledger.retire("second")
	if err := ledger.write(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != noticeLedgerName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the directory holds %v, want only %s", names, noticeLedgerName)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("the ledger on disk is not JSON: %q", raw)
	}
}

// ── the arbitration ─────────────────────────────────────────────────────────

func freshBoard() noticeBoard { return newNoticeBoard("", "", true) }

// ONE PER SLOT, AND THE HIGHER PRIORITY WINS. Two armed notices for one slot
// yield one id, and it is the more urgent of the two.
func TestOnePerSlotAndTheHigherPriorityWins(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{
		{id: "low", priority: 10, armed: true},
		{id: "high", priority: 90, armed: true},
		{id: "highest-but-idle", priority: 100, armed: false},
	}
	if got := b.pick(slotHint, cands, 1); got != "high" {
		t.Fatalf("the slot picked %q, want high", got)
	}
	b.take(slotHint, "high", 3, 1)
	// The one standing keeps standing against an equal, so the slot does not
	// flicker between two hints of the same weight.
	cands = append(cands, noticeCandidate{id: "equal", priority: 90, armed: true})
	if got := b.pick(slotHint, cands, 5); got != "high" {
		t.Fatalf("an equal took the slot from the one standing: %q", got)
	}
}

// THE QUIET GAP. A different hint may not take the slot until [noticeGap]
// turns have passed since it last changed hands — but the slot's first
// occupant waits on nothing, and a slot going empty never waits.
func TestTheHintSlotChangesHandsSlowly(t *testing.T) {
	b := freshBoard()
	first := []noticeCandidate{{id: "first", priority: 10, armed: true}}
	if got := b.pick(slotHint, first, 1); got != "first" {
		t.Fatalf("the first hint of the session waited: %q", got)
	}
	b.take(slotHint, "first", 3, 1)

	both := append(first, noticeCandidate{id: "second", priority: 50, armed: true})
	if got := b.pick(slotHint, both, 1+noticeGap-1); got != "first" {
		t.Fatalf("the slot changed hands inside the gap: %q", got)
	}
	if got := b.pick(slotHint, both, 1+noticeGap); got != "second" {
		t.Fatalf("the slot did not change hands after the gap: %q", got)
	}

	// Inside the gap, a standing hint that stopped being armed stands down at
	// once and the slot goes quiet rather than jumping to the next one.
	b = freshBoard()
	b.take(slotHint, "first", 3, 1)
	gone := []noticeCandidate{{id: "first", priority: 10, armed: false}, {id: "second", priority: 50, armed: true}}
	if got := b.pick(slotHint, gone, 1); got != "" {
		t.Fatalf("a disarmed hint was replaced inside the gap: %q", got)
	}

	// The note slot has no gap: news is said when it is due.
	b = freshBoard()
	b.take(slotHint, "first", 3, 1)
	if got := b.pick(slotNote, []noticeCandidate{{id: "news", priority: 1, armed: true}}, 1); got != "news" {
		t.Fatalf("the note slot waited on the hint slot's gap: %q", got)
	}
}

// A notice is counted once per session however many events re-decide the slot,
// and its last allowed showing retires it for the sessions after while leaving
// it up for this one.
func TestAShowingIsCountedOncePerSessionAndTheLastOneRetires(t *testing.T) {
	b := freshBoard()
	b.take(slotHint, "tip", 2, 1)
	b.take(slotHint, "", 2, 2)
	b.take(slotHint, "tip", 2, 3)
	if got := b.ledger.shown("tip"); got != 1 {
		t.Fatalf("one session counted %d showings", got)
	}
	if b.retired("tip") {
		t.Fatal("a first showing retired the notice")
	}

	// The next session: the second showing is the last allowed.
	next := newNoticeBoard("", "", true)
	next.ledger = b.ledger
	next.take(slotHint, "tip", 2, 1)
	if !next.retired("tip") {
		t.Fatal("the last allowed showing did not retire the notice")
	}
	if next.current[slotHint] != "tip" {
		t.Fatal("the last allowed showing was not shown")
	}
}

// Once retired in a session, a notice may not come back in it even while its
// arming rule is still true.
func TestARetiredNoticeNeverReturnsThisSession(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{{id: "tip", priority: 10, armed: true}}
	b.take(slotHint, "tip", 3, 1)
	b.retire("tip")
	if b.current[slotHint] != "" {
		t.Fatal("retiring did not clear the slot")
	}
	if got := b.pick(slotHint, cands, 9); got != "" {
		t.Fatalf("a retired notice came back: %q", got)
	}
}

// ── through the surface ─────────────────────────────────────────────────────

// noticeApp is [sheetApp] over a profile directory the caller already has: the
// same surface, a second time, for the tests that restart over one ledger. It
// must follow a sheetApp in the same test, which is what pins the environment.
func noticeApp(t *testing.T, dir string) *app {
	t.Helper()
	a := newApp(context.Background(), Options{
		Agent:      &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
	})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	a.welcome = welcome{spent: true}
	a.entries = nil
	a.touch()
	return a
}

// startTask is the engine accepting a task, as the loop sees it.
func startTask(t *testing.T, a *app) {
	t.Helper()
	drive(t, a, taskStartedMsg{kind: "single", id: "7", title: "port the parser"})
}

const taskPageTip = "ctrl+. sees every task this project has run"

// THE WHOLE ROAD. A hint arms on its moment, draws in the hint slot and only at
// the lowest rung there, retires on the gesture it teaches, and is still
// retired when the surface comes up again over the same profile.
func TestAHintArmsDrawsLowestRetiresAndStaysRetired(t *testing.T) {
	a, dir := sheetApp(t)
	if got := a.notices.current[slotHint]; got != "" {
		t.Fatalf("a fresh surface already holds hint %q", got)
	}
	if strings.Contains(plain(frame(a)), taskPageTip) {
		t.Fatal("the task page tip is up before any task has started")
	}

	startTask(t, a)
	if got := a.notices.current[slotHint]; got != "task-page-after-first-task" {
		t.Fatalf("a task starting armed %q", got)
	}
	if got := a.legendRight(a.width); got != taskPageTip {
		t.Fatalf("the hint slot reads %q, want the tip", got)
	}
	if !strings.Contains(plain(frame(a)), taskPageTip) {
		t.Fatalf("the tip is not on the frame:\n%s", plain(frame(a)))
	}

	// LOWEST RUNG. A running turn's own key outranks it, and so does a box with
	// words in it.
	a.state = stateWorking
	if got := a.legendRight(a.width); got != "esc interrupt" {
		t.Fatalf("a tip outranked a running turn's key: %q", got)
	}
	a.state = stateIdle
	a.input.setText("half a sentence")
	if got := a.legendRight(a.width); strings.Contains(got, taskPageTip) {
		t.Fatalf("a tip drew over a box with words in it: %q", got)
	}
	a.input.reset()
	if got := a.legendRight(a.width); got != taskPageTip {
		t.Fatalf("the tip did not come back over an empty box: %q", got)
	}

	// THE GESTURE RETIRES IT: the task page actually opening.
	a.comp.tasks = []session.TaskIndexEntry{pastTask("4", "port-the-parser", "Port the parser", time.Hour)}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the task page did not open")
	}
	a.closeTaskSheet()
	if got := a.notices.current[slotHint]; got != "" {
		t.Fatalf("the slot still holds %q after the gesture", got)
	}
	if !a.notices.retired("task-page-after-first-task") {
		t.Fatal("the gesture did not retire the hint")
	}
	if strings.Contains(plain(frame(a)), taskPageTip) {
		t.Fatal("the tip is still drawn after its gesture")
	}
	// Re-arming does nothing this session either.
	startTask(t, a)
	if got := a.notices.current[slotHint]; got == "task-page-after-first-task" {
		t.Fatal("a retired hint came back in the same session")
	}

	// And it is on disk, beside config.json, so the next surface knows.
	ledger := loadNoticeLedger(filepath.Join(dir, noticeLedgerName))
	if !ledger.retired("task-page-after-first-task") {
		t.Fatalf("the ledger on disk does not have it retired: %+v", ledger)
	}
	again := noticeApp(t, dir)
	startTask(t, again)
	if got := again.notices.current[slotHint]; got == "task-page-after-first-task" {
		t.Fatal("a retired hint came back after a restart")
	}
	if strings.Contains(plain(frame(again)), taskPageTip) {
		t.Fatal("the tip is drawn after a restart")
	}
}

// EVERY RETIRE EVENT IS PROVED BY ITS OWN GESTURE, through the real seam and
// not by calling [app.noticeEvent] by hand: each driver here does what a
// person does, and the event has to have been fired by it. A retire rule in the
// table with no driver here fails the test by name.
func TestEveryRetireEventIsProvedByItsGesture(t *testing.T) {
	drivers := map[string]func(*testing.T, *app){
		eventTurnEnded:   func(t *testing.T, a *app) { a.settle() },
		eventTaskStarted: startTask,
		eventTaskPageOpened: func(t *testing.T, a *app) {
			a.comp.tasks = []session.TaskIndexEntry{pastTask("4", "port-the-parser", "Port the parser", time.Hour)}
			if !openTaskPlaceWithRows(a) {
				t.Fatal("the task page did not open")
			}
		},
		eventMenuOpened: func(t *testing.T, a *app) {
			drive(t, a, key("/"))
			if !a.menu.open {
				t.Fatal("typing / did not open the command list")
			}
		},
		eventRewound: func(t *testing.T, a *app) {
			a.agent = &rewindFake{fakeAgent: &fakeAgent{model: "m", past: rewindPast()}}
			point := session.RewindPoint{Index: 4, Turn: true, Said: "three", Entry: 4}
			if err := a.rewindLand(point, "1 turn", nil, 0, func() {}); err != nil {
				t.Fatal(err)
			}
		},
		eventCopyEntered: func(t *testing.T, a *app) {
			a.note("something to copy")
			a.enterCopy()
			if !a.copy.on {
				t.Fatal("copy mode did not open")
			}
		},
		eventModelSwitched:   func(t *testing.T, a *app) { a.switchModel("openai/gpt-4.1", 1_000_000) },
		eventCompacted:       func(t *testing.T, a *app) { drive(t, a, compactedMsg{}) },
		eventFilesOpened:     func(t *testing.T, a *app) { a.slash("/files") },
		eventResumeOpened:    func(t *testing.T, a *app) { a.slash("/resume") },
		eventCostShown:       func(t *testing.T, a *app) { a.slash("/cost") },
		eventStandingOpened:  func(t *testing.T, a *app) { a.slash("/standing") },
		eventDeliverableMade: func(t *testing.T, a *app) { a.exportDone(exportedMsg{path: "/tmp/lab/talk.md"}) },
	}
	for _, name := range noticeEvents {
		if name == eventBoot {
			continue
		}
		driver, ok := drivers[name]
		if !ok {
			t.Errorf("event %q has no gesture driving it in this test", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			a, _ := sheetApp(t)
			driver(t, a)
			if !a.notices.seen[name] {
				t.Fatalf("the gesture for %q did not fire it", name)
			}
			for _, n := range notices {
				if n.retire == name && !a.notices.retired(n.id) {
					t.Errorf("notice %q did not retire on %q", n.id, name)
				}
			}
		})
	}
	// And the boot event fires when the surface comes up.
	a, _ := sheetApp(t)
	if !a.notices.seen[eventBoot] {
		t.Fatal("the surface came up without the boot event")
	}
}

// The compact hint is the one whose arming fact is a reading rather than an
// event: half the window. It stands when the reading says so and stands down
// the moment it does not, on the next turn end.
func TestTheCompactHintFollowsTheContextReading(t *testing.T) {
	a, _ := sheetApp(t)
	agent := a.agent.(*fakeAgent)
	a.ctxWindow = 1000
	agent.weight = 200
	a.settle()
	if got := a.notices.current[slotHint]; got == "compact-at-half" {
		t.Fatal("the compact hint armed at a fifth of the window")
	}
	agent.weight = 600
	a.settle()
	if got := a.notices.current[slotHint]; got != "compact-at-half" {
		t.Fatalf("at 60%% the slot holds %q", got)
	}
	agent.weight = 100
	a.turn += noticeGap
	a.settle()
	if got := a.notices.current[slotHint]; got == "compact-at-half" {
		t.Fatal("the compact hint stayed up after the reading fell")
	}
}

// The Display tab's "hints" row silences the slot, and the change lands at the
// next turn end, the way the mouse row's does.
func TestTheHintsRowSilencesTheSlot(t *testing.T) {
	a, dir := sheetApp(t)
	startTask(t, a)
	if got := a.legendRight(a.width); got != taskPageTip {
		t.Fatalf("the hint slot reads %q before the toggle", got)
	}

	a.openSettings()
	for i, tab := range settingTabs {
		if tab == tabDisplay {
			a.sheet.tab = i
		}
	}
	a.sheet.cursor, a.sheet.top = 0, 0
	a.sheet.build()
	cursorTo(t, a, config.KeyHints)
	drive(t, a, key("enter"))
	a.closeSettings()
	if config.HintsAt(dir) {
		t.Fatal("the toggle did not write the row off")
	}

	a.settle()
	if a.notices.enabled {
		t.Fatal("the turn end did not re-read the row")
	}
	if got := a.legendRight(a.width); got == taskPageTip {
		t.Fatal("a silenced slot still draws the tip")
	}
	// The next surface over this profile is quiet from the start.
	again := noticeApp(t, dir)
	startTask(t, again)
	if got := again.notices.current[slotHint]; got != "" {
		t.Fatalf("a silenced profile armed %q", got)
	}
}

// THE NEWS CHANNEL. A row marked news is said once, as a line in the
// conversation, on the first launch after the build changed — and never on a
// first launch, never twice, and never while the hints row is off. The table
// has no news at launch, so the test brings its own row.
func TestNewsIsSaidOnceAfterABuildChange(t *testing.T) {
	saved := notices
	notices = append([]notice{{
		id: "test-news", slot: slotNote, priority: 1, news: true, maxShown: 1,
		armed: func(*app) bool { return true },
		text:  "new · the test channel is open",
	}}, saved...)
	defer func() { notices = saved }()

	a, dir := sheetApp(t)
	path := filepath.Join(dir, noticeLedgerName)
	said := func(a *app) bool {
		for _, e := range a.entries {
			if e.kind == entryNote && strings.Contains(e.text, "test channel") {
				return true
			}
		}
		return false
	}
	boot := func(a *app, build string) {
		a.entries = nil
		a.notices = newNoticeBoard(path, build, config.HintsAt(dir))
		a.noticeEvent(eventBoot)
	}

	// A first launch: the ledger has no build, so nothing is new.
	if err := (noticeLedger{}).write(path); err != nil {
		t.Fatal(err)
	}
	boot(a, "build-one")
	if said(a) {
		t.Fatal("a first launch said news")
	}
	if got := loadNoticeLedger(path).Build; got != "build-one" {
		t.Fatalf("the first launch recorded build %q", got)
	}

	// The same build again: nothing.
	boot(a, "build-one")
	if said(a) {
		t.Fatal("the same build said news")
	}

	// A new build: once.
	boot(a, "build-two")
	if !said(a) {
		t.Fatal("a changed build said nothing")
	}
	boot(a, "build-two")
	if said(a) {
		t.Fatal("the news was said twice")
	}
	if !loadNoticeLedger(path).retired("test-news") {
		t.Fatal("a said news line was not retired")
	}

	// A binary the toolchain could not name has no news channel at all.
	boot(a, "")
	if said(a) {
		t.Fatal("an unnamed build said news")
	}
}

// A surface built without a profile — every test's bare app — still has a
// board that works for the session, and writes nothing anywhere.
func TestABareSurfaceKeepsNoticesForTheSession(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.notices.path != "" {
		t.Fatalf("a bare surface has a ledger at %q", a.notices.path)
	}
	startTask(t, a)
	if got := a.notices.current[slotHint]; got != "task-page-after-first-task" {
		t.Fatalf("a bare surface armed %q", got)
	}
}
