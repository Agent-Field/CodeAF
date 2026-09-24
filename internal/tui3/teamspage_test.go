package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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
		if tg.y < 0 || tg.y >= len(hits) {
			continue
		}
		if hits[tg.y] < 0 || i == cur {
			hits[tg.y] = i
		}
	}
	return hits
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
	bar := plain(a.placeTabBar(120, false, a.pal))
	if !strings.Contains(bar, "home   teams   sessions") {
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
	if !strings.Contains(lines[placeTabRow], "teams") {
		t.Fatalf("the bar does not say teams:\n%s", strings.Join(lines, "\n"))
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
	for _, want := range []string{"from Settings", "from harbor", "$5.00 a day"} {
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
	text := teamsFrameText(a)
	if !strings.Contains(text, "@far") || !strings.Contains(text, "not open") {
		t.Fatalf("a member this window does not hold is not listed:\n%s", text)
	}
	var member teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActMember && tg.arg == a.convKey(far) {
			member = tg
		}
	}
	if member.arg == "" {
		t.Fatalf("no target for @far:\n%s", teamsFrameText(a))
	}
	drive(t, a, runCmd(a.teamsDo(member))...)
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
