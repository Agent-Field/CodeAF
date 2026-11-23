package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE LIVE PANELS ───

// liveLab is a machine with two conversations of its own in one project and a
// third somewhere else, opened at a hundred and twenty by forty-five — two
// columns, room for every panel whole.
type liveLab struct {
	*homeLab
	now         time.Time
	mine, other string
	far         string
}

func newLiveLab(t *testing.T) *liveLab {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.workspace("alpha")
	beta := lab.workspace("beta")
	l := &liveLab{homeLab: lab, now: now}
	l.mine = lab.session("-alpha", "aaaa000000000001", "porting the resume picker", alpha, now.Add(-2*time.Minute))
	l.other = lab.session("-alpha", "aaaa000000000002", "prime sieve", alpha, now.Add(-time.Hour))
	l.far = lab.session("-beta", "bbbb000000000001", "pricing site", beta, now.Add(-3*time.Hour))
	return l
}

func (l *liveLab) open() *app {
	l.t.Helper()
	a := l.app(l.mine)
	a.width, a.height = 120, 45
	a.leaveAnswer = func(string, session.QuestionKind, uint64, string) error { return nil }
	a.openHome()
	homeText(a)
	return a
}

// live writes one session's whole presence — its question, its work and its
// jobs — as the heartbeat would.
func (l *liveLab) live(bucket, id string, p session.SessionPresence) {
	l.t.Helper()
	p.Schema, p.SessionID, p.Workspace, p.PID = 1, id, "/tmp/alpha", 4242
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = l.now
	}
	if p.State == "" {
		p.State = session.PresenceWorking
	}
	raw, err := json.Marshal(p)
	if err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.project(bucket), id, "presence.json"), append(raw, '\n'), 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// panelLines is every line of one panel, in order.
func panelLines(a *app, panel homePanelID) []homeLine {
	var out []homeLine
	for _, line := range a.home.lines {
		if got, ok := line.panelOf(); ok && got == panel {
			out = append(out, line)
		}
	}
	return out
}

// panelRows is one panel's rows as the cells they are drawn from, headings,
// whispers and folds left out.
func panelRows(a *app, panel homePanelID) []*homeCell {
	var out []*homeCell
	for _, line := range panelLines(a, panel) {
		if line.cell.kind == cellRow {
			out = append(out, line.cell)
		}
	}
	return out
}

// ── needs you ───────────────────────────────────────────────────────────────

// TWO QUESTIONS, THE LONGEST WAIT FIRST, AND THE ANSWERS ON THE TOP ONE ONLY: the
// consent line is the gate's own sentence, and the row below draws no second
// `1` because it would be a guess — and no `enter` either, because the door
// word is said under the cursor alone ([app.homeRowAnswers]).
func TestNeedsYouOrdersTheWaitsAndDrawsAnswersOnTheTopRowOnly(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(9, "needs your ok to run write", l.now.Add(-5*time.Minute))})
	a := l.open()
	rows := homeAttentionRows(a)
	if len(rows) != 2 || rows[0].title != "Prime Sieve" || rows[1].title != "Pricing Site" {
		t.Fatalf("needs you is not the two waits, oldest first: %+v", rows)
	}
	if rows[1].sub != "needs your ok to run bash" || rows[1].right != "3h" {
		t.Fatalf("the top row is not the gate's own sentence with its age: %+v", rows[0])
	}
	// THE SENTENCE AND THE CHIPS ARE DRAWN UNDER THE ROW BEING READ, and at rest a row is its
	// mark, its title and its wait.
	frame := homeText(a)
	if under := homeLineAfter(frame, "Pricing Site"); strings.Contains(under, "needs your ok") || strings.Contains(under, "allow once") {
		t.Fatalf("the top row draws its sentence with nobody reading it:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.title == "Pricing Site" })
	frame = homeText(a)
	if head := homeLineAfter(frame, "Pricing Site"); !strings.Contains(head, homeThreadWord+"Pricing Site") {
		t.Fatalf("the read row's description does not open with its thread's title:\n%s", frame)
	}
	if under := homeLineBelow(frame, "Pricing Site", 3); !strings.Contains(under, "1 allow once  2 always  3 deny") {
		t.Fatalf("the top row under the cursor does not draw its answers under the thread title:\n%s", frame)
	}
	if under := homeLineAfter(frame, "Prime Sieve"); strings.Contains(under, "allow once") || strings.Contains(under, "enter") {
		t.Fatalf("the second row drew answers or a door word the cursor is not on:\n%s", frame)
	}
	// The question's own sentence may say "needs your ok"; there is no heading.
	for _, text := range strings.Split(frame, "\n") {
		if strings.TrimSpace(text) == "needs you" {
			t.Fatalf("the removed heading is still drawn:\n%s", frame)
		}
	}
}

// A TASK THE RECORD MARKS AS YOUR CALL IS A ROW OF THE `unread` GROUP, one
// line of its own, under a group line that says what the group is. enter aims at
// the task rather than at the conversation's live edge.
func TestNeedsYouCarriesATaskWaitingOnYourCall(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "4", SessionID: "aaaa000000000002", Label: "fix the flaky sieve",
		Title: "fix the flaky sieve", Status: string(session.TaskUnverified), EndedAt: l.now.Add(-30 * time.Minute),
		FilesChanged: 3})
	a := l.open()
	rows := homeAttentionRows(a)
	// THE MARGIN IS WHEN IT LANDED AND NOTHING ELSE; its description leads with the thread it
	// belongs to, spelled as `threads` spells it, then the files it wrote. It used to read `3
	// files · 30m`.
	if len(rows) != 1 || rows[0].title != "fix the flaky sieve" || rows[0].right != "30m" || rows[0].thread != "Prime Sieve" || !strings.HasPrefix(rows[0].sub, "3 files · ") {
		t.Fatalf("the task's call is not a one-line row of needs you with its files in its description: %+v", rows)
	}
	if rows[0].mark != cellMarkNeeds {
		t.Fatalf("a landing wears a mark: %+v", rows[0])
	}
	if rows[0].panel != panelNeeds {
		t.Fatal("the question is not on the task's existing row")
	}
}

