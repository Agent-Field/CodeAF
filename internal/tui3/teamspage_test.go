package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAMS PAGE'S LAB ────────────────────────────────────────────────────

// teamsPlaceLab is the teams page over three conversations in two teams, orbit
// nested under harbor, with harbor (the conversation in front) selected and no
// manager yet, so the page draws the shared frame.
func teamsPlaceLab(t *testing.T) *app {
	t.Helper()
	a, _, _ := teamsPlaceLabIDs(t)
	return a
}

// teamsPlaceLabIDs is [teamsPlaceLab] with the two teams' ids.
func teamsPlaceLabIDs(t *testing.T) (a *app, harbor, orbit string) {
	t.Helper()
	a, harbor, orbit = menuApp(t)
	a.profileDir = t.TempDir()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetParent(orbit, harbor) }); err != nil {
		t.Fatal(err)
	}
	a.width, a.height = 120, 24
	if cmd := a.showPage(pageTeams); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageTeams) {
		t.Fatal("the teams place did not open")
	}
	a.teamsSync()
	a.frame()
	return a, harbor, orbit
}

// teamsHits is, for each row of the last frame, the index of the target the
// keyboard walks on it (the cursor's own where it is on that row), and -1 for
// a row with none.
func teamsHits(a *app) []int {
	a.frame()
	hits := make([]int, a.height)
	for i := range hits {
		hits[i] = -1
	}
	cur := a.teamsCursorIndex()
	for i, tg := range a.tp.targets {
		// The rail's rows, which the arrows walk as one list, and of those the
		// ones a press at the left edge (where the shared tests press) lands on.
		if tg.y < 0 || tg.y >= len(hits) || tg.pane || tg.x0 > 4 || tg.x1 <= 4 {
			continue
		}
		if hits[tg.y] < 0 || i == cur {
			hits[tg.y] = tg.line
		}
	}
	return hits
}

// teamsCursorLine is the body line the keyboard is on, -1 for none.
func teamsCursorLine(a *app) int {
	if t, ok := a.teamsCursorTarget(); ok {
		return t.line
	}
	return -1
}

// teamsFrameText is the whole frame, plain.
func teamsFrameText(a *app) string {
	f, _, _ := a.frame()
	return plain(f)
}

// teamsTargetOf is the first drawn target of act (and id, when given).
func teamsTargetOf(t *testing.T, a *app, act teamsAct, id string) teamsTarget {
	t.Helper()
	a.frame()
	for _, tg := range a.tp.targets {
		if tg.act == act && (id == "" || tg.id == id) {
			return tg
		}
	}
	t.Fatalf("no target %d %q on the frame:\n%s", act, id, teamsFrameText(a))
	return teamsTarget{}
}

// ── the place ───────────────────────────────────────────────────────────────

// TEAMS IS THE SECOND PLACE, right after home, on the bar and on the digits
// (ruling c-2), and /teams opens it too.
func TestTeamsIsTheSecondPlaceOnTheBarTheDigitsAndTheCommand(t *testing.T) {
	if placeOrder[1] != pageTeams {
		t.Fatalf("the second place is %q", placeOrder[1].word())
	}
	a := placeApp(t)
	bar := navPlaces(a, 120, false)
	if !placeWordsInOrder(bar, "home", "teams", "chats", "sessions") {
		t.Fatalf("the bar does not put teams after home: %q", bar)
	}
	drive(t, a, key("alt+2"))
	if !a.at(pageTeams) {
		t.Fatalf("alt+2 landed on %q", a.page.word())
	}
	a.leavePlace()
	typeLine(t, a, "/teams")
	if !a.at(pageTeams) {
		t.Fatalf("/teams landed on %q", a.page.word())
	}
}

// NO TEAMS IS ONE SENTENCE AND TWO BUTTONS, and the organize button opens the
// wall's proposal.
func TestTeamsWithNoTeamsSaysWhatTheyAreAndOffersTwoWays(t *testing.T) {
	a := placeApp(t)
	a.width, a.height = 110, 24
	drive(t, a, key("alt+2"))
	text := teamsFrameText(a)
	for _, want := range []string{"A team is a set of conversations", teamsOrganizeWord, teamsNewTeamWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("the empty page lost %q:\n%s", want, text)
		}
	}
}

