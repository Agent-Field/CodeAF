package tui3

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// homeLab builds a projects root on disk — the same shape the launch door
// writes (cmd/aforge's chatv3_layout.go) — so that these tests exercise the
// real reader rather than a fixture handed to it.
type homeLab struct {
	t    *testing.T
	root string
	// work is where [homeLab.workspace] mints project folders. It is OUTSIDE the
	// places root on purpose: a directory made under the root would be read back
	// as another bucket, and the test would grow a project nobody wrote.
	work string
}

func newHomeLab(t *testing.T) *homeLab {
	t.Helper()
	return &homeLab{t: t, root: t.TempDir(), work: t.TempDir()}
}

// workspace is a project folder that REALLY EXISTS, and it answers its path.
//
// A row whose recorded folder is not on the disk is marked `folder gone` and its
// card loses three of its keys, which is a fact about that row and about nothing
// else on this screen. So a test whose subject is a rollup word, a legend or a
// tick names a folder that is there, and only the tests about a missing one name
// one that is not.
func (l *homeLab) workspace(name string) string {
	l.t.Helper()
	dir := filepath.Join(l.work, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Fatal(err)
	}
	return dir
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
	// THE LEDGER IS THE LAB'S TOO. An empty path is the door's way of saying
	// "this machine's" (usage_ledger.go's [UsageCache] falls back to
	// [UsageLedgerPath]), so a lab that left it empty had the spend place read
	// the developer's real ~/.aforge — and a test about an empty spend place
	// went red the first time anything on the machine cost a cent.
	a.usageLedger = filepath.Join(l.root, session.UsageLedgerName)
	a.file = standing
	a.resume = func(string) (Agent, error) { return &fakeAgent{model: "m"}, nil }
	// AND THE WHOLE SEAM, because home is the switcher: enter on another
	// project's row asks for a conversation in THAT workspace, which the older
	// door cannot answer (tui3.go's [Options.Open]).
	a.open = func(workspace, transcript string) (Conversation, error) {
		return Conversation{
			Agent:       &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: transcript, Workspace: workspace, Resumed: true,
		}, nil
	}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{
			Agent:       &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: filepath.Join(workspace, "next", "transcript.jsonl"),
			Workspace:   workspace,
		}, nil
	}
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