// THE GROUP LINE IS DRAWN ONLY WHERE THE GROUP HAS ROWS, and it is not a stop:
// the cursor walks from the last question straight onto the first landing.
func TestToCheckDrawsNoGroupLineWithoutLandings(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	a := l.open()
	if frame := homeText(a); strings.Contains(frame, needsCheckWord) {
		t.Fatalf("a group with no rows drew its line:\n%s", frame)
	}
	for _, line := range panelLines(a, panelNeeds) {
		if line.cell.kind == cellGroup {
			t.Fatalf("a group line was built with no rows under it")
		}
	}
}

// BLOCKING FIRST, HOWEVER OLD THE LANDING IS: a consent asked a minute ago sits
// above a landing from a week back, and the landing is under the group line.
func TestNeedsBlockingRowsSortFirst(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-time.Minute))})
	l.task("-alpha", session.TaskIndexEntry{ID: "4", SessionID: "aaaa000000000002", Label: "fix the flaky sieve",
		Title: "fix the flaky sieve", Status: string(session.TaskUnverified), EndedAt: l.now.Add(-40 * time.Hour)})
	a := l.open()
	rows := homeAttentionRows(a)
	if len(rows) != 2 || rows[0].title != "Pricing Site" || rows[1].title != "fix the flaky sieve" {
		t.Fatalf("the week-old landing did not sort under the fresh question: %+v", rows)
	}
	if rows[0].mark != cellMarkNeeds || rows[1].mark != cellMarkNeeds {
		t.Fatalf("the mark is not on the stopped row alone: %+v", rows)
	}
	kinds := []homeCellKind{}
	for _, line := range panelLines(a, panelNeeds) {
		kinds = append(kinds, line.cell.kind)
	}
	if len(kinds) != 2 || kinds[0] != cellGroup || kinds[1] != cellRow {
		t.Fatalf("the group line does not stand between the question and the landing: %v", kinds)
	}
}

// THE LANDINGS ARE NEWEST FIRST, which is the opposite of the questions above
// them and is said in [needsPanel.rows].
func TestToCheckDrawsTheNewestLandingFirst(t *testing.T) {
	l := newLiveLab(t)
	for i, ago := range []time.Duration{30 * time.Minute, 5 * time.Hour} {
		id := itoa(i + 1)
		l.task("-alpha", session.TaskIndexEntry{ID: id, SessionID: "aaaa000000000002", Label: "call " + id,
			Title: "call " + id, Status: string(session.TaskUnverified), EndedAt: l.now.Add(-ago)})
	}
	a := l.open()
	if rows := homeAttentionRows(a); len(rows) != 2 || rows[0].title != "call 1" || rows[1].title != "call 2" {
		t.Fatalf("unread is not newest first: %+v", rows)
	}
}