// THE RAIL IS THE TREE: orbit under harbor, indented, and the needs-you mark
// only when a packet waits on the person.
func TestTeamsRailIsTheTreeWithMarksOnlyWhenSomethingHappens(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	rail := func() []string {
		lines := strings.Split(teamsFrameText(a), "\n")
		for i, l := range lines {
			if at := strings.Index(l, "│"); at >= 0 {
				lines[i] = l[:at]
			}
		}
		return lines
	}
	lines := rail()
	hy, oy := -1, -1
	for y, l := range lines {
		// The head is skipped: the strip's team chip reads `● harbor ▾` on
		// every page.
		if y < placeHeadRows {
			continue
		}
		if hy < 0 && strings.Contains(l, "harbor") {
			hy = y
		}
		if oy < 0 && strings.Contains(l, "orbit") {
			oy = y
		}
	}
	if hy < 0 || oy <= hy {
		t.Fatalf("harbor at %d, orbit at %d:\n%s", hy, oy, strings.Join(lines, "\n"))
	}
	if strings.Index(lines[oy], "orbit") <= strings.Index(lines[hy], "harbor") {
		t.Fatalf("orbit is not indented under harbor:\n%s\n%s", lines[hy], lines[oy])
	}
	if strings.Contains(lines[hy], "?") || strings.Contains(lines[oy], "?") {
		t.Fatalf("a quiet team carries a mark:\n%s", strings.Join(lines, "\n"))
	}
	a.tp.packets = []teamstore.Packet{{ID: "p1", Team: teamstore.Person, Origin: orbit,
		Kind: teamstore.PacketQuestion, RaisedBy: "boss", Question: "Friday or Monday?",
		State: teamstore.PacketOpen}}
	a.tp.top = teamsTopCache{}
	a.touch()
	lines = rail()
	if !strings.Contains(lines[oy], "? 1") {
		t.Fatalf("orbit does not say a packet waits on you:\n%s", lines[oy])
	}
	_ = harbor
}

// THE INBOX CARD DECIDES A PACKET with one press on an option's word.
func TestTeamsInboxCardDecidesAPacket(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	flushTeams(t, a)
	seam := a.teamsSeam()
	p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor,
		Kind: teamstore.PacketConflict, RaisedBy: "boss", Question: "Which parser wins?",
		Options: []teamstore.Option{{ID: "a", Label: "Keep the old one", Consequence: "no churn"},
			{ID: "b", Label: "Take the new one", Consequence: "two files move"}},
		Recommendation: &teamstore.Recommendation{Option: "b", Reason: "it is faster"}})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamsRead(false))...)
	text := teamsFrameText(a)
	for _, want := range []string{"Which parser wins?", "Keep the old one", "Take the new one", "recommended"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card lost %q:\n%s", want, text)
		}
	}
	var opt teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOption && tg.arg == p.ID && tg.opt == "b" {
			opt = tg
		}
	}
	if opt.opt == "" {
		t.Fatalf("no button for the recommended option:\n%s", text)
	}
	drive(t, a, runCmd(a.teamsDo(opt))...)
	list, _, _, err := seam.Packets(teamstore.ScopeAll, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range list {
		if q.ID == p.ID && q.Waiting() {
			t.Fatalf("the press did not decide the packet: %+v", q)
		}
	}
}

// flushTeams writes the lab's in-memory edits to its profile.
func flushTeams(t *testing.T, a *app) {
	t.Helper()
	if cmd := a.teamsWrite(); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
}

// teamsHostedLab is the lab with harbor's manager made of the conversation in
// front, so the pane hosts it.
func teamsHostedLab(t *testing.T) (a *app, harbor, orbit string) {
	t.Helper()
	a, harbor, orbit = teamsPlaceLabIDs(t)
	for _, tab := range a.tabList() {
		if tab.key == a.frontTabKey() {
			if err := a.teamMakeManager(harbor, tab); err != nil {
				t.Fatal(err)
			}
		}
	}
	drive(t, a, key("alt+1"))
	drive(t, a, key("alt+2"))
	if !a.teamsHosting() {
		t.Fatalf("the pane does not host harbor's manager:\n%s", teamsFrameText(a))
	}
	return a, harbor, orbit
}

