package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE PANELS (docs/design/home-mission-control/DESIGN.md §1, §3 G2–G6) ────

// homeLineAfter is the frame line under the first one holding a word.
func homeLineAfter(frame, word string) string {
	lines := strings.Split(frame, "\n")
	for y, line := range lines {
		if y >= placeHeadRows && strings.Contains(line, word) && y+1 < len(lines) {
			return lines[y+1]
		}
	}
	return ""
}

// A QUESTION IS ON ITS ROW WITH ITS ANSWERS: the row names the conversation, the
// line under it is what it asked, and the keys that answer it are drawn there.
func TestNeedsYouCarriesTheQuestionAndItsAnswersOnTheRow(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	frame := homeText(lab.a)
	under := homeLineAfter(frame, "Pricing Research")
	if !strings.Contains(under, "needs your ok to run bash") || !strings.Contains(under, "1 allow once") {
		t.Fatalf("the row does not carry its question and answers:\n%s", frame)
	}
	if !strings.Contains(frame, "needs you · 1") {
		t.Fatalf("the heading does not count what is waiting:\n%s", frame)
	}
}

// A DIGIT ANSWERS THE TOP QUESTION FROM ANYWHERE ON HOME (law 7): the cursor is
// on this window's own conversation, and `3` still reaches the other one.
func TestADigitAnswersTheTopQuestionWithTheCursorElsewhere(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	lab.a.home.point(lab.a.file)
	homeText(lab.a)
	lab.a.homeKey(key("3"))
	if len(*lab.sent) != 1 || (*lab.sent)[0].dir != lab.dir || (*lab.sent)[0].key != "3" {
		t.Fatalf("the digit did not answer the top question: %+v", *lab.sent)
	}
	if typed := lab.a.home.box.String(); typed != "" {
		t.Fatalf("the digit also typed %q", typed)
	}
}

// RUNNING DRAWS ITS ROWS WITH ONE MOVING CELL, on the first, and the line under
// each row says what it is doing. A presence row with no title of its own is
// named from the project's record.
func TestRunningDrawsTheWorkAndWhatItIsDoing(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	if !strings.Contains(frame, "running · 1") || !strings.Contains(frame, "read 40 filings") {
		t.Fatalf("running does not draw the work that is out:\n%s", frame)
	}
	if under := homeLineAfter(frame, "read 40 filings"); !strings.Contains(under, tabSignalWord(tabWorking)) {
		t.Fatalf("the running row does not say what it is doing:\n%s", frame)
	}
	if spin := a.home.spinAt(); spin < 0 || a.home.lines[spin].cell.panel != panelRunning {
		t.Fatalf("the one moving cell is not on the running panel's first row")
	}
}

// NO ROW WEARS A MARK BUT THE TWO (law 8): the question mark on a waiting row and
// the one moving cell. The quiet rows' circles are gone.
func TestNothingOnTheGridWearsAGlyphButTheTwoMarks(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	for _, banned := range []string{"○", "✓", "▸", "◦"} {
		body := strings.Join(strings.Split(frame, "\n")[placeHeadRows:], "\n")
		if strings.Contains(body, banned) {
			t.Fatalf("a row on the grid wears %q:\n%s", banned, frame)
		}
	}
}

// A `since you left` LINE IS A DOOR INTO ITS PLACE, on its own panel.
func TestSinceYouLeftIsItsOwnPanelOfDoors(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.seen = lab.now.Add(-4 * time.Hour)
	a.home.ledger = switcherLedgerInput{learned: 2}
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "since you left · 4h") || !strings.Contains(frame, "learned 2 things") {
		t.Fatalf("the ledger is not on its panel:\n%s", frame)
	}
	for at, line := range a.home.lines {
		if line.cell != nil && line.cell.title == "learned 2 things" {
			a.home.cursor = at
		}
	}
	a.homeKey(key("enter"))
	if !a.at(pageMemory) {
		t.Fatal("enter on the memory line did not open the memory place")
	}
}

