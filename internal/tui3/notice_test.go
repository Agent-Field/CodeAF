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

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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

// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW — and the notices' own memory is what that law cost
// most (#315). [noticeLedgerPath] answered "" for an empty directory and the
// board then held everything in RAM for the session, so on every launch that had
// not exported CODEAF_PROFILE_DIR — which is very nearly all of them — a hint
// meant to age out after three sessions was on its first session every time.
//
// The road here is the real one: three ordinary launches in a row, each firing
// the seam a finished turn fires, and then a fourth that finds the hint taken as
// read.
func TestAHintAgesOutAcrossOrdinaryLaunches(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv(config.ProfileDirEnv, "")

	if got, want := noticeLedgerPath(""), filepath.Join(root, noticeLedgerName); got != want {
		t.Fatalf("an ordinary launch keeps its notices at %q, want %q", got, want)
	}

	// The task tip is the one a first exchange arms highest (notice.go's
	// table); `/ shows every command` stood here until both feet said it.
	const hint = "task-in-chat"
	// A SHOWING IS A VISIBLE ONE: the row draws after a quiet minute, so each
	// launch is a turn ending and then a minute of nothing (chattip_test.go
	// proves the clock; here it is turned by hand).
	launch := func() *app {
		a := noticeApp(t, "")
		a.turn = 1
		a.noticeEvent(eventTurnEnded)
		quietMinute(a)
		return a
	}
	for session := 1; session <= noticeShownDefault; session++ {
		a := launch()
		if got := a.notices.current[hintSlotForTest]; got != hint {
			t.Fatalf("launch %d holds %q in the hint slot, want %q", session, got, hint)
		}
		if got := loadNoticeLedger(noticeLedgerPath("")).shown(hint); got != session {
			t.Fatalf("after launch %d the ledger on disk counts %d showings", session, got)
		}
	}
	if got := loadNoticeLedger(noticeLedgerPath("")); !got.retired(hint) {
		t.Fatalf("the hint is not retired after %d launches: %+v", noticeShownDefault, got)
	}
	if a := launch(); a.notices.current[hintSlotForTest] == hint {
		t.Fatalf("a hint shown in %d launches came back in the next one", noticeShownDefault)
	}
}

// hintSlotForTest names the slot these tests read, spelled once so the
// assertions above read as sentences.
const hintSlotForTest = slotHint

// quietMinute turns the conversation's clock by hand until the row's tip is
// due (notice.go's [app.noticeIdleBeat]).
func quietMinute(a *app) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.noticeTouched()
	a.noticeArmIdle()
	now = now.Add(chatHintIdle)
	a.noticeIdleBeat(a.notices.idleGen)
}

// AND THE NEWS CHANNEL HAS AN OLDER BUILD TO COMPARE AGAINST. It opens on the
// second build a profile meets, which on an ordinary launch it never did: the
// first build was never written down, so every launch was a first launch and a
// shipped feature had no way to announce itself.
func TestTheNewsChannelRemembersTheBuildOnAnOrdinaryLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv(config.ProfileDirEnv, "")
	path := noticeLedgerPath("")

	if first := newNoticeBoard(path, "build-one", true); first.news {
		t.Fatal("a first launch found news")
	}
	if got := loadNoticeLedger(path).Build; got != "build-one" {
		t.Fatalf("the first ordinary launch recorded build %q", got)
	}
	if second := newNoticeBoard(path, "build-two", true); !second.news {
		t.Fatal("a changed build found no news on an ordinary launch")
	}
}

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

// A ROTATION, NOT A RANKING: the first eligible tip stands, an event without
// an advance keeps it, an advance moves to the next eligible in the table's
// order, and the ring comes round.
func TestASlotRotatesThroughTheEligibleTipsInTableOrder(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{
		{id: "first", armed: true},
		{id: "idle", armed: false},
		{id: "second", armed: true},
		{id: "third", armed: true},
	}
	if got := b.pick(slotHint, cands); got != "first" {
		t.Fatalf("the slot picked %q, want the first eligible", got)
	}
	b.take(slotHint, "first", 6, true)
	if got := b.pick(slotHint, cands); got != "first" {
		t.Fatalf("an event without an advance moved the slot to %q", got)
	}
	for _, want := range []string{"second", "third", "first"} {
		b.advance[slotHint] = true
		got := b.pick(slotHint, cands)
		if got != want {
			t.Fatalf("the ring went to %q, want %q", got, want)
		}
		b.take(slotHint, got, 6, true)
	}
	// The note slot rotates on the same terms; with one candidate it is that one.
	if got := b.pick(slotNote, []noticeCandidate{{id: "news", armed: true}}); got != "news" {
		t.Fatalf("the note slot said %q", got)
	}
}