// THE PANE IS THE MANAGER'S REAL CONVERSATION: the bar still says teams, the
// rail stands on the left, the composer says whom it talks to, and a letter
// typed lands in the manager's own box.
func TestTeamsHostsTheManagersRealConversation(t *testing.T) {
	a, _, _ := teamsHostedLab(t)
	lines := strings.Split(teamsFrameText(a), "\n")
	if len(lines) != a.height {
		t.Fatalf("the hosted frame has %d rows, want %d", len(lines), a.height)
	}
	if !strings.Contains(lines[navRow], "teams") {
		t.Fatalf("the nav does not say teams:\n%s", strings.Join(lines, "\n"))
	}
	w, _ := a.size()
	if w != a.width-a.tp.railW {
		t.Fatalf("the hosted conversation is %d wide, want %d", w, a.width-a.tp.railW)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"All teams", "harbor", "orbit", "Settings", "Close…"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the hosted page lost %q:\n%s", want, text)
		}
	}
	drive(t, a, key("h"), key("i"))
	if got := a.input.String(); got != "hi" {
		t.Fatalf("typing on the hosted page put %q in the manager's box:\n%s", got, teamsFrameText(a))
	}
	if !a.at(pageTeams) {
		t.Fatalf("typing left the page for %q", a.page.word())
	}
	// alt+↓ puts the keyboard on the page's buttons, and esc gives it back.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	if !a.tp.focus {
		t.Fatal("alt+↓ did not put the keyboard on the page")
	}
	drive(t, a, key("esc"))
	if a.tp.focus || !a.at(pageTeams) {
		t.Fatalf("esc did not give the keyboard back (focus %v, page %q)", a.tp.focus, a.page.word())
	}
	// And tab still walks the places.
	drive(t, a, key("tab"))
	if a.at(pageTeams) {
		t.Fatal("tab on the hosted page did not walk on")
	}
}

// ── the team's card ─────────────────────────────────────────────────────────

// THE CARD SAYS WHERE EVERY VALUE COMES FROM: an inherited one dim with
// `· from Settings` or `· from <team>`, an override in ink with `reset`, and
// reset gives the value back to what it inherits.
func TestTeamsCardShowsProvenanceAndResets(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	five := 5.0
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.SetSettings(harbor, func(s *teamstore.Settings) { s.CapUSDDay = &five })
	}); err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamSheetOpen(orbit, teamSheetSettings))...)
	if !a.tsheet.on {
		t.Fatal("the card did not open")
	}
	text := teamsFrameText(a)
	for _, want := range []string{"from Settings", "from harbor", "$5 a day"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "reset") {
		t.Fatalf("a card with no override offers reset:\n%s", text)
	}
	a.teamSheetSave(tsDepth, "2")
	if got, _ := a.teamByID(orbit); got.Settings.DepthLimit == nil || *got.Settings.DepthLimit != 2 {
		t.Fatalf("the depth was not kept on orbit: %+v", got.Settings)
	}
	if text = teamsFrameText(a); !strings.Contains(text, "reset") || !strings.Contains(text, "2 levels") {
		t.Fatalf("an override does not offer reset:\n%s", text)
	}
	a.teamSheetDo(tsDepth + tsReset)
	if got, _ := a.teamByID(orbit); got.Settings.DepthLimit != nil {
		t.Fatalf("reset left the override: %+v", got.Settings)
	}
	// A value out of its band is refused in the card's own words.
	a.teamSheetSave(tsShare, "140")
	if a.tsheet.err == "" {
		t.Fatal("a share of 140% was taken")
	}
}

// ── closing ─────────────────────────────────────────────────────────────────

// NOTHING RUNNING IS ONE CLOSE AND AN UNDO; the team moves to Closed and Undo
// puts it back.
func TestTeamsCloseWithNothingRunningIsOneClickAndUndo(t *testing.T) {
	a, _, orbit := teamsPlaceLabIDs(t)
	drive(t, a, runCmd(a.teamsCloseAsk(orbit))...)
	if a.tsheet.on {
		t.Fatal("a quiet team asked before closing")
	}
	if got, _ := a.teamByID(orbit); !got.Closed() {
		t.Fatal("orbit did not close")
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "Undo") || !strings.Contains(text, "Closed · 1") {
		t.Fatalf("the close offers no Undo or no Closed fold:\n%s", text)
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActUndo, "")))...)
	if got, _ := a.teamByID(orbit); got.Closed() {
		t.Fatal("Undo did not reopen orbit")
	}
}