// WHERE YOU WERE: this window's own conversation first with `here`, the last
// thing said in it under it, then the most recent quiet ones, then the fold.
func TestWhereYouWereLeadsWithThisWindowsOwnConversation(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.last[lab.mine] = session.Summary{LastUser: "explain open addressing vs chaining"}
	a.home.build()
	frame := homeText(a)
	head, _ := homeRowOf(frame, "where you were")
	own, _ := homeRowOf(frame, "Porting the Resume Picker")
	if head < 0 || own != head+1 {
		t.Fatalf("this window's own conversation is not the first row of where you were:\n%s", frame)
	}
	lines := strings.Split(frame, "\n")
	if !strings.Contains(lines[own], homeHereWord) || !strings.Contains(lines[own+1], "explain open addressing") {
		t.Fatalf("the own row does not say here with its last words under it:\n%s", frame)
	}
	if !strings.Contains(frame, "6 more · "+homeFindWord) {
		t.Fatalf("the quiet tail is not folded behind one line:\n%s", frame)
	}
	// AND A ROW FROM ANOTHER FOLDER SAYS WHICH, where one from this folder does
	// not. The conversation mid-turn is here too: `running` lists the work a
	// conversation sent out and never the conversation, so this is its panel.
	if moving := lines[own+2]; !strings.Contains(moving, "Bounty Reward Companies") || !strings.Contains(moving, "beta") {
		t.Fatalf("a row from another folder does not carry its project:\n%s", frame)
	}
	if quiet := lines[own+3]; !strings.Contains(quiet, "Quiet Chat a") {
		t.Fatalf("the quiet rows do not follow in recency order:\n%s", frame)
	}
	// AND THE ROW WAITING ON A PERSON IS NOT HERE A SECOND TIME.
	if strings.Count(frame, "Swarm Task Splitting") != 1 {
		t.Fatalf("a conversation waiting on you is drawn on two panels:\n%s", frame)
	}
}

// AND UNTIL THE JOURNAL'S TAIL HAS BEEN READ THE `here` ROW IS ONE LINE: no
// caption, no placeholder — the next conversation stands right under it.
func TestTheHereRowIsOneLineUntilItsSnippetArrives(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.last = map[string]session.Summary{}
	a.home.build()
	frame := homeText(a)
	own, _ := homeRowOf(frame, "Porting the Resume Picker")
	if next := homeLineAfter(frame, "Porting the Resume Picker"); own < 0 || !strings.Contains(next, "Quiet Chat") {
		t.Fatalf("the here row carries a line under it with nothing said:\n%s", frame)
	}
}

// PRESELECT THE PREVIOUS THING (law 6): home opened from a conversation puts the
// cursor on the one this window was in before it, so enter is a switch in two
// keys.
func TestHomePreselectsTheConversationThisWindowWasInBefore(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.app(lab.mine)
	a.width, a.height = 120, 45
	var before string
	for _, row := range a.readWorld().Sessions() {
		if row.Title == "quiet chat c" {
			before = row.Transcript
		}
	}
	a.prev = []string{before}
	a.openHome()
	homeText(a)
	if got := a.home.focused(); got.Transcript != before {
		t.Fatalf("the cursor opened on %q, want the previous conversation %q", got.Title, before)
	}
}

// AND TYPING IS UNTOUCHED: with anything in the box the body is the search's
// drop-up, exactly as it was, and not a panel.
func TestTypingOnHomeStillRaisesTheSearch(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	for _, r := range "quiet" {
		a.homeKey(key(string(r)))
	}
	if a.home.gridOn() {
		t.Fatal("the grid is still up under a query")
	}
	for _, line := range a.home.lines {
		if line.cell != nil {
			t.Fatal("a line of the drop-up carries a panel's cell")
		}
	}
	if frame := homeText(a); !strings.Contains(frame, "Quiet Chat a") {
		t.Fatalf("the query did not find its rows:\n%s", frame)
	}
}

// homeLineOf puts the cursor on the first line a test predicate names.
func homeLineOf(t *testing.T, a *app, want func(homeLine) bool) {
	t.Helper()
	for at, line := range a.home.lines {
		if want(line) {
			a.home.cursor = at
			return
		}
	}
	t.Fatal("no line on home is the one the test wants")
}