// A TIP THAT HAS JUST BECOME TRUE JUMPS THE RING, once, whether or not the slot
// was asked to move — and then takes its turn like every other row.
func TestAFreshTipJumpsTheRing(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{
		{id: "compact", armed: false},
		{id: "first", armed: true},
		{id: "second", armed: true},
	}
	if got := b.pick(slotHint, cands); got != "first" {
		t.Fatalf("the slot picked %q", got)
	}
	b.take(slotHint, "first", 6, true)
	cands[0] = noticeCandidate{id: "compact", armed: true, fresh: true}
	if got := b.pick(slotHint, cands); got != "compact" {
		t.Fatalf("a fresh tip did not jump the ring: %q", got)
	}
	b.take(slotHint, "compact", 6, true)
	// No longer fresh: an event keeps it, and an advance walks on from it.
	cands[0].fresh = false
	if got := b.pick(slotHint, cands); got != "compact" {
		t.Fatalf("a tip that had jumped was moved by an event: %q", got)
	}
	b.advance[slotHint] = true
	if got := b.pick(slotHint, cands); got != "first" {
		t.Fatalf("the ring did not walk on from the fresh tip: %q", got)
	}
	// A tip disarming stands down at once, for the next eligible.
	b.take(slotHint, "first", 6, true)
	cands[1].armed = false
	if got := b.pick(slotHint, cands); got != "second" {
		t.Fatalf("a disarmed tip did not yield: %q", got)
	}
}

// Every change of hands is a showing, a slot re-decided to the same tip is not,
// and the last allowed showing retires the notice while leaving it up.
func TestEveryChangeOfHandsIsAShowingAndTheLastOneRetires(t *testing.T) {
	b := freshBoard()
	b.take(slotHint, "tip", 3, true)
	b.take(slotHint, "tip", 3, true)
	if got := b.ledger.shown("tip"); got != 1 {
		t.Fatalf("one standing counted %d showings", got)
	}
	b.take(slotHint, "", 3, true)
	b.take(slotHint, "tip", 3, true)
	if got := b.ledger.shown("tip"); got != 2 {
		t.Fatalf("a tip coming back counted %d showings, want 2", got)
	}
	if b.retired("tip") {
		t.Fatal("a second showing retired the notice")
	}
	// The next surface over the same ledger: the third showing is the last.
	next := newNoticeBoard("", "", true)
	next.ledger = b.ledger
	next.take(slotHome, "tip", 3, true)
	if !next.retired("tip") {
		t.Fatal("the last allowed showing did not retire the notice")
	}
	if next.current[slotHome] != "tip" {
		t.Fatal("the last allowed showing was not shown")
	}
}