// SOMETHING RUNNING PUTS UP THE CARD: `Close now` first when no manager runs
// the team, `Wrap up first` first when one does, and Cancel changes nothing.
func TestTeamsCloseCardOffersWrapUpNowAndCancel(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	a.state = stateWorking
	drive(t, a, runCmd(a.teamsCloseAsk(harbor))...)
	if !a.tsheet.on || a.tsheet.mode != teamSheetClose || a.tsheet.cursor != tsCloseNow {
		t.Fatalf("the card for a team with no manager: %+v", a.tsheet)
	}
	text := teamsFrameText(a)
	if strings.Contains(text, "Wrap up first") || !strings.Contains(text, "Close now") || !strings.Contains(text, "Cancel") {
		t.Fatalf("the card with no manager:\n%s", text)
	}
	a.teamSheetKey(key("esc"))
	if got, _ := a.teamByID(harbor); a.tsheet.on || got.Closed() {
		t.Fatal("Cancel closed the team or left the card up")
	}
	for _, tab := range a.tabList() {
		if tab.key == a.frontTabKey() {
			if err := a.teamMakeManager(harbor, tab); err != nil {
				t.Fatal(err)
			}
		}
	}
	drive(t, a, runCmd(a.teamsCloseAsk(harbor))...)
	if !a.tsheet.on || a.tsheet.cursor != tsWrapUp {
		t.Fatalf("a managed team's card does not lead with Wrap up first: %+v", a.tsheet)
	}
	drive(t, a, runCmd(a.teamSheetDo(tsWrapUp))...)
	if got, _ := a.teamByID(harbor); got.Closed() {
		t.Fatal("Wrap up first closed the team at once")
	}
	if !strings.Contains(a.tp.msg, "wrap up") {
		t.Fatalf("the wrap-up said nothing: %q", a.tp.msg)
	}
	drive(t, a, runCmd(a.teamsCloseAsk(harbor))...)
	drive(t, a, runCmd(a.teamSheetDo(tsCloseNow))...)
	if got, _ := a.teamByID(harbor); !got.Closed() {
		t.Fatal("Close now did not close the team")
	}
}

// THE CLOSED FOLD shows a closed team's report, members and dates with
// Reopen and Delete…, and a team under a closed parent offers to reopen the
// parent too.
func TestTeamsClosedFoldReopensWithItsParent(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	drive(t, a, runCmd(a.teamsCloseAsk(harbor))...)
	if got, _ := a.teamByID(orbit); !got.Closed() {
		t.Fatal("closing the parent left the sub-team open")
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActClosedFold, "")))...)
	drive(t, a, runCmd(a.teamsSelect(orbit))...)
	text := teamsFrameText(a)
	for _, want := range []string{"Reopen harbor too", "Delete…", "closed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the closed sub-team lost %q:\n%s", want, text)
		}
	}
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActReopenParent, orbit)))...)
	for _, id := range []string{harbor, orbit} {
		if got, _ := a.teamByID(id); got.Closed() {
			t.Fatalf("%s is still closed", got.Name)
		}
	}
}

// Contract 6.1: A linked local team with a history door draws its closing report in the closed pane.
func TestLinkedLocalClosedTeamDrawsItsReport(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	p := teamstore.Packet{ID: "closing-report", Team: teamstore.Person, Origin: harbor,
		Kind: teamstore.PacketClosing, Report: &teamstore.ClosingReport{
			Done: "the parser", Left: "the docs", Files: []string{"parser.go"}, SpendUSD: 1.5,
		}}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.Close(harbor, a.now(), p.ID) }); err != nil {
		t.Fatal(err)
	}
	door := localTeams(a.profileDir, &a.teamsDisk.watch)
	door.History = func(team string) ([]teamstore.Packet, error) {
		if team == harbor {
			return []teamstore.Packet{p}, nil
		}
		return nil, nil
	}
	a.teamsDisk.door = door
	drive(t, a, runCmd(a.teamsRead(true))...)
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActClosedFold, "")))...)
	drive(t, a, runCmd(a.teamsSelect(harbor))...)
	text := teamsFrameText(a)
	for _, want := range []string{"closing report", "done", "the parser", "left", "the docs", "files", "parser.go", "spent", "$1.50"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the local closed pane lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "not readable over this connection") {
		t.Fatalf("the local pane claimed its report could not be read:\n%s", text)
	}
}

