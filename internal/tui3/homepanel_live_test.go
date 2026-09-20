package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
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
	rows := panelRows(a, panelNeeds)
	if len(rows) != 2 || rows[0].title != "Pricing Site" || rows[1].title != "Prime Sieve" {
		t.Fatalf("needs you is not the two waits, oldest first: %+v", rows)
	}
	if rows[0].sub != "needs your ok to run bash" || rows[0].right != "2h" {
		t.Fatalf("the top row is not the gate's own sentence with its age: %+v", rows[0])
	}
	// THE SENTENCE AND THE CHIPS ARE DRAWN UNDER THE ROW BEING READ, and at
	// rest a row is its mark, its title and its wait (owner, 2026-09-17).
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
	// AND THE HEADING IS THE WORD ALONE. It used to count the questions
	// (`needs you · 2`); the rows are under it (owner, 2026-09-15).
	if !strings.Contains(frame, "needs you") || strings.Contains(frame, "needs you · ") {
		t.Fatalf("the heading counts its rows:\n%s", frame)
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
	rows := panelRows(a, panelNeeds)
	// THE MARGIN IS WHEN IT LANDED AND NOTHING ELSE; its description leads with
	// the thread it belongs to, spelled as `threads` spells it (owner,
	// 2026-09-17), then the files it wrote (owner, 2026-09-15: the right margin
	// of every field row is a time). It used to read `3 files · 30m`.
	if len(rows) != 1 || rows[0].title != "fix the flaky sieve" || rows[0].right != "30m" || rows[0].thread != "Prime Sieve" || !strings.HasPrefix(rows[0].sub, "3 files · ") {
		t.Fatalf("the task's call is not a one-line row of needs you with its files in its description: %+v", rows)
	}
	if rows[0].mark != cellMarkNone {
		t.Fatalf("a landing wears a mark: %+v", rows[0])
	}
	frame := homeText(a)
	// THE GROUP LINE IS THE WORD ALONE — no count after it and no clause at its
	// right (owner, 2026-09-15; it used to say `to check · 1` and `finished,
	// nobody has checked it`).
	if !strings.Contains(frame, needsCheckWord) || strings.Contains(frame, needsCheckWord+" · ") ||
		strings.Contains(frame, "nobody has checked") {
		t.Fatalf("the group line does not name the group, or says more than its name:\n%s", frame)
	}
	if strings.Contains(frame, "landed unchecked") {
		t.Fatalf("the retired sub-line is still drawn:\n%s", frame)
	}
	for _, line := range panelLines(a, panelNeeds) {
		if line.cell.kind == cellRow && (line.task == nil || line.task.ID != "4") {
			t.Fatalf("the row does not carry the task its door opens: %+v", line)
		}
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
	rows := panelRows(a, panelNeeds)
	if len(rows) != 2 || rows[0].title != "Pricing Site" || rows[1].title != "fix the flaky sieve" {
		t.Fatalf("the week-old landing did not sort under the fresh question: %+v", rows)
	}
	if rows[0].mark != cellMarkNeeds || rows[1].mark != cellMarkNone {
		t.Fatalf("the mark is not on the stopped row alone: %+v", rows)
	}
	kinds := []homeCellKind{}
	for _, line := range panelLines(a, panelNeeds) {
		kinds = append(kinds, line.cell.kind)
	}
	if len(kinds) < 4 || kinds[0] != cellHead || kinds[1] != cellRow || kinds[len(kinds)-2] != cellGroup {
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
	if rows := panelRows(a, panelNeeds); len(rows) != 2 || rows[0].title != "call 1" || rows[1].title != "call 2" {
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
	if rows := panelRows(a, panelNeeds); len(rows) != 2 || rows[0].title != "call 1" || rows[1].title != "call 2" {
		t.Fatalf("needs you is not the two fresh calls: %+v", rows)
	}
	frame := homeText(a)
	// AND NEITHER THE HEADING NOR THE GROUP LINE COUNTS ANYTHING: the rows are
	// under them. The one count is the fold's, which counts the aged landings
	// with everything else it hides — `3 more`, no longer `3 older · tasks`.
	if !strings.Contains(frame, "needs you") || strings.Contains(frame, "needs you · ") {
		t.Fatalf("the heading counted the landings:\n%s", frame)
	}
	if !strings.Contains(frame, needsCheckWord) || strings.Contains(frame, needsCheckWord+" · ") {
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
	if rows := panelRows(a, panelNeeds); len(rows) != 5 {
		t.Fatalf("opening needs you drew %d rows, want all five landings:\n%s", len(rows), homeText(a))
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
	for _, own := range panelRows(a, panelRecent) {
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

// ── running ─────────────────────────────────────────────────────────────────

// THE DAY'S TASKS, FLATTENED, NEWEST FIRST: what is running off presence, with
// what its worker is doing and how far its run has got, and what landed inside
// the last day off the record, as a title and a time; a task that landed before
// the day is left to the tasks place. The one moving cell is on the first
// running row, and enter on a landed row opens that task inside the tasks
// place — the door the place's own list takes for the same row.
func TestTasksListsTheDaysWorkNewestFirstAndOpensTheTask(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "3", Title: "generate the first 200 primes", State: "running", StartedAt: l.now.Add(-4 * time.Minute),
			Activity: "bash · 12s", Done: 2, Total: 5},
	}})
	l.task("-beta", session.TaskIndexEntry{ID: "1", SessionID: "bbbb000000000001", Label: "benchmark the sieve",
		Title: "benchmark the sieve", Status: string(session.TaskDone),
		StartedAt: l.now.Add(-2 * time.Hour), EndedAt: l.now.Add(-90 * time.Minute)})
	l.task("-beta", session.TaskIndexEntry{ID: "2", SessionID: "bbbb000000000001", Label: "old audit",
		Title: "old audit", Status: string(session.TaskDone),
		StartedAt: l.now.Add(-30 * time.Hour), EndedAt: l.now.Add(-26 * time.Hour)})
	a := l.open()
	rows := panelRows(a, panelRunning)
	if len(rows) != 2 {
		t.Fatalf("tasks is not the running task and the landed one: %+v", rows)
	}
	running, landed := rows[0], rows[1]
	if running.title != "generate the first 200 primes" || running.right != "4m" || running.sub != "bash · 12s · 2 of 5" || running.mark != cellMarkSpin {
		t.Fatalf("the running row is not title, clock, activity and progress: %+v", running)
	}
	if landed.title != "benchmark the sieve" || landed.right != sinceAt(l.now.Add(-90*time.Minute), l.now) ||
		!strings.HasPrefix(landed.sub, "beta") || landed.mark != cellMarkNone {
		t.Fatalf("the landed row is not its title, when it landed and its project: %+v", landed)
	}
	frame := homeText(a)
	if strings.Contains(frame, "old audit") || headingOf(a, panelRunning) != "tasks" {
		t.Fatalf("a task older than the day is drawn, or the heading is not the bare word:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.title == "benchmark the sieve" })
	if line := a.home.lines[a.home.cursor]; line.kind != homeLedger || !line.stop() {
		t.Fatalf("a task row is not a ledger line the cursor can rest on: %+v", line)
	}
	a.homeKey(key("enter"))
	if !a.at(pageTasks) || !a.taskSheet.detailOn || a.taskSheet.detail.Label != "benchmark the sieve" {
		t.Fatalf("enter on a task row did not open that task inside the tasks place (at %q, detail %v %q)",
			a.page.word(), a.taskSheet.detailOn, a.taskSheet.detail.Label)
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
// conversation offers it, and another window's task offers only its row options.
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
	for _, v := range a.runningVerbs(theirs) {
		if v.key == 's' {
			t.Fatal("another conversation's task offered a stop")
		}
	}
	stopped := false
	for _, v := range a.runningVerbs(mine) {
		if v.key == 's' {
			v.do()
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("this window's own task offered no stop")
	}
	if !a.stopping() || a.at(pageHome) || !strings.Contains(plain(mustFrame(a)), "Stop this task?") {
		t.Fatalf("s did not raise the stop card where it can be read:\n%s", plain(mustFrame(a)))
	}
}

// ON A THREE-COLUMN HOME A `tasks` ROW IS IN THE FIELD — it has rows, and
// that is what the field is (law 2, ruled 2026-09-15). `→` on it opens the
// row's own strip, which offers the tasks place's `stop it`, and `ctrl+x`
// raises the stop card without the strip. THE FOOT IS THE RESTING SENTENCE:
// the owner ruled (2026-09-15) that every row of the field rests on the one
// sentence, and since the arrows stay in their column (2026-09-17) it names no
// chord either.
func TestAThreeColumnRunningRowRestsOnTheOneFootAndStillStops(t *testing.T) {
	l := newLiveLab(t)
	l.live("-alpha", "aaaa000000000001", session.SessionPresence{RunningTasks: []session.PresenceTask{
		{ID: "5", Title: "mine", State: "running", StartedAt: l.now.Add(-time.Minute)},
	}})
	a := l.open()
	a.width, a.height = 180, 45
	homeText(a)
	a.agent = &cancelFake{fakeAgent: &fakeAgent{model: "m"}}
	a.tasks = map[uint64]*taskNode{5: {id: 5, state: session.TaskRunning}}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelRunning && l.cell.title == "mine" })
	mine := a.home.cursor
	if got := a.home.columnOf(mine); got != 0 {
		t.Fatalf("tasks has rows and stands in column %d of a three-column home, want the field at 0", got)
	}
	stop := false
	for _, v := range a.runningVerbs(a.home.lines[mine]) {
		stop = stop || (v.key == 's' && v.word == stopActWord)
	}
	if !stop {
		t.Fatal("the row's options omitted stop it")
	}
	if foot := a.homeHint(); foot != restingFoot(a) {
		t.Fatalf("the foot on a running row this window holds is %q, want the resting sentence and draft controls", foot)
	}
	a.placeKeyPress(key("right"))
	if !a.strip.open || a.home.cursor != mine {
		t.Fatalf("→ on a field row landed on line %d (strip %v), want the row's own strip on line %d",
			a.home.cursor, a.strip.open, mine)
	}
	a.placeKeyPress(key("left"))
	a.home.cursor = mine
	drive(t, a, key("ctrl+x"))
	if !a.stopping() || a.at(pageHome) || !strings.Contains(plain(mustFrame(a)), "Stop this task?") {
		t.Fatalf("ctrl+x did not raise the stop card where it can be read:\n%s", plain(mustFrame(a)))
	}
}

// cancelFake is an engine that can end work.
type cancelFake struct{ *fakeAgent }

func (cancelFake) Cancel(string) (string, error) { return "stopping", nil }

// TASKS SHOWS UP TO TEN, and folds the rest behind `N more`, which opens the
// panel; the heading is the bare word.
func TestTasksShowsTenAndFoldsTheRest(t *testing.T) {
	l := newLiveLab(t)
	var out []session.PresenceTask
	for i := 0; i < 12; i++ {
		out = append(out, session.PresenceTask{ID: itoa(i + 1), Title: "part " + itoa(i+1), State: "running",
			StartedAt: l.now.Add(-time.Duration(i+1) * time.Minute)})
	}
	l.live("-alpha", "aaaa000000000002", session.SessionPresence{RunningTasks: out})
	a := l.open()
	if rows, most := panelRows(a, panelRunning), homeSlotOf(panelRunning).most; len(rows) != most || most != 10 {
		t.Fatalf("tasks drew %d rows, want its budget of ten (%d)", len(rows), most)
	}
	if frame := homeText(a); !strings.Contains(frame, "2 more") || strings.Contains(frame, "more · tasks") || headingOf(a, panelRunning) != "tasks" {
		t.Fatalf("the fold does not name what it holds, or the heading is not the bare word:\n%s", frame)
	}
}

// A WATCH IN THE MIDDLE OF FIRING IS NOT A TASK. It used to be a row of this
// panel beside the tasks; the panel is the day's tasks now (owner, 2026-09-17),
// and the firing item keeps its row on `scheduled`, with its own verbs.
func TestAFiringStandingItemIsNotARowOfTasks(t *testing.T) {
	a, _ := itemHome(t)
	for _, line := range panelLines(a, panelRunning) {
		if line.kind == homeItem || (line.cell != nil && line.cell.title == "remind me on Fridays") {
			t.Fatalf("the firing item is a row of tasks: %+v", line.cell)
		}
	}
	found := false
	for _, line := range panelLines(a, panelNext) {
		if line.cell != nil && line.cell.title == "remind me on Fridays" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the firing item is on no panel at all:\n%s", homeText(a))
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
	// THE MARGIN IS WHEN EACH HAPPENED, and the conversation a file was made in
	// and what a task cost are the rows' descriptions (owner, 2026-09-15). They
	// used to be the margins — a name on one row, money on the next.
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