// Once retired in a session, a notice may not come back in it even while its
// arming rule is still true.
func TestARetiredNoticeNeverReturnsThisSession(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{{id: "tip", armed: true}}
	b.take(slotHint, "tip", 3, true)
	b.retire("tip")
	if b.current[slotHint] != "" {
		t.Fatal("retiring did not clear the slot")
	}
	if got := b.pick(slotHint, cands); got != "" {
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
	// The gesture that retires the hint is the task page OPENING ON ROWS, and
	// the row seeded below is dated from the fixture clock — so the surface goes
	// on it too ([pinFixtureClock] states the law).
	pinFixtureClock(a)
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
	// THE ROW WAITS FOR A QUIET MINUTE (notice.go's THE CONVERSATION'S CLOCK);
	// the clock itself is proved in chattip_test.go, and here the minute is
	// taken as passed.
	if got := a.noticeHint(); got != "" {
		t.Fatalf("the tip drew before the person had been quiet: %q", got)
	}
	a.notices.due = true
	if got := a.noticeHint(); got != taskPageTip {
		t.Fatalf("the tip row reads %q, want the tip", got)
	}
	if got := a.footHint(a.width); strings.Contains(got, taskPageTip) {
		t.Fatalf("the keys row still carries the tip: %q", got)
	}
	if !strings.Contains(plain(frame(a)), taskPageTip) {
		t.Fatalf("the tip is not on the frame:\n%s", plain(frame(a)))
	}

	// OVER NOTHING THAT IS HAPPENING. A running turn outranks it, and so does a
	// box with words in it.
	a.state = stateWorking
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a tip drew over a running turn: %q", got)
	}
	a.state = stateIdle
	a.input.setText("half a sentence")
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a tip drew over a box with words in it: %q", got)
	}
	a.input.reset()
	if got := a.noticeHint(); got != taskPageTip {
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
	again.notices.due = true
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
			pinFixtureClock(a)
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
		eventAsked:           func(t *testing.T, a *app) { a.askHere("what is this") },
		eventTaskTyped:       func(t *testing.T, a *app) { a.slash("/task") },
		eventManualAsked:     func(t *testing.T, a *app) { a.slash("/manual") },
		eventTabReopened:     func(t *testing.T, a *app) { drive(t, a, reopenPress()) },
		eventAtOpened: func(t *testing.T, a *app) {
			drive(t, a, key("@"), key("s"), key("h"))
			if !a.comp.open {
				t.Fatal("typing @ did not open the completion")
			}
		},
		eventAttached:        func(t *testing.T, a *app) { a.slash("/attach") },
		eventFolderPicked:    func(t *testing.T, a *app) { a.slash("/folder") },
		eventModelListOpened: func(t *testing.T, a *app) { a.slash("/model") },
		eventCrewShown:       func(t *testing.T, a *app) { a.slash("/crew") },
		eventBudgetShown:     func(t *testing.T, a *app) { a.slash("/budget") },
		eventSpendOpened:     func(t *testing.T, a *app) { a.slash("/spend") },
		eventSteered: func(t *testing.T, a *app) {
			a.state = stateWorking
			a.input.setText("go left instead")
			drive(t, a, key("enter"))
		},
		eventQueued: func(t *testing.T, a *app) {
			a.state = stateWorking
			a.input.setText("and then this")
			drive(t, a, key("ctrl+q"))
		},
		eventChatStarted:      func(t *testing.T, a *app) { drive(t, a, key("ctrl+t")) },
		eventPlaceJumped:      func(t *testing.T, a *app) { drive(t, a, key("alt+3")) },
		eventRemembered:       func(t *testing.T, a *app) { a.slash("/remember the parser is under internal") },
		eventSearchOpened:     func(t *testing.T, a *app) { a.slash("/search") },
		eventSubharnessOpened: func(t *testing.T, a *app) { a.slash("/subharness") },
		eventConnectOpened:    func(t *testing.T, a *app) { a.slash("/connect") },
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
	a.settle()
	if got := a.notices.current[slotHint]; got == "compact-at-half" {
		t.Fatal("the compact hint stayed up after the reading fell")
	}
}

// The Workspace tab's "disable hints" row silences the slot, and the change
// lands at the next turn end, the way the mouse row's does.
func TestTheHintsRowSilencesTheSlot(t *testing.T) {
	a, dir := sheetApp(t)
	startTask(t, a)
	a.notices.due = true
	if got := a.noticeHint(); got != taskPageTip {
		t.Fatalf("the tip row reads %q before the toggle", got)
	}

	a.openSettings()
	for i, tab := range settingTabs {
		if tab == tabWorkspace {
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
	if got := a.noticeHint(); got == taskPageTip {
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
		id: "test-news", slot: slotNote, news: true, maxShown: 1,
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

// A board handed no path at all still works for the session and writes nothing
// anywhere — which is what this suite's bare app is pinned to (tui3_test.go),
// and what an embedding that wires its own storage gets.
//
// It is NOT what a launch with no CODEAF_PROFILE_DIR gets: that is the ordinary
// launch, and its notices live in the state root with everything else it
// remembers ([TestAHintAgesOutAcrossOrdinaryLaunches]).
func TestABoardWithNoPathKeepsNoticesForTheSession(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.notices.path != "" {
		t.Fatalf("the pinned board has a ledger at %q", a.notices.path)
	}
	startTask(t, a)
	if got := a.notices.current[slotHint]; got != "task-page-after-first-task" {
		t.Fatalf("a session-only board armed %q", got)
	}
	if err := a.notices.ledger.write(""); err != nil {
		t.Fatalf("a pathless ledger refused to be written away: %v", err)
	}
}

func TestUnreadProfileKeysNoticeTracksTheSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, noticeLedgerName)
	boot := func(keys []string) *app {
		a := newTestApp(&fakeAgent{model: "m"})
		a.notices = newNoticeBoard(path, "", true)
		a.showUnreadProfileKeys(keys)
		return a
	}
	said := func(a *app, text string) bool {
		for _, entry := range a.entries {
			if entry.kind == entryNote && strings.Contains(entry.text, text) {
				return true
			}
		}
		return false
	}
	if said(boot(nil), "config.json") {
		t.Fatal("flat config showed a notice")
	}
	if !said(boot([]string{"models"}), "models") {
		t.Fatal("nested models key was not named")
	}
	if said(boot([]string{"models"}), "models") {
		t.Fatal("unchanged unread set repeated")
	}
	if !said(boot([]string{"models", "tiers"}), "models, tiers") {
		t.Fatal("changed unread set did not show")
	}
	if said(boot([]string{"models"}), "models") {
		t.Fatal("previous unread set showed again")
	}
}