// Contract 4.3: The header, packet card, team card and Settings row agree on a sub-cent cap.
func TestTeamSurfacesSpellSubCentCapTheSameWay(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	cap := 0.001
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.SetSettings(harbor, func(s *teamstore.Settings) { s.CapUSDDay = &cap })
	}); err != nil {
		t.Fatal(err)
	}
	a.tp.defaultsOK = true
	a.tp.spend = map[string]teamstore.Spend{harbor: {USD: cap}}
	team, ok := a.teamByID(harbor)
	if !ok {
		t.Fatal("no team")
	}
	if words := a.teamsSpendWords(team); !strings.Contains(words, "$0.001 of $0.001 today") {
		t.Fatalf("header cap: %q", words)
	}
	p := teamstore.Packet{ID: "cap", Team: teamstore.Person, Origin: harbor, Kind: teamstore.PacketCap,
		Question: "harbor reached its $0.001 cap today", Cap: &teamstore.CapFacts{Team: harbor, CapUSD: cap, SpentUSD: cap},
		Options: []teamstore.Option{{ID: teamstore.OptionRaiseCap, Label: "Raise to $0.002", Consequence: "continue"}}}
	card := plain(strings.Join(a.teamsCard(&teamsDraw{a: a}, p, 80, 0), "\n"))
	if !strings.Contains(card, "spent $0.001 of $0.001 today") {
		t.Fatalf("cap packet card: %s", card)
	}
	var settingsCard string
	for _, row := range a.teamSheetRows(team) {
		if row.code == tsCap {
			settingsCard = row.value
		}
	}
	if settingsCard != "$0.001 a day" {
		t.Fatalf("team settings card: %q", settingsCard)
	}
	a.sheet.farTeams = &teamstore.Defaults{CapUSDDay: cap}
	if settings, ok := a.sheet.farTeamValue(config.KeyTeamsCapUSDDay); !ok || settings != "$0.001" {
		t.Fatalf("Settings Teams row: %q, %v", settings, ok)
	}
}

// ── members ─────────────────────────────────────────────────────────────────

// A MEMBER THIS WINDOW DOES NOT HOLD IS RESUMED BEHIND, in its own tab, and
// the person stays where they are.
func TestTeamsMemberPressResumesItBehind(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	far := "/tmp/lab/far-away.jsonl"
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.AddMember(harbor, teamstore.Member{Key: a.convKey(far), File: far, Where: "/tmp/lab", Word: "far", Handle: "far"})
	}); err != nil {
		t.Fatal(err)
	}
	opened := ""
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: where, SessionFile: file}, nil
	}
	front := a.frontTabKey()
	// An idle member is not on the header; the members card lists it, with
	// `Resume` for one this window does not hold, and never says `not open`.
	drive(t, a, runCmd(a.teamsDo(teamsTargetOf(t, a, teamsActCrew, harbor)))...)
	text := teamsFrameText(a)
	if !strings.Contains(text, "@far") || !strings.Contains(text, "Resume") || strings.Contains(text, "not open") {
		t.Fatalf("the members card does not list the member this window does not hold:\n%s", text)
	}
	var member teamsCrewRow
	for _, r := range a.teamCrewRows() {
		if r.key == a.convKey(far) {
			member = r
		}
	}
	if member.key == "" {
		t.Fatalf("no row for @far:\n%s", teamsFrameText(a))
	}
	drive(t, a, runCmd(a.teamCrewGo(member))...)
	if opened != far {
		t.Fatalf("the press opened %q", opened)
	}
	if a.frontTabKey() != front || !a.at(pageTeams) {
		t.Fatalf("the resume moved the person: front %q page %q", a.frontTabKey(), a.page.word())
	}
	if !a.trafficHeld(a.convKey(far)) {
		t.Fatal("the member is not held behind")
	}
}

// ── over --host ─────────────────────────────────────────────────────────────

// OVER --host THE SETTINGS TEAMS TAB SAYS WHOSE DEFAULTS THE TEAMS READ and
// does not edit this machine's.
func TestTeamsSettingsTabOverHostIsReadOnlyAndSaysWhose(t *testing.T) {
	a := placeApp(t)
	a.host = "spark"
	drive(t, a, key(placeChord(pageSettings)))
	for i, title := range settingTabs {
		if title == tabTeams {
			a.sheet.tab = i
		}
	}
	a.sheet.build()
	if note := a.sheet.footNote(); !strings.Contains(note, "on spark") {
		t.Fatalf("the Teams tab over --host says %q", note)
	}
	before := a.sheet.items[a.sheet.cursor].row.Value()
	drive(t, a, key("enter"))
	if a.sheet.edit != nil || !strings.Contains(a.sheet.msg, "spark") {
		t.Fatalf("a Teams row took an edit over --host (msg %q)", a.sheet.msg)
	}
	if after := a.sheet.items[a.sheet.cursor].row.Value(); after != before {
		t.Fatalf("the row changed from %q to %q", before, after)
	}
	a.host = ""
	a.sheet.host = ""
	if note := a.sheet.footNote(); !strings.Contains(note, "a team can override any of these on its card") {
		t.Fatalf("the Teams tab says %q", note)
	}
}