// ONE LINE AT REST AND TWO UNDER THE CURSOR: the landing grows the report's
// first sentence and the task's own two answers, and the words are the ask's.
func TestALandingGrowsItsReportAndAnswersUnderTheCursor(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "4", SessionID: "aaaa000000000002", Label: "fix the flaky sieve",
		Title: "fix the flaky sieve", Status: string(session.TaskUnverified), EndedAt: l.now.Add(-30 * time.Minute),
		Outcome: "Reseeded the generator and the sieve is stable over a thousand runs."})
	a := l.open()
	at := -1
	for i, line := range a.home.lines {
		if line.task != nil && line.task.ID == "4" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the landing is not a line of the column")
	}
	if frame := homeText(a); strings.Contains(frame, "Reseeded the generator") {
		t.Fatalf("the landing grew its line with the cursor elsewhere:\n%s", frame)
	}
	a.home.cursor = at
	frame := homeText(a)
	under := homeLineAfter(frame, "fix the flaky sieve")
	// THE THREAD'S TITLE LINE COMES FIRST, then a blank, then the sentence with
	// the answers beside it.
	if cell := a.home.lines[at].cell; cell.thread != "Prime Sieve" || !strings.HasPrefix(cell.sub, "Reseeded the generator") {
		t.Fatalf("the landing is not headed by its thread over the report's first sentence: %+v", cell)
	}
	if !strings.Contains(under, homeThreadWord+"Prime Sieve") {
		t.Fatalf("the cursor row did not grow its thread's title line:\n%s", frame)
	}
	under = homeLineBelow(frame, "fix the flaky sieve", 3)
	if !strings.Contains(under, "Reseeded the generator") {
		t.Fatalf("the report's first sentence is not two lines under the thread title:\n%s", frame)
	}
	if !strings.Contains(under, needsYesKey+" accept") || !strings.Contains(under, needsNoKey+" not right") {
		t.Fatalf("the grown line does not carry the ask's own answers:\n%s", frame)
	}
}

// AND THE KEY THE GROWN LINE DRAWS IS THE KEY THAT ANSWERS IT, through the one
// door a landing is answered by anywhere ([app.homeAnswerLanding]).
func TestALandingUnderTheCursorTakesItsOwnAnswerKey(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "4", SessionID: "aaaa000000000002", Label: "fix the flaky sieve",
		Title: "fix the flaky sieve", Status: string(session.TaskUnverified), EndedAt: l.now.Add(-30 * time.Minute)})
	a := l.open()
	var left []string
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		left = append(left, string(kind)+"/"+itoa64(id)+"/"+key)
		return nil
	}
	for i, line := range a.home.lines {
		if line.task != nil && line.task.ID == "4" {
			a.home.cursor = i
		}
	}
	homeText(a)
	// AND A BARE LETTER STILL TYPES, which is why the key on this screen is a
	// digit: `a` is the landing's key everywhere else and must not be one here.
	if _, took := a.homeGridAnswer(session.LandingYesKey); took {
		t.Fatal("a bare letter answered a landing from home")
	}
	if _, took := a.homeGridAnswer(needsYesKey); !took {
		t.Fatal("the landing under the cursor did not take the accept it draws")
	}
	if len(left) != 1 || left[0] != string(session.QuestionLanding)+"/4/"+session.LandingYesKey {
		t.Fatalf("the accept did not reach the conversation's doorstep as a landing answer: %v", left)
	}
}