// PROJECTS: this window's folder first, each with its chats and what is running
// in it, and the repository's state at the right.
func TestProjectsListsThisFolderFirstWithItsCountsAndRepository(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	beta := lab.workspace("beta")
	a.home.tilde = lab.work
	a.home.repos = map[string]homeRepoReading{beta: {line: "master · 2 files dirty"}}
	a.home.build()
	frame := homeText(a)
	head, _ := homeRowOf(frame, "projects")
	lines := strings.Split(frame, "\n")
	if head < 0 || head+2 >= len(lines) {
		t.Fatalf("projects is not drawn:\n%s", frame)
	}
	if !strings.Contains(lines[head+1], "~/alpha") || !strings.Contains(lines[head+1], "2 chats") {
		t.Fatalf("this window's folder is not the first project:\n%s", frame)
	}
	second := lines[head+2]
	for _, want := range []string{"beta", "10 chats · 1 running", "master, 2 files dirty"} {
		if !strings.Contains(second, want) {
			t.Fatalf("the beta row does not say %q:\n%s", want, frame)
		}
	}
}

// ENTER ON A PROJECT STARTS A CONVERSATION THERE, and home steps aside for it.
func TestEnterOnAProjectStartsAConversationInThatFolder(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	beta := lab.workspace("beta")
	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeProjectRow && l.proj.Path == beta })
	a.homeKey(key("enter"))
	if a.at(pageHome) || a.workspace != beta {
		t.Fatalf("enter on the beta project left home=%v in %q, want a conversation in %q", a.at(pageHome), a.workspace, beta)
	}
}

// AND ITS VERBS ARE ITS CHATS AND ITS FOLDER.
func TestAProjectOffersItsChatsAndItsFolder(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeProjectRow && l.project == "beta" })
	verbs := a.homeRowVerbs()
	if len(verbs) != 2 || verbs[0].word != homeProjectChatsWord || verbs[1].word != homeProjectFolderWord {
		t.Fatalf("a project offers %+v", verbs)
	}
	verbs[0].do()
	if a.home.box.String() != "beta" || a.home.gridOn() {
		t.Fatalf("its chats did not search the project: box %q", a.home.box.String())
	}
}

// ON THE PROJECTS PANEL A FOLDER IS A PATH: the home directory's row reads `~`
// there, because the rule that retired `~` is about a NAME standing alone on a
// chat row — and a project that recorded no folder draws nothing.
func TestTheHomeDirectoryIsAPathOnTheProjectsPanel(t *testing.T) {
	if got := projectWord(session.Project{Name: "~", Path: "/home/pat"}, "/home/pat"); got != "~" {
		t.Fatalf("the home directory's path cell reads %q, want ~", got)
	}
	if got := projectWord(session.Project{Name: "-bucket"}, "/home/pat"); got != "" {
		t.Fatalf("a folderless project is called %q", got)
	}
	if got := projectWord(session.Project{Name: "site", Path: "/home/pat/site"}, "/home/pat"); got != "~/site" {
		t.Fatalf("a project under home is called %q, want ~/site", got)
	}
}

// THE LAUNCH FOLDER IS ALWAYS A ROW (DESIGN §4): a window opened in a folder
// nobody has spoken in, on a machine with no conversations at all, still draws
// `projects` with this folder under it — its path and no count clause.
func TestProjectsDrawsTheLaunchFolderOverAnEmptyWorld(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.workspace, a.tilde = lab.workspace("aforge-v2"), lab.work
	a.width, a.height = 120, 45
	a.openHome()
	frame := homeText(a)
	head, _ := homeRowOf(frame, "projects")
	lines := strings.Split(frame, "\n")
	if head < 0 || head+1 >= len(lines) {
		t.Fatalf("projects is not drawn over an empty world:\n%s", frame)
	}
	row := lines[head+1]
	if !strings.Contains(row, "~/aforge-v2") || strings.Contains(row, "chat") {
		t.Fatalf("the launch folder is not the first project, bare of counts:\n%s", frame)
	}
	if got := len(panelRows(a, panelProjects)); got != 1 {
		t.Fatalf("projects drew %d rows over an empty world, want the launch folder alone", got)
	}
}