// ── organize ────────────────────────────────────────────────────────────────

// ORGANIZE OFFERS TO CLOSE THE QUIET TEAMS, ticked like every suggestion,
// never on its own, and Undo reopens them.
func TestOrganizeOffersToCloseQuietTeamsWithUndo(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	old := a.now().Add(-10 * 24 * time.Hour)
	if err := a.teamEdit(func(f *teamstore.File) error {
		for i := range f.Teams {
			f.Teams[i].Made = old
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	flushTeams(t, a)
	_ = a.openWall()
	a.wallSetTeam("")
	drive(t, a, runCmd(a.wallOrganizeOpen())...)
	var quiet *orgProp
	for i := range a.wall.org.props {
		if a.wall.org.props[i].team == orgCloseRow {
			quiet = &a.wall.org.props[i]
		}
	}
	if quiet == nil || len(quiet.closes) != 2 || quiet.name != "Close 2 quiet teams" {
		t.Fatalf("Organize did not offer the quiet teams: %+v", a.wall.org.props)
	}
	// Its row is its sentence whole, then the teams: no colour dot and no
	// second count, and not cut to a team name's width.
	a.width, a.height = 110, 30
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if !strings.Contains(frame, "☑ Close 2 quiet teams  harbor, orbit") {
		t.Fatalf("the quiet-teams row reads:\n%s", frame)
	}
	for _, id := range []string{harbor, orbit} {
		if got, _ := a.teamByID(id); got.Closed() {
			t.Fatal("a suggestion closed a team before Apply")
		}
	}
	a.wallOrganizeApply()
	for _, id := range []string{harbor, orbit} {
		if got, _ := a.teamByID(id); !got.Closed() {
			t.Fatalf("Apply left %s open", got.Name)
		}
	}
	a.wallOrganizeUndo()
	for _, id := range []string{harbor, orbit} {
		if got, _ := a.teamByID(id); got.Closed() {
			t.Fatalf("Undo left %s closed", got.Name)
		}
	}
}

// ── the session's contract (DESIGN.md 8.8) ──────────────────────────────────

// WRAP UP FIRST IS THE ONE REQUEST LINE in the team's Traffic, which the
// manager's session reads; the team stays open until its report is accepted,
// and accepting it closes the team on its report.
func TestTeamsWrapUpAsksTheManagerAndTheReportCloses(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	flushTeams(t, a)
	a.state = stateWorking
	drive(t, a, runCmd(a.teamsCloseAsk(harbor))...)
	if a.tsheet.cursor != tsWrapUp {
		t.Fatalf("the card does not lead with Wrap up first: %+v", a.tsheet)
	}
	drive(t, a, runCmd(a.teamSheetDo(tsWrapUp))...)
	log, err := teamstore.ReadTraffic(a.profileDir, harbor, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	asked := false
	for _, e := range log {
		asked = asked || teamstore.IsWrapUp(e)
	}
	if !asked {
		t.Fatalf("no wrap-up request in harbor's Traffic: %+v", log)
	}
	a.state = stateIdle
	seam := a.teamsSeam()
	p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor, Kind: teamstore.PacketClosing,
		RaisedBy: teamstore.FromManager, Question: "close harbor?",
		Options: []teamstore.Option{{ID: teamstore.OptionClose, Label: "Close", Consequence: "the team closes on this report"},
			{ID: teamstore.OptionKeepGoing, Label: "Keep going", Consequence: "the team goes on"}},
		Recommendation: &teamstore.Recommendation{Option: teamstore.OptionClose, Reason: "everything is committed"},
		Report:         &teamstore.ClosingReport{Done: "the parser ships", Left: "the docs", SpendUSD: 1.5}})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamsRead(false))...)
	text := teamsFrameText(a)
	for _, want := range []string{"close harbor?", "the parser ships", "the docs", "Keep going"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the closing report lost %q:\n%s", want, text)
		}
	}
	var closeBtn teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOption && tg.arg == p.ID && tg.opt == teamstore.OptionClose {
			closeBtn = tg
		}
	}
	drive(t, a, runCmd(a.teamsDo(closeBtn))...)
	if got, _ := a.teamByID(harbor); !got.Closed() || got.Report != p.ID {
		t.Fatalf("accepting the report did not close harbor on it: %+v", got)
	}
}