// A YOUR-CALL OLDER THAN TWO DAYS IS HISTORY, NOT A QUESTION: two fresh calls
// are the panel's rows and its count, and the three that landed days ago are
// one door into tasks under them.
func TestNeedsYouAgesOldCallsOntoTheFold(t *testing.T) {
	l := newLiveLab(t)
	for i, ago := range []time.Duration{30 * time.Minute, 5 * time.Hour, 3 * 24 * time.Hour, 8 * 24 * time.Hour, 9 * 24 * time.Hour} {
		id := itoa(i + 1)
		l.task("-alpha", session.TaskIndexEntry{ID: id, SessionID: "aaaa000000000002", Label: "call " + id, Title: "call " + id,
			Status: string(session.TaskUnverified), EndedAt: l.now.Add(-ago)})
	}
	a := l.open()
	if rows := homeAttentionRows(a); len(rows) != 2 || rows[0].title != "call 1" || rows[1].title != "call 2" {
		t.Fatalf("needs you is not the two fresh calls: %+v", rows)
	}
	frame := homeText(a)
	// AND NEITHER THE HEADING NOR THE GROUP LINE COUNTS ANYTHING: the rows are
	// under them. The one count is the fold's, which counts the aged landings
	// with everything else it hides — `3 more`, no longer `3 older · tasks`.
	if strings.Contains(frame, "needs you") {
		t.Fatalf("the heading counted the landings:\n%s", frame)
	}
	if !strings.Contains(frame, needsCheckWord) {
		t.Fatalf("the group line says more than its name:\n%s", frame)
	}
	fold := a.home.lines[homeFoldDoor(t, a, panelNeeds)].cell.title
	if fold != "3 more" {
		t.Fatalf("the fold reads %q, want the three aged landings counted as `3 more`", fold)
	}
	// AND OPENING THE FOLD BRINGS THEM BACK: a fold that opened to show nothing
	// of what it counted would have lied about its own number.
	a.home.cursor = homeFoldDoor(t, a, panelNeeds)
	drive(t, a, key("enter"))
	if rows := homeAttentionRows(a); len(rows) != 5 {
		t.Fatalf("opening needs you drew %d rows, want all five landings:\n%s", len(rows), homeText(a))
	}
}

// A LIVE QUESTION IS NEVER AGED OUT, however long it has waited.
func TestNeedsYouKeepsALiveQuestionPastTwoDays(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-5*24*time.Hour))})
	a := l.open()
	if rows := homeAttentionRows(a); len(rows) != 1 || rows[0].title != "Pricing Site" {
		t.Fatalf("a question five days old is not on needs you: %+v", rows)
	}
	for _, line := range panelLines(a, panelNeeds) {
		if line.cell.kind == cellFold {
			t.Fatalf("a live question was counted as history: %q", line.cell.title)
		}
	}
}

// AN EMPTY PANEL WHISPERS what arrives there, and never that it is empty.
func TestNeedsYouWhispersWhenNothingWaits(t *testing.T) {
	a := newLiveLab(t).open()
	if frame := homeText(a); strings.Contains(frame, "questions from any chat or task land here") || strings.Contains(frame, "needs you") {
		t.Fatalf("an empty attention panel is still visible:\n%s", frame)
	}
}

// consentQuestionAt is [consentQuestion] asked at a named instant.
func consentQuestionAt(id uint64, text string, asked time.Time) session.PresenceQuestion {
	q := consentQuestion(id, text)
	q.Asked = asked
	return q
}

// ── threads ──────────────────────────────────────────────────────────

// A BRAND-NEW LAUNCH'S OWN ROW IS ONE LINE: `new conversation` in bold, no age
// and nothing under it — whatever the journal's tail has on hand — until
// its person says something, and then the line under it is what they said.
func TestAFreshLaunchHasNoHomeRowUntilItsFirstMessage(t *testing.T) {
	l := newLiveLab(t)
	dir := filepath.Join(l.project("-alpha"), "aaaa000000000009")
	fresh := filepath.Join(dir, "transcript.jsonl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte(`{"type":"session","version":1,"id":"aaaa000000000009"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := session.Meta{ID: "aaaa000000000009", Workspace: l.workspace("alpha"), Created: l.now.Add(-time.Minute)}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	openOn := func() *app {
		a := l.app(fresh)
		a.width, a.height = 120, 45
		openHomeFixtureTabs(a)
		a.openHome()
		a.home.last = map[string]session.Summary{fresh: {LastUser: "explain open addressing"}}
		a.home.build()
		return a
	}
	a := openOn()
	for _, own := range panelRows(a, panelSessions) {
		if own.title == unnamedConversationWord {
			t.Fatalf("empty launch has a saved row: %+v", own)
		}
	}
	meta.LastUserAt = l.now
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	wide := openOn()
	wide.openingPrompt = "explain open addressing"
	wide.home.build()
	wide.width = 180
	homeText(wide)
	wide.home.point(fresh)
	if frame := homeText(wide); !strings.Contains(frame, "explain open addressing") {
		t.Fatalf("the first message did not reach the description column:\n%s", frame)
	}
}

// cancelFake is an engine that can end work.
type cancelFake struct{ *fakeAgent }

func (cancelFake) Cancel(string) (string, error) { return "stopping", nil }

// ── since you left ──────────────────────────────────────────────────────────