// openHomeOn opens home and stands the cursor on one conversation.
//
// HOME OPENS AT REST (homebridge.go's [homeView.openAt]) — on no row at all,
// with the machine's own card on the right — which is right for a person
// arriving at a dashboard and beside the point for a test whose subject is a
// ROW. This puts the cursor where the first ↓ or the first click would put it,
// so what follows is about the row it names and not about where home opens.
func openHomeOn(a *app, transcript string) {
	a.openHome()
	a.home.point(transcript)
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

// homeCardNow is the detail column exactly as it stands, without moving the
// cursor and without widening anything.
//
// IT EXISTS BESIDE [homeCardFor] BECAUSE THE TWO CARDS ARE TWO CARDS. At rest
// the column is the switcher's acting card ([app.homeSwitchCard]); while
// something is typed the row under the cursor was built by [homeView.buildWorld]
// and carries no reading line, so the pane is the older band card with its own
// legend. A helper that pointed the cursor would rebuild nothing but would move
// a person off the match a drop-up test had just walked them onto.
func homeCardNow(t *testing.T, a *app) []string {
	t.Helper()
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the detail pane nothing", width)
	}
	var out []string
	for _, line := range a.homeDetail(right, 20, a.pal) {
		out = append(out, ansi.Strip(line))
	}
	return out
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
	if !a.at(pageHome) {
		t.Fatal("/home did not open")
	}
	text := homeText(a)
	// The screen names itself with the program's own name now, on the pulse line
	// at the top of it (pulse.go).
	for _, want := range []string{product, "alpha", "beta", "Porting the Resume Picker", "Pricing Research"} {
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
	if a.at(pageHome) {
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
	if !a.at(pageHome) {
		t.Fatal("the first esc left home instead of clearing the box")
	}
	if !a.home.box.empty() {
		t.Fatalf("the box still holds %q", a.home.box.String())
	}
	a.homeKey(key("esc"))
	if a.at(pageHome) {
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
//
// THE JUDGEMENT IS UNCHANGED AND ONLY THE COLUMN IT IS SPELLED IN MOVED. The
// resting list is the ranked reading now, and a quiet conversation's row carries
// its project, its note and its age and nothing about a task index
// ([switcherConversationNote]) — so what the ROW owes here is the negative claim,
// that nothing on this screen calls a stale row running, and the positive one is
// the work band's `◌ incomplete` on the card beside it (homeband_work.go).
func TestHomeWillNotCallAStaleRowRunning(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the long one", lab.workspace("alpha"), now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000001",
	})

	a := lab.app(mine)
	// A card tier, because the sentence this test is about is on the card and
	// there is no card at all below [homeCardMin] (homebridge.go).
	a.width, a.height = 200, 30
	openHomeOn(a, mine)
	row := a.home.focused()
	if row.Tasks.Running != 0 {
		t.Fatalf("home called %d rows running in a conversation nobody is holding", row.Tasks.Running)
	}
	if row.Tasks.Incomplete != 1 {
		t.Fatalf("home counted %d incomplete, want 1", row.Tasks.Incomplete)
	}
	card := strings.Join(homeCardFor(t, a, mine), "\n")
	if !strings.Contains(card, taskRecordStoppedWord) {
		t.Fatalf("the card does not say the work is incomplete:\n%s", card)
	}
	// NOT ON THE ROW AND NOT ON THE CARD. The switcher spells a live count `1
	// task running` and the work band spells it `● running`, so both spellings
	// are barred rather than the one the tree used to draw.
	text := homeText(a)
	for _, banned := range []string{"1 running", "1 task running", homeLiveGlyph + " running"} {
		if strings.Contains(text, banned) {
			t.Fatalf("home drew a stale row as %q:\n%s", banned, text)
		}
	}
}

// A session that says it is alive AND names the node it has out is the only
// case in which home will draw the word `running`.
func TestHomeCallsARowRunningWhenTheSessionSaysItHasThatNodeOut(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// The running one is a SECOND window's conversation, which is the case this
	// screen exists for — this window cannot see that turn any other way.
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now.Add(-2*time.Hour))
	lab.session("-tmp-alpha", "aaaa000000000002", "the long one", here, now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "7", Title: "Port the thing", State: "running", StartedAt: now.Add(-time.Minute)})

	a := lab.app(mine)
	// A card tier: the row says what is running in one clause and the card says
	// what it IS and where the window holding it stands (homebridge.go).
	a.width, a.height = 200, 30
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
	// THE ROW'S OWN NOTE CARRIES THE COUNT, in the reading's words rather than
	// the tree's — `1 task running`, which is the one fact a person reading the
	// list without pointing at anything needs ([switcherConversationNote]).
	if !strings.Contains(text, "1 task running") {
		t.Fatalf("the row does not say what is running:\n%s", text)
	}
	// AND THE CARD SAYS WHAT IT IS AND WHOSE WINDOW HAS IT. The work band puts
	// the name first with the state under it (homeband_work.go), and the place
	// line carries `open in another window · working` (place_home.go's
	// [app.homeCardPlace]).
	card := strings.Join(homeCardFor(t, a, row.Transcript), "\n")
	for _, want := range []string{"Port the thing", homeLiveGlyph + " running",
		homeHeldWord + " · working"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	if strings.Contains(text, taskRecordStoppedWord) {
		t.Fatalf("home called vouched-for work incomplete:\n%s", text)
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
	// The word lives on the card's work band now, so this is asked at a width
	// where a card exists ([TestHomeWillNotCallAStaleRowRunning] says why).
	a.width, a.height = 200, 30
	openHomeOn(a, mine)
	row := a.home.focused()
	if row.Tasks.Running != 0 || row.Tasks.Incomplete != 1 {
		t.Fatalf("rolled up %d running / %d incomplete, want 0 / 1", row.Tasks.Running, row.Tasks.Incomplete)
	}
	if card := strings.Join(homeCardFor(t, a, mine), "\n"); !strings.Contains(card, taskRecordStoppedWord) {
		t.Fatalf("the card does not say the work is incomplete:\n%s", card)
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
	openHomeOn(a, mine)
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
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", here, now)
	lab.session("-tmp-alpha", "aaaa000000000002", "middle of the road", here, now.Add(-time.Hour))
	lab.session("-tmp-alpha", "aaaa000000000003", "pricing research", here, now.Add(-6*time.Hour))
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
	// THE MARK IS THE READING'S OWN AND IT IS THE FIRST CELL OF THE ROW. It used
	// to be [homeAskGlyph]'s triangle drawn by the tree; the switcher paints every
	// row from one vocabulary — `?` needs you, `◐` moving, `○` at rest, `=` paused
	// (switcher.go's [switcherPaintRow]) — so the claim is the same claim in the
	// new alphabet.
	if !strings.Contains(text, tokens.GlyphNeedsHuman+" Pricing Research") {
		t.Fatalf("the row does not wear the needs-you mark:\n%s", text)
	}
	// AND WHAT IT IS STOPPED ON IS ON THE ROW ITSELF, not one keystroke away in a
	// pane. `waiting on you` was the tree repeating the glyph in words; the note
	// spends the same cells saying the thing nobody could otherwise know
	// ([switcherConversationNote]), which is why this assertion got stronger
	// rather than moving to the card — it holds with the cursor nowhere near the
	// row and at every width down to eighty columns.
	if !strings.Contains(text, "asks: can I run: rm -rf build/") {
		t.Fatalf("the row does not show what it is stopped on:\n%s", text)
	}
	// And it really is the first ROW of the column — asserted on the lines the
	// left column is built from rather than on where the words land in the
	// frame, because the detail pane repeats the focused conversation's name and
	// a search over the whole screen would find that copy first.
	// ONE ROW PER THING. A waiting conversation used to have two rows — its own
	// and a `needs you` strip's — and the strips are gone: the rank IS the list
	// now, so the waiting one is simply first (place_home.go).
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

// ONE FOLD FOR THE WHOLE MACHINE, AND NOT ONE PER PROJECT.
//
// This used to pin `…3 more` under a project's own heading: the tree drew a few
// of each project's conversations open and whispered that project's tail away,
// so a machine with five projects had five folds and the rows behind them were
// hidden by WHERE they lived rather than by whether anybody wanted them. The
// resting list is one flat ranked reading now (switcher.go), so the law those
// folds were protecting — that a long machine does not become a long screen —
// is kept by a single door at the foot of the whole list, standing over rows
// from every project at once.
//
// The cap itself is pinned in the pure reading
// ([TestTheSwitcherCapsAllGroupsInAttentionOrder]) and the door in
// place_home_test.go; what belongs HERE is that the surface built from a real
// disk obeys both ACROSS PROJECTS, which neither of those fixtures can say.
func TestHomeFoldsTheWholeMachinesQuietTailBehindOneDoor(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	var mine string
	for i := 0; i < switcherShown+4; i++ {
		bucket, work := "-tmp-alpha", alpha
		if i%2 == 1 {
			bucket, work = "-tmp-beta", beta
		}
		file := lab.session(bucket, fmt.Sprintf("aaaa%012d", i+1),
			fmt.Sprintf("chat %02d", i), work, now.Add(-time.Duration(i)*time.Hour))
		if i == 0 {
			mine = file
		}
	}
	a := lab.app(mine)
	// A FRAME TOO SHORT TO HOLD TWELVE ROWS, because the cap is the frame's now
	// and never a bare number: the list draws as many conversations as the column
	// can hold and folds only what is genuinely still under them (switcher.go's
	// [switcherShown] is the FLOOR). At sixteen rows the floor is what is left,
	// which is the eight this test is about.
	a.width, a.height = 100, 17
	a.openHome()
	text := homeText(a)
	if !strings.Contains(text, "more, quiet since") {
		t.Fatalf("home did not fold the quiet tail:\n%s", text)
	}

	// EXACTLY THE CAP, COUNTED OVER THE WHOLE MACHINE. Twelve conversations in
	// two projects come to eight rows and one door, and never to eight rows per
	// project.
	projects := map[string]bool{}
	rows := 0
	for _, line := range a.home.lines {
		if line.kind == homeSession {
			rows++
			projects[line.project] = true
		}
	}
	if rows != switcherShown {
		t.Fatalf("the resting list drew %d conversations, want the cap of %d:\n%s", rows, switcherShown, text)
	}
	if len(projects) != 2 {
		t.Fatalf("the eight rows came from %v, and the machine holds two projects:\n%s", projects, text)
	}
	// AND THERE IS ONE DOOR, not one per project.
	folds := 0
	for _, line := range a.home.lines {
		if line.kind == homeSwitchFold {
			folds++
		}
	}
	if folds != 1 {
		t.Fatalf("the resting list drew %d folds, want exactly one:\n%s", folds, text)
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
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	if !strings.Contains(homeText(a), "Pricing Research") {
		t.Fatal("the query matched nothing, so this proves nothing")
	}
	runCmd(a.homeEnter())
	if a.at(pageHome) {
		t.Fatal("enter on the action row left home open")
	}
	if len(next.sent) != 1 || next.sent[0] != "pricing" {
		t.Fatalf("the new conversation was sent %v", next.sent)
	}
}

// Walking UP off the action row is the decision to pick from the list instead,
// and it sticks.
//
// IT USED TO BE ↓, and the arrow turned round with the action row. The row sits
// at the BOTTOM of the list now, against the box a person is typing into
// ([homeAction]), so the matches are above it and walking into them is walking
// up the screen. WHICH match the walk reaches is
// [TestTheBestMatchSitsNextToTheActionRow].
//
// IT IS TWO ↑ AND NOT ONE, because `ask here` sits between the action row and
// the matches (homeexchange.go): the two rows that do something with the
// SENTENCE are one cluster against the box, and the rows that are other
// conversations begin above them.
func TestWalkingOffTheActionRowPicksFromTheList(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	a := lab.app(mine)
	a.openHome()
	for _, r := range "pric" {
		a.homeKey(key(string(r)))
	}
	a.homeKey(key("up"))
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAskHere {
		t.Fatalf("the first ↑ should reach `ask here` (kind %v)", line.kind)
	}
	a.homeKey(key("up"))
	if row := a.home.focused(); row.Transcript != mine {
		t.Fatal("↑ did not land on the match")
	}
	a.homeKey(key("i"))
	if row := a.home.focused(); row.Transcript != mine {
		t.Fatal("typing after ↑ threw the cursor back to the action row")
	}
	// And ↓ walks back down through the same two rows to the action row, which
	// is where the sentence is.
	a.homeKey(key("down"))
	a.homeKey(key("down"))
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("↓ did not come back to the action row (kind %v)", line.kind)
	}
}

// TYPING IS ONE CLUSTER AT THE FOOT, and this pins the geometry that makes it
// one.
//
// The defect it answers: the characters landed in the box at the very bottom of
// the frame while the row saying what enter would do with them stood at the very
// top, so the eye had to jump between the two ends of the screen and the cursor
// was at one end while the caret blinked at the other. The action row now sits
// on the LAST body row — directly above the rule and the box — with the matches
// rising above it.
//
// THE RESTING SCREEN IS THE OTHER SHAPE, and [TestHomeWithNothingTypedHangsFromTheTop]
// pins it: a dashboard from the top with the preview card beside it. The lift is
// what typing does, and only what typing does.
func TestTypingClustersAtTheFootOfHome(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing sheet import", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}

	width, height := a.size()
	lines, _, _, caretY := a.homeFrame(width, height)
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	action := -1
	for i, row := range rows {
		if strings.Contains(row, homeStartWord+`: "pricing"`) {
			action = i
		}
	}
	if action < 0 {
		t.Fatalf("the action row is not on the frame:\n%s", strings.Join(rows, "\n"))
	}
	// THE BOX IS THE ROW THE CARET IS ON, and the action row is three rows above
	// it: the list's padding row, then the frame's own foot rule (home.go's
	// [app.homeFrame] states why the list never touches that rule). Anything more
	// than that is the split this test exists to stop coming back.
	if caretY-action != 3 {
		t.Fatalf("the action row is %d rows above the box, want 3:\n%s", caretY-action, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[caretY], "pricing") {
		t.Fatalf("row %d is not the box:\n%s", caretY, strings.Join(rows, "\n"))
	}
	// AND THE MATCHES ARE ABOVE IT, not below — the list grew upward out of the
	// box rather than downward from the title.
	match := -1
	for i, row := range rows {
		if strings.Contains(row, "Pricing Research") {
			match = i
		}
	}
	if match < 0 || match > action {
		t.Fatalf("the matches are not above the action row (match %d, action %d):\n%s",
			match, action, strings.Join(rows, "\n"))
	}
	// The hint under the box names the arrow that is actually true of the screen —
	// ↑, because the matches rise ABOVE the action row the caret sits against.
	//
	// IT IS ASKED OF THE SENTENCE AND NOT OF THE DRAWN ROW, and that is not a
	// weaker question. The foot is a hundred and fourteen cells with the router's
	// keys on it and this frame is a hundred wide, so [hintFit] drops the clause
	// nearest the way out to make it fit — by design, and the ladder it drops down
	// is pinned by [TestAHintDropsWholeClausesAndKeepsTheWayOut]. Asked of the
	// drawn row this assertion was really asking how wide the lab happens to be,
	// and it passed for a year only because the old fitter sliced the tail off
	// mid-word instead — the foot on this very screen read `… · tab next …`. The
	// law it was written for is about the arrow, so the arrow is where it looks.
	if hint := a.homeHintWords(); !strings.Contains(hint, "↑ pick a match") {
		t.Fatalf("the hint names the wrong arrow: %s", hint)
	}
	// AND THE FOOT THAT IS DRAWN IS STILL WHOLE CLAUSES OF THAT SENTENCE, never a
	// word with its end sliced off.
	for _, clause := range strings.Split(strings.TrimSpace(rows[len(rows)-1]), railSep) {
		if !strings.Contains(placeTailed(a.homeHintWords()), clause) {
			t.Fatalf("the foot drew %q, which is not a clause of the hint:\n%s",
				clause, rows[len(rows)-1])
		}
	}
}

// homeLineY is the screen row one LINE OF THE LEFT COLUMN was drawn on, resolved
// through the same hit table a click is resolved through — so an assertion about
// where the column put something cannot disagree with where a press would land.
//
// IT IS THE HIT TABLE AND NOT THE TEXT, and that is not fussiness: the preview
// card across the gutter repeats the focused conversation's NAME, top-anchored, so
// a search over the frame's text for that name finds the card's copy at row four
// whatever the list beside it did. A geometry assertion written that way passes on
// a column dropped to the bottom of the screen — which is exactly how the
// bottom-anchored resting list shipped past this suite.
func homeLineY(t *testing.T, a *app, line int) int {
	t.Helper()
	width, height := a.size()
	_, hits, _, _ := a.homeFrame(width, height)
	for y, hit := range hits {
		if hit == line {
			return y
		}
	}
	t.Fatalf("column line %d is not on the frame", line)
	return -1
}

// homeCursorY is that, asked of the cursor.
func homeCursorY(t *testing.T, a *app) int {
	t.Helper()
	return homeLineY(t, a, a.home.cursor)
}

// homeRowY is that, asked of the row holding a transcript.
func homeRowY(t *testing.T, a *app, transcript string) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == transcript {
			return homeLineY(t, a, at)
		}
	}
	t.Fatalf("no column line holds %s", transcript)
	return -1
}

// homeRestLab is the fixture the resting laws are read off: three projects,
// this window standing in the newest of them, on a frame of the given width.
//
// THE WIDTH IS A PARAMETER BECAUSE THE LADDER HAS TWO RUNGS. Below [homeCardMin]
// the list is the whole frame and there is no card at all (homebridge.go), so a
// test whose subject is the LIST asks for an ordinary width and one whose
// subject is the CARD asks for a spare one — and neither has to know which
// number the other chose.
func homeRestLab(t *testing.T, width int) (*app, string) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", lab.workspace("alpha"), now)
	lab.session("-tmp-alpha", "aaaa000000000002", "an older one", lab.workspace("alpha"), now.Add(-2*time.Hour))
	lab.session("-tmp-beta", "bbbb000000000001", "somewhere else", lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width = width
	// The rest these tests are about is an empty BOX, not the cursor's own rest:
	// they are about the shape of a home nobody is typing at, so the cursor
	// stands on a row exactly as the first ↓ would leave it ([openHomeOn]).
	openHomeOn(a, mine)
	return a, mine
}

// AND WITH NOTHING TYPED THE LIST HANGS FROM THE TOP, because at rest home is a
// DASHBOARD somebody is reading and not a thing they are typing at.
//
// THIS LAW WAS TAKEN AWAY ONCE AND HAD TO BE PUT BACK. A wave anchored the column
// at the foot in both states so the cursor never moved between them, and the cost
// was the screen: a machine with a handful of conversations drew most of a frame
// of nothing with a clump of rows against the box. The drop-up is what TYPING
// needs ([TestTypingClustersAtTheFootOfHome]); it is not what home is.
func TestHomeWithNothingTypedHangsFromTheTop(t *testing.T) {
	a, mine := homeRestLab(t, 100)

	// The head is four rows — the pulse, the map, the rule and a blank — and the
	// first row of the reading follows it, with at most the `since you left`
	// heading and the section claim in between (switcher.go). Anything further
	// down is a list that floated to the bottom of the frame with nobody typing
	// at it.
	if at := homeRowY(t, a, mine); at > 9 {
		t.Fatalf("the list did not hang from the top (row %d):\n%s", at, homeText(a))
	}
	if strings.Contains(homeText(a), homeStartWord) {
		t.Fatal("the action row is drawn with nothing typed")
	}
	// AND THE CURSOR IS ON THIS WINDOW'S CONVERSATION — not lifted anywhere, and
	// above all not on a fold line.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
		t.Fatalf("the resting cursor is not on a conversation (kind %v)", line.kind)
	}
}

// THE RIGHT PANE IS DRAWN AT REST. This is the regression that shipped, and it
// shipped because nothing asserted the obvious.
//
// The cause was not the anchoring itself but what the anchoring did to the
// CURSOR. The preview card is built from the row under the cursor and draws
// NOTHING for a heading, a fold line or the action row — so a resting cursor that
// came to rest on a `…14 more` line left half the screen blank. Which is exactly
// what a fresh launch did: the conversation home greets you over has no message
// in it yet, so it sorts to the foot of the list behind the fold, so the row the
// cursor was supposed to open on was not on the column.
//
// THE CARD IS ASKED FOR AT A CARD WIDTH. There is no second column at all below
// [homeCardMin] now (homebridge.go), and the regression this test guards is
// about WHAT the cursor came to rest on rather than about how wide the frame
// was — so the frame is widened to a rung that has a card and the claim is
// unchanged: with an empty box, the column beside the list is never blank.
func TestTheRightPaneIsDrawnAtRest(t *testing.T) {
	a, mine := homeRestLab(t, 200)
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the detail pane nothing", width)
	}
	if card := a.homeDetail(right, 12, a.pal); len(card) == 0 {
		t.Fatalf("the preview pane is empty at rest, with the cursor on %q", homeName(a.home.focused()))
	}
	// And it is really on the frame, across the gutter from the list: the card's
	// second line is WHERE this conversation is, with the door word after it
	// (place_home.go's [app.homeCardPlace]).
	//
	// THE DOOR WORD IS `here` AND NO LONGER `open here`. The place line used to
	// ask [app.homeHolding] first, which spells this window's own conversation
	// `open here`; SCREEN 1d spells the whole line `~/aforge-v2 · master, 1 file
	// dirty · here`, so the card asks [app.homeMark] first and the design's word
	// wins (FIDELITY.md item 8). `open here` did not die — it is still what a
	// card assembled from the registry says ([drawKeysBand] reads it through the
	// same function) — it is simply not what the switcher's card says. The clause
	// is demanded as a SUFFIX rather than as a substring because `here` also
	// rides the box row's scope chip, and a bare Contains would pass on a frame
	// with no card on it at all.
	row := a.home.focused()
	if row.Transcript != mine {
		t.Fatalf("the resting cursor is on %q, want this window's conversation", homeName(row))
	}
	card := homeCardNow(t, a)
	if len(card) <= homeCardPlaceRow || !strings.Contains(card[homeCardPlaceRow], filepath.Base(row.Workspace)) ||
		!strings.HasSuffix(strings.TrimSpace(card[homeCardPlaceRow]), " · "+homeHereWord) {
		t.Fatalf("the card's place line is not the conversation's own folder:\n%s", strings.Join(card, "\n"))
	}
	if !strings.Contains(homeText(a), strings.TrimSpace(card[homeCardPlaceRow])) {
		t.Fatalf("the card is not on the resting frame:\n%s", homeText(a))
	}
}

// HOME'S RESTING FOOT, WORD FOR WORD — the two rows the design spells and the
// clause it deliberately leaves out (SCREEN 1a and 2b, FIDELITY.md item 3).
//
// It is pinned as a literal rather than against the constants alone because the
// constants are what a careless edit changes: the owner's order was "follow the
// exact design", and the only assertion that can catch a sentence drifting is
// one that carries the sentence.
//
// THE LAW THAT DIED IS `esc close` ON THE RESTING ROW. Home used to end every
// hint it drew with the way out, and the box row used to say [homeFootWord]
// instead of the shared prompt. The design's foot names four keys and no more,
// so the fifth clause left the resting row — and only the resting row: `esc`
// still closes home, and every other row's hint still ends with it, which is the
// second half of this test.
func TestHomesRestingFootIsTheDesignsSentence(t *testing.T) {
	const design = "type to search or start something new · ↑↓ pick · enter open · tab next place"
	if homeRestHint != design {
		t.Fatalf("home's resting foot reads %q, want the design's own sentence %q", homeRestHint, design)
	}

	a, _ := homeRestLab(t, 120)
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	if len(lines) < 2 {
		t.Fatalf("home drew %d rows", len(lines))
	}
	// The last row is the hint and the row above it is the box, which is the
	// order pages.go assembles every place's foot in.
	if got := strings.TrimSpace(ansi.Strip(lines[len(lines)-1])); got != design {
		t.Fatalf("the resting hint reads %q, want %q", got, design)
	}
	// THE BOX ROW IS THE SAME SENTENCE AS EVERY OTHER PLACE'S (SCREEN 2b). What
	// home's box ALSO does — filter the list — is said on the hint above, which is
	// where the design puts it; the box says only what enter will do with what is
	// typed into it. The scope chip rides the right edge of the same row, so the
	// prompt is demanded as a prefix.
	box := strings.TrimSpace(ansi.Strip(lines[len(lines)-2]))
	if !strings.HasPrefix(box, "› "+placeRestWord) {
		t.Fatalf("the box row reads %q, want the design's prompt", box)
	}
	// AND THE CLAUSE THAT LEFT IS REALLY GONE from the foot — not merely absent
	// from the constant this test already compared.
	for _, row := range []string{box, design} {
		if strings.Contains(row, "esc") {
			t.Fatalf("the resting foot names esc: %q", row)
		}
	}

	// ESC STILL WORKS, which is why losing the clause is a wording change and
	// not a capability going quiet.
	a.homeKey(key("esc"))
	if a.at(pageHome) {
		t.Fatal("esc did not close home")
	}

	// AND EVERY ROW THAT IS NOT THE RESTING ONE STILL ENDS WITH IT. The old law
	// held for the whole screen; it holds now for the rows the design does not
	// spell itself, which is every state home enters once a person acts.
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	// The clause names what esc will do on THAT row — `esc clear` on a typed box,
	// `esc close` on a card — so what is demanded is the key in the last slot
	// rather than one spelling of it.
	hint := a.placeHint()
	clauses := strings.Split(hint, " · ")
	if last := clauses[len(clauses)-1]; !strings.HasPrefix(last, "esc ") {
		t.Fatalf("a typed home's hint reads %q, want a way out on the end", hint)
	}
}

// AND A LAUNCH WHOSE OWN CONVERSATION IS NOT ON THE LIST OPENS ON THE FIRST
// CONVERSATION INSTEAD, which is what became of the case that broke. A session
// folder nobody has spoken in yet has no age for the ranking to sort on, so it
// falls to the very bottom of the reading and the one fold hides it: the cursor
// has no own-row to open on, and it falls to the first row a cursor may stand on
// (homebridge.go's [homeView.openAt]), a conversation with a card of its own,
// rather than the fold line that used to swallow it.
func TestAFreshLaunchOpensOnTheFirstConversationWhenItsOwnIsNotListed(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// MORE CONVERSATIONS THAN THE ONE LIST DRAWS, so there is a fold at the foot
	// — the line the cursor wrongly came to rest on — and so that a conversation
	// with no age at all sorts behind it.
	work := lab.workspace("alpha")
	for i := 0; i < switcherShown+4; i++ {
		lab.session("-tmp-alpha", fmt.Sprintf("aaaa%012d", i+1),
			fmt.Sprintf("chat %02d", i), work, now.Add(-time.Duration(i+1)*time.Hour))
	}
	// This window's own folder, written the way a launch writes one: a journal and
	// a meta that names it, and no message spoken in it yet.
	dir := filepath.Join(lab.project("-tmp-alpha"), "zzzz000000000001")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(mine, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID: "zzzz000000000001", Title: "brand new", Workspace: work, Created: now,
	}); err != nil {
		t.Fatal(err)
	}

	a := lab.app(mine)
	// A card tier, because the last thing this test asks is that the row the
	// first `↓` finds has a card, and there is none below [homeCardMin]; and a
	// SHORT one, because the list draws as many rows as the frame can hold now
	// (switcher.go's [switcherView.room]) and a tall window over thirteen
	// conversations has nothing left to fold.
	a.width, a.height = 200, 17
	a.openHome()
	// The window's own conversation is adopted into the world ([app.readWorld])
	// but it has never been spoken in, so it sorts to the very bottom of the
	// ranked reading and the one fold swallows it — which is the case this test
	// is about: the cursor has no own-row to open on.
	if a.home.focused().Transcript == mine {
		t.Fatal("the list drew this window's own conversation, so this proves nothing")
	}
	if !strings.Contains(homeText(a), "more") {
		t.Fatal("nothing collapsed, so the fold line this guards against is not on the screen")
	}
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the launch landed on kind %v, want a conversation:\n%s", line.kind, homeText(a))
	}
	if a.home.cursor != a.home.placesTop() {
		t.Fatalf("the launch landed on line %d, want the first standable row at %d:\n%s",
			a.home.cursor, a.home.placesTop(), homeText(a))
	}
	// AND `↑` OFF THE TOP ROW LEAVES THE LIST'S OWN CURSOR ALONE. It reaches the
	// TAB BAR, which is a row of the frame rather than of this list (pages.go's
	// [barCursor]); the row a person walked up off is still the row `↓` puts them
	// back on, and home's card never stops being about it.
	a.frame()
	drive(t, a, key("up"))
	if !a.bar.on {
		t.Fatalf("↑ off the top row did not reach the tab bar:\n%s", homeText(a))
	}
	if a.home.cursor != a.home.placesTop() {
		t.Fatalf("↑ onto the bar moved home's own cursor to line %d", a.home.cursor)
	}
	drive(t, a, key("down"))
	if a.bar.on {
		t.Fatal("↓ from the bar left the cursor on it")
	}
	line, ok = a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the first ↓ landed on kind %v, want a conversation:\n%s", line.kind, homeText(a))
	}
	width, _ := a.size()
	_, right := homeColumns(width)
	if card := a.homeDetail(right, 12, a.pal); len(card) == 0 {
		t.Fatalf("the row the first ↓ found draws no preview card:\n%s", homeText(a))
	}
}