// A HEADING NEVER DRAWS OVER NOTHING: a panel with no rows and no whisper is not
// on the page at all.
func TestAPanelWithNoRowsAndNoWhisperDrawsNothing(t *testing.T) {
	p := homeGridPanel{slot: homeSlotOf(panelProjects)}
	if p.height() != 0 || len(p.lines()) != 0 {
		t.Fatalf("an empty projects panel draws %d rows: %+v", p.height(), p.lines())
	}
}

// SPEND READS THE FORTNIGHT THE SPEND PLACE READS: today's figure, the total,
// and the model most of it went to.
func TestSpendReadsTodayAndTheFortnight(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.Local)
	lines := []session.UsageLine{
		{At: now.Add(-time.Hour), USD: 0.25, Model: "anthropic/claude-opus-5"},
		{At: now.AddDate(0, 0, -3), USD: 0.75, Model: "anthropic/claude-opus-5"},
		{At: now.AddDate(0, 0, -5), USD: 1.00, Model: "deepseek/deepseek-v4-flash"},
		{At: now.AddDate(0, 0, -30), USD: 9.00, Model: "anthropic/claude-opus-5"},
	}
	s := readHomeSpend(lines, now, 20)
	if s.today != 0.25 || s.total != 2.00 || len(s.days) != homeSpendDays {
		t.Fatalf("today %v, fortnight %v over %d days", s.today, s.total, len(s.days))
	}
	if s.share != 0.5 || s.top == "" {
		t.Fatalf("the top model is %q at %v, want half the fortnight", s.top, s.share)
	}
}

// AND THE PANEL SAYS IT: today against the allowance on the heading, the bar
// under it, the fortnight after that — and every row opens the spend place.
func TestSpendDrawsTodayTheBarAndTheFortnightAsDoors(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	days := make([]float64, homeSpendDays)
	days[3], days[13] = 2, 0.14
	a.home.spend = homeSpendReading{today: 0.14, ceiling: 20, days: days, total: 34.10, top: "opus", share: 0.63}
	a.home.build()
	frame := homeText(a)
	if row, _ := homeRowOf(frame, "today $0.14 of $20.00"); row < 0 {
		t.Fatalf("the spend heading does not carry today:\n%s", frame)
	}
	if row, _ := homeRowOf(frame, "14 days $34.10 · opus 63%"); row < 0 {
		t.Fatalf("the fortnight is not drawn:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.kind == cellSpark })
	a.homeKey(key("enter"))
	if !a.at(pageSpend) {
		t.Fatal("enter on the fortnight did not open the spend place")
	}
}

// NEXT UP IS SOONEST FIRST, three of them, then the fold into standing.
func TestNextUpIsSoonestFirstAndFoldsIntoStanding(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	due := func(id, words string, in time.Duration) StandingItemView {
		return StandingItemView{Item: standing.Item{ID: id, Words: words, Status: standing.StatusActive,
			When: standing.When{Kind: standing.WhenAt}, NextDue: lab.now.Add(in)}}
	}
	dir := a.home.world.Projects[0].Dir
	a.home.items = map[string][]StandingItemView{dir: {
		due("w4", "the fourth thing", 9*time.Hour), due("w1", "the 6am repo watch", 20*time.Hour),
		due("w2", "top movers before the open", 2*time.Hour), due("w3", "water the plants", 5*time.Hour),
	}}
	a.home.build()
	frame := homeText(a)
	first, _ := homeRowOf(frame, "top movers before the open")
	second, _ := homeRowOf(frame, "water the plants")
	if first < 0 || second != first+1 || !strings.Contains(strings.Split(frame, "\n")[first], " in 1h") {
		t.Fatalf("next up is not soonest first with its clause:\n%s", frame)
	}
	if row, _ := homeRowOf(frame, "1 more · standing"); row < 0 {
		t.Fatalf("the fourth order is not behind the fold:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelNext && l.stop() })
	a.homeKey(key("enter"))
	if !a.at(pageStanding) {
		t.Fatal("enter on a next-up row did not open standing")
	}
}