// A CAP PACKET SAYS ITS FIGURES, and `Raise to $X` is the decision alone: the
// session lifts the day's ceiling, so the team's own cap is not rewritten.
func TestTeamsCapPacketSaysItsFiguresAndRaisingWritesNoSetting(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	flushTeams(t, a)
	p, err := a.teamsSeam().Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor, Kind: teamstore.PacketCap,
		RaisedBy: teamstore.FromManager, Question: "harbor reached its $5 cap today",
		Options: []teamstore.Option{{ID: teamstore.OptionRaiseCap, Label: "Raise to $10", Consequence: "harbor may spend $10 today"},
			{ID: teamstore.OptionStopToday, Label: "Stop for today", Consequence: "nothing new starts until tomorrow"}},
		Recommendation: &teamstore.Recommendation{Option: teamstore.OptionStopToday, Reason: "the day is nearly done"},
		Cap:            &teamstore.CapFacts{Team: harbor, Day: teamstore.Today(), CapUSD: 5, SpentUSD: 5.2, RaiseTo: 10}})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamsRead(false))...)
	text := teamsFrameText(a)
	for _, want := range []string{"spent $5.20 of $5 today", "Raise to $10", "Stop for today"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the cap card lost %q:\n%s", want, text)
		}
	}
	var raise teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOption && tg.arg == p.ID && tg.opt == teamstore.OptionRaiseCap {
			raise = tg
		}
	}
	drive(t, a, runCmd(a.teamsDo(raise))...)
	got, err := teamstore.PacketByID(a.profileDir, p.ID)
	if err != nil || got.Decision != teamstore.OptionRaiseCap || got.DecidedBy != teamstore.Person {
		t.Fatalf("the raise was not decided by the person: %+v %v", got, err)
	}
	if team, _ := a.teamByID(harbor); team.Settings.CapUSDDay != nil {
		t.Fatalf("the raise rewrote harbor's cap: %+v", team.Settings)
	}
}

// WAKE IS ON THE CARD with where it comes from, and a shared member says whose
// manager it reports to.
func TestTeamsCardShowsWakeAndASharedMemberSaysWhoseItIs(t *testing.T) {
	a, harbor, orbit := teamsHostedLab(t)
	drive(t, a, runCmd(a.teamSheetOpen(harbor, teamSheetSettings))...)
	if text := teamsFrameText(a); !strings.Contains(text, "team messages wake") {
		t.Fatalf("the card has no wake row:\n%s", text)
	}
	a.teamSheetDo(tsWake)
	if got, _ := a.teamByID(harbor); got.Settings.Wake == nil {
		t.Fatal("the wake row did not override")
	}
	a.tsheet = teamSheet{}
	// orbit's member, made a member of harbor too, keeps orbit as its home.
	o, _ := a.teamByID(orbit)
	m := o.Members[0]
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(harbor, teamstore.Member{Key: m.Key, File: m.File, Where: m.Where, Word: m.Word, Handle: m.Handle}); err != nil {
			return err
		}
		if err := f.AddMember(orbit, teamstore.Member{Key: "orbit-boss", Handle: "oboss", Word: "orbit's boss"}); err != nil {
			return err
		}
		if err := f.SetManager(orbit, "orbit-boss"); err != nil {
			return err
		}
		return f.SetHome(m.Key, orbit)
	}); err != nil {
		t.Fatal(err)
	}
	a.tp.top = teamsTopCache{}
	// It says so on the members card, as a quiet tag whose hint names the
	// manager, never as prose on the header.
	if text := teamsFrameText(a); strings.Contains(text, "reports to orbit") {
		t.Fatalf("the header still says whose the shared member is in prose:\n%s", text)
	}
	drive(t, a, runCmd(a.teamCrewOpen(harbor))...)
	if text := teamsFrameText(a); !strings.Contains(text, "also in orbit") {
		t.Fatalf("the members card does not tag the shared member:\n%s", text)
	}
	for i, r := range a.teamCrewRows() {
		if r.key == m.Key {
			a.tcrew.cursor = i
		}
	}
	if hint := a.teamCrewHint(); !strings.Contains(hint, "reports to orbit's manager") {
		t.Fatalf("the shared member's hint does not say whose it is: %q", hint)
	}
}

