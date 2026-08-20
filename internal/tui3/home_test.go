package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	// The cursor opens on the first row, so the detail shows the question.
	if !strings.Contains(text, "can I run: rm -rf build/") {
		t.Fatalf("the detail does not show what it is stopped on:\n%s", text)
	}
	// And it really is drawn above the newer, idle rows.
	ask := strings.Index(text, "Pricing Research")
	newest := strings.Index(text, "The Newest Chat")
	if ask < 0 || newest < 0 || ask > newest {
		t.Fatalf("the row that needs somebody is not above the newer ones:\n%s", text)
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

// `@` turns the column into a search over every conversation on the machine,
// and drops the headings with it.
func TestHomeAtFindsAcrossEveryProject(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	for _, r := range "@pricing" {
		a.homeKey(key(string(r)))
	}
	text := homeText(a)
	if !strings.Contains(text, "Pricing Research") {
		t.Fatalf("the filter lost the conversation it should have found:\n%s", text)
	}
	if strings.Contains(text, "Porting the Resume Picker") {
		t.Fatalf("the filter kept a conversation that does not match:\n%s", text)
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
