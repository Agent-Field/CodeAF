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
	// A SHOWING ON THE CONVERSATION'S ROW IS THE SLOT TAKING THE TIP, counted
	// once per session however many events re-decide it — so each launch is one
	// turn ending, and the ledger on disk has one more showing after it.
	launch := func() *app {
		a := noticeApp(t, "")
		a.turn = 1
		a.noticeEvent(eventTurnEnded)
		return a
	}
	for session := 1; session <= noticeShownDefault; session++ {
		a := launch()
		if got := a.notices.current[hintSlotForTest]; got != hint {
			t.Fatalf("launch %d holds %q in the hint slot, want %q", session, got, hint)
		}
		// A second event in the same session counts nothing more.
		a.noticeEvent(eventTurnEnded)
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

// fixedLimit is a limit lookup answering n for every notice, for the board
// tests that need no table.
func fixedLimit(n int) func(string) int { return func(string) int { return n } }

// HOME'S ROW IS A ROTATION AND NOT A RANKING: the first eligible tip stands,
// an event without an advance keeps it, an advance moves to the next eligible
// in the table's order, and the ring comes round. (The conversation's row is
// the ranking, below.)
func TestHomesRowRotatesThroughTheEligibleTipsInTableOrder(t *testing.T) {
	b := freshBoard()
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	cands := []noticeCandidate{
		{id: "first", armed: true},
		{id: "idle", armed: false},
		{id: "second", armed: true},
		{id: "third", armed: true},
	}
	if got := b.pick(slotHome, cands, 0); got != "first" {
		t.Fatalf("the slot picked %q, want the first eligible", got)
	}
	b.take(slotHome, "first", true, now, fixedLimit(6), 0)
	if got := b.pick(slotHome, cands, 0); got != "first" {
		t.Fatalf("an event without an advance moved the slot to %q", got)
	}
	for _, want := range []string{"second", "third", "first"} {
		b.advance[slotHome] = true
		got := b.pick(slotHome, cands, 0)
		if got != want {
			t.Fatalf("the ring went to %q, want %q", got, want)
		}
		b.take(slotHome, got, true, now, fixedLimit(6), 0)
	}
	// The note slot rotates on the same terms; with one candidate it is that one.
	if got := b.pick(slotNote, []noticeCandidate{{id: "news", armed: true}}, 0); got != "news" {
		t.Fatalf("the note slot said %q", got)
	}
}

// A TIP THAT HAS JUST BECOME TRUE JUMPS HOME'S RING, once, whether or not the
// slot was asked to move — and then takes its turn like every other row.
func TestAFreshTipJumpsTheRing(t *testing.T) {
	b := freshBoard()
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	cands := []noticeCandidate{
		{id: "compact", armed: false},
		{id: "first", armed: true},
		{id: "second", armed: true},
	}
	if got := b.pick(slotHome, cands, 0); got != "first" {
		t.Fatalf("the slot picked %q", got)
	}
	b.take(slotHome, "first", true, now, fixedLimit(6), 0)
	cands[0] = noticeCandidate{id: "compact", armed: true, fresh: true}
	if got := b.pick(slotHome, cands, 0); got != "compact" {
		t.Fatalf("a fresh tip did not jump the ring: %q", got)
	}
	b.take(slotHome, "compact", true, now, fixedLimit(6), 0)
	// No longer fresh: an event keeps it, and an advance walks on from it.
	cands[0].fresh = false
	if got := b.pick(slotHome, cands, 0); got != "compact" {
		t.Fatalf("a tip that had jumped was moved by an event: %q", got)
	}
	b.advance[slotHome] = true
	if got := b.pick(slotHome, cands, 0); got != "first" {
		t.Fatalf("the ring did not walk on from the fresh tip: %q", got)
	}
	// A tip disarming stands down at once, for the next eligible.
	b.take(slotHome, "first", true, now, fixedLimit(6), 0)
	cands[1].armed = false
	if got := b.pick(slotHome, cands, 0); got != "second" {
		t.Fatalf("a disarmed tip did not yield: %q", got)
	}
}

// A SHOWING ON HOME'S ROW IS A TIP THAT STOOD TWENTY SECONDS WHERE IT COULD BE
// SEEN. A flash on the way through is nothing; a tip that stood is counted when it leaves; a tip
// decided while the row could not be seen counts nothing until the row comes
// into view; and the last allowed showing retires the notice.
func TestAShowingIsATipThatStoodLongEnoughToBeRead(t *testing.T) {
	b := freshBoard()
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	limit := fixedLimit(3)
	// A flash: the tip leaves five seconds after it came.
	b.take(slotHome, "tip", true, now, limit, 0)
	now = now.Add(5 * time.Second)
	b.take(slotHome, "", true, now, limit, 0)
	if got := b.ledger.shown("tip"); got != 0 {
		t.Fatalf("a five-second flash counted %d showings", got)
	}
	// Re-deciding the same tip is nothing, and standing twenty seconds is one.
	b.take(slotHome, "tip", true, now, limit, 0)
	b.take(slotHome, "tip", true, now, limit, 0)
	now = now.Add(noticeReadTime)
	if b.settle(slotHome, now, limit); b.ledger.shown("tip") != 1 {
		t.Fatalf("a tip that stood %s counted %d showings, want 1", noticeReadTime, b.ledger.shown("tip"))
	}
	// A settled tip does not count again until it is seen again.
	now = now.Add(time.Minute)
	if b.settle(slotHome, now, limit); b.ledger.shown("tip") != 1 {
		t.Fatalf("a settled tip counted again: %d", b.ledger.shown("tip"))
	}
	// Decided while the row is out of sight: no standing until it is visible.
	b.take(slotHome, "", false, now, limit, 0)
	b.take(slotHome, "tip", false, now, limit, 0)
	now = now.Add(time.Hour)
	if b.settle(slotHome, now, limit); b.ledger.shown("tip") != 1 {
		t.Fatalf("a tip nobody could see counted: %d", b.ledger.shown("tip"))
	}
	b.visible(slotHome, now)
	now = now.Add(noticeReadTime)
	b.take(slotHome, "other", true, now, limit, 0)
	if got := b.ledger.shown("tip"); got != 2 {
		t.Fatalf("the tip counted %d showings after coming into view and standing, want 2", got)
	}
	if b.retired("tip") {
		t.Fatal("a second showing retired the notice")
	}
	// The next surface over the same ledger: the third showing is the last,
	// and the slot is cleared as the notice retires.
	next := newNoticeBoard("", "", true)
	next.ledger = b.ledger
	next.take(slotHome, "tip", true, now, limit, 0)
	now = now.Add(noticeReadTime)
	next.settle(slotHome, now, limit)
	if !next.retired("tip") {
		t.Fatal("the last allowed showing did not retire the notice")
	}
}

// Once retired in a session, a notice may not come back in it even while its
// arming rule is still true.
func TestARetiredNoticeNeverReturnsThisSession(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{{id: "tip", armed: true}}
	b.take(slotHint, "tip", true, time.Now(), fixedLimit(3), 0)
	b.retire("tip")
	if b.current[slotHint] != "" {
		t.Fatal("retiring did not clear the slot")
	}
	if got := b.pick(slotHint, cands, 0); got != "" {
		t.Fatalf("a retired notice came back: %q", got)
	}
}

// A LEDGER WRITTEN UNDER THE OLD COUNTING RULE IS FORGIVEN ONCE. The tips it
// spent on flashes come back, the ones a gesture retired stay retired, and the
// rule is written down so the next launch forgives nothing.
func TestTheLedgerForgivesWhatTheOldCountingRuleSpent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, noticeLedgerName)
	old := noticeLedger{Build: "abc", Seen: map[string]noticeMark{
		"spent-by-count":   {Shown: noticeShownDefault, Retired: "2026-09-22T12:00:00Z"},
		"used":             {Shown: 2, Retired: "2026-09-22T12:00:00Z"},
		"still-going":      {Shown: 4},
		"spent-and-beyond": {Shown: noticeShownDefault + 2, Retired: "2026-09-22T12:00:00Z"},
	}}
	if err := old.write(path); err != nil {
		t.Fatal(err)
	}
	b := newNoticeBoard(path, "abc", true)
	if b.retired("spent-by-count") || b.retired("spent-and-beyond") {
		t.Fatal("a tip the old rule spent was not forgiven")
	}
	if b.ledger.shown("spent-by-count") != 0 {
		t.Fatalf("a forgiven tip keeps %d showings", b.ledger.shown("spent-by-count"))
	}
	if !b.retired("used") {
		t.Fatal("a tip retired by its gesture was forgiven")
	}
	if b.ledger.shown("still-going") != 4 {
		t.Fatalf("a live tip's count changed to %d", b.ledger.shown("still-going"))
	}
	written := loadNoticeLedger(path)
	if written.Rule != noticeLedgerRule || written.retired("spent-by-count") {
		t.Fatalf("the forgiveness was not written down: %+v", written)
	}
	// And the next launch forgives nothing: a tip spent under the new rule
	// stays spent.
	b.ledger.Seen["spent-by-count"] = noticeMark{Shown: noticeShownDefault, Retired: "2026-09-22T13:00:00Z"}
	b.save()
	again := newNoticeBoard(path, "abc", true)
	if !again.retired("spent-by-count") {
		t.Fatal("a ledger already on the new rule was forgiven again")
	}
}

