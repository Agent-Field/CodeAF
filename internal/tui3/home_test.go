package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// homeLab builds a projects root on disk — the same shape the launch door
// writes (cmd/aforge's chatv3_layout.go) — so that these tests exercise the
// real reader rather than a fixture handed to it.
type homeLab struct {
	t    *testing.T
	root string
}

func newHomeLab(t *testing.T) *homeLab {
	t.Helper()
	return &homeLab{t: t, root: t.TempDir()}
}

// project makes a bucket and answers its directory.
func (l *homeLab) project(bucket string) string {
	l.t.Helper()
	dir := filepath.Join(l.root, bucket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Fatal(err)
	}
	return dir
}

// session writes one session folder: a transcript, and the meta.json a picker
// reads instead of it. The transcript's contents do not matter to any assertion
// here — what the world reads off a folder is its identity file.
func (l *homeLab) session(bucket, id, title, workspace string, spoke time.Time) string {
	l.t.Helper()
	dir := filepath.Join(l.project(bucket), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"session","version":1,"id":"`+id+`"}`+"\n"), 0o600); err != nil {
		l.t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID:         id,
		Title:      title,
		Workspace:  workspace,
		LaunchDir:  workspace,
		Created:    spoke.Add(-time.Hour),
		LastUserAt: spoke,
	}); err != nil {
		l.t.Fatal(err)
	}
	return transcript
}

// presence writes one session's presence.json — what a live conversation says
// about itself. The shape is [session.SessionPresence]'s own, written here as
// the file rather than through the heartbeat, because these tests are about
// what a READER makes of a file it finds on disk.
func (l *homeLab) presence(bucket, id string, state session.PresenceState, reason string, at time.Time, out ...session.PresenceTask) {
	l.t.Helper()
	dir := filepath.Join(l.project(bucket), id)
	raw, err := json.Marshal(map[string]any{
		"schema":       1,
		"sessionId":    id,
		"workspace":    "/tmp/alpha",
		"pid":          4242,
		"updatedAt":    at.Format(time.RFC3339Nano),
		"state":        string(state),
		"reason":       reason,
		"runningTasks": out,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), append(raw, '\n'), 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// task appends one row to a bucket's index.
func (l *homeLab) task(bucket string, entry session.TaskIndexEntry) {
	l.t.Helper()
	path := filepath.Join(l.project(bucket), "tasks.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.t.Fatal(err)
	}
	defer file.Close()
	line, err := json.Marshal(entry)
	if err != nil {
		l.t.Fatal(err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		l.t.Fatal(err)
	}
}

// app builds a surface pointed at this lab, standing in the conversation whose
// transcript is given.
func (l *homeLab) app(standing string) *app {
	l.t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 24
	a.homeRoot = l.root
	a.file = standing
	a.resume = func(string) (Agent, error) { return &fakeAgent{model: "m"}, nil }
	return a
}

// launch drives the first frame the way [newApp] does: the welcome box is
// decided, then the landing over it, in that order and against this lab's
// projects rather than against the machine the suite is running on.
//
// It stops short of calling [newApp] itself for one reason: the root home reads
// is a field set after construction (home.go's [app.placesRoot]), so a real
// constructor here would walk the developer's own ~/.aforge before the test
// could point it anywhere. [TestALaunchThatNamedASessionIsNotGreeted] covers
// the one line this skips.
func (l *homeLab) launch(standing string, landing bool) *app {
	l.t.Helper()
	a := l.app(standing)
	a.landing = landing
	a.entries = nil
	a.welcome = welcome{}
	a.openWelcome()
	a.landHome()
	return a
}

// mustFrame is the whole screen, whatever is on it — home, or the conversation
// under it once home has gone.
func mustFrame(a *app) string {
	frame, _, _ := a.frame()
	return frame
}

// homeText is the frame as one string, with the paint stripped so an assertion
// is about words rather than about escape sequences.
func homeText(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	return ansi.Strip(strings.Join(lines, "\n"))
}

// homeNotes is what the conversation underneath was told, which is where a
// refusal that never opened the screen lands.
func homeNotes(a *app) string {
	var said []string
	for _, e := range a.entries {
		said = append(said, e.text)
	}
	return strings.Join(said, "\n")
}

func TestHomeListsEveryProjectAndItsConversations(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now.Add(-2*time.Minute))
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-3*time.Hour))

	a := lab.app(mine)
	a.openHome()
	if !a.home.open {
		t.Fatal("/home did not open")
	}
	text := homeText(a)
	for _, want := range []string{"home", "alpha", "beta", "Porting the Resume Picker", "Pricing Research"} {
		if !strings.Contains(text, want) {
			t.Fatalf("home does not mention %q:\n%s", want, text)
		}
	}
}

// THE FRAME IS THE FRAME. A fullscreen surface that answered with fewer rows
// than it was asked for would leave the conversation showing underneath it.
func TestHomeTakesExactlyTheWholeFrame(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	for _, size := range [][2]int{{100, 24}, {60, 12}, {44, 8}, {120, 40}} {
		a.width, a.height = size[0], size[1]
		width, height := a.size()
		lines, hits, _, _ := a.homeFrame(width, height)
		if len(lines) != height {
			t.Fatalf("at %dx%d home drew %d rows, want %d", size[0], size[1], len(lines), height)
		}
		if len(hits) != len(lines) {
			t.Fatalf("at %dx%d home answered %d hits for %d rows", size[0], size[1], len(hits), len(lines))
		}
	}
}

func TestHomeEscGoesBackToTheConversation(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("esc did not close home")
	}
}

// esc peels one layer: a box with something in it is cleared before the screen
// is left.
func TestHomeEscClearsTheBoxBeforeItLeaves(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	a.homeKey(key("@"))
	a.homeKey(key("x"))
	a.homeKey(key("esc"))
	if !a.home.open {
		t.Fatal("the first esc left home instead of clearing the box")
	}
	if !a.home.box.empty() {
		t.Fatalf("the box still holds %q", a.home.box.String())
	}
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("the second esc did not close home")
	}
}

// THE EMPTINESS LAW. A conversation that ran nothing and spent nothing says
// nothing about either.
func TestHomeSaysNothingAboutNoTasksAndNoSpend(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "a quiet chat", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	for _, banned := range []string{"0 tasks", "$0.00", "0 running", "spent $0"} {
		if strings.Contains(text, banned) {
			t.Fatalf("home drew %q, which is the absence of a fact:\n%s", banned, text)
		}
	}
}

// A `running` row is a row the file wrote when the work started. Nobody is
// holding this conversation, so nothing is running in it.
func TestHomeWillNotCallAStaleRowRunning(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the long one", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000001",
	})

	a := lab.app(mine)
	a.openHome()
	row := a.home.focused()
	if row.Tasks.Running != 0 {
		t.Fatalf("home called %d rows running in a conversation nobody is holding", row.Tasks.Running)
	}
	if row.Tasks.Incomplete != 1 {
		t.Fatalf("home counted %d incomplete, want 1", row.Tasks.Incomplete)
	}
	text := homeText(a)
	if !strings.Contains(text, "incomplete") {
		t.Fatalf("home does not say the work is incomplete:\n%s", text)
	}
	if strings.Contains(text, "1 running") {
		t.Fatalf("home drew a stale row as running:\n%s", text)
	}
}

// A session that says it is alive AND names the node it has out is the only
// case in which home will draw the word `running`.
func TestHomeCallsARowRunningWhenTheSessionSaysItHasThatNodeOut(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// The running one is a SECOND window's conversation, which is the case this
	// screen exists for — this window cannot see that turn any other way.
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now.Add(-2*time.Hour))
	lab.session("-tmp-alpha", "aaaa000000000002", "the long one", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "7", Title: "Port the thing", State: "running", StartedAt: now.Add(-time.Minute)})

	a := lab.app(mine)
	a.openHome()
	// The cursor opens on the conversation this window is in, so the running one
	// — which is the SECOND window's — is stepped onto here.
	a.home.point(filepath.Join(lab.project("-tmp-alpha"), "aaaa000000000002", "transcript.jsonl"))
	row := a.home.focused()
	if !row.Live {
		t.Fatal("a session that refreshed its presence a moment ago is not live")
	}
	if row.Tasks.Running != 1 || row.Tasks.Incomplete != 0 {
		t.Fatalf("rolled up %d running / %d incomplete, want 1 / 0", row.Tasks.Running, row.Tasks.Incomplete)
	}
	text := homeText(a)
	for _, want := range []string{"1 running", "running Port the thing", "open in another window · working"} {
		if !strings.Contains(text, want) {
			t.Fatalf("home does not say %q:\n%s", want, text)
		}
	}
}

// A live session that does NOT name the node is the case the presence file was
// built for: the work is over or was abandoned, whatever the index still says.
func TestHomeCallsARowIncompleteWhenTheLiveSessionDoesNotNameIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the long one", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000001",
	})
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceIdle, "", now)

	a := lab.app(mine)
	a.openHome()
	row := a.home.focused()
	if row.Tasks.Running != 0 || row.Tasks.Incomplete != 1 {
		t.Fatalf("rolled up %d running / %d incomplete, want 0 / 1", row.Tasks.Running, row.Tasks.Incomplete)
	}
	if !strings.Contains(homeText(a), "incomplete") {
		t.Fatalf("home does not say the work is incomplete:\n%s", homeText(a))
	}
}

// THE AGE IS THE WHOLE OF THE CLAIM. A presence file nobody has refreshed is
// not believed, and the row falls back to what it would have said without one.
func TestHomeDoesNotBelieveAStalePresence(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the long one", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000001",
	})
	// A minute old is four times the window a reader believes.
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWorking, "", now.Add(-time.Minute),
		session.PresenceTask{ID: "7", Title: "Port the thing", State: "running"})

	a := lab.app(mine)
	a.openHome()
	row := a.home.focused()
	if row.Live {
		t.Fatal("home believed a presence nobody had refreshed for a minute")
	}
	if row.Tasks.Running != 0 || row.Tasks.Incomplete != 1 {
		t.Fatalf("rolled up %d running / %d incomplete, want 0 / 1", row.Tasks.Running, row.Tasks.Incomplete)
	}
	if strings.Contains(homeText(a), "working") {
		t.Fatalf("home drew a dead window as working:\n%s", homeText(a))
	}
}

// The most valuable row on the screen: a session stopped on a question wears
// the triangle, says so, sorts above everything, and shows what it is stuck on.
func TestHomePutsASessionThatNeedsYouFirst(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// The one that needs somebody is the OLDEST, so recency alone would sink it.
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "middle of the road", "/tmp/alpha", now.Add(-time.Hour))
	lab.session("-tmp-alpha", "aaaa000000000003", "pricing research", "/tmp/alpha", now.Add(-6*time.Hour))
	lab.presence("-tmp-alpha", "aaaa000000000003", session.PresenceWaiting, "can I run: rm -rf build/", now)

	a := lab.app(mine)
	a.openHome()

	world := a.home.world
	if len(world.Projects) != 1 {
		t.Fatalf("read %d projects, want 1", len(world.Projects))
	}
	first := world.Projects[0].Sessions[0]
	if !first.NeedsPerson() {
		t.Fatalf("the first row is %q, which is not the one waiting on somebody", first.Title)
	}
	if world.Projects[0].NeedsPerson() != 1 {
		t.Fatalf("the project counted %d rows needing somebody, want 1", world.Projects[0].NeedsPerson())
	}
	if first.Reason() != "can I run: rm -rf build/" {
		t.Fatalf("the row is stopped on %q", first.Reason())
	}

	text := homeText(a)
	if !strings.Contains(text, homeAskGlyph+" Pricing Research") {
		t.Fatalf("the row does not wear the triangle:\n%s", text)
	}
	if !strings.Contains(text, string(session.PresenceWaiting)) {
		t.Fatalf("the row does not say it is waiting on you:\n%s", text)
	}
	// The cursor opens on the conversation THIS window is in, so the question is
	// one keystroke up rather than already on screen.
	a.home.point(first.Transcript)
	if detail := homeText(a); !strings.Contains(detail, "can I run: rm -rf build/") {
		t.Fatalf("the detail does not show what it is stopped on:\n%s", detail)
	}
	// And it really is the first ROW of the column — asserted on the lines the
	// left column is built from rather than on where the words land in the
	// frame, because the detail pane repeats the focused conversation's name and
	// a search over the whole screen would find that copy first.
	var order []string
	for _, line := range a.home.lines {
		if line.kind == homeSession {
			order = append(order, homeName(line.row))
		}
	}
	if len(order) != 3 || order[0] != "Pricing Research" {
		t.Fatalf("the column reads %v, want the waiting one first", order)
	}
}

// A session that has gone quiet stops asking. Nothing on home may keep somebody
// on the hook for a window that is not there any more.
func TestHomeStopsSayingNeedsYouWhenTheWindowIsGone(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000003", "pricing research", "/tmp/alpha", now.Add(-6*time.Hour))
	lab.presence("-tmp-alpha", "aaaa000000000003", session.PresenceWaiting, "can I run: rm -rf build/", now.Add(-time.Minute))

	a := lab.app(mine)
	a.openHome()
	for _, project := range a.home.world.Projects {
		for _, row := range project.Sessions {
			if row.NeedsPerson() {
				t.Fatalf("%q still claims to need somebody an hour after its window went", row.Title)
			}
		}
	}
	if strings.Contains(homeText(a), string(session.PresenceWaiting)) {
		t.Fatalf("home is still asking for a window that is gone:\n%s", homeText(a))
	}
}

func TestHomeCollapsesTheQuietTailOfAProject(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	var mine string
	for i := 0; i < homeShown+3; i++ {
		file := lab.session("-tmp-alpha", "aaaa00000000000"+string(rune('1'+i)),
			"chat "+string(rune('a'+i)), "/tmp/alpha", now.Add(-time.Duration(i)*time.Hour))
		if i == 0 {
			mine = file
		}
	}
	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if !strings.Contains(text, "…3 more") {
		t.Fatalf("home did not whisper the quiet tail:\n%s", text)
	}
}

// ── the omnibox ─────────────────────────────────────────────────────────────

// TYPING DOES BOTH JOBS AT ONCE. The characters are a new conversation waiting
// to be sent AND a live query over the machine, and the cursor stays on the
// action row so that type-and-enter means exactly what it always meant.
func TestTypingFiltersLiveWhileTheActionRowStaysTheDefault(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	text := homeText(a)
	if !strings.Contains(text, "Pricing Research") {
		t.Fatalf("the query lost the conversation it should have found:\n%s", text)
	}
	if strings.Contains(text, "Porting the Resume Picker") {
		t.Fatalf("the query kept a conversation that does not match:\n%s", text)
	}
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeAction {
		t.Fatalf("the cursor left the action row while typing (kind %v)", line.kind)
	}
	if !strings.Contains(text, homeStartWord+`: "pricing"`) {
		t.Fatalf("the action row does not say what enter will do:\n%s", text)
	}
}

// …and enter therefore still starts a conversation, with matches on screen.
func TestEnterStillStartsAChatWithMatchesOnScreen(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	a := lab.app(mine)
	next := &fakeAgent{model: "m"}
	a.fresh = func() (Agent, string, error) { return next, "/tmp/alpha/next/transcript.jsonl", nil }
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	if !strings.Contains(homeText(a), "Pricing Research") {
		t.Fatal("the query matched nothing, so this proves nothing")
	}
	runCmd(a.homeEnter())
	if a.home.open {
		t.Fatal("enter on the action row left home open")
	}
	if len(next.sent) != 1 || next.sent[0] != "pricing" {
		t.Fatalf("the new conversation was sent %v", next.sent)
	}
}

// One ↓ is the decision to pick from the list instead, and it sticks.
func TestWalkingOffTheActionRowPicksFromTheList(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	a := lab.app(mine)
	a.openHome()
	for _, r := range "pric" {
		a.homeKey(key(string(r)))
	}
	a.homeKey(key("down"))
	if row := a.home.focused(); row.Transcript != mine {
		t.Fatal("↓ did not land on the match")
	}
	a.homeKey(key("i"))
	if row := a.home.focused(); row.Transcript != mine {
		t.Fatal("typing after ↓ threw the cursor back to the action row")
	}
}

// A FILTER THAT CANNOT SEE WHAT IT HIDES IS A FILTER LYING ABOUT THE MACHINE.
func TestAMatchBehindTheCollapseIsFoundAnyway(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest one", "/tmp/alpha", now)
	for i := 0; i < homeShown+3; i++ {
		lab.session("-tmp-alpha", "bbbb00000000000"+string(rune('a'+i)),
			"filler "+string(rune('a'+i)), "/tmp/alpha", now.Add(-time.Duration(i+1)*time.Hour))
	}
	lab.session("-tmp-alpha", "cccc000000000001", "buried treasure", "/tmp/alpha", now.Add(-40*time.Hour))

	a := lab.app(mine)
	a.openHome()
	if !strings.Contains(homeText(a), "more") {
		t.Fatal("nothing was collapsed, so this proves nothing")
	}
	if strings.Contains(homeText(a), "Buried Treasure") {
		t.Fatal("the row was not behind the collapse to begin with")
	}
	for _, r := range "treasure" {
		a.homeKey(key(string(r)))
	}
	if !strings.Contains(homeText(a), "Buried Treasure") {
		t.Fatalf("the query could not see behind the collapse:\n%s", homeText(a))
	}
	a.homeKey(key("ctrl+u"))
	if !strings.Contains(homeText(a), "more, quiet since") {
		t.Fatalf("the collapse did not come back on an empty query:\n%s", homeText(a))
	}
	if strings.Contains(homeText(a), "Buried Treasure") {
		t.Fatal("the row stayed out after the query was cleared")
	}
}

// The tail line is a door: enter and → open it, ← folds it back, a click toggles.
func TestTheCollapseLineOpensAndFolds(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest one", "/tmp/alpha", now)
	for i := 0; i < homeShown+3; i++ {
		lab.session("-tmp-alpha", "bbbb00000000000"+string(rune('a'+i)),
			"filler "+string(rune('a'+i)), "/tmp/alpha", now.Add(-time.Duration(i+1)*time.Hour))
	}
	a := lab.app(mine)
	a.openHome()

	quiet := -1
	for at, line := range a.home.lines {
		if line.kind == homeQuiet {
			quiet = at
		}
	}
	if quiet < 0 {
		t.Fatal("no tail line to open")
	}
	a.home.cursor = quiet
	a.homeKey(key("enter"))
	if !a.home.expanded[a.home.lines[a.home.cursor].dir] {
		t.Fatal("enter did not open the project")
	}
	if text := homeText(a); !strings.Contains(text, "fewer") || strings.Contains(text, "more") {
		t.Fatalf("the tail does not offer to fold the rows back:\n%s", text)
	}
	if !strings.Contains(homeText(a), "Filler G") {
		t.Fatalf("opening the project did not draw the rows behind it:\n%s", homeText(a))
	}
	if line, _ := a.home.focusedLine(); line.kind != homeQuiet {
		t.Fatal("the cursor left the line that did the opening")
	}

	a.homeKey(key("left"))
	if len(a.home.expanded) != 0 {
		t.Fatal("← did not fold the project back")
	}
	a.homeKey(key("right"))
	if len(a.home.expanded) != 1 {
		t.Fatal("→ did not open it again")
	}

	a.width, a.height = 100, 30
	_, hits, _, _ := a.homeFrame(a.width, a.height)
	row := -1
	for y, at := range hits {
		if at == a.home.cursor {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("the tail line is not on screen")
	}
	a.homePress(4, row)
	if len(a.home.expanded) != 0 {
		t.Fatal("a click did not fold the project")
	}
}

// The outcome sentence is the most informative text the index holds, and it is
// searched — the closest thing to recalling something by what happened.
func TestAQueryMatchesWhatATaskCameTo(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "tuesday", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "wednesday", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "n", Label: "A nondescript job", Title: "A nondescript job",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		Outcome: "Rewrote the postgres connection pool and the flakes stopped.",
	})
	a := lab.app(mine)
	a.openHome()
	for _, r := range "postgres" {
		a.homeKey(key(string(r)))
	}
	text := homeText(a)
	if !strings.Contains(text, "Wednesday") {
		t.Fatalf("a query over what the work came to found nothing:\n%s", text)
	}
	if strings.Contains(text, "Tuesday") {
		t.Fatalf("it matched a conversation with no such outcome:\n%s", text)
	}
	a.homeKey(key("down"))
	if !strings.Contains(homeText(a), "Rewrote the postgres") {
		t.Fatalf("the pane does not show what the work came to:\n%s", homeText(a))
	}
}

// AT THE SAME MATCH QUALITY, THE ROW THAT WANTS SOMEBODY WINS — and no amount
// of the other being newer can change that, because the boost is larger than
// the whole recency range.
func TestNeedsYouOutranksAColdRowItTiesWith(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000009", "somewhere else", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000001", "auth work", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "auth work", "/tmp/alpha", now.Add(-31*24*time.Hour))
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWaiting, "which branch?", now)

	a := lab.app(mine)
	a.openHome()
	for _, r := range "auth" {
		a.homeKey(key(string(r)))
	}
	var order []session.SessionRow
	for _, line := range a.home.lines {
		if line.kind == homeSession {
			order = append(order, line.row)
		}
	}
	if len(order) != 2 {
		t.Fatalf("expected two matches, got %d", len(order))
	}
	if !order[0].NeedsPerson() {
		t.Fatal("the newer cold row outranked the one waiting on somebody")
	}
	// The boost is smaller than one rung at even the WEAKEST field, so it can
	// never override a better match.
	if rung := (session.MatchWord - session.MatchPrefix) * homeFieldOutcome; homeBoostNeedsYou >= rung {
		t.Fatalf("the needs-you boost (%d) is big enough to beat a better match (%d)", homeBoostNeedsYou, rung)
	}
	// And larger than the whole recency range, which is what the order above
	// actually turns on.
	if homeBoostNeedsYou <= homeRecencyBoost {
		t.Fatalf("the needs-you boost (%d) can be outweighed by recency (%d)", homeBoostNeedsYou, homeRecencyBoost)
	}
}

// esc peels one layer at a time.
func TestEscPeelsTheQueryThenCloses(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	for _, r := range "abc" {
		a.homeKey(key(string(r)))
	}
	a.homeKey(key("esc"))
	if !a.home.open {
		t.Fatal("the first esc left home instead of clearing the query")
	}
	if !a.home.box.empty() {
		t.Fatalf("the box still holds %q", a.home.box.String())
	}
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("the second esc did not close home")
	}
}

// ── the two columns ─────────────────────────────────────────────────────────

// THE RIGHT PANE'S EDGE IS A STRAIGHT LINE. It is the only thing separating the
// two columns — this surface draws no borders — so it has to be findable on
// every row without looking for it.
func TestTheGutterIsAStraightLine(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "a short one", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "a conversation with a considerably longer name than that", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "n", Label: "Something", Title: "Something",
		Status: string(session.TaskDone), Cost: 1, Tokens: 100, SessionID: "aaaa000000000001",
		EndedAt: now.Add(-time.Minute), Outcome: "It worked.",
	})
	a := lab.app(mine)
	a.openHome()
	for _, width := range []int{100, 84, 120} {
		a.width, a.height = width, 24
		left, right := homeColumns(width)
		if right == 0 {
			t.Fatalf("width %d dropped the detail column, so there is no gutter to test", width)
		}
		lines, _, _, _ := a.homeFrame(width, a.height)
		// The head is four lines and the foot three; between them is the body,
		// which is the only part that has two columns in it.
		for i := 4; i < len(lines)-3; i++ {
			plain := []rune(ansi.Strip(lines[i]))
			if len(plain) <= left+homeGutter {
				continue
			}
			gutter := string(plain[left : left+homeGutter])
			if strings.TrimSpace(gutter) != "" {
				t.Fatalf("at width %d row %d puts %q in the gutter:\n%s",
					width, i, gutter, ansi.Strip(strings.Join(lines, "\n")))
			}
		}
	}
}

// The facts footer is the emptiness law at its most literal.
func TestTheFactsFooterOmitsWhatIsNotAFact(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	quiet := lab.session("-tmp-alpha", "aaaa000000000001", "just talking", "/tmp/alpha", now.Add(-2*time.Hour))
	a := lab.app(quiet)
	a.openHome()
	text := homeText(a)
	for _, banned := range []string{"spent $0", "0 tokens", "$0.00"} {
		if strings.Contains(text, banned) {
			t.Fatalf("the footer drew %q:\n%s", banned, text)
		}
	}
	if !strings.Contains(text, "last active 2h") {
		t.Fatalf("the footer lost the one fact it had:\n%s", text)
	}

	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "n", Label: "Something", Title: "Something",
		Status: string(session.TaskDone), Cost: 1.25, Tokens: 34000,
		SessionID: "aaaa000000000001", EndedAt: now.Add(-time.Minute),
	})
	a = lab.app(quiet)
	a.openHome()
	if got := homeText(a); !strings.Contains(got, "spent $1.25 · 34k tokens · last active 1m") {
		t.Fatalf("the footer does not read as one line of facts:\n%s", got)
	}
}

// A short frame drops bands from the bottom and never takes the title.
func TestTheTitleBandSurvivesAShortFrame(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the focused one", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "n", Label: "Something", Title: "Something",
		Status: string(session.TaskDone), Cost: 3, Tokens: 90000,
		SessionID: "aaaa000000000001", EndedAt: now.Add(-time.Minute),
		Outcome: "A sentence about what it came to.",
	})
	lab.session("-tmp-alpha", "aaaa000000000002", "another", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, height := range []int{24, 16, 12, 10, 9} {
		a.width, a.height = 100, height
		text := homeText(a)
		if !strings.Contains(text, "The Focused One") {
			t.Fatalf("at height %d the pane lost its title:\n%s", height, text)
		}
	}
	a.width, a.height = 100, 9
	if strings.Contains(homeText(a), "spent $3.00") {
		t.Fatalf("a short frame kept the footer instead of dropping it:\n%s", homeText(a))
	}
}

// Enter opens a conversation of THIS window's project, and says where to go for
// one that is somewhere else — it never half-opens it.
func TestHomeOpensThisProjectAndNamesWhereTheOthersLive(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	sibling := lab.session("-tmp-alpha", "aaaa000000000002", "the other alpha chat", "/tmp/alpha", now.Add(-time.Minute))
	lab.session("-tmp-beta", "bbbb000000000001", "somebody else's project", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	a.home.point(sibling)
	a.homeEnter()
	if a.home.open {
		t.Fatal("opening a conversation left home on the screen")
	}
	if a.file != sibling {
		t.Fatalf("home opened %q, want %q", a.file, sibling)
	}

	a = lab.app(mine)
	a.openHome()
	for at, line := range a.home.lines {
		if line.kind == homeSession && strings.Contains(line.dir, "beta") {
			a.home.cursor = at
		}
	}
	a.homeEnter()
	if a.file != mine {
		t.Fatalf("enter on another project's conversation opened %q", a.file)
	}
	if !a.home.open {
		t.Fatal("a refused open closed home")
	}
	if !strings.Contains(a.home.msg, homeElsewhereWord) || !strings.Contains(a.home.msg, "/tmp/beta") {
		t.Fatalf("home said %q, which does not name where to go", a.home.msg)
	}
	if !strings.Contains(homeText(a), homeElsewhereWord) {
		t.Fatalf("the row does not say it is elsewhere:\n%s", homeText(a))
	}
}

// Typing anything that is not a search is the start of a new conversation.
func TestHomeTypingStartsANewConversationAndSendsIt(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	next := &fakeAgent{model: "m"}
	a.fresh = func() (Agent, string, error) { return next, "/tmp/alpha/next/transcript.jsonl", nil }
	a.openHome()
	for _, r := range "plan a trip" {
		a.homeKey(key(string(r)))
	}
	cmd := a.homeEnter()
	if cmd == nil {
		t.Fatal("enter on a typed sentence did no work")
	}
	// The submit is a command, because talking to a session talks to a lock and
	// possibly a provider — so it is run here the way the loop would run it.
	// [runCmd] walks into the batch rather than stopping at the message that
	// stands for one.
	runCmd(cmd)
	if a.home.open {
		t.Fatal("starting a conversation left home on the screen")
	}
	if a.agent != Agent(next) {
		t.Fatal("the sentence did not land in a fresh conversation")
	}
	if len(next.sent) != 1 || next.sent[0] != "plan a trip" {
		t.Fatalf("the new conversation was sent %v", next.sent)
	}
}

// The screen is a reading of the disk, so a conversation somebody had in
// another window shows up on the next tick.
func TestHomeRescanPicksUpAConversationFromAnotherWindow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	a := lab.app(mine)
	a.openHome()
	if strings.Contains(homeText(a), "Arrived Later") {
		t.Fatal("the conversation was there before it was written")
	}
	lab.session("-tmp-alpha", "aaaa000000000002", "arrived later", "/tmp/alpha", now.Add(-time.Minute))
	a.refreshHome()
	if !strings.Contains(homeText(a), "Arrived Later") {
		t.Fatalf("the rescan missed a new conversation:\n%s", homeText(a))
	}
	if a.home.focused().Transcript != mine {
		t.Fatal("the rescan moved the cursor off the conversation it was on")
	}
}

// Home rides its own clock, and it stops when the screen closes.
func TestHomeBeatStopsWhenHomeCloses(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	if cmd := a.openHome(); cmd == nil {
		t.Fatal("opening home started no clock")
	}
	if cmd := a.homeBeat(); cmd == nil {
		t.Fatal("a beat on an open home did not ask for the next one")
	}
	a.closeHome()
	if cmd := a.homeBeat(); cmd != nil {
		t.Fatal("a beat kept the clock turning after home closed")
	}
}

// ── the landing ─────────────────────────────────────────────────────────────

// A person opening aforge on a machine they have worked on is greeted by home,
// with the conversation the door picked loaded underneath it.
func TestHomeIsTheFirstFrameOfAnOrdinaryLaunch(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one the door picked", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "yesterday's chat", "/tmp/alpha", now.Add(-20*time.Hour))

	a := lab.launch(mine, true)
	if !a.home.open {
		t.Fatal("a bare launch did not open on home")
	}
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), "esc close") {
		t.Fatalf("the first frame is not home:\n%s", ansi.Strip(frame))
	}
	// AND THE CURSOR IS ON THE CONVERSATION THAT IS LOADED UNDERNEATH, so the
	// cheapest keystroke on the screen is the calm one.
	if got := a.home.focused().Transcript; got != mine {
		t.Fatalf("the cursor opened on %q, want the conversation this window is in", got)
	}
}

// THE EMPTINESS LAW, APPLIED TO A WHOLE SURFACE. A machine whose only
// conversation is the one this launch opened has nothing home could say.
func TestAFirstRunGoesStraightToTheChat(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the only one", "/tmp/alpha", time.Now())

	a := lab.launch(mine, true)
	if a.home.open {
		t.Fatal("home greeted a machine with nowhere else to go")
	}
	// And the welcome box is untouched: a first run gets the greeting it always
	// got.
	if !a.welcome.open {
		t.Fatal("the welcome box did not open on a launch home stayed out of")
	}
}

// A machine with no conversations at all is the same case one step earlier.
func TestAnEmptyMachineGoesStraightToTheChat(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.launch("", true)
	if a.home.open {
		t.Fatal("home greeted a machine with nothing on it")
	}
}

// Naming a conversation means that conversation. The door does not set Landing
// for --session or for the picker, and the surface does not second-guess it.
func TestALaunchThatNamedASessionIsNotGreeted(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I named", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "some other chat", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.launch(mine, false)
	if a.home.open {
		t.Fatal("home greeted a launch that named its conversation")
	}

	// And `aforge resume` is already greeting them with its picker.
	a = lab.app(mine)
	a.landing, a.pickSession = true, true
	a.landHome()
	if a.home.open {
		t.Fatal("home opened behind the resume picker — a launch gets one greeting")
	}
}

// Two greeters is one too many: home lists every conversation the box would
// have, so the box retires without drawing and never comes back.
func TestTheWelcomeBoxRetiresWhenHomeLands(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one the door picked", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "yesterday's chat", "/tmp/alpha", now.Add(-20*time.Hour))

	a := lab.launch(mine, true)
	if a.welcome.open {
		t.Fatal("the welcome box is open underneath home")
	}
	if !a.welcome.spent {
		t.Fatal("the welcome box was hidden rather than retired, so it can come back")
	}
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("esc did not leave home")
	}
	if a.welcome.open {
		t.Fatalf("the welcome box appeared after home closed:\n%s", ansi.Strip(mustFrame(a)))
	}
	if strings.Contains(ansi.Strip(mustFrame(a)), "recent sessions") {
		t.Fatalf("the box drew itself behind home:\n%s", ansi.Strip(mustFrame(a)))
	}
}

// esc drops into the conversation that was loaded underneath all along.
func TestEscFromTheLandingLandsInTheSession(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one the door picked", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "yesterday's chat", "/tmp/alpha", now.Add(-20*time.Hour))

	a := lab.launch(mine, true)
	a.homeKey(key("esc"))
	if a.home.open {
		t.Fatal("esc did not close the landing")
	}
	if a.file != mine {
		t.Fatalf("esc changed the conversation to %q", a.file)
	}
}

// enter on the row the window is already in is the same door, and it says
// nothing on the way through: the conversation is what happens next.
func TestEnterOnTheRowYouAreInJustStepsIntoIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one the door picked", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "yesterday's chat", "/tmp/alpha", now.Add(-20*time.Hour))

	a := lab.launch(mine, true)
	before := len(a.entries)
	a.homeEnter()
	if a.home.open {
		t.Fatal("enter on the conversation this window is in did not close home")
	}
	if a.file != mine {
		t.Fatalf("enter reopened %q instead of stepping into the one already loaded", a.file)
	}
	if len(a.entries) != before {
		t.Fatalf("enter narrated the door it walked through: %v", a.entries[before:])
	}
}

func TestHomeSaysNothingOnAMachineWithNoProjects(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.openHome()
	if !strings.Contains(homeText(a), homeEmptyWord) {
		t.Fatalf("an empty machine does not say so:\n%s", homeText(a))
	}
}

// Over --host the projects under this process's state root belong to the wrong
// machine, so the screen refuses rather than drawing a confident lie.
func TestHomeRefusesOverHost(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.host = "box"
	a.openHome()
	if a.home.open {
		t.Fatal("home opened over --host")
	}
	if !strings.Contains(homeNotes(a), homeRemoteWord) {
		t.Fatalf("home did not say why it refused:\n%s", homeNotes(a))
	}
}

// The world's own reader, asked directly: a bucket's index is grouped by the
// conversation that ran each row, and a project is named from what its
// conversations recorded rather than from the encoded directory.
func TestReadWorldNamesProjectsFromWhatTheSessionsRecorded(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "a", Label: "A", Title: "A", Status: string(session.TaskDone),
		Cost: 0.5, SessionID: "aaaa000000000001", EndedAt: now.Add(-time.Minute),
	})
	world := session.ReadWorld(lab.root)
	if len(world.Projects) != 1 {
		t.Fatalf("read %d projects, want 1", len(world.Projects))
	}
	project := world.Projects[0]
	if project.Name != "alpha" {
		t.Fatalf("project is called %q, want %q", project.Name, "alpha")
	}
	if len(project.Sessions) != 1 {
		t.Fatalf("read %d conversations, want 1", len(project.Sessions))
	}
	rollup := project.Sessions[0].Tasks
	if rollup.Done != 1 || rollup.Total() != 1 {
		t.Fatalf("rolled up %+v", rollup)
	}
	if rollup.Spend != 0.5 {
		t.Fatalf("spend is %v, want 0.5", rollup.Spend)
	}
}

// A folder nobody ever spoke in is the shell a launch mints and the groom
// reuses; it is not a conversation and does not draw a row.
func TestReadWorldSkipsAFolderNobodySpokeIn(t *testing.T) {
	lab := newHomeLab(t)
	dir := filepath.Join(lab.project("-tmp-alpha"), "aaaa000000000001")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: "aaaa000000000001", Workspace: "/tmp/alpha", Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if world := session.ReadWorld(lab.root); len(world.Projects) != 0 {
		t.Fatalf("read %d projects from a bucket holding nothing anybody said", len(world.Projects))
	}
}

func TestReadWorldOnAMissingRootIsAnEmptyWorld(t *testing.T) {
	if world := session.ReadWorld(filepath.Join(t.TempDir(), "never")); len(world.Projects) != 0 {
		t.Fatalf("read %d projects off a root that is not there", len(world.Projects))
	}
}

// ── the door home from inside a conversation ────────────────────────────────

// doorLab is a surface sitting in a conversation with somewhere else to go, so
// the door is open. It is [homeLab.app] plus the one cached fact the door reads.
func (l *homeLab) door(standing string) *app {
	l.t.Helper()
	a := l.app(standing)
	a.landHome()
	return a
}

// TWO SPACES IN AN EMPTY BOX GO HOME.
func TestDoubleSpaceInAnEmptyBoxGoesHome(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut on a machine with somewhere to go")
	}
	a.key(key(" "))
	if got := a.input.String(); got != " " {
		t.Fatalf("the first space did not type itself: %q", got)
	}
	if a.home.open {
		t.Fatal("one space opened home")
	}
	a.key(key(" "))
	if !a.home.open {
		t.Fatal("two spaces did not open home")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the gesture left %q behind in the box", got)
	}
}

// …AND IT CANNOT EAT A SPACE SOMEBODY WANTED. The first one types itself and
// stays typed unless the very next key is another space.
func TestASingleSpaceThenALetterTypesNormally(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.key(key(" "))
	a.key(key("x"))
	if got := a.input.String(); got != " x" {
		t.Fatalf("the box holds %q, want %q", got, " x")
	}
	if a.home.open {
		t.Fatal("typing a space and a letter opened home")
	}
	// And a space in a box that already has words in it is just a space.
	a.key(key(" "))
	a.key(key(" "))
	if a.home.open {
		t.Fatal("the gesture fired in a box that had text in it")
	}
	if got := a.input.String(); got != " x  " {
		t.Fatalf("the box holds %q", got)
	}
}

// A PASTE IS NOT A GESTURE. Pasted text arrives as its own message and never
// reaches the key router, so two leading spaces in pasted text are two spaces.
func TestAPasteThatStartsWithTwoSpacesDoesNotGoHome(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	runCmd(a.paste("  indented like code"))
	if a.home.open {
		t.Fatal("a paste beginning with two spaces opened home")
	}
	if got := a.input.String(); got != "  indented like code" {
		t.Fatalf("the paste landed as %q", got)
	}
}

// The door is not offered where there is nowhere to go, and the gesture is
// inert there too — a door that is drawn is a door that works.
func TestTheDoorIsShutWhenThereIsNowhereToGo(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the only one", "/tmp/alpha", time.Now())
	a := lab.door(mine)
	if a.homeDoorOpen() || a.homeDoorShowing() {
		t.Fatal("the door is open on a machine with only this conversation")
	}
	a.key(key(" "))
	a.key(key(" "))
	if a.home.open {
		t.Fatal("the gesture fired with nowhere to go")
	}
	if got := a.input.String(); got != "  " {
		t.Fatalf("the spaces did not type themselves: %q", got)
	}
}

// The advertisement shows at rest and vanishes on the first character.
func TestTheDoorIsAdvertisedWhileIdleAndEmpty(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	if !a.homeDoorShowing() {
		t.Fatal("the door is not advertised at rest")
	}
	if got := a.legendRight(a.width); got != homeDoorWord+" · "+microcopy {
		t.Fatalf("the hint slot reads %q", got)
	}
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), homeDoorWord) {
		t.Fatalf("the door is not on the frame:\n%s", ansi.Strip(frame))
	}

	a.key(key("h"))
	if a.homeDoorShowing() {
		t.Fatal("the door is still advertised while something is being typed")
	}
	if got := a.legendRight(a.width); got != microcopy {
		t.Fatalf("the slot reads %q while typing", got)
	}
}

// And it is a thing you can press.
func TestClickingTheDoorGoesHome(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.width, a.height = 100, 24
	// The frame has to be laid out before the span it wrote can be read — the
	// same order every column-aware press on this surface keeps.
	frame, _, _ := a.frame()
	if !a.homeDoor.pressable() {
		t.Fatalf("laying out the frame recorded no columns for the door:\n%s", ansi.Strip(frame))
	}
	row := -1
	for y := 0; y < a.height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeLegend {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("no legend row on the frame")
	}
	if _, took := a.homeDoorPress(a.homeDoor.from, row); !took {
		t.Fatal("a click on the door did nothing")
	}
	if !a.home.open {
		t.Fatal("the click did not open home")
	}

	// A press on the rule beside it is a press on a rule.
	a.closeHome()
	a.frame()
	if _, took := a.homeDoorPress(1, row); took {
		t.Fatal("a click on the bare rule opened home")
	}
}

// THE ROUND TRIP: home → enter → the conversation → space space → home.
func TestTheDoorAndHomeBounceBackAndForth(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.launch(mine, true)
	if !a.home.open {
		t.Fatal("the launch did not land on home")
	}
	a.homeEnter()
	if a.home.open {
		t.Fatal("enter did not step into the conversation")
	}
	if a.file != mine {
		t.Fatalf("enter landed in %q", a.file)
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.home.open {
		t.Fatal("the gesture did not go back home")
	}
	a.homeKey(key("esc"))
	if a.home.open || a.file != mine {
		t.Fatal("esc did not come back to the conversation")
	}
}

// A turn running underneath is no obstacle, and is not disturbed.
func TestTheGestureWorksWhileATurnIsRunning(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.state = stateWorking
	a.key(key(" "))
	a.key(key(" "))
	if !a.home.open {
		t.Fatal("the gesture did not work with a turn running")
	}
	if a.state != stateWorking {
		t.Fatal("opening home disturbed the running turn")
	}
}

// ── a conversation another window is holding ────────────────────────────────

// hold takes a real exclusive flock on a session's journal and keeps it until
// the test ends — the same lock a second aforge would meet, taken the same way
// (internal/session's sessionfile.go), so these tests exercise the actual
// condition rather than a flag standing in for it.
func (l *homeLab) hold(transcript string) {
	l.t.Helper()
	file, err := os.Open(transcript)
	if err != nil {
		l.t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		l.t.Fatalf("could not hold %s: %v", transcript, err)
	}
	l.t.Cleanup(func() {
		unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
	})
}

// THE ROW SAYS SO BEFORE IT IS PRESSED. This is the half of the trap that made
// a locked door look like every other row on the list.
func TestALockedRowSaysSoInTheList(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	text := homeText(a)
	if !strings.Contains(text, homeHeldShort) {
		t.Fatalf("the list does not say the row is held:\n%s", text)
	}
	// And the detail column spells it out in full.
	a.home.point(theirs)
	if detail := homeText(a); !strings.Contains(detail, homeHeldWord) {
		t.Fatalf("the pane does not say the conversation is open elsewhere:\n%s", detail)
	}
}

// ENTER REFUSES WITHOUT TRYING, keeps home open, and never touches the
// conversation underneath.
func TestEnterOnALockedRowRefusesInHomesOwnVoice(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	asked := 0
	a.resume = func(string) (Agent, error) {
		asked++
		return &fakeAgent{model: "m"}, nil
	}
	a.openHome()
	a.home.point(theirs)
	before := len(a.entries)

	a.homeKey(key("enter"))
	if asked != 0 {
		t.Fatalf("home tried the door it already knew was locked (%d times)", asked)
	}
	if !a.home.open {
		t.Fatal("the refusal closed home")
	}
	if a.file != mine {
		t.Fatalf("the refusal moved this window to %q", a.file)
	}
	if a.home.msg != sessionBusyWord {
		t.Fatalf("home said %q", a.home.msg)
	}
	if len(a.entries) != before {
		t.Fatalf("the refusal was written into the conversation: %v", a.entries[before:])
	}
	if !strings.Contains(homeText(a), "go there, or start a new conversation here") {
		t.Fatalf("the refusal is not on the screen:\n%s", homeText(a))
	}
	// NO RAW PATH ANYWHERE. The whole defect was sixty characters of somebody
	// else's bookkeeping wrapped across two lines.
	for _, banned := range []string{"transcript.jsonl", "resume failed", "aforge/v3"} {
		if strings.Contains(homeText(a), banned) {
			t.Fatalf("the refusal leaked %q:\n%s", banned, homeText(a))
		}
	}
}

// A SECOND PRESS SAYS IT ONCE. The refusal lives in home's own line and is
// replaced, where a note in the conversation would have stacked.
func TestASecondEnterOnALockedRowDoesNotStack(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	before := len(a.entries)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))
	if len(a.entries) != before {
		t.Fatalf("three presses wrote %d lines into the conversation", len(a.entries)-before)
	}
	if got := strings.Count(homeText(a), "go there, or start"); got != 1 {
		t.Fatalf("the refusal is on the screen %d times", got)
	}
}

// The lock can appear between the scan and the keystroke, so the open itself
// can still lose. It loses in the same words, in the same place.
func TestTheRaceLosesInTheSameWordsNotARawError(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	// The door answers the way the engine does when it meets the flock, which
	// is the state a lock taken microseconds ago leaves the surface in.
	a.resume = func(file string) (Agent, error) {
		return nil, &session.SessionLockedError{Path: file}
	}
	a.openHome()
	a.home.point(theirs)
	before := len(a.entries)

	a.homeKey(key("enter"))
	if !a.home.open {
		t.Fatal("losing the race closed home")
	}
	if a.home.msg != sessionBusyWord {
		t.Fatalf("home said %q", a.home.msg)
	}
	if len(a.entries) != before {
		t.Fatalf("the race wrote into the conversation: %v", a.entries[before:])
	}
	if strings.Contains(homeText(a), "transcript.jsonl") {
		t.Fatalf("the race leaked the path:\n%s", homeText(a))
	}
}

// THE WINDOW KEEPS WHAT IT HAD. A refusal used to close this window's agent
// before discovering it could not open the other one.
func TestARefusedResumeLeavesThisWindowWhereItWas(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	held := a.agent.(*fakeAgent)
	a.resume = func(file string) (Agent, error) {
		return nil, &session.SessionLockedError{Path: file}
	}
	if _, refusal := a.openSession(Session{File: theirs}); refusal != sessionBusyWord {
		t.Fatalf("openSession answered %q", refusal)
	}
	if held.closes != 0 {
		t.Fatal("the conversation on screen was closed before the other one failed to open")
	}
	if a.agent != Agent(held) || a.file != mine {
		t.Fatal("the surface moved off the conversation it was in")
	}
}

// And a row nobody is holding still opens, which is the whole point of being
// careful about the ones that are.
func TestAnUnlockedRowStillOpens(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	free := lab.session("-tmp-alpha", "aaaa000000000002", "nobody has this one", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	a.home.point(free)
	if strings.Contains(homeText(a), homeHeldShort) {
		t.Fatalf("an unheld row was drawn as held:\n%s", homeText(a))
	}
	a.homeKey(key("enter"))
	if a.home.open {
		t.Fatal("opening a free conversation left home up")
	}
	if a.file != free {
		t.Fatalf("home opened %q, want %q", a.file, free)
	}
}

// The picker and the welcome box go through the same door, so they get the same
// sentence — no path, no "resume failed".
func TestTheOtherDoorsAlsoStopDumpingThePath(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", "/tmp/alpha", now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.resume = func(file string) (Agent, error) {
		return nil, &session.SessionLockedError{Path: file}
	}
	a.resumeSession(Session{File: theirs, Title: "the other terminal"})
	said := homeNotes(a)
	if !strings.Contains(said, sessionBusyWord) {
		t.Fatalf("the picker's door said %q", said)
	}
	for _, banned := range []string{"transcript.jsonl", "resume failed"} {
		if strings.Contains(said, banned) {
			t.Fatalf("the picker's door leaked %q: %s", banned, said)
		}
	}
}

// HOME NAMES OTHER PEOPLE'S DIRECTORIES, AND THEY ARE DOORS (pathlink.go).
//
// Home is the one surface whose whole subject is work that is somewhere else,
// so the place band under a conversation's name is the fastest route to the
// project it belongs to. It is a full-frame surface and does not pass through
// the conversation's row pass, which is why the link is hung here by hand.
func TestHomeLinksTheProjectDirectoryItNames(t *testing.T) {
	lab := newHomeLab(t)
	// A workspace that is REALLY THERE, because a path that is not found on
	// disk is drawn plain and this test would then be asserting nothing.
	workspace := t.TempDir()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the picker", workspace, time.Now())

	a := lab.app(mine)
	// Whether a link is written at all is read off TERM at construction, and
	// TERM belongs to whoever ran the tests.
	a.pathLinks = true
	a.openHome()

	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	frame := strings.Join(lines, "\n")
	if !strings.Contains(frame, "\x1b]8;;"+fileURI(workspace)) {
		t.Fatalf("home's place band is not a door to %q:\n%s", workspace, ansi.Strip(frame))
	}
	// AND THE SCREEN IS THE SAME SCREEN. A link occupies no cells, so nothing
	// home drew has moved.
	a.pathLinks = false
	plain, _, _, _ := a.homeFrame(width, height)
	if got, want := ansi.Strip(frame), ansi.Strip(strings.Join(plain, "\n")); got != want {
		t.Fatalf("the link changed what home says:\n got %q\nwant %q", got, want)
	}
	for i := range lines {
		if ansi.StringWidth(lines[i]) != ansi.StringWidth(plain[i]) {
			t.Fatalf("row %d changed width when linked", i)
		}
	}
}

// A directory that is not on this disk is named and not linked — the honesty
// rule, on the one surface that routinely names places this machine has never
// had (a project recorded on another machine, a folder since deleted).
func TestHomeDoesNotLinkAProjectThatIsGone(t *testing.T) {
	lab := newHomeLab(t)
	gone := filepath.Join(t.TempDir(), "deleted-since")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the picker", gone, time.Now())

	a := lab.app(mine)
	a.pathLinks = true
	a.openHome()

	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	if frame := strings.Join(lines, "\n"); strings.Contains(frame, "\x1b]8;;") {
		t.Fatalf("home linked a directory that is not there:\n%s", frame)
	}
}