// A LINE PER TASK THAT LANDED AND PER FILE MADE, newest first, under a heading
// that says how long the person was away: the task with its outcome and its
// cost, a task that stopped with why, the file with the conversation that made
// it. A task's part is on its record and not on this panel.
func TestSinceYouLeftNamesEachLandedTaskAndFile(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "tower fleet ssh audit", Title: "tower fleet ssh audit",
		Status: string(session.TaskDone), Outcome: "2 hosts up, 1 not", Cost: 0.42,
		EndedAt: l.now.Add(-3 * time.Hour)})
	l.task("-alpha", session.TaskIndexEntry{ID: "2", SessionID: "aaaa000000000002", Label: "port the parser", Title: "port the parser",
		Status: string(session.TaskFailed), Ending: session.TaskEndingWire, EndedAt: l.now.Add(-2 * time.Hour)})
	l.task("-alpha", session.TaskIndexEntry{ID: "3", Parent: "1", SessionID: "aaaa000000000002", Label: "a part", Title: "a part",
		Status: string(session.TaskDone), EndedAt: l.now.Add(-150 * time.Minute)})
	a := l.open()
	a.artifacts = filepath.Join(l.root, "artifacts.jsonl")
	writeArtifacts(t, a.artifacts, session.Artifact{Path: "/tmp/reports/apartments-minto.md",
		Session: "bbbb000000000001", Title: "apartments", Created: l.now.Add(-time.Hour)})
	a.home.seen = l.now.Add(-12 * time.Hour)
	a.readSwitchLedger()
	a.home.build()
	rows := panelRows(a, panelLeft)
	want := []string{"made apartments-minto.md", "port the parser · lost the connection", "tower fleet ssh audit · 2 hosts up, 1 not"}
	if len(rows) != len(want) {
		t.Fatalf("since you left drew %d lines, want %d:\n%s", len(rows), len(want), homeText(a))
	}
	for i, title := range want {
		if rows[i].title != title {
			t.Fatalf("line %d is %q, want %q", i, rows[i].title, title)
		}
	}
	// THE MARGIN IS WHEN EACH HAPPENED, and the conversation a file was made in and what a
	// task cost are the rows' descriptions. They used to be the margins — a name on one row,
	// money on the next.
	if rows[0].right != "1h" || rows[1].right != "2h" || rows[2].right != "3h" {
		t.Fatalf("the right margins are not when each happened: %+v", rows)
	}
	if rows[0].sub != "Pricing Site" || rows[2].sub != "$0.42" || rows[1].sub != "" || !rows[0].grows || !rows[2].grows {
		t.Fatalf("the descriptions are not the conversation and the cost, under the cursor: %+v", rows)
	}
	if frame := homeText(a); !strings.Contains(frame, "since you left · 12h") {
		t.Fatalf("the heading does not say how long you were away:\n%s", frame)
	}
}

// A LANDED TASK'S LINE IS A DOOR INTO ITS RECORD.
func TestSinceYouLeftOpensALandedTasksRecord(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "tower fleet ssh audit", Title: "tower fleet ssh audit",
		Status: string(session.TaskDone), Outcome: "all up", EndedAt: l.now.Add(-time.Hour)})
	a := l.open()
	a.home.seen = l.now.Add(-4 * time.Hour)
	a.home.build()
	for at, line := range a.home.lines {
		if line.cell != nil && line.cell.panel == panelLeft && line.cell.kind == cellRow {
			a.home.cursor = at
		}
	}
	a.homeKey(key("enter"))
	if !a.at(pageTasks) || !a.taskSheet.detailOn || a.taskSheet.detail.ID != "1" {
		t.Fatal("enter on a landed task did not open its record")
	}
}

// A FIRST LOOK HAS NO "SINCE": with no look stamp the panel whispers.
func TestSinceYouLeftWhispersOnAFirstLook(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "audit", Title: "audit",
		Status: string(session.TaskDone), EndedAt: l.now.Add(-time.Hour)})
	a := l.open()
	if frame := homeText(a); !strings.Contains(frame, homeWhisper[panelLeft]) || len(panelRows(a, panelLeft)) != 0 {
		t.Fatalf("a first look drew news:\n%s", frame)
	}
}

