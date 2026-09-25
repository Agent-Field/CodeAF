package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── THE PANELS (docs/design/home-mission-control/DESIGN.md §1, §3 G2–G6) ────

// homeLineAfter is the frame line under the first one holding a word.
func homeLineAfter(frame, word string) string { return homeLineBelow(frame, word, 1) }

// homeLineBelow is the line n rows under the first line of home's body that
// carries a word: a read row's thread title is one under it, and its sentence
// with its answers three under it, past the blank.
func homeLineBelow(frame, word string, n int) string {
	lines := strings.Split(frame, "\n")
	for y, line := range lines {
		if y >= placeHeadRows && strings.Contains(line, word) && y+n < len(lines) {
			return lines[y+n]
		}
	}
	return ""
}

// A QUESTION IS ON ITS ROW WITH ITS ANSWERS: the row names the conversation, the
// line under it is what it asked, and the keys that answer it are drawn there.
func TestNeedsYouCarriesTheQuestionAndItsAnswersOnTheRow(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	// The lab's cursor is on the row, which is when its question and answers
	// are drawn under it; walked off, the row is its mark and title alone.
	frame := homeText(lab.a)
	if head := homeLineAfter(frame, "Pricing Research"); !strings.Contains(head, homeThreadWord+"Pricing Research") {
		t.Fatalf("the read row's description does not open with its thread's title line:\n%s", frame)
	}
	under := homeLineBelow(frame, "Pricing Research", 3)
	if !strings.Contains(under, "needs your ok to run bash") || !strings.Contains(under, "1 allow once") {
		t.Fatalf("the row does not carry its question and answers under the thread title:\n%s", frame)
	}
	lab.a.home.point(lab.a.file)
	if under := homeLineAfter(homeText(lab.a), "Pricing Research"); strings.Contains(under, "needs your ok to run bash") {
		t.Fatalf("the row draws its question with the cursor elsewhere:\n%s", homeText(lab.a))
	}
	if !strings.Contains(frame, "needs you") || strings.Contains(frame, "needs you · ") {
		t.Fatalf("the heading counts what is waiting, and it should be the word alone:\n%s", frame)
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

// WHERE YOU WERE: this window's own conversation first, in bold, the last thing
// said in it under it, then the most recent quiet ones. A row from another
// folder carries its project as its description, under the cursor — the margin
// is a time on every row (owner, 2026-09-15; the project used to be a tag
// beside the age, and `here` the own row's margin).
func TestWhereYouWereLeadsWithThisWindowsOwnConversation(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.last[lab.mine] = session.Summary{LastUser: "explain open addressing vs chaining"}
	a.home.build()
	frame := homeText(a)
	own, col := homeRowOf(frame, "Porting the Resume Picker")
	if own != placeHeadRows+1 || col != homeGridMargin+homeGridLead || strings.Contains(frame, "threads") {
		t.Fatalf("conversations do not start with bullets under Sessions:\n%s", frame)
	}
	lines := strings.Split(frame, "\n")
	if mine := panelRows(a, panelSessions)[0]; !mine.bold || mine.sub != "" {
		t.Fatalf("the narrow own row is not one bare bold line: %+v", mine)
	}
	// AND A ROW FROM ANOTHER FOLDER SAYS WHICH, where one from this folder does
	// not. The conversation mid-turn is here too: `running` lists the work a
	// conversation sent out and never the conversation, so this is its panel.
	if moving := lines[own+2]; !strings.Contains(moving, "Bounty Reward Companies") || strings.Contains(moving, "beta") {
		t.Fatalf("a row from another folder wears its project on its own line:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.title == "Bounty Reward Companies" })
	if bounty := a.home.lines[a.home.cursor]; bounty.cell.sub != "" || bounty.cell.grows {
		t.Fatalf("a row from another folder does not carry its project as its description: %+v", bounty.cell)
	}
	if quiet := lines[own+3]; !strings.Contains(quiet, "Quiet Chat a") {
		t.Fatalf("the quiet rows do not follow in recency order:\n%s", frame)
	}
	// AND THE TWO ROWS THAT ARE ON OTHER PANELS ARE NOT HERE A SECOND TIME. The
	// head is not counted: the strip of chats over home carries the tabs this
	// window has open, which is a different list from home's panels.
	body := strings.Join(lines[placeHeadRows:], "\n")
	if strings.Count(body, "Swarm Task Splitting") != 1 || strings.Count(body, "Bounty Reward Companies") > 1 {
		t.Fatalf("a conversation is drawn on two panels:\n%s", frame)
	}
}

// AND UNTIL THE JOURNAL'S TAIL HAS BEEN READ THE `here` ROW IS ONE LINE: no
// caption, no placeholder — the next conversation stands right under it (the
// one mid-turn, which stays on this panel: `running` lists work, never chats).
func TestTheHereRowIsOneLineUntilItsSnippetArrives(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	a.home.last = map[string]session.Summary{}
	a.home.build()
	frame := homeText(a)
	own, _ := homeRowOf(frame, "Porting the Resume Picker")
	if next := homeLineAfter(frame, "Porting the Resume Picker"); own < 0 || !strings.Contains(next, "Swarm Task Splitting") {
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
	_ = a.tabsRow(a.width)
	rememberUnheldTab(a, before, a.workspace, "Quiet Chat c")
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

// Project clicks choose the next message's folder without moving the keyboard
// cursor. Exercise the painted names at every layout width, then send through
// the real home entry point to prove the selection is more than a label.
func TestHomeProjectClickSelectsNextMessageDestination(t *testing.T) {
	for _, width := range []int{80, 120, 180} {
		t.Run(itoa(width), func(t *testing.T) {
			lab := newSwitchLab(t)
			a := lab.open(width, 60)
			a.tilde = lab.work
			a.home.tilde = lab.work
			a.home.build()
			frame := homeText(a)
			y, x := homeRowOf(frame, "~/beta")
			if y < 0 {
				t.Fatalf("the project name is missing:\n%s", frame)
			}
			for _, line := range a.home.lines {
				if line.kind == homeProjectRow && line.stop() {
					t.Fatal("a project became a keyboard stop")
				}
			}
			cursor, file, workspace := a.home.cursor, a.file, a.workspace
			before := a.targetWhere()
			drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
			if a.home.cursor != cursor || a.targetWhere() != before {
				t.Fatal("hover changed the cursor or selected project")
			}
			if row := homeFrameRow(a, y); !strings.Contains(row, a.pal.underline(a.pal.ink("~/beta"))) {
				t.Fatalf("the hovered project did not underline: %q", row)
			}
			beta := lab.workspace("beta")
			for i := 0; i < 2; i++ {
				drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				if a.target.where != beta || a.page != pageHome || a.home.cursor != cursor || a.file != file || a.workspace != workspace || a.strip.open {
					t.Fatalf("project click changed more than its target: target=%q page=%v cursor=%d file=%q workspace=%q", a.target.where, a.page, a.home.cursor, a.file, a.workspace)
				}
				if a.home.msg != "" || !strings.Contains(homeText(a), targetProjectLead+targetPathWord(a)) {
					t.Fatal("the message-region project did not show the selection quietly")
				}
			}
			drive(t, a, key("alt+p"))
			if a.target.where != lab.workspace("alpha") {
				t.Fatal("Option+P did not continue from the clicked project")
			}
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseMotionMsg{X: 0, Y: 0})
			if a.home.projectHover != -1 {
				t.Fatal("the project stayed hovered after the pointer left")
			}
			var opened string
			next := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
			a.start = func(where string) (Conversation, error) {
				opened = where
				return Conversation{Agent: next, SessionFile: where + "/next/transcript.jsonl", Workspace: where}, nil
			}
			typeHome(a, "explain the lexer")
			runCmd(a.homeEnter())
			if opened != beta || len(next.sent) != 1 || next.sent[0] != "explain the lexer" {
				t.Fatalf("message missed the clicked project: opened=%q sent=%q", opened, next.sent)
			}
			runCmd(a.openHome())
			if a.targetWhere() != beta {
				t.Fatal("returning home forgot the clicked project")
			}
		})
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

// Project rows retain the absolute root or home prefix and cut on the right.
func TestProjectPathsKeepTheirRootsAndTruncateOnTheRight(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	for _, path := range []string{"/tmp/landing-test", "/tmp/very-long-project-name/another-directory", "~/Documents/agentfield/code/codeaf"} {
		cell := &homeCell{panel: panelProjects, path: true, pad: homeProjectPad,
			title: path, note: "61 chats", right: "master, 3 files dirty"}
		for _, width := range []int{56, 48, 32, 20} {
			row := plain(homeCellBody(cell, width, a.pal, false))
			prefix := "/tmp/"
			if strings.HasPrefix(path, "~/") {
				prefix = "~/"
			}
			if ansi.StringWidth(row) > width || !strings.HasPrefix(row, prefix) || strings.HasPrefix(row, "…") {
				t.Fatalf("at %d cells the project path %q reads %q", width, path, row)
			}
			if width >= 48 && (!strings.Contains(row, "61 chats") || !strings.Contains(row, "master, 3 files dirty")) {
				t.Fatalf("at %d cells the path displaced its facts: %q", width, row)
			}
		}
	}
	for _, tc := range []struct{ path, want string }{
		{"/tmp/landing-test", "/tmp/landing-test"},
		{"/home/pat", "~"},
		{"/home/pat/src", "~/src"},
		{"/home/patrick/src", "/home/patrick/src"},
	} {
		word := projectWord(session.Project{Path: tc.path}, "/home/pat")
		row := plain(homeCellBody(&homeCell{panel: panelProjects, path: true, title: word}, 80, a.pal, false))
		if row != tc.want {
			t.Fatalf("project %q reads %q, want %q", tc.path, row, tc.want)
		}
	}
}

// `~` IS NEVER A TAG: a chat in the home directory or in a scratch folder at the
// top of /tmp wears no project word — while a checkout that merely lives under
// a temporary directory keeps its name.
func TestAChatRowWearsNoTagForHomeOrAScratchFolder(t *testing.T) {
	row := func(project, workspace string) switcherRow {
		return switcherRow{project: project, session: session.SessionRow{Workspace: workspace}}
	}
	for _, r := range []switcherRow{row("~", "/home/pat"), row("af-stop-ws", "/tmp/af-stop-ws"), row("pat", "/home/pat")} {
		if tag := chatProjectTag(r, "/home/pat"); tag != "" {
			t.Fatalf("a chat in %s wears the tag %q", r.session.Workspace, tag)
		}
	}
	if tag := chatProjectTag(row("site", "/tmp/build/site"), "/home/pat"); tag != "site" {
		t.Fatalf("a checkout under /tmp/build lost its tag: %q", tag)
	}
	// AND THE PROJECTS PANEL STILL LISTS THE SCRATCH FOLDER, as a path.
	if got := projectWord(session.Project{Name: "af-stop-ws", Path: "/tmp/af-stop-ws"}, "/home/pat"); got != "/tmp/af-stop-ws" {
		t.Fatalf("the scratch folder is listed as %q", got)
	}
}

// THE LAUNCH FOLDER IS ALWAYS A ROW (DESIGN §4): a window opened in a folder
// nobody has spoken in, on a machine with no conversations at all, still draws
// `projects` with this folder under it — its path and no count clause.
func TestProjectsDrawsTheLaunchFolderOverAnEmptyWorld(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.workspace, a.tilde = lab.workspace("codeaf"), lab.work
	a.width, a.height = 120, 45
	a.openHome()
	frame := homeText(a)
	head, _ := homeRowOf(frame, "projects")
	lines := strings.Split(frame, "\n")
	if head < 0 || head+1 >= len(lines) {
		t.Fatalf("projects is not drawn over an empty world:\n%s", frame)
	}
	row := lines[head+1]
	if !strings.Contains(row, "~/codeaf") || strings.Contains(row, "chat") {
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
// the loudest day, and the two models most of it went to.
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
	if len(s.models) != 2 || s.models[0].share != 0.5 || s.models[1].share != 0.5 {
		t.Fatalf("the models are %+v, want two halves of the fortnight", s.models)
	}
	if want := strings.ToLower(now.AddDate(0, 0, -5).Format("Mon")); s.loud != want || s.loudUSD != 1.00 {
		t.Fatalf("the loudest day is %q at %v, want %q at $1", s.loud, s.loudUSD, want)
	}
}

// homeSpendLab is a fortnight a person would recognise: fourteen days, one loud
// sunday, two models, and a day of three chats and one task.
func homeSpendLab(t *testing.T) *app {
	t.Helper()
	lab := newSwitchLab(t)
	a := lab.open(120, 45)
	days := make([]float64, homeSpendDays)
	for i := range days {
		days[i] = 4
	}
	days[6], days[13] = 88.10, 170
	a.home.spend = homeSpendReading{today: 170, ceiling: 500, days: days, total: 204.36, loud: "sun", loudUSD: 88.10,
		models: []homeSpendModel{{name: "glm-5.3", share: 0.55}, {name: "opus", share: 0.31}}}
	a.home.build()
	return a
}

// spendPanelText is the spend panel painted at one column's width, a line of
// plain text a screen row.
func spendPanelText(a *app, width int) []string {
	var out []string
	for at, line := range a.home.lines {
		if got, ok := line.panelOf(); !ok || got != panelSpend {
			continue
		}
		for _, row := range a.homeLineRows(line, at, width, a.pal, false, false) {
			out = append(out, plain(row.text))
		}
	}
	return out
}

// SPEND IS A SMALL HUD: a bare heading, one thin meter, the fortnight in
// block cells with its total and
// its loudest day, and the models beside what the day was spent on.
func TestSpendIsASmallHudOfThreeLines(t *testing.T) {
	a := homeSpendLab(t)
	rows := spendPanelText(a, 58)
	if len(rows) != 4 {
		t.Fatalf("the spend panel is %d rows at 58 cells, want a heading and three lines:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(rows[0]) != "spend" {
		t.Fatalf("the heading repeats the top line's daily spend: %q", rows[0])
	}
	if !strings.Contains(rows[1], homeSpendRun+homeSpendRest) || !strings.HasSuffix(rows[1], " 34%") {
		t.Fatalf("the meter is not one thin line with its share: %q", rows[1])
	}
	spark := strings.TrimSpace(rows[2])
	if !strings.HasPrefix(spark, "▁▁▁▁▁▁▅▁▁▁▁▁▁█") || !strings.Contains(spark, "14 days $204.36 · loudest sun $88.10") {
		t.Fatalf("the fortnight is not fourteen block cells with its total and loudest day: %q", rows[2])
	}
	for _, braille := range tokensBraille {
		if strings.Contains(strings.Join(rows, ""), braille) {
			t.Fatalf("the panel still draws braille: %q", rows[2])
		}
	}
	if !strings.Contains(rows[3], "glm-5.3 55% · opus 31%") {
		t.Fatalf("the models are not on the last line: %q", rows[3])
	}
	// AT FORTY CELLS every line still fits and no clause is cut mid-word: the
	// loudest day gives way whole, and the fortnight keeps its fourteen cells.
	for _, row := range spendPanelText(a, 40) {
		if len([]rune(row)) > 40 || strings.Contains(row, glyphMore) {
			t.Fatalf("at 40 cells the panel draws %q", row)
		}
	}
	// AND NOTHING UNDER THE HEADING IS A STOP OR A DOOR (owner, 2026-09-17):
	// the cursor steps over every one of the panel's lines, and a press on one
	// leaves home up. The heading is still the door into the spend place.
	for at, line := range a.home.lines {
		if line.cell == nil || line.cell.panel != panelSpend || line.cell.kind == cellHead {
			continue
		}
		if line.stop() {
			t.Fatalf("line %d of the spend panel (%q) is a cursor stop", at, line.cell.title)
		}
		if line.kind != homeReadout {
			t.Fatalf("line %d of the spend panel is a %v line, want a readout", at, line.kind)
		}
	}
	// The rows are found under the painted heading, because a press resolves
	// against the frame that was drawn and never against the list: the three
	// lines under `spend` are the meter, the fortnight and the models.
	placeFrameText(a)
	x, y, ok := homeHeadingAt(a, "spend")
	if !ok {
		t.Fatalf("the spend heading is not on the frame:\n%s", homeText(a))
	}
	frame := strings.Split(homeText(a), "\n")
	for dy := 1; dy <= 3; dy++ {
		drive(t, a, tea.MouseClickMsg{X: x + homeGridLead, Y: y + dy, Button: tea.MouseLeft})
		if a.page != pageHome {
			t.Fatalf("a press on the spend row %q opened %q", strings.TrimSpace(frame[y+dy]), a.page.word())
		}
		if line, ok := a.home.focusedLine(); ok && line.cell != nil && line.cell.panel == panelSpend {
			t.Fatalf("a press on the spend row %q put the cursor on it", strings.TrimSpace(frame[y+dy]))
		}
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.at(pageSpend) {
		t.Fatal("a press on the spend heading did not open the spend place")
	}
}

// tokensBraille is the braille spark ramp the panel no longer draws.
var tokensBraille = []string{"⣀", "⣄", "⣤", "⣦", "⣶", "⣷", "⣿"}

// UNDER A TWENTIETH OF THE ALLOWANCE THERE IS NO METER, and with nothing on the
// ledger the panel is its heading and its whisper.
func TestSpendDrawsNoMeterForASliverAndWhispersOverAnEmptyLedger(t *testing.T) {
	a := homeSpendLab(t)
	a.home.spend.today = 6.51
	a.home.build()
	for _, row := range spendPanelText(a, 58) {
		if strings.Contains(row, homeSpendRun) || strings.Contains(row, "%") && !strings.Contains(row, "glm") {
			t.Fatalf("a day at 1%% of its allowance draws a meter: %q", row)
		}
	}
	a.home.spend = homeSpendReading{}
	a.home.build()
	rows := spendPanelText(a, 58)
	if len(rows) != 2 || strings.TrimSpace(rows[0]) != "spend" || strings.TrimSpace(rows[1]) != homeWhisper[panelSpend] {
		t.Fatalf("an empty ledger draws %q, want the heading and the whisper", rows)
	}
}

// NEXT UP IS SOONEST FIRST, as many as its budget, then the fold into standing.
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
		due("w5", "the fifth thing", 10*time.Hour), due("w6", "the sixth thing", 11*time.Hour),
	}}
	a.home.build()
	frame := homeText(a)
	first, _ := homeRowOf(frame, "top movers before the open")
	second, _ := homeRowOf(frame, "water the plants")
	// AND NOTHING AT THE ROW'S RIGHT: the time is said once, in the description
	// (owner, 2026-09-15). It used to read `in 1h` at the margin.
	if first < 0 || second != first+1 || strings.Contains(strings.Split(frame, "\n")[first], " in 1h") {
		t.Fatalf("scheduled is not soonest first with nothing at its right:\n%s", frame)
	}
	for _, cell := range panelRows(a, panelNext) {
		if cell.right != "" {
			t.Fatalf("a scheduled row carries %q at its right, want nothing", cell.right)
		}
	}
	if row, _ := homeRowOf(frame, "1 more"); row < 0 {
		t.Fatalf("the fourth order is not behind the fold:\n%s", frame)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelNext && l.stop() })
	a.homeKey(key("enter"))
	if !a.at(pageStanding) {
		t.Fatal("enter on a next-up row did not open standing")
	}
}

// A `standing` ROW SAYS ITS TIME ONCE, IN ITS DESCRIPTION, IN THE ONE SHAPE
// ITS KIND HAS (owner, 2026-09-15): a reminder the moment it goes off, a routine
// its cadence, its next and its last outcome, a watch how often it looks, when
// it last looked and what it found, a rule its own words. Nothing at the
// margin, and an order that has never woken has no `last`.
func TestScheduledSaysEachKindsTimeOneWayInItsDescription(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 45)
	dir := a.home.world.Projects[0].Dir
	now := a.home.world.Read
	watch := StandingItemView{Item: standing.Item{ID: "w", Words: "tell me when CI goes red", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenProbe, Words: "every five minutes"}, NextDue: now.Add(2 * time.Minute),
		LastChecked: now.Add(-3 * time.Minute), LastCheckLine: "the last five runs on master are green"}}
	quiet := StandingItemView{Item: standing.Item{ID: "q", Words: "watch the lockfile", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenFile, Words: "when go.sum changes"}, LastChecked: now.Add(-time.Hour)}}
	routine := StandingItemView{Item: standing.Item{ID: "r", Words: "sweep the repo every morning at nine", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenEvery, Words: "every morning at nine"}, NextDue: now.Add(26 * time.Hour),
		LastFired: now.Add(-11 * time.Hour), LastOutcome: "done: two branches landed, both in internal/tui3"}}
	fresh := StandingItemView{Item: standing.Item{ID: "f", Words: "remind me at six to leave", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenAt, Words: "at 6 today"}, NextDue: now.Add(4 * time.Hour)}}
	rule := StandingItemView{Item: standing.Item{ID: "h", Words: "never change the public API without telling me", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenHold, Words: "always"}}}
	stopped := StandingItemView{Item: standing.Item{ID: "s", Words: "keep main green", Status: standing.StatusActive,
		When: standing.When{Kind: standing.WhenProbe, Words: "when CI goes red"}, NeedsPerson: "the fix touches migrations"}}
	a.home.items = map[string][]StandingItemView{dir: {watch, quiet, routine, fresh, rule, stopped}}
	a.home.build()
	rows := map[string]*homeCell{}
	for _, cell := range panelRows(a, panelNext) {
		rows[cell.title] = cell
	}
	if _, drawn := rows["keep main green"]; drawn {
		t.Fatalf("an order stopped on a person is drawn on scheduled as well as needs you:\n%s", homeText(a))
	}
	want := []struct{ title, sub string }{
		{"tell me when CI goes red", "watch · every five minutes · last looked 3m ago · found: the last five runs on master are green"},
		{"watch the lockfile", "watch · when go.sum changes · last looked 1h ago · found nothing"},
		{"sweep the repo every morning at nine", "routine · every morning at nine · next " + homeClockAt(routine.Item.NextDue, now) + " · last: done: two branches landed, both in internal/tui3"},
		{"remind me at six to leave", "reminder · goes off " + homeClockAt(fresh.Item.NextDue, now)},
		{"never change the public API without telling me", "rule · always"},
	}
	for _, w := range want {
		cell, ok := rows[w.title]
		if !ok {
			t.Fatalf("scheduled does not draw %q:\n%s", w.title, homeText(a))
		}
		if cell.right != "" || cell.sub != w.sub {
			t.Fatalf("%q reads %q / %q, want nothing at the right and the sentence %q", w.title, cell.right, cell.sub, w.sub)
		}
	}
	if !strings.HasPrefix(homeClockAt(fresh.Item.NextDue, now), "today ") && !strings.HasPrefix(homeClockAt(fresh.Item.NextDue, now), "tomorrow ") {
		t.Fatalf("a moment four hours off reads %q, want today or tomorrow with a clock", homeClockAt(fresh.Item.NextDue, now))
	}
	if got := homeClockAt(now.Add(10*24*time.Hour), now); !strings.Contains(got, " ") || strings.HasPrefix(got, "today") {
		t.Fatalf("a moment ten days off reads %q, want a date and a clock", got)
	}
	// AND THE SENTENCE IS THERE AT EVERY WIDTH. With nothing at the right it is
	// the only place the row says its kind and its time, so a frame too narrow
	// for the description column draws it as the line under the cursor's row
	// rather than a bare title (review of #1046).
	for _, width := range []int{120, 80} {
		// The column count reaches the panels through the draw, as it does on a
		// terminal ([switchLab.open] states why).
		a.width = width
		homeText(a)
		if homeDescOn(a.home.cols) {
			t.Fatalf("%d columns has a description column; the test wants a frame without one", width)
		}
		// A narrow frame squeezes `standing` first (law 5), so the claim is
		// about every row it still draws and not about how many those are.
		sentence := map[string]string{}
		for _, w := range want {
			sentence[w.title] = w.sub
		}
		drawn := panelRows(a, panelNext)
		if len(drawn) == 0 {
			t.Fatalf("scheduled draws no row at all at %d columns:\n%s", width, homeText(a))
		}
		for _, cell := range drawn {
			if cell.sub != sentence[cell.title] || !cell.grows {
				t.Fatalf("at %d columns %q reads %q (grows %v), want the sentence %q under the cursor", width, cell.title, cell.sub, cell.grows, sentence[cell.title])
			}
		}
	}
}

// headingOf is a panel's heading exactly as it is built — the bare word, or the
// word with whatever clause the panel put after it.
func headingOf(a *app, panel homePanelID) string {
	for _, line := range a.home.lines {
		if line.cell != nil && line.cell.panel == panel && line.cell.kind == cellHead {
			return line.cell.title
		}
	}
	return ""
}
