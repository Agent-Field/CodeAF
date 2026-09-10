package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE LIVE PANELS (docs/design/home-mission-control/DESIGN.md §3 P1–P3) ───

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
// consent line is the gate's own sentence, and the row below says `enter`
// because a second `1` on screen would be a guess.
func TestNeedsYouOrdersTheWaitsAndDrawsAnswersOnTheTopRowOnly(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-2*time.Hour))})
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(9, "needs your ok to run write", l.now.Add(-5*time.Minute))})
	a := l.open()
	rows := panelRows(a, panelNeeds)
	if len(rows) != 2 || rows[0].title != "Pricing Site" || rows[1].title != "Prime Sieve" {
		t.Fatalf("needs you is not the two waits, oldest first: %+v", rows)
	}
	if rows[0].sub != "needs your ok to run bash" || rows[0].right != "2h" {
		t.Fatalf("the top row is not the gate's own sentence with its age: %+v", rows[0])
	}
	frame := homeText(a)
	if under := homeLineAfter(frame, "Pricing Site"); !strings.Contains(under, "1 allow once  2 always  3 deny") {
		t.Fatalf("the top row does not draw its answers:\n%s", frame)
	}
	if under := homeLineAfter(frame, "Prime Sieve"); strings.Contains(under, "allow once") || !strings.Contains(under, "enter") {
		t.Fatalf("the second row drew a second set of answers:\n%s", frame)
	}
	if !strings.Contains(frame, "needs you · 2") {
		t.Fatalf("the heading does not count what waits:\n%s", frame)
	}
}

// A TASK THE RECORD MARKS AS YOUR CALL IS A ROW OF ITS OWN, with the engine's
// reason under it, and enter aims at the task rather than at the conversation's
// live edge.
func TestNeedsYouCarriesATaskWaitingOnYourCall(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "4", SessionID: "aaaa000000000002", Label: "fix the flaky sieve",
		Title: "fix the flaky sieve", Status: string(session.TaskUnverified), EndedAt: l.now.Add(-30 * time.Minute)})
	a := l.open()
	rows := panelRows(a, panelNeeds)
	if len(rows) != 1 || rows[0].title != "fix the flaky sieve" || rows[0].sub == "" || rows[0].subRight != needsOpenWord {
		t.Fatalf("the task's call is not a row of needs you: %+v", rows)
	}
	for _, line := range panelLines(a, panelNeeds) {
		if line.cell.kind == cellRow && (line.task == nil || line.task.ID != "4") {
			t.Fatalf("the row does not carry the task its door opens: %+v", line)
		}
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
	if rows := panelRows(a, panelNeeds); len(rows) != 2 || rows[0].title != "call 2" || rows[1].title != "call 1" {
		t.Fatalf("needs you is not the two fresh calls: %+v", rows)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "needs you · 2") || !strings.Contains(frame, "3 older · tasks") {
		t.Fatalf("the heading does not count what is listed, or the fold does not count what aged:\n%s", frame)
	}
}

// A LIVE QUESTION IS NEVER AGED OUT, however long it has waited.
func TestNeedsYouKeepsALiveQuestionPastTwoDays(t *testing.T) {
	l := newLiveLab(t)
	l.live("-beta", "bbbb000000000001", session.SessionPresence{State: session.PresenceWaiting,
		Question: consentQuestionAt(7, "needs your ok to run bash", l.now.Add(-5*24*time.Hour))})
	a := l.open()
	if rows := panelRows(a, panelNeeds); len(rows) != 1 || rows[0].title != "Pricing Site" {
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
	if frame := homeText(a); !strings.Contains(frame, "questions from any chat or task land here") {
		t.Fatalf("an empty needs you does not whisper:\n%s", frame)
	}
}

// consentQuestionAt is [consentQuestion] asked at a named instant.
func consentQuestionAt(id uint64, text string, asked time.Time) session.PresenceQuestion {
	q := consentQuestion(id, text)
	q.Asked = asked
	return q
}

// ── running ─────────────────────────────────────────────────────────────────

// A ROW PER PIECE OF WORK, the last started first: a task with what its worker
// is doing and how far its run has got, and a job with where it is and how long
// it has been up. The one moving cell is on the first row.
func TestRunningDrawsEachTaskAndJobWithWhatItIsDoing(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "3", Title: "generate the first 200 primes", State: "running", StartedAt: l.now.Add(-4 * time.Minute),
			Activity: "bash · 12s", Done: 2, Total: 5},
	}})
	l.live("-beta", "bbbb000000000001", session.SessionPresence{Jobs: []session.PresenceJob{
		{ID: "1", Title: "npm run dev", StartedAt: l.now.Add(-3 * time.Hour)},
	}})
	a := l.open()
	rows := panelRows(a, panelRunning)
	if len(rows) != 2 {
		t.Fatalf("running is not the task and the job: %+v", rows)
	}
	task, job := rows[0], rows[1]
	if task.title != "generate the first 200 primes" || task.right != "4m" || task.sub != "bash · 12s · 2 of 5" || task.mark != cellMarkSpin {
		t.Fatalf("the task row is not title, clock, activity and progress: %+v", task)
	}
	if !strings.HasSuffix(job.title, rowSep+runningJobWord) || !strings.HasPrefix(job.title, "npm run dev · ") ||
		job.right != "up 3h" || job.mark != cellMarkNone {
		t.Fatalf("the job row is not `<title> · <project> · a background job` up its age: %+v", job)
	}
	if frame := homeText(a); !strings.Contains(frame, "running · 2") {
		t.Fatalf("the heading does not count the work:\n%s", frame)
	}
}