// writeArtifacts writes a deliverables index.
func writeArtifacts(t *testing.T, path string, rows ...session.Artifact) {
	t.Helper()
	var b strings.Builder
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(raw, '\n'))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Question rows may live in either the conversation or task list now.
func homeAttentionRows(a *app) []*homeCell {
	var out []*homeCell
	for _, line := range a.home.lines {
		if line.cell != nil && line.cell.kind == cellRow && line.cell.mark == cellMarkNeeds {
			out = append(out, line.cell)
		}
	}
	return out
}

func liveHomeRig(t *testing.T, online ...string) (*app, *fakeTaker) {
	t.Helper()
	a, _ := devicesRig(online...)
	t.Cleanup(a.doorLine.close)
	a.width, a.height = 200, 70
	taker := a.taker.(*fakeTaker)
	drain(t, a, a.openHome())
	a.rebuildMachines()
	return a, taker
}

func wireFakeOpenLive(t *testing.T, a *app, fail error) (calls *[]chatlist.Row, agents *[]*fakeAgent) {
	t.Helper()
	var seen []chatlist.Row
	var built []*fakeAgent
	root := t.TempDir()
	a.openLive = func(_ context.Context, row chatlist.Row) (Conversation, error) {
		seen = append(seen, row)
		if fail != nil {
			return Conversation{}, fail
		}
		agent := &fakeAgent{model: "m"}
		built = append(built, agent)
		return Conversation{
			Agent:       agent,
			SessionFile: filepath.Join(root, row.Cell, "session.jsonl"),
			Live: &LiveSeam{
				Host:   row.Device,
				Device: row.DeviceID,
				Cell:   row.Cell,
				Row:    row,
				Read:   func() LiveRead { return LiveRead{Kind: LiveOn} },
			},
			Link: &LinkSeam{},
		}, nil
	}
	return &seen, &built
}

// V1 (contract 1): pressing Enter on a held row of an online paired computer
// opens a live view of that chat through OpenLive without calling the taker or
// changing the hold, and closes Home into the live conversation.
func TestEnterOnAHeldRowOfAnOnlineDeviceOpensItLiveAndLeavesTheHold(t *testing.T) {
	a, taker := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)

	standOn(t, a, "c1")

	if len(*calls) != 1 || (*calls)[0].Cell != "c1" || (*calls)[0].DeviceID != "dev_tower" {
		t.Fatalf("OpenLive calls = %+v, want one call for c1 on dev_tower", *calls)
	}
	if len(taker.cells) != 0 {
		t.Fatalf("taker was called during a live open: %v", taker.cells)
	}
	if a.liveFront() == nil || a.liveFront().Host != "tower" || a.liveFront().Cell != "c1" {
		t.Fatalf("front conversation live seam = %+v, want live view of tower c1", a.liveFront())
	}
	if a.at(pageHome) {
		t.Fatal("Home stayed open after a successful live open")
	}
	if a.homeTaking != "" {
		t.Fatalf("homeTaking = %q, want empty", a.homeTaking)
	}
	if a.machineRead.rows[0].Status != chatlist.Running || a.machineRead.rows[0].DeviceID != "dev_tower" {
		t.Fatalf("live open altered the held row: %+v", a.machineRead.rows[0])
	}

	// Phone sheet's open door on a machine row also takes the live road.
	phoneApp, phoneTaker := liveHomeRig(t, "dev_tower")
	phoneCalls, _ := wireFakeOpenLive(t, phoneApp, nil)
	phoneApp.width, phoneApp.height = 44, 24
	phoneApp.rebuildMachines()
	for i, line := range phoneApp.home.lines {
		if line.remote != nil && line.remote.Cell == "c1" {
			phoneApp.home.cursor = i
			break
		}
	}
	phoneApp.openHomeSheet()
	drain(t, phoneApp, phoneApp.homeSheetOpen())
	if len(*phoneCalls) != 1 || len(phoneTaker.cells) != 0 || phoneApp.liveFront() == nil || phoneApp.at(pageHome) {
		t.Fatalf("phone sheet open did not open live: calls=%d takes=%v live=%+v home=%v", len(*phoneCalls), phoneTaker.cells, phoneApp.liveFront(), phoneApp.at(pageHome))
	}
}