// BESIDE THE MANAGER THE INBOX LEAVES THE CONVERSATION ROOM. Three packets on
// a laptop's 34 rows used to take the whole pane, leaving the manager's chat
// one row; now the newest card is whole, the older ones fold to one line each
// that still says whose they are, and a press unfolds one. Each card leads
// with whose it is: the needs-you `?`, never the manager's mark.
func TestTeamsHostedInboxLeavesTheConversationRoom(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 110, 34
	flushTeams(t, a)
	seam := a.teamsSeam()
	raise := func(kind, q string) teamstore.Packet {
		p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor, Kind: kind, RaisedBy: "boss", Question: q,
			Options:        []teamstore.Option{{ID: "a", Label: "One way", Consequence: "this happens"}, {ID: "b", Label: "The other", Consequence: "that happens"}},
			Recommendation: &teamstore.Recommendation{Option: "b", Reason: "it is cheaper"}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := raise(teamstore.PacketQuestion, "Ship on Friday or Monday?")
	raise(teamstore.PacketConflict, "Which parser wins?")
	raise(teamstore.PacketQuestion, "Keep the old field names?")
	drive(t, a, runCmd(a.teamsRead(false))...)
	a.teamsSync()
	if top := a.teamsHostTopHeight(); top > a.height/2 {
		t.Fatalf("the inbox takes %d of %d rows over the manager's chat:\n%s", top, a.height, teamsFrameText(a))
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "Keep the old field names?") || !strings.Contains(text, "The other") {
		t.Fatalf("the newest card is not whole:\n%s", text)
	}
	lines := strings.Split(text, "\n")
	folded := 0
	for _, l := range lines {
		if strings.Contains(l, "▸ question · Ship on Friday") || strings.Contains(l, "▸ conflict · Which parser") {
			folded++
			if !strings.Contains(l, "waiting on you") {
				t.Fatalf("a folded card does not say it waits on you: %q", l)
			}
		}
		if strings.Contains(l, teamManagerGlyph+" question") || strings.Contains(l, teamManagerGlyph+" conflict") {
			t.Fatalf("a card waiting on the person leads with the manager's mark: %q", l)
		}
	}
	if folded != 2 {
		t.Fatalf("%d cards folded, want 2:\n%s", folded, text)
	}
	if !strings.Contains(text, "? question · raised by @boss") {
		t.Fatalf("the whole card does not lead with the needs-you mark:\n%s", text)
	}
	// A press on a folded card unfolds it and keeps the budget.
	var fold teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOption && tg.arg == first.ID && tg.opt == "" {
			fold = tg
		}
	}
	drive(t, a, runCmd(a.teamsDo(fold))...)
	if text := teamsFrameText(a); !strings.Contains(text, "One way") || !strings.Contains(text, "Ship on Friday or Monday?") {
		t.Fatalf("the press did not unfold the oldest card:\n%s", text)
	}
	if top := a.teamsHostTopHeight(); top > a.height/2 {
		t.Fatalf("an unfolded card let the inbox take %d of %d rows", top, a.height)
	}
}

// THE MEMBERS CARD COUNTS WHAT THE HEADER COUNTS: `◆ Manager  1 member` on the
// header is `◆ Manager · 1 member` on the card, never `2 members`.
func TestTeamsMembersCardCountsLikeTheHeader(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	head := teamsFrameText(a)
	if !strings.Contains(head, "1 member") {
		t.Fatalf("the header's count:\n%s", head)
	}
	drive(t, a, runCmd(a.teamCrewOpen(harbor))...)
	if text := teamsFrameText(a); !strings.Contains(text, "harbor · "+teamManagerGlyph+" Manager · 1 member") {
		t.Fatalf("the card's title does not count like the header:\n%s", text)
	}
}

// A TEAM'S CARD STANDS OVER THE PANE, so the rail beside it still says which
// team is selected.
func TestTeamsCardsStandOverThePane(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	a.width, a.height = 110, 34
	a.teamsSync()
	drive(t, a, runCmd(a.teamSheetOpen(harbor, teamSheetSettings))...)
	teamsFrameText(a)
	if rail := teamsRailCols(a.width); a.tsheet.rect.x0 < rail {
		t.Fatalf("the settings card starts at %d, over the rail's %d columns:\n%s", a.tsheet.rect.x0, rail, teamsFrameText(a))
	}
}