// A PIECE OF WORK UNDER A CHECK SAYS SO: the phase is the line when the worker
// is not the node's life, which is exactly when the engine leaves the activity
// off the file.
func TestRunningSaysThePhaseWhenThereIsNoActivity(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "3", Title: "sieve", State: "running", StartedAt: l.now.Add(-time.Minute), Phase: session.TaskPhaseChecking},
	}})
	rows := panelRows(l.open(), panelRunning)
	if len(rows) != 1 || rows[0].sub != taskCheckingWord {
		t.Fatalf("the running row does not say it is being checked: %+v", rows)
	}
}

// `s` STOPS ONLY WHAT THIS WINDOW HOLDS: a task of this window's own
// conversation offers it, and one in another window offers nothing.
func TestRunningOffersStopOnlyOnThisWindowsOwnTask(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000001", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "5", Title: "mine", State: "running", StartedAt: l.now.Add(-time.Minute)},
	}})
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "6", Title: "theirs", State: "running", StartedAt: l.now.Add(-2 * time.Minute)},
	}})
	a := l.open()
	a.agent = &cancelFake{fakeAgent: &fakeAgent{model: "m"}}
	a.tasks = map[uint64]*taskNode{5: {id: 5, state: session.TaskRunning}, 6: {id: 6, state: session.TaskRunning}}
	var mine, theirs homeLine
	for _, line := range panelLines(a, panelRunning) {
		switch {
		case line.cell.title == "mine":
			mine = line
		case line.cell.title == "theirs":
			theirs = line
		}
	}
	if verbs := a.runningVerbs(theirs); len(verbs) != 0 {
		t.Fatalf("another conversation's task offered a stop: %+v", verbs)
	}
	verbs := a.runningVerbs(mine)
	if len(verbs) != 1 || verbs[0].key != 's' {
		t.Fatalf("this window's own task offered no stop: %+v", verbs)
	}
	verbs[0].do()
	if !a.stopping() || a.at(pageHome) || !strings.Contains(plain(mustFrame(a)), "Stop this task?") {
		t.Fatalf("s did not raise the stop card where it can be read:\n%s", plain(mustFrame(a)))
	}
}

// cancelFake is an engine that can end work.
type cancelFake struct{ *fakeAgent }

func (cancelFake) Cancel(string) (string, error) { return "stopping", nil }

// RUNNING GROWS INTO A TALL FRAME UP TO ITS BUDGET, and folds the rest behind a
// door into tasks.
func TestRunningGrowsToItsBudgetAndFoldsTheRestIntoTasks(t *testing.T) {
	l := newLiveLab(t)
	var out []session.PresenceTask
	for i := 0; i < 10; i++ {
		out = append(out, session.PresenceTask{ID: itoa(i + 1), Title: "part " + itoa(i+1), State: "running",
			StartedAt: l.now.Add(-time.Duration(i+1) * time.Minute)})
	}
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: out})
	a := l.open()
	if rows, most := panelRows(a, panelRunning), homeSlotOf(panelRunning).most; len(rows) != most {
		t.Fatalf("running drew %d rows, want its budget of %d", len(rows), most)
	}
	if frame := homeText(a); !strings.Contains(frame, "2 more · tasks") || !strings.Contains(frame, "running · 10") {
		t.Fatalf("the fold does not name what it holds:\n%s", frame)
	}
}

// AN EMPTY PANEL WHISPERS.
func TestRunningWhispersWhenNothingIsOut(t *testing.T) {
	a := newLiveLab(t).open()
	if frame := homeText(a); !strings.Contains(frame, homeWhisper[panelRunning]) {
		t.Fatalf("an empty running does not whisper:\n%s", frame)
	}
}

// ── since you left ──────────────────────────────────────────────────────────

// A LINE PER TASK THAT LANDED AND PER FILE MADE, newest first, under a heading
// that says how long the person was away: the task with its outcome and its
// cost, a task that stopped with why, the file with the conversation that made
// it. A task's part is on its record and not on this panel.
func TestSinceYouLeftNamesEachLandedTaskAndFile(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "spark fleet ssh audit", Title: "spark fleet ssh audit",
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
	want := []string{"made apartments-minto.md", "port the parser · lost the connection", "spark fleet ssh audit · 2 hosts up, 1 not"}
	if len(rows) != len(want) {
		t.Fatalf("since you left drew %d lines, want %d:\n%s", len(rows), len(want), homeText(a))
	}
	for i, title := range want {
		if rows[i].title != title {
			t.Fatalf("line %d is %q, want %q", i, rows[i].title, title)
		}
	}
	if rows[0].right != "Pricing Site" || rows[2].right != "$0.42" || rows[1].right != "" {
		t.Fatalf("the right margins are not the conversation and the cost: %+v", rows)
	}
	if frame := homeText(a); !strings.Contains(frame, "since you left · 12h") {
		t.Fatalf("the heading does not say how long you were away:\n%s", frame)
	}
}

// A LANDED TASK'S LINE IS A DOOR INTO ITS RECORD.
func TestSinceYouLeftOpensALandedTasksRecord(t *testing.T) {
	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "spark fleet ssh audit", Title: "spark fleet ssh audit",
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
	if frame := homeText(a); !strings.Contains(frame, homeWhisper[panelLeft]) || strings.Contains(frame, "audit") {
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