// THE CURSOR MOVES BETWEEN THE TWO STATES, and that is the accepted price of
// keeping the dashboard. Each state's geometry is pinned on its own: at rest the
// cursor is up in the list, and the first character takes it to the foot with the
// action row. Clearing the box brings it back.
func TestTheCursorGoesToTheFootWhileTypingAndBackAtRest(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing sheet import", "/tmp/beta", now.Add(-time.Hour))
	lab.session("-tmp-gamma", "cccc000000000001", "nothing to do with it", "/tmp/gamma", now.Add(-9*time.Hour))

	a := lab.app(mine)
	// The rest in the name is the BOX's, so the cursor stands on a row exactly as
	// the first ↓ would leave it ([openHomeOn]).
	openHomeOn(a, mine)
	_, height := a.size()

	// AT REST: up in the list, well clear of the box, and on a conversation. The
	// bound leaves room for the head and for whatever the reading puts above its
	// first row — the `since you left` heading and the section claim (switcher.go).
	rest := homeCursorY(t, a)
	if rest > 9 {
		t.Fatalf("the resting cursor is on row %d, want it up in the list:\n%s", rest, homeText(a))
	}
	if row := a.home.focused(); row.Transcript != mine {
		t.Fatalf("the resting cursor is on %q, want this window's conversation", homeName(row))
	}

	// TYPING: the action row, on the last body row — FIVE up from the bottom of
	// the frame, because the body now ends one row short of the rule: the padding
	// row, then the rule, the box and the hint.
	a.homeKey(key("p"))
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("the first character did not put the cursor on the action row (kind %v)", line.kind)
	}
	if at := homeCursorY(t, a); at != height-5 {
		t.Fatalf("the typing cursor is on row %d of %d, want the last body row %d:\n%s",
			at, height, height-5, homeText(a))
	}

	// AND BACK: the box empties, the dashboard returns, the cursor is off the foot.
	a.homeKey(key("backspace"))
	if at := homeCursorY(t, a); at == height-5 {
		t.Fatalf("clearing the box left the cursor at the foot:\n%s", homeText(a))
	}
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
		t.Fatalf("clearing the box left the cursor on kind %v, want a conversation", line.kind)
	}
}

// ── THE BEST MATCH IS THE ONE UNDER YOUR HAND ───────────────────────────────
//
// A ranked list read DOWNWARD puts its best answer first. The drop-up is read
// UPWARD out of the box, so it has to put its best answer LAST — and it did not.
// With three matches on screen one ↑ landed on the WORST of them and the best
// took three keystrokes, which is the ranking being drawn at the wrong end of the
// column. The scoring was never wrong; the drawing was.

// ONE ↑ FROM THE ACTION ROW IS THE TOP-RANKED MATCH. That is the whole law, and
// it is asserted against the scores themselves rather than against a list of
// names, so a change to [homeRank] cannot quietly make this test agree with a
// column it no longer describes.
func TestTheBestMatchSitsNextToTheActionRow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// Three hits of DIFFERENT quality on "pricing": the bare name is the strongest,
	// then two that carry it among other words. Which is which is decided by
	// [homeRank] below, not by this comment.
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing sheet import", "/tmp/beta", now.Add(-time.Hour))
	lab.session("-tmp-gamma", "cccc000000000001", "quarterly pricing deck", "/tmp/gamma", now.Add(-9*time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}

	// The matches in DRAWN order, each with the score the ranking gave it.
	type hit struct {
		name  string
		score int
	}
	var drawn []hit
	for _, line := range a.home.lines {
		if line.kind != homeSession {
			continue
		}
		var project session.Project
		for _, p := range a.home.world.Projects {
			if p.Dir == line.dir {
				project = p
			}
		}
		score, ok := homeRank(line.row, project, "pricing", a.home.world.Read)
		if !ok {
			t.Fatalf("%q is on the column but does not match the query", homeName(line.row))
		}
		drawn = append(drawn, hit{homeName(line.row), score})
	}
	if len(drawn) != 3 {
		t.Fatalf("expected three matches, got %d: %+v", len(drawn), drawn)
	}

	// SCORE RISES AS YOU GO DOWN THE COLUMN, so the bottom row is the best answer
	// and the top row is the weakest.
	for i := 1; i < len(drawn); i++ {
		if drawn[i].score < drawn[i-1].score {
			t.Fatalf("the column is drawn best-first: %+v", drawn)
		}
	}
	best := drawn[len(drawn)-1]
	if best.score == drawn[0].score {
		t.Fatalf("every match tied, so the order proves nothing: %+v", drawn)
	}

	// AND THE ACTION ROW IS STILL BELOW THEM ALL, so the best match is the FIRST
	// conversation the walk reaches rather than the row furthest from the key.
	// The row between them is `ask here` (homeexchange.go), which is the other
	// thing enter can do with the sentence and not a match.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("the cursor did not rest on the action row (kind %v)", line.kind)
	}
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	if got := homeName(a.home.focused()); got != best.name {
		t.Fatalf("walking up landed on %q, want the top-ranked %q (%+v)", got, best.name, drawn)
	}
	// Further ↑ walks into weaker matches, in order.
	for i := len(drawn) - 2; i >= 0; i-- {
		a.homeKey(key("up"))
		if got := homeName(a.home.focused()); got != drawn[i].name {
			t.Fatalf("walking up reached %q, want %q (%+v)", got, drawn[i].name, drawn)
		}
	}
	// And ↓ comes back down toward the box, through `ask here` and onto the
	// action row — one step per match, plus the one for the row between them
	// (homeexchange.go).
	for range drawn {
		a.homeKey(key("down"))
	}
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAskHere {
		t.Fatalf("↓ did not walk back to `ask here` (kind %v)", line.kind)
	}
	a.homeKey(key("down"))
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("↓ did not walk back to the action row (kind %v)", line.kind)
	}
}