// THROUGH HOME: a tip that stood on home's row for a bounce is not a showing,
// one that stood twenty seconds is, and the ledger says so when home is left.
func TestABounceThroughHomeIsNotAShowingAndAStandIs(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.showPage(pageHome)
	first := a.notices.current[slotHome]
	if first == "" {
		t.Fatal("home opened with nothing on its row")
	}
	now = now.Add(3 * time.Second)
	a.closeHome()
	if got := a.notices.ledger.shown(first); got != 0 {
		t.Fatalf("a three-second bounce through home counted %d showings of %q", got, first)
	}
	a.showPage(pageHome)
	second := a.notices.current[slotHome]
	if second == "" || second == first {
		t.Fatalf("the second visit holds %q", second)
	}
	now = now.Add(noticeReadTime + time.Second)
	a.closeHome()
	if got := a.notices.ledger.shown(second); got != 1 {
		t.Fatalf("a tip that stood %s on home counted %d showings, want 1", noticeReadTime, got)
	}
	if got := a.notices.ledger.shown(first); got != 0 {
		t.Fatalf("the bounced tip was counted later: %d", got)
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

// THE SUITE'S FIXTURE TIP, and the moment that arms it. These tests are about
// a tip's whole road — armed by something that happens mid-session, drawn,
// retired by a different gesture — so they need a row with an event on both
// ends. It was `ctrl+. sees every task this project has run` until 2026-09-22,
// when the owner took that row off the table.
const deliverTip = "/files finds files codeaf wrote for you"

// makeDeliverable is an export landing on disk, as the loop sees it: the first
// thing written for the person, which is what arms [deliverTip].
func makeDeliverable(t *testing.T, a *app) {
	t.Helper()
	a.exportDone(exportedMsg{path: filepath.Join(t.TempDir(), "talk.md")})
}

// THE WHOLE ROAD. A hint arms on its moment, draws in the hint slot and only at
// the lowest rung there, retires on the gesture it teaches, and is still
// retired when the surface comes up again over the same profile.
func TestAHintArmsDrawsLowestRetiresAndStaysRetired(t *testing.T) {
	a, dir := sheetApp(t)
	if got := a.notices.current[slotHint]; got != "" {
		t.Fatalf("a fresh surface already holds hint %q", got)
	}
	if strings.Contains(plain(frame(a)), deliverTip) {
		t.Fatal("the files tip is up before anything has been written")
	}

	makeDeliverable(t, a)
	if got := a.notices.current[slotHint]; got != "files-after-first-deliverable" {
		t.Fatalf("an export landing armed %q", got)
	}
	// AND IT IS UP THE MOMENT IT ARMS, on the keys row at the foot: the
	// conversation's tip is on no clock (chattip_test.go holds the whole of
	// where it draws).
	if got := a.noticeHint(); got != deliverTip {
		t.Fatalf("the tip row reads %q, want the tip", got)
	}
	if got := plain(a.footHint(a.width)); !strings.Contains(got, deliverTip) {
		t.Fatalf("the keys row does not carry the tip: %q", got)
	}
	if !strings.Contains(plain(frame(a)), deliverTip) {
		t.Fatalf("the tip is not on the frame:\n%s", plain(frame(a)))
	}

	// OVER NOTHING THAT IS HAPPENING. A running turn outranks it, and so does a
	// box with words in it.
	a.state = stateWorking
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a tip drew over a running turn: %q", got)
	}
	// AND THE ROW SAYS THE ONE KEY THAT MATTERS WHILE A TURN IS RUNNING. esc is
	// the interrupt again (#1388), and no tip outranks it.
	if got := plain(a.footHint(a.width)); !strings.Contains(got, "esc interrupt") {
		t.Fatalf("a running turn's keys row does not offer the interrupt: %q", got)
	}
	a.state = stateIdle
	a.input.setText("half a sentence")
	if got := a.noticeHint(); got != "" {
		t.Fatalf("a tip drew over a box with words in it: %q", got)
	}
	a.input.reset()
	if got := a.noticeHint(); got != deliverTip {
		t.Fatalf("the tip did not come back over an empty box: %q", got)
	}

	// THE GESTURE RETIRES IT: /files actually reached for.
	a.slash("/files")
	if got := a.notices.current[slotHint]; got != "" {
		t.Fatalf("the slot still holds %q after the gesture", got)
	}
	if !a.notices.retired("files-after-first-deliverable") {
		t.Fatal("the gesture did not retire the hint")
	}
	if strings.Contains(plain(frame(a)), deliverTip) {
		t.Fatal("the tip is still drawn after its gesture")
	}
	// Re-arming does nothing this session either.
	makeDeliverable(t, a)
	if got := a.notices.current[slotHint]; got == "files-after-first-deliverable" {
		t.Fatal("a retired hint came back in the same session")
	}

	// And it is on disk, beside config.json, so the next surface knows.
	ledger := loadNoticeLedger(filepath.Join(dir, noticeLedgerName))
	if !ledger.retired("files-after-first-deliverable") {
		t.Fatalf("the ledger on disk does not have it retired: %+v", ledger)
	}
	again := noticeApp(t, dir)
	makeDeliverable(t, again)
	if got := again.notices.current[slotHint]; got == "files-after-first-deliverable" {
		t.Fatal("a retired hint came back after a restart")
	}
	if strings.Contains(plain(frame(again)), deliverTip) {
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
		eventAttached:     func(t *testing.T, a *app) { a.slash("/attach") },
		eventFolderPicked: func(t *testing.T, a *app) { a.slash("/folder") },
		// /project IS HOME'S ALONE (projectcmd.go), so its gesture is made
		// there — and with a real directory after it, which is the form that
		// takes a folder without opening anything.
		eventProjectSet: func(t *testing.T, a *app) {
			runCmd(a.showPage(pageHome))
			runCmd(a.homeSlash("/project " + t.TempDir()))
		},
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
		eventAutonomyAsked:    func(t *testing.T, a *app) { a.slash("/autonomy") },
		eventHomeGesture: func(t *testing.T, a *app) {
			// A door that can open a conversation is what opens home at all
			// ([app.homeDoorOpen]); the bare test app has none.
			a.open = func(string, string) (Conversation, error) { return Conversation{}, nil }
			drive(t, a, key(" "), key(" "))
			if !a.at(pageHome) {
				t.Fatal("two spaces did not open home")
			}
		},
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
	makeDeliverable(t, a)
	if got := a.noticeHint(); got != deliverTip {
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
	if got := a.noticeHint(); got == deliverTip {
		t.Fatal("a silenced slot still draws the tip")
	}
	// The next surface over this profile is quiet from the start.
	again := noticeApp(t, dir)
	makeDeliverable(t, again)
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
	makeDeliverable(t, a)
	if got := a.notices.current[slotHint]; got != "files-after-first-deliverable" {
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

// ── THE CONVERSATION'S RULE ─────────────────────────────────────────────────

// A RANKING, NOT A ROTATION: the first eligible row in the table's order takes
// the conversation's slot, the one already standing wins its own tie, and a row
// that stops being eligible stands down at once. This is the rule this surface
// shipped with, restored on 2026-09-22 after a build that put the conversation
// on home's rotation.
func TestTheConversationsSlotTakesTheFirstEligibleInTableOrder(t *testing.T) {
	b := freshBoard()
	cands := []noticeCandidate{
		{id: "first", armed: false},
		{id: "second", armed: true},
		{id: "third", armed: true},
	}
	if got := b.pick(slotHint, cands, 0); got != "second" {
		t.Fatalf("the slot picked %q, want the first eligible in table order", got)
	}
	b.take(slotHint, "second", false, time.Time{}, fixedLimit(6), 0)

	// A row ABOVE the one standing is the more urgent thing to say, and takes
	// the slot once the gap has passed — the table's order is the ranking.
	cands[0].armed = true
	if got := b.pick(slotHint, cands, noticeGap); got != "first" {
		t.Fatalf("a row above the one standing did not take the slot: %q", got)
	}
	// And the one standing yields at once when it stops being true, whatever
	// else is armed.
	cands[0].armed = false
	cands[1].armed = false
	if got := b.pick(slotHint, cands, noticeGap); got != "third" {
		t.Fatalf("a row that stopped being true did not yield: %q", got)
	}
	// With nothing eligible the row is empty.
	for i := range cands {
		cands[i].armed = false
	}
	if got := b.pick(slotHint, cands, noticeGap); got != "" {
		t.Fatalf("an empty list still says %q", got)
	}
}

// THE CONVERSATION'S SLOT CHANGES HANDS SLOWLY. A different line may take it
// only once [noticeGap] turns have passed, so three tips arming in three turns
// are read one at a time. The first occupant of the session waits on nothing,
// and a slot going empty never waits.
func TestTheConversationsSlotChangesHandsSlowly(t *testing.T) {
	b := freshBoard()
	first := []noticeCandidate{{id: "first", armed: true}}
	if got := b.pick(slotHint, first, 1); got != "first" {
		t.Fatalf("the first tip of the session waited: %q", got)
	}
	b.take(slotHint, "first", false, time.Time{}, fixedLimit(6), 1)

	both := []noticeCandidate{{id: "second", armed: true}, {id: "first", armed: true}}
	if got := b.pick(slotHint, both, 1+noticeGap-1); got != "first" {
		t.Fatalf("the slot changed hands inside the gap: %q", got)
	}
	if got := b.pick(slotHint, both, 1+noticeGap); got != "second" {
		t.Fatalf("the slot did not change hands after the gap: %q", got)
	}

	// Inside the gap, a standing tip that stopped being armed stands down at
	// once and the slot goes quiet rather than jumping to the next one.
	b = freshBoard()
	b.take(slotHint, "first", false, time.Time{}, fixedLimit(6), 1)
	gone := []noticeCandidate{{id: "second", armed: true}, {id: "first", armed: false}}
	if got := b.pick(slotHint, gone, 1); got != "" {
		t.Fatalf("a disarmed tip was replaced inside the gap: %q", got)
	}

	// Home's row is not on turns at all: it is on visits and a clock.
	b = freshBoard()
	b.take(slotHint, "first", false, time.Time{}, fixedLimit(6), 1)
	b.advance[slotHome] = true
	if got := b.pick(slotHome, both, 1); got != "second" {
		t.Fatalf("home's row waited on the conversation's gap: %q", got)
	}
}

// A TIP IS COUNTED ONCE PER SESSION ON THE CONVERSATION'S ROW, however many
// events re-decide the slot, and its last allowed showing retires it for the
// sessions after while leaving it up for this one.
func TestTheConversationsRowCountsOnceASession(t *testing.T) {
	b := freshBoard()
	limit := fixedLimit(2)
	b.take(slotHint, "tip", false, time.Time{}, limit, 1)
	b.take(slotHint, "", false, time.Time{}, limit, 2)
	b.take(slotHint, "tip", false, time.Time{}, limit, 3)
	if got := b.ledger.shown("tip"); got != 1 {
		t.Fatalf("one session counted %d showings", got)
	}
	if b.retired("tip") {
		t.Fatal("a first showing retired the notice")
	}

	// The next session: the second showing is the last allowed.
	next := newNoticeBoard("", "", true)
	next.ledger = b.ledger
	next.take(slotHint, "tip", false, time.Time{}, limit, 1)
	if !next.retired("tip") {
		t.Fatal("the last allowed showing did not retire the notice")
	}
	if next.current[slotHint] != "tip" {
		t.Fatal("the last allowed showing was not shown")
	}
}