// V1 (contract 2): when OpenLive returns an error, Home shows the error's text
// on its foot and raises the Move card for that row as fallback.
func TestEnterOnAHeldRowFallsBackToTheMoveCardWhenTheLiveOpenFails(t *testing.T) {
	a, taker := liveHomeRig(t, "dev_tower")
	wantErr := "tower is not running this chat right now."
	calls, _ := wireFakeOpenLive(t, a, errors.New(wantErr))

	standOn(t, a, "c1")

	if len(*calls) != 1 {
		t.Fatalf("OpenLive called %d times, want 1", len(*calls))
	}
	if len(taker.cells) != 0 {
		t.Fatalf("fallback to the Move card already ran taker: %v", taker.cells)
	}
	if !a.at(pageHome) {
		t.Fatal("Home closed when OpenLive failed")
	}
	if a.home.msg != wantErr {
		t.Fatalf("Home foot message = %q, want %q", a.home.msg, wantErr)
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Kind != homeContinueKind || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port" {
		t.Fatalf("Move card was not raised on OpenLive failure: ok=%v question=%+v", ok, ask.question)
	}
}

// V27 (contract 2): a door that has nothing to open live and nothing to say,
// such as a computer that is off with no copy kept, answers ErrNoLiveView, and
// Enter asks the Move card with nothing on Home's foot, as it did before remote
// control.
func TestEnterOnAHeldRowWhoseDoorHasNothingToOpenAsksTheMoveCardInSilence(t *testing.T) {
	a, taker := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, ErrNoLiveView)

	standOn(t, a, "c1")

	if len(*calls) != 1 {
		t.Fatalf("OpenLive called %d times, want 1", len(*calls))
	}
	if len(taker.cells) != 0 {
		t.Fatalf("the quiet fallback already ran the taker: %v", taker.cells)
	}
	if a.home.msg != "" && a.home.msg != conversationOpeningWord {
		t.Fatalf("Home's foot says %q after a door with nothing to say", a.home.msg)
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Kind != homeContinueKind || ask.question.Head != moveAsk {
		t.Fatalf("Move card was not raised: ok=%v question=%+v", ok, ask.question)
	}
}

// V14 (contract 2 and contract 9): when OpenLive is nil (remote control off),
// pressing Enter on a held row raises the Move card exactly as before.
func TestWithoutTheLiveSeamEnterOnAHeldRowAsksTheMoveCardAsBefore(t *testing.T) {
	a, taker := liveHomeRig(t, "dev_tower")
	if a.openLive != nil {
		t.Fatal("expected default a.openLive to be nil")
	}

	standOn(t, a, "c1")

	if len(taker.cells) != 0 {
		t.Fatalf("Enter raised a take before confirmation: %v", taker.cells)
	}
	if !a.at(pageHome) || a.liveFront() != nil {
		t.Fatalf("unexpected state: atHome=%v liveFront=%+v", a.at(pageHome), a.liveFront())
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Kind != homeContinueKind || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port" || ask.pick != continueYesAt {
		t.Fatalf("Move card not raised as before: ok=%v pick=%d question=%+v", ok, ask.pick, ask.question)
	}
}