// A PROJECT'S HEADING STAYS ABOVE ITS OWN ROWS. Sections stack by rank and the
// rows inside one do too, but a name drawn UNDER the things it names reads
// upside-down — so the turn is applied to the order of the sections and of the
// rows, never to the heading's place within its section.
func TestTheInvertedDropUpKeepsHeadingsAboveTheirRows(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "pricing sheet import", "/tmp/alpha", now.Add(-time.Hour))
	lab.session("-tmp-beta", "bbbb000000000001", "quarterly pricing deck", "/tmp/beta", now.Add(-9*time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	seen := map[string]bool{}
	for _, line := range a.home.lines {
		switch line.kind {
		case homeHeading:
			seen[line.dir] = true
		case homeSession:
			if !seen[line.dir] {
				t.Fatalf("%q is drawn above its project's heading", homeName(line.row))
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("the filtered column drew %d headings, want one per matching project", len(seen))
	}
}

// ── THE RIGHT PANE IS NOT PART OF THE STATE ─────────────────────────────────
//
// Home ALWAYS has two panes. What changes with the box is where the LEFT one is
// anchored — top at rest, against the box while typing. The right one previews
// whatever the cursor is on, in both states and through every keystroke, and
// empties only when the focused row is not a conversation.

// THE CARD FOLLOWS THE CURSOR THROUGH A FILTER. Walking the matches is choosing
// between conversations, and choosing between them by name alone is the thing the
// card exists to stop.
//
// The first ↑ here lands on the TOP-RANKED match, which is
// [TestTheBestMatchSitsNextToTheActionRow]'s law; what this one is about is that
// the card changes with the cursor whichever row that turns out to be.
func TestThePreviewCardFollowsTheCursorWhileTyping(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing sheet import", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	// A card tier: the pane this test is about does not exist below [homeCardMin]
	// (homebridge.go). What typing does to it is the same at every width — there
	// simply is no `it` to watch on a narrow frame.
	a.width, a.height = 200, 24
	a.openHome()
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the detail pane nothing", width)
	}

	// ON THE ACTION ROW THE PANE IS EMPTY, and that is the emptiness law rather
	// than an omission: "start a new conversation" is a chat that does not exist
	// yet, so there is nothing true to preview about it.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("typing did not rest the cursor on the action row (kind %v)", line.kind)
	}
	if card := a.homeDetail(right, 12, a.pal); len(card) != 0 {
		t.Fatalf("the pane previewed a conversation that does not exist yet:\n%s", strings.Join(card, "\n"))
	}

	// ↑ ONTO A MATCH DRAWS THAT MATCH'S CARD. Two of them: `ask here` is the row
	// in between, and it is a thing that does not exist yet exactly as the action
	// row is, so its pane is empty for the same reason (homeexchange.go).
	a.homeKey(key("up"))
	if card := a.homeDetail(right, 12, a.pal); len(card) != 0 {
		t.Fatalf("the pane previewed the `ask here` row:\n%s", strings.Join(card, "\n"))
	}
	a.homeKey(key("up"))
	first := a.home.focused()
	if first.Transcript == "" {
		t.Fatal("↑ did not land on a match")
	}
	if card := a.homeDetail(right, 12, a.pal); len(card) == 0 {
		t.Fatalf("the pane is empty with the cursor on %q", homeName(first))
	}
	if text := homeText(a); !strings.Contains(text, homeName(first)) {
		t.Fatalf("the card for %q is not on the frame:\n%s", homeName(first), text)
	}

	// AND ANOTHER ↑ SWITCHES IT. The pane is following the cursor, not holding the
	// first thing it was shown.
	a.homeKey(key("up"))
	second := a.home.focused()
	if second.Transcript == first.Transcript {
		t.Fatal("the second ↑ did not move to another match, so this proves nothing")
	}
	card := strings.Join(a.homeDetail(right, 12, a.pal), "\n")
	if !strings.Contains(card, homeName(second)) {
		t.Fatalf("the card still names %q after the cursor moved to %q:\n%s",
			homeName(first), homeName(second), card)
	}
	if strings.Contains(card, homeName(first)) {
		t.Fatalf("the card kept the row the cursor left:\n%s", card)
	}

	// …AND ↓ BACK ONTO THE ACTION ROW EMPTIES IT AGAIN. Three steps: two matches
	// and the `ask here` row between them and the box (homeexchange.go).
	a.homeKey(key("down"))
	a.homeKey(key("down"))
	a.homeKey(key("down"))
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeAction {
		t.Fatalf("↓ did not come back to the action row (kind %v)", line.kind)
	}
	if card := a.homeDetail(right, 12, a.pal); len(card) != 0 {
		t.Fatalf("the pane kept a card after the cursor left the match:\n%s", strings.Join(card, "\n"))
	}
}

// AND THE DROP-UP DOES NOT MOVE THE CARD. The list lifts against the box; the
// card is assembled downward from its title and stays where it is, which is why
// lifting it would take the facts off the bottom rather than move it down the
// frame.
func TestTheDropUpDoesNotLiftTheCardWithIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing sheet import", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	// A card tier, because the subject IS the card and there is none below
	// [homeCardMin] (homebridge.go).
	a.width, a.height = 200, 24
	openHomeOn(a, mine)
	// THE CARD'S OWN TITLE ROW AND NOT THE LIST'S. Both columns draw the focused
	// conversation's name, and the list's copy DOES move with the drop-up — which
	// is the whole point — so a search over the whole row would find the wrong one
	// and pass on a card that had been lifted with it. The card starts after the
	// list's cells and the gutter, so the search begins there.
	titleRow := func() int {
		width, height := a.size()
		left, _ := homeColumns(width)
		lines, _, _, _ := a.homeFrame(width, height)
		for y, line := range lines {
			plain := []rune(ansi.Strip(line))
			if len(plain) <= left+homeGutter {
				continue
			}
			if strings.Contains(string(plain[left+homeGutter:]), homeName(a.home.focused())) {
				return y
			}
		}
		return -1
	}
	rest := titleRow()
	if rest < 0 {
		t.Fatal("no card on the resting frame")
	}
	for _, r := range "pricing" {
		a.homeKey(key(string(r)))
	}
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	if at := titleRow(); at != rest {
		t.Fatalf("the card moved from row %d to row %d when the list became a drop-up", rest, at)
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
	// A FRAME THE ROWS DO NOT FIT IN, because the resting list draws as many as
	// the column can hold now and folds only what is genuinely under them
	// (switcher.go's [switcherShown] is the floor, not the cap).
	a.width, a.height = 100, 17
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

// THE FOLD AT THE FOOT IS A DOOR AND EVERY GESTURE OPENS IT: → opens, ← folds it
// back, and a click toggles.
//
// It used to be one such door per project ([homeQuiet], which only the phone tier
// builds now). The law is the one it always was — a line that says rows are being
// hidden and cannot be asked to stop hiding them is a dead end somebody hits and
// gives up at — carried by the single fold the flat list has ([homeSwitchFold]).
// place_home_test.go pins `enter` on it; the other three gestures are here.
//
// ONE THING THE OLD DOOR DID THAT THIS ONE DOES NOT: the cursor used to stay on
// the line that did the opening, so the gesture could be reversed without moving
// ([homeView.fold], [homeView.foldProject] and [homeView.foldItems] all re-find
// their own line and set `picked`). [homeView.foldSwitch] rebuilds through
// [homeView.build], which follows a conversation, an item, a project or an errand
// and knows nothing about a fold — so the cursor lands back at the top of the
// list. That is why each gesture below finds the fold again first.
func TestTheOneFoldOpensAndFoldsOnEveryGesture(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	work := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest one", work, now)
	for i := 0; i < switcherShown+4; i++ {
		lab.session("-tmp-alpha", fmt.Sprintf("bbbb%012d", i+1),
			fmt.Sprintf("filler %02d", i), work, now.Add(-time.Duration(i+1)*time.Hour))
	}
	a := lab.app(mine)
	// A FRAME THE THIRTEEN ROWS DO NOT FIT IN, because the list draws as many as
	// the column can hold now and folds only what is genuinely under them
	// (switcher.go's [switcherShown] is the floor, not the cap). At seventeen
	// rows the floor is what is left, so the fold stands over five.
	a.width, a.height = 100, 17
	a.openHome()

	// onTheFold stands the cursor on the fold wherever the last rebuild left it,
	// and fails when the list has no fold at all.
	onTheFold := func() {
		t.Helper()
		for at, line := range a.home.lines {
			if line.kind == homeSwitchFold {
				a.home.cursor, a.home.picked = at, true
				return
			}
		}
		t.Fatalf("the list has no fold to press:\n%s", homeText(a))
	}
	// hidden is the row the fold is standing over, which must be off the list
	// while it is shut and on it once it is open. It is the FIRST row behind the
	// fold: what a fold hides is now exactly what the frame had no room for, so
	// the row that comes back when it opens is the one the window can just
	// reach.
	const hidden = "Filler 07"

	onTheFold()
	if strings.Contains(homeText(a), hidden) {
		t.Fatalf("%q was not behind the fold to begin with:\n%s", hidden, homeText(a))
	}
	a.homeKey(key("right"))
	if !a.home.moreOpen {
		t.Fatal("→ did not open the fold")
	}
	if !strings.Contains(homeText(a), hidden) {
		t.Fatalf("opening the fold did not draw the rows behind it:\n%s", homeText(a))
	}
	// AND THE LINE TURNS ROUND RATHER THAN VANISHING: it is the way back, so it
	// still says how many rows it stands for and wears the opened mark
	// ([switcherReading.addFold]).
	//
	// IT SAYS `fewer` AND NO LONGER `more`. This assertion used to want
	// `▾ 5 more` over five rows that were on the frame, where the glyph was the
	// only thing telling "five are hidden" from "five of these are the ones you
	// asked for"; [foldWords] settles it, and `fewer` is what pressing the line
	// again would do.
	if !strings.Contains(homeText(a), tokens.GlyphExpanded+" 5 fewer") {
		t.Fatalf("the opened fold is not the way back:\n%s", homeText(a))
	}
	if strings.Contains(homeText(a), tokens.GlyphExpanded+" 5 more") {
		t.Fatalf("an opened fold still says it is hiding five rows:\n%s", homeText(a))
	}

	onTheFold()
	a.homeKey(key("left"))
	if a.home.moreOpen {
		t.Fatal("← did not fold the rows back")
	}
	if strings.Contains(homeText(a), hidden) {
		t.Fatalf("← left the hidden rows on the list:\n%s", homeText(a))
	}

	// AND A CLICK IS THE SAME DOOR. The row is resolved through the hit table the
	// frame wrote, so a press cannot reach a line the draw did not put on screen.
	onTheFold()
	_, hits, _, _ := a.homeFrame(a.width, a.height)
	row := -1
	for y, at := range hits {
		if at == a.home.cursor {
			row = y
		}
	}
	if row < 0 {
		t.Fatalf("the fold line is not on screen:\n%s", homeText(a))
	}
	a.homePress(4, row)
	if !a.home.moreOpen {
		t.Fatalf("a click did not open the fold:\n%s", homeText(a))
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
	// A card tier: the second half of this test is about the PANE beside the
	// match, and there is none below [homeCardMin] (homebridge.go).
	a.width, a.height = 200, 24
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
	// ↑ walks off the action row, past `ask here` (homeexchange.go), and up into
	// the match — which is where the matches are now ([homeAction]).
	a.homeKey(key("up"))
	a.homeKey(key("up"))
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
	// THE TOP-RANKED ROW IS THE LAST ONE DRAWN, because the drop-up is read
	// upward out of the box ([TestTheBestMatchSitsNextToTheActionRow] states the
	// law). The RANKING is what this test is about and it has not moved; only
	// which end of the column it is written at.
	if !order[len(order)-1].NeedsPerson() {
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
	if !a.at(pageHome) {
		t.Fatal("the first esc left home instead of clearing the query")
	}
	if !a.home.box.empty() {
		t.Fatalf("the box still holds %q", a.home.box.String())
	}
	a.homeKey(key("esc"))
	if a.at(pageHome) {
		t.Fatal("the second esc did not close home")
	}
}

// ── the two columns ─────────────────────────────────────────────────────────

// THE RIGHT PANE'S EDGE IS A STRAIGHT LINE. It is the only thing separating the
// two columns — this surface draws no borders — so it has to be findable on
// every row without looking for it.
//
// EVERY WIDTH HERE IS A CARD WIDTH NOW. The gutter is a fact about the two-column
// rung and there is no second column at all below [homeCardMin] (homebridge.go),
// so the widths this walks are the tier's floor and two frames above it rather
// than the three ordinary ones it used to walk — which is the same claim asked
// where there is something to ask it about.
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
	for _, width := range []int{homeCardMin, 200, 240} {
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
//
// TWO BANDS BECAME ONE LINE AND THE LAW DID NOT MOVE. What the card spends and
// what it thinks at are one sentence now ([app.homeCardFacts]) — so the line
// this reads is the switcher card's own rather than the `spend` band's, and it
// is asked at a card width, since the card itself only exists past [homeCardMin].
func TestTheFactsFooterOmitsWhatIsNotAFact(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	quiet := lab.session("-tmp-alpha", "aaaa000000000001", "just talking", lab.workspace("alpha"), now.Add(-2*time.Hour))
	a := lab.app(quiet)
	a.width, a.height = 200, 30
	openHomeOn(a, quiet)
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
	a.width, a.height = 200, 30
	openHomeOn(a, quiet)
	if got := homeText(a); !strings.Contains(got, "spent $1.25 · 34k tokens · last active 1m") {
		t.Fatalf("the footer does not read as one line of facts:\n%s", got)
	}
	// AND THE RUNG IS ON THE LINE, which is where the emptiness law stops: the
	// clause is the INSTALL'S rung — what work started from this card would think
	// at — and an install that has chosen nothing runs at the shipped one, so
	// there is a fact here and it is drawn ([app.homeCardFacts], SCREEN 1d,
	// FIDELITY.md item 8; effortscope_test.go's [TestCtrlVOnAConversationRow…]
	// states the same reading from the other side).
	//
	// THIS ASSERTION USED TO READ THE OTHER WAY and it was passing for the wrong
	// reason. It demanded that no rung appear, on the argument that the machine's
	// default is not a fact about this chat — a law SCREEN 1d had already
	// overruled — and what kept it green was a defect rather than the design:
	// [app.effortProfile] answered "no profile" on an empty profile directory,
	// this lab names none, and so the clause was silenced here exactly as it was
	// silenced on every real launch (#322).
	if got := strings.Join(homeCardFor(t, a, quiet), "\n"); !strings.Contains(got, effortClauseWord+effort.Ship.String()) {
		t.Fatalf("the facts line does not state the rung work started here would think at:\n%s", got)
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
	openHomeOn(a, mine)
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

// ENTER OPENS ANY ROW ON THIS SCREEN, whichever project it belongs to, and the
// conversation you were in stays open behind it.
//
// This test replaces the one that asserted the opposite. Home used to refuse
// every project but this window's own with a dim `elsewhere` and a sentence
// saying where to go instead; that refusal is the thing this wave removed.
func TestHomeOpensAnotherProjectAndTheOneYouLeaveGoesOnRunning(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	other := lab.session("-tmp-beta", "bbbb000000000001", "the other project", lab.project("-tmp-beta"), now.Add(-time.Hour))

	standing := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := lab.app(mine)
	a.agent = standing
	a.openHome()
	a.home.point(other)
	a.homeEnter()

	if a.at(pageHome) {
		t.Fatalf("opening another project left home up saying %q", a.home.msg)
	}
	if a.file != other {
		t.Fatalf("home opened %q, want %q", a.file, other)
	}
	if standing.closed {
		t.Fatal("the conversation left behind was closed — it goes on running")
	}
	if !a.holding(mine) {
		t.Fatal("the conversation left behind is not open")
	}
	if a.openCount() != 2 {
		t.Fatalf("this terminal holds %d conversations", a.openCount())
	}
	// AND THE WORD `elsewhere` IS GONE FROM THE SCREEN.
	a.openHome()
	if text := homeText(a); strings.Contains(text, "elsewhere") {
		t.Fatalf("home still says elsewhere:\n%s", text)
	}
}

// A row this terminal is holding says `open`, never `another window` — the flock
// it would meet is our own.
func TestARowThisTerminalHoldsSaysOpenAndNeverAnotherWindow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	other := lab.session("-tmp-beta", "bbbb000000000001", "the other project", lab.project("-tmp-beta"), now.Add(-time.Hour))

	a := lab.app(mine)
	a.agent = &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.openHome()
	a.home.point(other)
	a.homeEnter()

	a.openHome()
	text := homeText(a)
	if strings.Contains(text, homeHeldShort) {
		t.Fatalf("a conversation this terminal holds was called another window:\n%s", text)
	}
	if !strings.Contains(text, " "+homeOpenWord) {
		t.Fatalf("the row this terminal holds does not say open:\n%s", text)
	}
	for at, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == mine {
			a.home.cursor = at
		}
	}
	a.homeEnter()
	if a.file != mine {
		t.Fatalf("enter on a row we hold went to %q", a.file)
	}
}

// A project folder that is gone refuses, home stays up, and the conversation on
// screen is untouched.
func TestHomeRefusesARowWhoseFolderIsGone(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	gone := lab.session("-tmp-gone", "cccc000000000001", "a project that moved",
		filepath.Join(lab.root, "no-such-repository"), now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	a.home.point(gone)
	a.homeEnter()

	if !a.at(pageHome) {
		t.Fatal("a refused open closed home")
	}
	if a.file != mine {
		t.Fatalf("a refused open moved the surface to %q", a.file)
	}
	if !strings.HasPrefix(a.home.msg, WorkspaceGoneWord+" · ") {
		t.Fatalf("home said %q", a.home.msg)
	}
}

// AND IT SAYS SO BEFORE ANYTHING IS PRESSED. The refusal above lands on the last
// line of the screen, which on a tall terminal is nowhere near the cursor — so
// the fact stands against the address it is about, wherever that address is
// drawn.
//
// WHERE IT IS DRAWN CHANGED WITH THE SHAPE. The resting list is switcher.go's
// pure reading and it is handed no disk at all, so a row of it cannot say
// `folder gone` — the fact lives on the card's PLACE LINE, in place of the
// branch it cannot have (place_home.go's [app.homeCardPlace]). The moment
// something is typed the column is [homeView.buildWorld]'s drop-up again, whose
// rows do carry [homeGoneShort] and whose card carries the legend; both halves
// are asserted below, so neither spelling can be lost without this failing.
func TestHomeMarksARowWhoseFolderIsGoneWhereverItsAddressIsDrawn(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", lab.project("-tmp-alpha"), now)
	gone := lab.session("-tmp-gone", "cccc000000000001", "a project that moved",
		filepath.Join(lab.root, "no-such-repository"), now.Add(-time.Hour))

	a := lab.app(mine)
	a.width, a.height = 200, 30
	a.openHome()

	// AT REST: THE CARD'S PLACE LINE, which is [homeCardPlaceRow] — the title's
	// own second row — and never further down, because a short frame drops bands
	// from the bottom and the address is part of the one band no band may
	// displace.
	card := homeCardFor(t, a, gone)
	if at := cardLine(card, WorkspaceGoneWord); at != homeCardPlaceRow {
		t.Fatalf("the sentence is on card line %d, want the place line:\n%s", at, strings.Join(card, "\n"))
	}

	// TYPED: the row itself, in the width the column has for it.
	for _, r := range "moved" {
		a.homeKey(key(string(r)))
	}
	if !strings.Contains(homeText(a), homeGoneShort) {
		t.Fatalf("no %q on the drop-up's row:\n%s", homeGoneShort, homeText(a))
	}
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	if got := a.home.focused().Transcript; got != gone {
		t.Fatalf("↑ landed on %q, want the row whose folder is gone", got)
	}
	typed := strings.Join(homeCardNow(t, a), "\n")
	if !strings.Contains(typed, WorkspaceGoneWord) {
		t.Fatalf("the drop-up's card never said %q:\n%s", WorkspaceGoneWord, typed)
	}
	// AND THAT CARD'S LEGEND NAMES ONLY KEYS THAT WORK.
	for _, dead := range []string{"enter open", "ctrl+t new chat here", "ctrl+o open folder"} {
		if strings.Contains(typed, dead) {
			t.Fatalf("the card still offered %q for a folder that is gone:\n%s", dead, typed)
		}
	}
	for _, alive := range []string{"ctrl+y copy path", "→ more"} {
		if !strings.Contains(typed, alive) {
			t.Fatalf("the card lost %q, which needs no folder:\n%s", alive, typed)
		}
	}
}

// A project that is still on the disk is untouched by any of it — no mark on the
// resting card's place line, none on the drop-up's row, and the ordinary legend
// whole.
func TestHomeLeavesARowWhoseFolderIsThereAlone(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", here, now.Add(-time.Hour))

	a := lab.app(mine)
	a.width, a.height = 200, 30
	a.openHome()

	if strings.Contains(homeText(a), homeGoneShort) {
		t.Fatalf("a folder that is there was marked gone:\n%s", homeText(a))
	}
	if card := strings.Join(homeCardFor(t, a, mine), "\n"); strings.Contains(card, WorkspaceGoneWord) {
		t.Fatalf("a folder that is there was called gone:\n%s", card)
	}
	// AND THE DOORS ARE ALL STILL OFFERED. The resting card names them in words
	// on the strip's own line ([app.homeCardVerbs]) and the drop-up's card names
	// them with their keys, so both spellings are read: the first from the verbs
	// the row actually answers to, the second off the legend itself.
	offered := map[string]bool{}
	for _, v := range a.homeRowVerbs() {
		offered[v.word] = true
	}
	for _, word := range []string{"new chat here", "open folder", "copy path"} {
		if !offered[word] {
			t.Fatalf("the row lost the verb %q: %v", word, offered)
		}
	}
	for _, r := range "porting" {
		a.homeKey(key(string(r)))
	}
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	card := strings.Join(homeCardNow(t, a), "\n")
	for _, clause := range []string{"enter open", "ctrl+t new chat here", "ctrl+o open folder"} {
		if !strings.Contains(card, clause) {
			t.Fatalf("the ordinary legend lost %q:\n%s", clause, card)
		}
	}
}

// THE DISK IS ASKED ONCE PER READING AND NEVER ONCE PER FRAME. The column and
// the card beside it are repainted on every keystroke and every pointer
// movement; a stat from the draw would be thousands a second to re-learn
// something that changes about as often as a repository is deleted.
func TestHomeStatsAFolderOncePerReadingAndNotPerFrame(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", lab.project("-tmp-alpha"), now)
	lab.session("-tmp-alpha", "aaaa000000000002", "two", lab.project("-tmp-alpha"), now.Add(-time.Minute))
	gone := lab.session("-tmp-gone", "cccc000000000001", "moved",
		filepath.Join(lab.root, "no-such-repository"), now.Add(-time.Hour))

	was := homeFolderThere
	asked := map[string]int{}
	homeFolderThere = func(where string) bool {
		asked[where]++
		return was(where)
	}
	t.Cleanup(func() { homeFolderThere = was })

	a := lab.app(mine)
	a.openHome()
	// ONE PER PROJECT DIRECTORY, not one per conversation: two conversations in
	// -tmp-alpha carry the same recorded folder and are one syscall between them.
	opened := map[string]int{}
	for where, count := range asked {
		opened[where] = count
	}
	for where, count := range opened {
		if count != 1 {
			t.Fatalf("the reading statted %q %d times", where, count)
		}
	}
	if len(opened) == 0 {
		t.Fatal("the reading statted nothing at all")
	}

	// Now draw the screen many times over, with the cursor on the gone row and
	// on a live one, and nothing more may be asked of the disk.
	a.home.point(gone)
	for i := 0; i < 20; i++ {
		homeText(a)
		homeCardFor(t, a, gone)
		homeCardFor(t, a, mine)
	}
	for where, count := range asked {
		if count != opened[where] {
			t.Fatalf("painting statted %q %d more times", where, count-opened[where])
		}
	}
}

// Every sentence typed at home opens a conversation OF ITS OWN, however many are
// already open, and each one is sent its own sentence and nobody else's.
//
// THE DEFECT THIS CLOSES, in the owner's own words: "whenever I create a new
// chat, it seems to go into the same chat instead of creating a new one". Home
// closed itself, asked [app.renew] for a conversation, and sent the sentence
// whether or not one came back — so from the eighth conversation onward, where
// the keeper used to refuse another, every new chat typed at home was delivered
// to the conversation that was already on the screen. The same one, every time,
// with the refusal noted underneath it.
//
// THE CAP ITSELF IS GONE (keeper.go, owner's ruling 2026-08-31), so the count
// here deliberately runs well past the eight that used to be the whole of the
// defect: the ninth sentence and the twentieth get conversations of their own
// exactly as the first did.
func TestHomeTypingOpensItsOwnConversationEveryTime(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	var made []*fakeAgent
	a.start = func(string) (Conversation, error) {
		next := &fakeAgent{model: "m"}
		made = append(made, next)
		return Conversation{Agent: next,
			SessionFile: fmt.Sprintf("/tmp/alpha/next-%d/transcript.jsonl", len(made))}, nil
	}
	// One round of what a person does: open home, type a sentence, press enter.
	say := func(text string) {
		a.openHome()
		for _, r := range text {
			a.homeKey(key(string(r)))
		}
		runCmd(a.homeEnter())
	}
	// EVERY SENTENCE GETS ITS OWN CONVERSATION, twenty of them. The first
	// replaces the fresh empty one this window opened on; every one after it is
	// added beside what is already running, and none of them is refused.
	const sentences = 20
	for i := 0; i < sentences; i++ {
		say(fmt.Sprintf("message %d", i))
	}
	if len(made) != sentences {
		t.Fatalf("%d sentences opened %d conversations", sentences, len(made))
	}
	for i, agent := range made {
		want := fmt.Sprintf("message %d", i)
		if len(agent.sent) != 1 || agent.sent[0] != want {
			t.Fatalf("conversation %d was sent %v, not %q alone", i, agent.sent, want)
		}
	}
	// AND EVERY ONE OF THEM IS A DIFFERENT CONVERSATION. Two sentences landing
	// on one agent is the defect this test exists for, so identity is asserted
	// rather than inferred from the count.
	seen := map[*fakeAgent]bool{}
	for i, agent := range made {
		if seen[agent] {
			t.Fatalf("conversation %d was the same agent as an earlier one", i)
		}
		seen[agent] = true
	}
	// The window is holding all twenty, and the twentieth is the one in front.
	if got := a.openCount(); got != sentences {
		t.Fatalf("the window holds %d conversations after %d sentences", got, sentences)
	}
	// AND HOME IS OUT OF THE WAY EACH TIME, because nothing refused. A refusal
	// would have left home standing with its own sentence on it.
	if a.at(pageHome) {
		t.Fatal("home stayed up after a conversation opened")
	}
	if a.home.msg != "" {
		t.Fatalf("home said %q about a conversation that opened", a.home.msg)
	}
}

// Typing anything that is not a search is the start of a new conversation.
func TestHomeTypingStartsANewConversationAndSendsIt(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
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
	if a.at(pageHome) {
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
	openHomeOn(a, mine)
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
	if cmd := a.homeBeat(a.homeGen); cmd == nil {
		t.Fatal("a beat on an open home did not ask for the next one")
	}
	a.closeHome()
	if cmd := a.homeBeat(a.homeGen); cmd != nil {
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
	if !a.at(pageHome) {
		t.Fatal("a bare launch did not open on home")
	}
	// THE FRAME IS RECOGNISED BY HOME'S OWN FOOT, WHICH IS NOW THE DESIGN'S
	// SENTENCE. It used to be recognised by `esc close`, and that clause is not
	// on the resting foot any more: SCREEN 1a spells the whole line as four keys
	// — `type to search or start something new · ↑↓ pick · enter open · tab next
	// place` — and the owner ordered the design followed exactly (FIDELITY.md
	// item 3). The old law ("every hint this screen draws ends with `esc`,
	// because the way out is the first thing a person looks for") died on the
	// RESTING row only; [TestHomesRestingFootIsTheDesignsSentence] pins both
	// halves of what replaced it.
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), homeRestHint) {
		t.Fatalf("the first frame is not home:\n%s", ansi.Strip(frame))
	}
	// AND THE CURSOR IS VISIBLY ON THE CONVERSATION THE DOOR PICKED: home opens
	// with the selection on screen — the row esc drops back into — so the first
	// frame answers "where am I" before a key is pressed (homebridge.go's
	// [homeView.openAt]). One ↑ off the top of the list reaches the tab bar.
	if got := homeName(a.home.focused()); got != "The One the Door Picked" {
		t.Fatalf("a greeted launch opened on %q, want the door's own conversation", got)
	}
}

// A GREETING NEEDS SOMEWHERE ELSE TO GO. A machine whose only conversation is
// the one this launch opened is not greeted by home — that is [app.landHome]'s
// third condition, and it is unchanged by the door being open: being greeted
// and being able to go there are two questions ([app.homeDoorOpen]).
func TestAFirstRunGoesStraightToTheChat(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the only one", "/tmp/alpha", time.Now())

	a := lab.launch(mine, true)
	if a.at(pageHome) {
		t.Fatal("home greeted a machine with nowhere else to go")
	}
	// And the welcome box is untouched: a first run gets the greeting it always
	// got.
	if !a.welcome.open {
		t.Fatal("the welcome box did not open on a launch home stayed out of")
	}
	// But home is one gesture away all the same.
	if !a.homeDoorOpen() {
		t.Fatal("a first run that was not greeted has no door to home")
	}
}

// A machine with no conversations at all is the same case one step earlier.
func TestAnEmptyMachineGoesStraightToTheChat(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.launch("", true)
	if a.at(pageHome) {
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
	if a.at(pageHome) {
		t.Fatal("home greeted a launch that named its conversation")
	}

	// And `aforge resume` is already greeting them with its picker.
	a = lab.app(mine)
	a.landing, a.pickSession = true, true
	a.landHome()
	if a.at(pageHome) {
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
	if a.at(pageHome) {
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
	if a.at(pageHome) {
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
	// The landing opens at rest, so this is a person walking into the list and
	// pressing enter on the row they were already in ([openHomeOn]).
	a.home.point(mine)
	before := len(a.entries)
	a.homeEnter()
	if a.at(pageHome) {
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
	// The sentence stands in the places column now, a clause to a row
	// ([homeEmptyLines]), so it is looked for clause by clause.
	for _, part := range homeEmptyLines() {
		if !strings.Contains(homeText(a), part) {
			t.Fatalf("an empty machine does not say so (%q):\n%s", part, homeText(a))
		}
	}
}

// Over --host home lists THE MACHINE THE SESSION RUNS ON, and not one row of
// this laptop's is on it.
//
// THIS TEST HAS BEEN THREE TESTS. It pinned a refusal — the door shut, the
// screen not raised. Then it pinned one honest sentence where the rows would
// have been, because every place opens and home's own state root belonged to the
// wrong machine. It now pins the repair: the world crosses the wire
// (internal/remote's Places.World) and home draws the far machine's projects,
// with that machine's name at the right end of the tab bar.
func TestHomeOverHostListsTheFarMachine(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	lab.session("-alpha", "aaaa000000000001", "porting the picker", lab.workspace("alpha"), now)
	a := lab.app("")
	a.host = "box"
	// The world the ENGINE would have answered with — one project this laptop
	// has never heard of, exactly as it arrives off the wire.
	a.world = func() (session.World, bool) {
		return session.World{
			Read: now,
			Projects: []session.Project{{
				Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
				Sessions: []session.SessionRow{{
					ID: "bbbb000000000002", Dir: "/srv/.aforge/v3/projects/-srv-code-api/bbbb000000000002",
					Transcript: "/srv/.aforge/v3/projects/-srv-code-api/bbbb000000000002/transcript.jsonl",
					Title:      "rewriting the importer", Project: "api", ProjectDir: "-srv-code-api",
					Workspace: "/srv/code/api", At: now, Created: now,
				}},
			}},
		}, true
	}
	if !a.homeDoorOpen() {
		t.Fatal("the door to home is shut over --host")
	}
	a.openHome()
	if !a.at(pageHome) {
		t.Fatal("home did not open over --host")
	}
	text := homeText(a)
	if !strings.Contains(strings.ToLower(text), "rewriting the importer") {
		t.Fatalf("home over --host did not list the far machine's work:\n%s", text)
	}
	// AND NOT ONE OF ITS ROWS IS MARKED GONE. The folder is on the other machine
	// and this process cannot see it; a stat here would report every remote row
	// as deleted ([homeView.readGone]).
	if strings.Contains(text, homeGoneWord) {
		t.Fatalf("home over --host statted the far machine's paths on this disk:\n%s", text)
	}
	// AND NOT ONE OF THIS MACHINE'S CONVERSATIONS IS ON IT.
	if strings.Contains(strings.ToLower(text), "porting the picker") {
		t.Fatalf("home over --host listed this machine's projects:\n%s", text)
	}
}

// And a world the far machine has not answered yet is NOT an empty machine.
// `nothing here yet — say something and this fills up` over a server full of
// work is the one wrong sentence this screen can say about somebody else's disk,
// so what is drawn in that moment is nothing at all.
func TestHomeDrawsNothingUntilTheFarMachineHasAnswered(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.host = "box"
	a.world = func() (session.World, bool) { return session.World{}, false }
	a.openHome()
	text := homeText(a)
	for _, part := range homeEmptyLines() {
		if strings.Contains(text, part) {
			t.Fatalf("home called an unanswered machine empty (%q):\n%s", part, text)
		}
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

// door is a surface sitting in a conversation, launched the way [newApp]
// launches one, so the door at the foot is in whatever state a real launch
// leaves it. It is [homeLab.app] plus the landing — which no longer changes the
// door at all ([app.homeDoorOpen]), and is kept here so these tests stay true to
// the order a real launch runs in.
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
	if a.at(pageHome) {
		t.Fatal("one space opened home")
	}
	a.key(key(" "))
	if !a.at(pageHome) {
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
	if a.at(pageHome) {
		t.Fatal("typing a space and a letter opened home")
	}
	// And a space in a box that already has words in it is just a space.
	a.key(key(" "))
	a.key(key(" "))
	if a.at(pageHome) {
		t.Fatal("the gesture fired in a box that had text in it")
	}
	if got := a.input.String(); got != " x  " {
		t.Fatalf("the box holds %q", got)
	}
}

// A BOX THAT SHOWS NOTHING IS A BOX THE GESTURE ANSWERS. This is the bug the
// owner hit: `ctrl+enter` and `shift+enter` arrive as a bare `ctrl+j` on every
// terminal that cannot disambiguate them, and input.go spends `ctrl+j` on
// opening a line — so the two chords the steer wave taught left a NEWLINE in a
// box that had nothing in it. Nothing on the screen changed: [editor.empty]
// calls a whitespace-only draft empty, so the foot went on advertising
// `space space home`, and the gesture — which asked for exactly one space and
// found "\n " — never fired again. Worse, [writeDraft] kept that draft on disk
// and the next window on the directory ADOPTED it, so the door stayed dead
// across restarts.
func TestDoubleSpaceGoesHomeFromABoxThatShowsNothingButHoldsANewline(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	// ctrl+j is the key a terminal sends for both of the enter chords it cannot
	// spell, and over an empty box it opens a line.
	a.key(key("ctrl+j"))
	if got := a.input.String(); got != "\n" {
		t.Fatalf("ctrl+j left %q in the box, want a newline", got)
	}
	if !a.homeDoorShowing() {
		t.Fatal("the foot stopped advertising the door, so the test is no longer about the bug")
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatalf("two spaces did not open home from a box holding %q", a.input.String())
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the gesture left %q behind in the box", got)
	}
}

// AND THE LAW IN ONE SENTENCE: WHEREVER THE DOOR IS ADVERTISED, TWO SPACES OPEN
// IT. The advertisement and the gesture used to ask different questions about
// the same box — one whitespace-insensitive, one demanding exactly one space —
// and every draft the two disagreed about was a door drawn over a gesture that
// could not fire.
func TestEveryBoxTheFootCallsEmptyAnswersTheDoubleSpace(t *testing.T) {
	for _, held := range []string{"", " ", "  ", "\n", "\n\n", "\n  ", " \n", "\t"} {
		lab := newHomeLab(t)
		now := time.Now()
		mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
		lab.session("-tmp-alpha", "aaaa000000000002", "elsewhere", "/tmp/alpha", now.Add(-time.Hour))

		a := lab.door(mine)
		a.input.setText(held)
		if !a.homeDoorShowing() {
			t.Fatalf("a box holding %q is not advertising the door", held)
		}
		a.key(key(" "))
		a.key(key(" "))
		if !a.at(pageHome) {
			t.Errorf("a box holding %q advertised the door and refused the gesture", held)
		}
	}
}

// AND THE CARET IS WHAT "THE SPACE YOU JUST TYPED" MEANS. A space typed at the
// FRONT of a box holding a newline is behind the caret exactly as one typed at
// the back is, so the gesture fires either way — it is the same two keystrokes
// against the same blank-looking box.
func TestTheGestureReadsTheSpaceBehindTheCaretAndNotTheEndOfTheDraft(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "elsewhere", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.input.setText("\n")
	// Straight onto the caret: [editor.home] is line-relative, and the line this
	// draft ends on is the empty one after the break.
	a.input.cursor = 0
	a.key(key(" "))
	if got := a.input.String(); got != " \n" {
		t.Fatalf("the first space landed as %q", got)
	}
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("two spaces at the front of a blank-looking box did not open home")
	}
}

// AND A DRAFT WITH WORDS IN IT IS STILL A DRAFT. The widened gesture may not
// reach past the one thing it was always forbidden to touch: a sentence.
func TestTheGestureStillRefusesABoxWithWordsInIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "elsewhere", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.input.setText("indent this line:\n")
	a.key(key(" "))
	a.key(key(" "))
	if a.at(pageHome) {
		t.Fatal("two spaces on a new line of a real draft opened home")
	}
	if got := a.input.String(); got != "indent this line:\n  " {
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
	if a.at(pageHome) {
		t.Fatal("a paste beginning with two spaces opened home")
	}
	if got := a.input.String(); got != "  indented like code" {
		t.Fatalf("the paste landed as %q", got)
	}
}

// A PASTE WHILE HOME IS OPEN LANDS IN HOME'S OWN BOX. Home is fullscreen, so
// the chat's draft is not on the page at all — and that is exactly where a
// paste used to go, silently, which read as the paste doing nothing until home
// was closed and the text turned out to have been sitting in the chat box.
func TestAPasteWhileHomeIsOpenLandsInHomesBox(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("home did not open")
	}
	runCmd(a.paste("find the pricing thread"))
	if got := a.home.box.String(); got != "find the pricing thread" {
		t.Fatalf("home's box holds %q", got)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the paste leaked into the chat draft behind home: %q", got)
	}
}

// HOME'S BOX WRAPS A LONG DRAFT. It used to be one truncated row: type past
// the frame's edge and the tail of the sentence became an ellipsis while the
// caret pinned to the last column — typing into cells nobody could see.
func TestHomesBoxWrapsALongDraftInsteadOfTruncatingIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("home did not open")
	}
	long := "please research " + strings.Repeat("the market and ", 12) + "REPORTBACK"
	runCmd(a.paste(long))
	if got := homeText(a); !strings.Contains(got, "REPORTBACK") {
		t.Fatalf("the tail of a long draft is not on the page:\n%s", got)
	}
}

// HOME IS ALWAYS REACHABLE. This pin used to say the opposite — that the door
// was shut and the gesture inert on a machine whose only conversation was this
// one, because a door that opened on nothing should be neither drawn nor bound.
// The owner ruled the other way: being greeted by home and being able to GO
// there are two questions, and an empty home is a designed screen rather than a
// refusal ([app.homeDoorOpen]). So the reversal is deliberate and pinned here.
func TestTheDoorIsOpenWithOnlyThisConversation(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-alpha", "aaaa000000000001", "the only one", lab.workspace("alpha"), time.Now())
	a := lab.door(mine)
	if !a.homeDoorOpen() || !a.homeDoorShowing() {
		t.Fatal("the door is shut on a machine whose only conversation is this one")
	}
	if got := a.legendRight(a.width); got != homeDoorWord+" · "+microcopy {
		t.Fatalf("the hint slot reads %q on a one-conversation machine", got)
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("two spaces did not open home with only this conversation")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the gesture left %q behind in the box", got)
	}
	// And the home that opens is a full home: this project's heading, this
	// conversation's row, the foot that starts something new.
	// The row wears its title cased the way every row does ([homeName]), so
	// the look is case-blind: the claim is that the conversation is there.
	text := strings.ToLower(homeText(a))
	for _, want := range []string{"alpha", "the only one", homeFootWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("a one-conversation home is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, homeEmptyLines()[0]) {
		t.Fatalf("a home holding this conversation says it holds nothing:\n%s", text)
	}
}

// AND ON A MACHINE THAT HOLDS NOTHING AT ALL. The gesture, the advertisement
// and the click all work on the first minute of a fresh install, and what they
// open is an empty home rather than nothing.
func TestTheDoorIsOpenOnAMachineThatHoldsNothing(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	if !a.homeDoorOpen() || !a.homeDoorShowing() {
		t.Fatal("the door is shut on an empty machine")
	}
	if got := a.legendRight(a.width); got != homeDoorWord+" · "+microcopy {
		t.Fatalf("the hint slot reads %q on an empty machine", got)
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("two spaces did not open home on an empty machine")
	}
	for _, part := range homeEmptyLines() {
		if !strings.Contains(homeText(a), part) {
			t.Fatalf("an empty home does not say so (%q):\n%s", part, homeText(a))
		}
	}
}

// AN EMPTY HOME IS THE SAME SCREEN WITH FEWER ROWS. At every width tier the
// head, the foot and whatever furniture that tier draws stand where a full home
// puts them, and `nothing here yet` sits where the rows will be — so a person
// who opens home on a fresh machine sees a home, not a broken page.
func TestAnEmptyHomeKeepsItsShapeAtEveryWidth(t *testing.T) {
	lab := newHomeLab(t)
	cases := []struct {
		width int
		want  []string
	}{
		// The card tier, and the two below it. NO TIER DRAWS FURNITURE OF ITS
		// OWN ANY MORE — the strips that used to keep their labels over nothing
		// went with the tree (place_home.go), and an empty machine at every
		// width is the head, the sentence and the foot.
		{homeCardMin, nil},
		{homeSwitchFull, nil},
		{80, nil},
	}
	for _, tc := range cases {
		a := lab.app("")
		a.width, a.height = tc.width, 20
		a.openHome()
		text := homeText(a)
		// The sentence is there whole at every tier, a clause to a row, and
		// never cut to an ellipsis ([homeEmptyLines]).
		//
		// AND THE FOOT IS THE FOOT A FULL HOME DRAWS, both of its rows: the box
		// row every place shares (SCREEN 2b) and home's own resting hint (SCREEN
		// 1a). `esc close` was asked for here and is gone from the resting foot —
		// the design's own foot does not name it (FIDELITY.md item 3) — so what is
		// demanded instead is the pair of sentences the design does spell, which
		// is a stricter claim than the two fragments this asked for before.
		want := append(append(tc.want, homeEmptyLines()...), "› "+placeRestWord, homeRestHint)
		for _, want := range want {
			if !strings.Contains(text, want) {
				t.Fatalf("at %d columns an empty home is missing %q:\n%s", tc.width, want, text)
			}
		}
		// AND AN EMPTY HOME HAS NO ROW TO STAND ON, which is the emptiness law
		// rather than a broken cursor: the one line it draws is the sentence
		// saying the machine is empty, and that names an absence rather than a
		// thing to open ([homeEmptyRow]). `↑` from there reaches the tab bar,
		// which is the whole of what this screen has to offer onward.
		if len(placeHome{}.stops(a)) != 0 {
			t.Fatalf("at %d columns an empty home offered a row to stand on", tc.width)
		}
		// The arrows have nothing to land on and must not land on the furniture.
		drive(t, a, key("down"))
		drive(t, a, key("down"))
		if line, ok := a.home.focusedLine(); ok && !line.stop() {
			t.Fatalf("at %d columns the cursor landed on furniture of kind %v", tc.width, line.kind)
		}
		// And the box is live: typing offers a new conversation, exactly as a
		// full home does.
		for _, r := range "pricing" {
			drive(t, a, key(string(r)))
		}
		if !strings.Contains(homeText(a), homeStartWord+`: "pricing"`) {
			t.Fatalf("at %d columns typing on an empty home does not offer a new conversation:\n%s", tc.width, homeText(a))
		}
	}
}

// THE SCREEN IS NEVER EMPTIER THAN THE MACHINE. A fresh launch's folder holds a
// meta.json nobody has spoken into and no transcript yet, which the world walk
// skips on purpose — but the window sitting in it is real, so home puts its row
// back under its project ([app.readWorld], [session.World.Adopt]).
func TestAFreshConversationTheWalkCannotSeeStillHasARow(t *testing.T) {
	lab := newHomeLab(t)
	alpha := lab.workspace("alpha")
	dir := filepath.Join(lab.project("-alpha"), "aaaa000000000001")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: "aaaa000000000001", Workspace: alpha, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "transcript.jsonl")
	if world := session.ReadWorld(lab.root); len(world.Projects) != 0 {
		t.Fatalf("the walk found %d projects in a folder nobody has spoken in", len(world.Projects))
	}
	a := lab.app(mine)
	a.workspace, a.title = alpha, "first thing"
	a.openHome()
	// The row wears the title the way every row does ([homeName] cases it), so
	// the comparison is case-blind: the claim is that the title is there.
	text := strings.ToLower(homeText(a))
	for _, want := range []string{"alpha", "first thing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("home opened from a fresh conversation does not list it (%q):\n%s", want, text)
		}
	}
	if strings.Contains(text, homeEmptyLines()[0]) {
		t.Fatalf("home says nothing is here while this conversation is:\n%s", text)
	}
	a.home.point(mine)
	if got := a.home.focused().Transcript; got != mine {
		t.Fatalf("the cursor cannot reach the row this window is in: on %q", got)
	}
	// AND THE ADOPTION IS NOT A DUPLICATE. Once the walk can see the
	// conversation, the world holds it once.
	seen := lab.session("-alpha", "bbbb000000000001", "spoken in", alpha, time.Now())
	b := lab.app(seen)
	b.openHome()
	rows := 0
	for _, project := range b.home.world.Projects {
		for _, row := range project.Sessions {
			if row.Transcript == seen {
				rows++
			}
		}
	}
	if rows != 1 {
		t.Fatalf("a conversation the walk found is listed %d times", rows)
	}
}

// AND IT INVENTS NOTHING OUTSIDE THE ROOT. A journal that is not a session
// folder's transcript two levels under the places root — a memory-only surface,
// a fixture standing elsewhere — is not adopted, because the world answers for
// the root alone.
func TestAdoptInventsNothingOutsideTheRoot(t *testing.T) {
	lab := newHomeLab(t)
	world := session.ReadWorld(lab.root)
	for _, file := range []string{
		filepath.Join(t.TempDir(), "next", "transcript.jsonl"),
		filepath.Join(lab.root, "-alpha", "transcript.jsonl"),
		filepath.Join(lab.root, "-alpha", "aaaa000000000001", "notes.txt"),
		"",
	} {
		if world.Adopt(lab.root, session.SessionRow{Transcript: file}, time.Now()) {
			t.Fatalf("adopted %q, which is not a session under the root", file)
		}
	}
	if len(world.Projects) != 0 {
		t.Fatalf("the world grew %d projects from journals outside it", len(world.Projects))
	}
}

// …AND IT STAYS OPEN WHEN THIS WINDOW STARTS A SECOND CONVERSATION. The door
// used to be a cached fact written at the launch's own walk, and a launch that
// found one conversation shut it for the rest of the session: the first /new
// made "somewhere else" true and nothing ever asked again. The fact is gone
// ([app.homeDoorOpen] asks nothing about the machine), so this pins the old
// defect from the other side — the surface moves onto another conversation and
// the door is exactly where it was.
func TestTheDoorOpensWhenThisWindowStartsASecondConversation(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the only one", "/tmp/alpha", time.Now())
	a := lab.door(mine)
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut on a machine with only this conversation")
	}

	// /new: another conversation in this project, which leaves the one the
	// launch opened behind as somewhere to go back to (app.go's [app.renew]).
	renewed, started := a.renew()
	if !started {
		t.Fatal("/new refused to open a second conversation")
	}
	runCmd(renewed)
	if a.file == mine {
		t.Fatal("/new did not move the surface onto another conversation")
	}
	if !a.homeDoorOpen() {
		t.Fatal("the door shut when this window started a second conversation")
	}
	if !a.homeDoorShowing() {
		t.Fatal("the door works and is not advertised")
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("the gesture did not open home")
	}
}

// …AND A WALK THAT COULD NOT BE TAKEN DOES NOT SHUT IT. [session.ReadWorld]
// answers an empty world both for a machine holding nothing and for a walk that
// failed, and home's own tick believing the second one used to shut the door for
// the rest of the session. The door no longer reads the world at all, so this
// pins that a refresh over a vanished root leaves home closable and the door
// standing.
func TestAReadingOfNothingDoesNotShutTheDoor(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.door(mine)
	runCmd(a.openHome())
	// The root goes out from under the walk, which is what a bucket being
	// groomed or a descriptor the process could not get looks like from here.
	a.homeRoot = filepath.Join(t.TempDir(), "gone")
	a.refreshHome()
	a.closeHome()
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut after home closed on a reading of nothing")
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("the gesture did not open home")
	}
}

// A LAUNCH THAT IS NOT BEING GREETED DOES NOT WALK THE DISK TO GET ITS FIRST
// FRAME UP. [app.landHome] runs inside [newApp], before bubbletea exists, and
// the walk under the places root is four system calls per session across every
// project on the machine. So a launch that named a conversation — `--session`,
// `aforge resume`, `--once`, every headless frame — reads nothing at all: the
// door at the foot of the conversation stopped depending on what the disk holds
// ([app.homeDoorOpen]), so there is no question left for the launch to answer.
func TestALaunchThatIsNotGreetedNeverWalksTheDiskForTheDoor(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "somewhere else", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.landHome()
	if len(a.home.world.Projects) != 0 {
		t.Fatal("the launch walked the disk for a door nothing was waiting on")
	}
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut on a launch that read nothing")
	}

	// AND A GREETED LAUNCH TAKES THE WALK EXACTLY ONCE, because the frame it is
	// about to draw IS home and every row on it comes out of that reading.
	greeted := lab.launch(mine, true)
	if !greeted.at(pageHome) {
		t.Fatal("the landing launch was not greeted, so this proves nothing")
	}
	if len(greeted.home.world.Projects) == 0 {
		t.Fatal("the greeted launch drew home off a reading of nothing")
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
	if !a.at(pageHome) {
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
	if !a.at(pageHome) {
		t.Fatal("the launch did not land on home")
	}
	// The landing opens at rest and enter has nothing to open there, so the trip
	// starts where the first ↓ would leave it: on this window's own row.
	a.home.point(mine)
	a.homeEnter()
	if a.at(pageHome) {
		t.Fatal("enter did not step into the conversation")
	}
	if a.file != mine {
		t.Fatalf("enter landed in %q", a.file)
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("the gesture did not go back home")
	}
	a.homeKey(key("esc"))
	if a.at(pageHome) || a.file != mine {
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
	if !a.at(pageHome) {
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
	if err := filelock.Lock(file, true, true); err != nil {
		file.Close()
		l.t.Fatalf("could not hold %s: %v", transcript, err)
	}
	l.t.Cleanup(func() {
		filelock.Unlock(file)
		file.Close()
	})
}

// THE SCREEN SAYS SO BEFORE THE ROW IS PRESSED. This is the half of the trap
// that made a locked door look like every other row.
//
// IT IS THE SAME LAW IN THE TWO PLACES THE SHAPE LEFT FOR IT. The resting list
// is switcher.go's pure reading, which is handed no lock table and cannot ask
// one — so the fact rides the card's place line beside the address it is about
// ([app.homeCardPlace] reads it through [app.homeHolding]) — and the drop-up's
// rows, built by [homeView.buildWorld], still carry [homeHeldShort] in the width
// the column has for it. Both are asserted, so neither spelling can go quietly.
func TestALockedRowSaysSoOnItsCardAndOnItsRowWhenTyped(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", here, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", here, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.width, a.height = 200, 30
	a.openHome()
	card := homeCardFor(t, a, theirs)
	if at := cardLine(card, homeHeldWord); at != homeCardPlaceRow {
		t.Fatalf("the card says it on line %d, want the place line:\n%s", at, strings.Join(card, "\n"))
	}
	if !strings.Contains(homeText(a), homeHeldWord) {
		t.Fatalf("the frame does not say the conversation is open elsewhere:\n%s", homeText(a))
	}

	// AND THE DROP-UP'S ROW SAYS IT IN THE SHORT WORDS.
	for _, r := range "other" {
		a.homeKey(key(string(r)))
	}
	if !strings.Contains(homeText(a), homeHeldShort) {
		t.Fatalf("the drop-up's row does not say the row is held:\n%s", homeText(a))
	}
}

// ENTER OFFERS RATHER THAN REFUSING, keeps home open, and never touches the
// conversation underneath. The row another terminal is holding is the row a
// person most often wants; what it costs is said before anything is done about
// it (takeover.go).
func TestEnterOnALockedRowOffersToMoveItInHomesOwnVoice(t *testing.T) {
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
	if !a.at(pageHome) {
		t.Fatal("the offer closed home")
	}
	if a.file != mine {
		t.Fatalf("the offer moved this window to %q", a.file)
	}
	if !strings.Contains(a.home.msg, "enter again to move it here") {
		t.Fatalf("home said %q", a.home.msg)
	}
	if len(a.entries) != before {
		t.Fatalf("the offer was written into the conversation: %v", a.entries[before:])
	}
	// AND IT STILL NAMES NO PATH. The whole original defect was sixty characters
	// of somebody else's bookkeeping wrapped across two lines.
	for _, banned := range []string{"transcript.jsonl", "resume failed", "aforge/v3"} {
		if strings.Contains(homeText(a), banned) {
			t.Fatalf("the line leaked %q:\n%s", banned, homeText(a))
		}
	}
}

// A SECOND PRESS ASKS, AND A THIRD ASKS NOTHING MORE. The line lives in home's
// own foot and is replaced, where a note in the conversation would have stacked
// — and the request itself is one file, written once.
func TestPressingEnterOverAndOverOnAHeldRowAsksOnce(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
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
	if !a.waitingToTakeOver() {
		t.Fatal("three presses left the window waiting for nothing")
	}
	if got := strings.Count(homeText(a), "moving it here"); got != 1 {
		t.Fatalf("the moving line is on the screen %d times", got)
	}
}

// The lock can appear between the scan and the keystroke, so the open itself
// can still lose. It loses in the same words, in the same place.
func TestTheRaceLosesInTheSameWordsNotARawError(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))

	a := lab.app(mine)
	// The door answers the way the engine does when it meets the flock, which
	// is the state a lock taken microseconds ago leaves the surface in.
	a.open = func(_, file string) (Conversation, error) {
		return Conversation{}, &session.SessionLockedError{Path: file}
	}
	a.openHome()
	a.home.point(theirs)
	before := len(a.entries)

	a.homeKey(key("enter"))
	if !a.at(pageHome) {
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
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))

	a := lab.app(mine)
	held := a.agent.(*fakeAgent)
	a.open = func(_, file string) (Conversation, error) {
		return Conversation{}, &session.SessionLockedError{Path: file}
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
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	free := lab.session("-tmp-alpha", "aaaa000000000002", "nobody has this one", where, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	a.home.point(free)
	if strings.Contains(homeText(a), homeHeldShort) {
		t.Fatalf("an unheld row was drawn as held:\n%s", homeText(a))
	}
	a.homeKey(key("enter"))
	if a.at(pageHome) {
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
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))

	a := lab.app(mine)
	a.open = func(_, file string) (Conversation, error) {
		return Conversation{}, &session.SessionLockedError{Path: file}
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
// so the place line under a conversation's name is the fastest route to the
// project it belongs to. It is a full-frame surface and does not pass through
// the conversation's row pass, which is why the link is hung here by hand.
//
// THE ANCHOR COVERS THE WHOLE PLACE LINE NOW and not the path half of it: the
// address and what is true about it are one reading, and a link that stopped at
// the first clause would be a target a narrow card had already cut off
// ([app.homeCardPlace]). The line only exists on a card, so this is asked at a
// card width.
func TestHomeLinksTheProjectDirectoryItNames(t *testing.T) {
	lab := newHomeLab(t)
	// A workspace that is REALLY THERE, because a path that is not found on
	// disk is drawn plain and this test would then be asserting nothing.
	workspace := t.TempDir()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the picker", workspace, time.Now())

	a := lab.app(mine)
	a.width, a.height = 200, 24
	openHomeOn(a, mine)

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

// ── one list, and nothing folded away from it ───────────────────────────────

// homeTierLab is six projects with one conversation each, THIS WINDOW STANDING
// IN THE OLDEST OF THEM — so that "the window's own conversation is the one
// marked here" cannot be mistaken for "the most recent one is".
func homeTierLab(t *testing.T) (*app, *homeLab, string) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	var mine string
	for i, name := range []string{"alpha", "beta", "gamma", "delta", "eps", "zeta"} {
		file := lab.session("-tmp-"+name, strings.Repeat(string(rune('a'+i)), 4)+"000000000001",
			name+" chat", "/tmp/"+name, now.Add(-time.Duration(i+1)*time.Hour))
		if name == "zeta" {
			mine = file
		}
	}
	a := lab.app(mine)
	a.width, a.height = 100, 40
	openHomeOn(a, mine)
	return a, lab, mine
}

// homeKinds is every line of the column, in order, as (kind, project) pairs.
func homeKinds(a *app, kind homeRowKind) []string {
	var out []string
	for _, line := range a.home.lines {
		if line.kind == kind {
			out = append(out, line.project)
		}
	}
	return out
}

// NOTHING IS FOLDED AWAY BY BEING IN THE WRONG PROJECT.
//
// The list used to be two tiers: three projects drawn open with their
// conversations under them, and every other project on the machine one line
// each under a dim `elsewhere` rule. That shape bought five rows of scaffolding
// to reach twenty leaves, and it could put a conversation that needs a person
// three folds down under six projects nobody has touched.
//
// THE LAW THOSE TIERS WERE PROTECTING IS THE ONE THIS TEST NOW PINS: what wants
// you is reachable without opening anything. There is one flat ranked list, the
// project is a tag on the row, and the only thing folded is the quiet tail past
// the eighth row (switcher.go, place_home.go).
func TestHomeDrawsOneListWithNoProjectTierAndNoElsewhereBlock(t *testing.T) {
	a, _, mine := homeTierLab(t)
	if headings := homeKinds(a, homeHeading); len(headings) != 0 {
		t.Fatalf("the resting list still draws project headings: %v\n%s", headings, homeText(a))
	}
	if folded := homeKinds(a, homeProject); len(folded) != 0 {
		t.Fatalf("the resting list still folds projects away: %v\n%s", folded, homeText(a))
	}
	text := homeText(a)
	if strings.Contains(text, homeElsewhereWord) {
		t.Fatalf("the elsewhere block survived:\n%s", text)
	}
	// EVERY PROJECT'S CONVERSATION IS ON THE ONE LIST, each wearing the project
	// it came from — six projects, six rows, no door between them.
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "eps", "zeta"} {
		if !strings.Contains(text, strings.ToUpper(name[:1])+name[1:]+" Chat") {
			t.Fatalf("%s's conversation is not on the list:\n%s", name, text)
		}
	}
	rows := 0
	for _, line := range a.home.lines {
		if line.kind != homeSession || line.sw == nil {
			continue
		}
		rows++
		if line.project == "" {
			t.Fatalf("a row lost the project it came from:\n%s", text)
		}
	}
	if rows != 6 {
		t.Fatalf("the list drew %d conversations, and the machine holds six:\n%s", rows, text)
	}
	// AND THIS WINDOW'S OWN CONVERSATION IS THE ONE MARKED, wherever the rank
	// happens to put it — which is what the tiers' "your project first" rule was
	// really for.
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == mine {
			if line.sw == nil || !line.sw.row.here {
				t.Fatalf("this window's own conversation does not wear `here`:\n%s", text)
			}
			return
		}
	}
	t.Fatalf("this window's own conversation is not on the list:\n%s", text)
}

// THE LIST NEVER TOUCHES THE RULE ABOVE THE BOX: one blank row, at every
// height, in both of home's shapes.
func TestTheListIsPaddedOffTheFoot(t *testing.T) {
	a, _, _ := homeTierLab(t)
	for _, typed := range []bool{false, true} {
		if typed {
			a.homeKey(key("c"))
			a.homeKey(key("h"))
		}
		for _, height := range []int{8, 12, 24, 40, 60} {
			a.width, a.height = 100, height
			width, h := a.size()
			lines, _, _, _ := a.homeFrame(width, h)
			// The foot is the rule, the box and the hint; the row above it is the
			// padding, and it is empty whatever the list did.
			pad := len(lines) - 4
			if got := strings.TrimSpace(ansi.Strip(lines[pad])); got != "" {
				t.Fatalf("at height %d (typed %v) the list touches the foot: row %d is %q\n%s",
					height, typed, pad, got, strings.Join(lines, "\n"))
			}
		}
	}
}

// THE CARET STANDS IN THE DRAFT, HOWEVER TALL THE DRAFT IS. The foot's budget
// was once a constant that assumed a one-row box, so a question long enough to
// wrap pushed the frame past the window, the tail-clamp slid every row up, and
// the terminal's cursor — computed before the slide — blinked on the hint line
// under the box. The list must give up the rows a wrapping draft takes, and
// the caret must sit on the row that holds the end of what was typed.
func TestHomeCaretStaysInTheDraftWhenItWraps(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", now)
	// Enough conversations that the list fills every row it is given: the bug
	// only fired when the body had no slack to absorb the draft's extra rows.
	for i := 0; i < 30; i++ {
		lab.session("-tmp-alpha", fmt.Sprintf("aaaa%012d", i+2), fmt.Sprintf("conversation %d", i), "/tmp/alpha", now.Add(-time.Duration(i+1)*time.Minute))
	}
	a := lab.app(mine)
	a.openHome()
	a.width, a.height = 100, 20

	draft := strings.Repeat("build a highly detailed and aesthetic animated website ", 3) + "ending-word"
	a.home.box.setText(draft)

	// [app.frame] arms the caret on every render and the surfaces that have
	// nowhere to type switch it off; calling homeFrame directly starts from
	// the same armed state.
	a.caret = true
	width, height := a.size()
	lines, _, caretX, caretY := a.homeFrame(width, height)
	if len(lines) != height {
		t.Fatalf("home drew %d rows, want exactly %d", len(lines), height)
	}
	if !a.caret {
		t.Fatal("the caret is hidden while a draft is being typed")
	}
	if caretY < 0 || caretY >= len(lines) {
		t.Fatalf("caret row %d is outside the frame of %d rows", caretY, len(lines))
	}
	row := ansi.Strip(lines[caretY])
	if !strings.Contains(row, "ending-word") {
		t.Fatalf("the caret stands on row %d %q, not on the draft's last line", caretY, row)
	}
	if caretX <= ansi.StringWidth("ending-word") {
		t.Fatalf("caret column %d sits before the text it should follow", caretX)
	}
}

// PUTTING A ROW AWAY TAKES IT OFF THE LIST, AND ITS NAME BRINGS IT BACK.
//
// It used to gather under an `archive` fold at the foot of the resting list.
// The resting list is the ranked reading now and archived rows are simply not
// in it (switcher.go) — so the law the fold was protecting, that nothing a
// person put away is LOST, is kept by the box instead: typing its name finds
// it, exactly as typing any other name does, and ctrl+e there brings it back.
func TestPuttingARowAwayTakesItOffTheListAndItsNameFindsItAgain(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "keep this one", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "the junk drawer plan", "/tmp/alpha", now.Add(-time.Minute))
	a := lab.app(mine)
	a.openHome()
	a.width, a.height = 100, 30

	// Walk the cursor onto the junk row and press ctrl+e.
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeSession && strings.Contains(line.row.Title, "junk drawer") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the junk row is not on home:\n%s", homeText(a))
	}
	a.home.cursor, a.home.picked = at, true
	drive(t, a, key("ctrl+e"))

	if text := homeText(a); strings.Contains(text, "Junk Drawer") {
		t.Fatalf("the put-away row is still on the resting list:\n%s", text)
	}
	// AND THE SCREEN SAYS WHERE IT WENT. A row that vanished with no sentence
	// would be the surface hiding something on a keystroke.
	if !strings.Contains(a.home.msg, homePutAwayWord) {
		t.Fatalf("putting a row away said %q", a.home.msg)
	}

	// A search still finds it, which is the whole of the way back.
	a.home.box.setText("junk")
	a.home.build()
	back := -1
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.row.Archived {
			back = i
			break
		}
	}
	if back < 0 {
		t.Fatalf("typing its name does not find the put-away row:\n%s", homeText(a))
	}
	a.home.cursor, a.home.picked = back, true
	drive(t, a, key("ctrl+e"))
	a.home.box.reset()
	a.home.build()
	if !strings.Contains(homeText(a), "Junk Drawer") {
		t.Fatalf("ctrl+e did not bring the row back to the list:\n%s", homeText(a))
	}
}

// A BARE LETTER ALWAYS TYPES. The foot promises "type to search or start
// something new", and this screen learned twice that any gate on that promise
// is a mode: first the letter doors fired off whatever row the cursor was
// resting near, then they fired off a row somebody had merely walked onto or
// pointed at — and either way a person into "make me a site" or "one more
// thing" watched a letter act on a card instead of landing in their sentence.
// So the doors ride chords now (ctrl+e, ctrl+o, ctrl+y, ctrl+t) and the
// arrows, which can never begin a word, and a letter is a letter under every
// cursor, hover and pick this screen can be in.
func TestALetterAlwaysTypesWhateverIsChosen(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	a.width, a.height = 100, 30

	// At rest a letter types.
	drive(t, a, key("m"))
	if got := a.home.box.String(); got != "m" {
		t.Fatalf("an at-rest m did not type; the box holds %q", got)
	}
	a.home.box.reset()
	a.home.build()

	// Walked onto a row — picked, the strongest gesture there is — the chord
	// the card's legend names acts on it...
	drive(t, a, key("down"))
	if !a.home.picked {
		t.Fatal("walking onto a row did not pick it")
	}
	drive(t, a, key("ctrl+y"))
	if !strings.Contains(homeText(a), "copied") {
		t.Fatalf("ctrl+y on a picked row did not copy the path:\n%s", homeText(a))
	}

	// ...and the same letters that used to be doors still type: m, o, y, e
	// and n land in the box as the word "moyen".
	for _, letter := range []string{"m", "o", "y", "e", "n"} {
		drive(t, a, key(letter))
	}
	if got := a.home.box.String(); got != "moyen" {
		t.Fatalf("letters on a picked row did not all type; the box holds %q", got)
	}
}

// AND NOT ONE OF ITS ROWS IS RESOLVED ON THIS DISK EITHER. Every row on home is
// asked whether this process holds it ([app.homeTrue]), and the key that answers
// is a transcript path with its symlinks walked ([convKey]) — a walk of THIS
// laptop for a file on the far machine. That walk is not free: on macOS `/home`
// is an automounter's mount point, so each row's `Lstat("/home/santosh")` waited
// on autofs, and home over --host cost up to a second per frame with the keys
// queued behind it. A hosted key is the cleaned spelling and the disk is never
// asked ([app.convKey]); this counts the asking so it cannot come back.
func TestHomeOverHostNeverResolvesTheFarMachinesPathsOnThisDisk(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	a := lab.app("")
	a.host = "box"
	a.world = func() (session.World, bool) {
		return session.World{
			Read: now,
			Projects: []session.Project{{
				Dir: "-home-far-src-api", Path: "/home/far/src/api", Name: "api",
				Sessions: []session.SessionRow{{
					ID: "bbbb000000000002", Dir: "/home/far/.aforge/v3/projects/-home-far-src-api/bbbb000000000002",
					Transcript: "/home/far/.aforge/v3/projects/-home-far-src-api/bbbb000000000002/transcript.jsonl",
					Title:      "rewriting the importer", Project: "api", ProjectDir: "-home-far-src-api",
					Workspace: "/home/far/src/api", At: now, Created: now,
				}, {
					ID: "bbbb000000000003", Dir: "/home/far/.aforge/v3/projects/-home-far-src-api/bbbb000000000003",
					Transcript: "/home/far/.aforge/v3/projects/-home-far-src-api/bbbb000000000003/transcript.jsonl",
					Title:      "porting the picker", Project: "api", ProjectDir: "-home-far-src-api",
					Workspace: "/home/far/src/api", At: now, Created: now,
				}},
			}},
		}, true
	}
	walked := 0
	prior := resolveTranscript
	resolveTranscript = func(path string) (string, error) { walked++; return path, nil }
	defer func() { resolveTranscript = prior }()
	a.openHome()
	if !a.at(pageHome) {
		t.Fatal("home did not open over --host")
	}
	// At rest, and then under a query that keeps both rows on the screen — the
	// rows are what ask, so a query that matched nothing would prove nothing.
	homeText(a)
	typeInto(t, a, "importer")
	if text := homeText(a); !strings.Contains(strings.ToLower(text), "rewriting the importer") {
		t.Fatalf("the far rows were not drawn under the query:\n%s", text)
	}
	if walked != 0 {
		t.Fatalf("a hosted home walked this disk for the far machine's transcripts %d times", walked)
	}
	// And the key it does use is still one key per transcript, so the keeper's
	// lookups agree with each other: a cleaned far path keys the same way twice.
	if a.convKey("/home/far/x/../y/transcript.jsonl") != a.convKey("/home/far/y/transcript.jsonl") {
		t.Fatal("two spellings of one far transcript keyed differently")
	}
}

// ── the door from every place ───────────────────────────────────────────────
//
// THE DOOR IS UNIVERSAL. The gesture used to live past the rung where a place
// takes the whole keyboard, so `space space` only ever opened home from inside
// a conversation; these tests hold the law on the place side, one test per
// shape of room rather than one per tab: the places that type into their own
// box (tasks, memory, spend, search), the panel whose space is already a verb
// (settings), and home itself, where the door is a no-op.

// driveToPlace lands a session, then stands the person on `where` the way the
// router does — [app.showPage] — and answers the place's box.
func driveToPlace(t *testing.T, lab *homeLab, where page) (*app, *editor) {
	t.Helper()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", time.Now())
	lab.session("-tmp-alpha", "aaaa000000000002", "elsewhere", "/tmp/alpha", time.Now().Add(-time.Hour))
	a := lab.door(mine)
	if cmd := a.showPage(where); cmd == nil && !a.at(where) {
		t.Fatalf("%v did not open", where)
	}
	return a, a.placeBox()
}

// TWO SPACES IN A PLACE'S OWN EMPTY BOX GO HOME — every place that types into
// that box. The first space types itself into the place's own filter, exactly
// as it does into a conversation's draft, and the second opens home and leaves
// nothing behind in the box.
func TestDoubleSpaceFromEveryTypingPlaceGoesHome(t *testing.T) {
	for _, where := range []page{pageTasks, pageMemory, pageSpend, pageSearch} {
		lab := newHomeLab(t)
		a, box := driveToPlace(t, lab, where)
		if box == nil {
			t.Fatalf("%v has no box to type into", where)
		}
		a.key(key(" "))
		if got := box.String(); got != " " {
			t.Fatalf("%v: the first space did not type itself: %q", where, got)
		}
		if a.at(pageHome) {
			t.Fatalf("%v: one space opened home", where)
		}
		a.key(key(" "))
		if !a.at(pageHome) {
			t.Fatalf("%v: two spaces did not open home", where)
		}
		if got := box.String(); got != "" {
			t.Fatalf("%v: the gesture left %q behind in the box", where, got)
		}
	}
}

// AND ON THE PLACES THE DOOR ALREADY READS, WALKING THERE KEEPS IT WORKING.
// Standing's box is the one place box the place's own keys never type into, so
// the test drives the box to the armed state the way an earlier room leaves it
// — one space behind the caret — and holds the door open from there.
func TestDoubleSpaceFromStandingGoesHome(t *testing.T) {
	lab := newHomeLab(t)
	a, box := driveToPlace(t, lab, pageStanding)
	if box == nil {
		t.Fatal("standing has no box to type into")
	}
	box.insert(" ")
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("two spaces did not open home from the standing place")
	}
	if got := box.String(); got != "" {
		t.Fatalf("the gesture left %q behind in the box", got)
	}
}

// SPACE IS SETTINGS' OWN VERB, and the door loses to it: `activate` is what the
// panel draws space meaning on every row, and a door that swallowed the key
// under it would be a door that decided somebody's setting was activated. The
// panel's search box refuses space characters outright (settings.go), so the
// door cannot arm there at all — which is the whole of why this is honest.
func TestSpaceStaysTheVerbOnSettings(t *testing.T) {
	lab := newHomeLab(t)
	a, _ := driveToPlace(t, lab, pageSettings)
	a.key(key(" "))
	a.key(key(" "))
	if a.at(pageHome) {
		t.Fatal("the door opened home over settings, where space is a verb on a row")
	}
	if got := a.sheet.query.String(); got != "" {
		t.Fatalf("the panel's search box picked up %q", got)
	}
}

// SPACE THEN A LETTER ON A PLACE TYPES NORMALLY. The door reads the box the
// place types into and no other: a sentence aimed at a filter is nobody's way
// of asking for home.
func TestASingleSpaceThenALetterTypesNormallyOnAPlace(t *testing.T) {
	for _, where := range []page{pageTasks, pageMemory, pageSpend, pageSearch} {
		lab := newHomeLab(t)
		a, box := driveToPlace(t, lab, where)
		a.key(key(" "))
		a.key(key("x"))
		if got := box.String(); got != " x" {
			t.Fatalf("%v: the box holds %q, want %q", where, got, " x")
		}
		if a.at(pageHome) {
			t.Fatalf("%v: typing a space and a letter opened home", where)
		}
		// And a space in a box that already has words in it is just a space.
		a.key(key(" "))
		a.key(key(" "))
		if a.at(pageHome) {
			t.Fatalf("%v: the gesture fired in a box that had text in it", where)
		}
	}
}

// AND ON HOME ITSELF THE DOOR IS A NO-OP: the foot draws no door there, and
// two spaces into home's own filter type two spaces, the way they always did.
func TestThePlaceDoorIsShutOnHome(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", time.Now())
	lab.session("-tmp-alpha", "aaaa000000000002", "elsewhere", "/tmp/alpha", time.Now().Add(-time.Hour))
	a := lab.door(mine)
	a.openHome()
	if !a.at(pageHome) {
		t.Fatal("home did not open")
	}
	if a.homeDoorOpen() {
		t.Fatal("the door advertised itself on home")
	}
	a.key(key(" "))
	a.key(key(" "))
	if !a.at(pageHome) {
		t.Fatal("home moved")
	}
	if got := a.home.box.String(); got != "  " {
		t.Fatalf("home's own filter holds %q, want the two spaces typed plainly", got)
	}
}

// AND THE DOOR STAYS SHUT OVER A PLACE'S OWN WHOLE-KEYBOARD LAYER. Memory's card
// editor was the one that got away: it lives inside the place's own key handler,
// below the door, and its box is the very box the door reads — so a card cleared
// down to its last space armed the door, and the second space wiped the wording
// and stood the person on home mid-edit. [placeMemory.owns] now claims the
// keyboard while the editor is open, the same claim settings makes for its value
// editor, and this holds it: the two spaces type into the card, and the person
// stays where they are.
func TestTheDoorStaysShutOverTheMemoryCardEditor(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{{ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Tags: []string{"editor"}, UseCount: 7, Scope: store.MemoryScopeUser}})
	memory.origins["m1"] = memoryOrigin{title: "Editor setup", at: time.Now().Add(-2 * time.Hour)}
	// THE DOOR HAS TO EXIST FOR THIS TEST TO MEAN ANYTHING. The gesture is bound
	// only on a machine home is reachable from ([app.homeDoorOpen] — a.canOpen()),
	// and the memory fixture builds with no seam set, so a test that left it
	// unset was holding the law against a door that did not exist and could not
	// have failed however wrong the fix was. Hand the app one seam, and then
	// insist the door is open before driving the editor — a fixture whose door
	// went dark again must fail here, not pass silently below.
	a.resume = func(string) (Agent, error) { return &fakeAgent{model: "m"}, nil }
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut, so this test would hold nothing: give the fixture a seam")
	}
	a.slash("/memory")
	if _, ok := a.mem.shelfUnder(); !ok {
		t.Fatalf("the cursor did not open on a shelf heading")
	}
	drive(t, a, key("enter")) // roll the biggest shelf up
	if strings.Contains(plain(frame(a)), "uses neovim") {
		t.Fatalf("enter did not close the shelf")
	}
	drive(t, a, key("enter")) // and back open, the way the hand walks it
	drive(t, a, key("down"))  // onto the line under the cursor
	if got, ok := a.mem.choice(); !ok || got.ID != "m1" {
		t.Fatalf("down did not land on the line: %v %v; frame:\n%s", got, ok, plain(frame(a)))
	}
	drive(t, a, key("right")) // the line's verbs
	drive(t, a, key("e"))     // fix: the card's wording, pre-loaded into the editor
	if a.mem.edit == nil {
		if _, ok := a.mem.choice(); !ok {
			t.Fatalf("the cursor never reached the line; frame:\n%s", plain(frame(a)))
		}
		t.Fatalf("the card editor did not open; frame:\n%s", plain(frame(a)))
	}
	drive(t, a, key("ctrl+u")) // clear the wording down to nothing
	typeInto(t, a, " ")        // the first space arms the door's law
	drive(t, a, key(" "))      // the second, which must not open it
	if a.at(pageHome) {
		t.Fatal("the door opened home over the memory card editor")
	}
	if a.mem.edit == nil {
		t.Fatal("the door closed the card editor")
	}
	if got := a.mem.edit.String(); got != "  " {
		t.Fatalf("the card editor holds %q, want the two spaces typed plainly", got)
	}
}

// AND THE DOOR YIELDS TO THE TASK ROOM, WHERE SPACE IS THE CARD'S OWN VERB.
// `space` pages a record the way `pgdown` and `ctrl+f` do ([app.taskCardKey]),
// and the card's arm used to sit BELOW the door in this place's key handler — so
// a filter left holding one space, which is the state the door's own first press
// creates and which draws nothing a person can see, armed the door on a box the
// card never types into, and the space meant to scroll took them to home
// instead. [placeTasks.owns] now claims the keyboard while the card is up, and
// this holds it: the space still pages, the filter is untouched, and the person
// is still standing in the room they were reading.
func TestSpaceInTheTaskRoomPagesTheCardAndDoesNotOpenHome(t *testing.T) {
	lab := newHomeLab(t)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
	})
	a, box := driveToPlace(t, lab, pageTasks)
	if box == nil {
		t.Fatal("the tasks place has no box to type into")
	}
	// THE DOOR HAS TO EXIST FOR THIS TEST TO MEAN ANYTHING, the same insistence
	// [TestTheDoorStaysShutOverTheMemoryCardEditor] makes and for its reason: a
	// fixture whose door went dark would pass this however wrong the fix was.
	if !a.homeDoorOpen() {
		t.Fatal("the door is shut, so this test would hold nothing: give the fixture a seam")
	}
	// One space typed on the ROSTER lands in the filter and arms the door — this
	// is the ordinary first half of the gesture, and it is what makes the room's
	// next space dangerous.
	a.key(key(" "))
	if got := box.String(); got != " " {
		t.Fatalf("the first space did not land in the filter: %q", got)
	}
	drive(t, a, key("enter")) // into the room, over the roster
	if !a.taskSheet.detailOn {
		t.Fatalf("the record did not open; frame:\n%s", plain(frame(a)))
	}
	a.key(key(" "))
	if a.at(pageHome) {
		t.Fatal("the door opened home over the task room, where space pages the card")
	}
	if !a.at(pageTasks) || !a.taskSheet.detailOn {
		t.Fatalf("the space left the room: tasks=%v card=%v", a.at(pageTasks), a.taskSheet.detailOn)
	}
	if got := box.String(); got != " " {
		t.Fatalf("the gesture emptied the filter behind the card: %q", got)
	}
	// AND IT PAGED, which is the positive half: `space` and `pgdown` are one key
	// on this card, so the two must leave the record in the same place.
	paged := a.taskSheet.detailTop
	if paged == 0 {
		t.Fatal("the record did not page at all, so the second half of this test holds nothing")
	}
	a.taskSheet.detailTop = 0
	a.key(key("pgdown"))
	if a.taskSheet.detailTop != paged {
		t.Fatalf("space left the record at %d and pgdown at %d; they are the same key here", paged, a.taskSheet.detailTop)
	}
}