// V8 (contract 3): with OpenLive wired, the Move chords (alt+m bringWork and
// device picker, alt+c resume offer, and a local Home row that turns out held
// elsewhere) still raise the Move card and never call OpenLive.
func TestTheMoveChordsStillOfferTheMoveCardWhenLiveIsAvailable(t *testing.T) {
	// 1. alt+m with one online device (bringWork).
	a, _ := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	if cmd := a.bringWork(); cmd != nil {
		t.Fatalf("bringWork started work: %v", cmd)
	}
	if len(*calls) != 0 {
		t.Fatalf("bringWork called OpenLive %d times, want 0", len(*calls))
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port" {
		t.Fatalf("bringWork did not raise the Move card: ok=%v question=%+v", ok, ask.question)
	}

	// 2. alt+m with several online devices (devicePickShown).
	multi, _ := liveHomeRig(t, "dev_tower", "dev_desk")
	multiCalls, _ := wireFakeOpenLive(t, multi, nil)
	multi.bringWork()
	pickAsk, ok := multi.homeAsking()
	if !ok || pickAsk.question.Head != devicePickAsk {
		t.Fatalf("several online devices did not raise picker: ok=%v question=%+v", ok, pickAsk.question)
	}
	pickAsk.local(session.Answer{Key: "1", Picked: []string{"1"}})
	if len(*multiCalls) != 0 {
		t.Fatalf("device picker called OpenLive %d times, want 0", len(*multiCalls))
	}
	if ask, ok = multi.homeAsking(); !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port" {
		t.Fatalf("device picker did not raise the Move card: ok=%v question=%+v", ok, ask.question)
	}

	// 3. alt+c from the resume card (offerContinue).
	resumeApp, _ := liveHomeRig(t, "dev_tower")
	resumeCalls, _ := wireFakeOpenLive(t, resumeApp, nil)
	resumeApp.considerResume()
	if resumeApp.leftOff.row == nil {
		t.Fatal("expected resume offer for offline desk's chat")
	}
	cmd, took := resumeApp.resumeKeyHandler(resumeKey)
	if !took || cmd != nil {
		t.Fatalf("resumeKeyHandler(alt+c) took=%v cmd=%v", took, cmd)
	}
	if len(*resumeCalls) != 0 {
		t.Fatalf("alt+c called OpenLive %d times, want 0", len(*resumeCalls))
	}
	if ask, ok = resumeApp.homeAsking(); !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Docs" {
		t.Fatalf("alt+c did not raise the Move card: ok=%v question=%+v", ok, ask.question)
	}

	// 4. A local Home row that turns out held elsewhere on open (homeLocalOpenReady).
	localApp, _ := liveHomeRig(t, "dev_tower")
	localCalls, _ := wireFakeOpenLive(t, localApp, nil)
	heldRow := localApp.machineRead.rows[0]
	localLine := homeLine{
		kind: homeSession,
		row:  session.SessionRow{ID: heldRow.Cell, Transcript: filepath.Join(t.TempDir(), heldRow.Cell, "session.jsonl")},
	}
	drain(t, localApp, localApp.homeLocalOpenReady(homeLocalOpenMsg{
		line:       localLine,
		fromFile:   localApp.file,
		generation: localApp.continueGeneration,
		reading:    homeMachinesMsg{rows: []chatlist.Row{heldRow}},
	}))
	if len(*localCalls) != 0 {
		t.Fatalf("homeLocalOpenReady called OpenLive %d times, want 0", len(*localCalls))
	}
	if ask, ok = localApp.homeAsking(); !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port" {
		t.Fatalf("homeLocalOpenReady did not raise the Move card: ok=%v question=%+v", ok, ask.question)
	}
}

// V1/V10 (contract 1 and contract 2): Enter on a row whose computer is not in
// the roster, is this computer itself, or when this computer is marked removed
// does not call OpenLive and takes the existing non-live road.
func TestEnterIsNotLiveForAnOfflineOrRemovedOrUnknownComputer(t *testing.T) {
	t.Run("unknown computer not in roster", func(t *testing.T) {
		a, _ := liveHomeRig(t, "dev_tower")
		calls, _ := wireFakeOpenLive(t, a, nil)
		a.machineRead.rows = []chatlist.Row{
			{Cell: "c-unknown", Title: "Unknown box", Device: "stranger", DeviceID: "dev_stranger", Status: chatlist.Running},
		}
		a.rebuildMachines()

		standOn(t, a, "c-unknown")

		if len(*calls) != 0 {
			t.Fatalf("OpenLive called %d times for a computer not in the roster", len(*calls))
		}
		ask, ok := a.homeAsking()
		if !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Unknown box" {
			t.Fatalf("unknown computer did not raise the Move card: ok=%v question=%+v", ok, ask.question)
		}
	})

	t.Run("this computer in roster", func(t *testing.T) {
		a, _ := liveHomeRig(t, "dev_tower")
		calls, _ := wireFakeOpenLive(t, a, nil)
		selfRow := chatlist.Row{Cell: "c-self", Title: "Own chat", Device: "This Mac", DeviceID: "dev_me", Status: chatlist.Running}
		drain(t, a, a.homeMachineEnter(homeLine{kind: homeMachineRow, remote: &selfRow}))

		if len(*calls) != 0 {
			t.Fatalf("OpenLive called %d times for this computer itself", len(*calls))
		}
		ask, ok := a.homeAsking()
		if !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Own chat" {
			t.Fatalf("self row did not fall back to the Move card: ok=%v question=%+v", ok, ask.question)
		}
	})

	t.Run("computer marked removed", func(t *testing.T) {
		a, _ := liveHomeRig(t, "dev_tower")
		calls, _ := wireFakeOpenLive(t, a, nil)
		a.machineRead.computerRemoved = true

		standOn(t, a, "c1")

		if len(*calls) != 0 {
			t.Fatalf("OpenLive called %d times when computerRemoved was true", len(*calls))
		}
		if !strings.Contains(a.home.msg, chatlist.RemovedChat) {
			t.Fatalf("removed computer foot = %q, want %q", a.home.msg, chatlist.RemovedChat)
		}
	})
}
