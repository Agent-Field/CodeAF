package tui3

import (
	"fmt"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// helpersApp is [tabApp] with its teams on a profile of the test's own and no
// team yet: a person three conversations into their work, in front of
// "Shipping the parser" in /tmp/lab.
func helpersApp(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	a, _, _ := tabApp(t)
	a.profileDir = t.TempDir()
	a.teamsEnsure()
	fake, _ := a.agent.(*fakeAgent)
	if fake == nil {
		t.Fatal("the fixture's agent is not the fake")
	}
	return a, fake
}

// helpersDisk is the teams this window's profile holds on disk.
func helpersDisk(t *testing.T, a *app) []team {
	t.Helper()
	disk, err := loadTeams(a.profileDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return disk
}

// lastPersonLine is the newest line of the person's in the transcript.
func lastPersonLine(t *testing.T, a *app) string {
	t.Helper()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryUser {
			return a.entries[i].text
		}
	}
	t.Fatal("no line of the person's in the transcript")
	return ""
}

// transcriptSays reports whether any note in the transcript holds want.
func transcriptSays(a *app, want string) bool {
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, want) {
			return true
		}
	}
	return false
}

// helpersMenuRow is the Teams dropdown's `+ New from this session` row, and
// whether it is the last row.
func helpersMenuRow(t *testing.T, a *app) (teamMenuRow, bool) {
	t.Helper()
	rows := a.teamMenuRows()
	for i, r := range rows {
		if !r.rule && r.code == wallPopHelpers {
			return r, i == len(rows)-1
		}
	}
	t.Fatalf("the dropdown has no %q row: %+v", helpersRowWord, rows)
	return teamMenuRow{}, false
}

// /helpers MAKES A TEAM WITH THIS CONVERSATION AS ITS MANAGER, and only then
// sends the person's words. The team is on disk with the conversation as
// manager and only member; the strip is narrowed to it; the column is the
// manager's; and the model is handed exactly the words the transcript shows
// after `/helpers`, and nothing else.
func TestHelpersMakesThisConversationTheManagerAndSendsWhatWasTyped(t *testing.T) {
	a, fake := helpersApp(t)
	front := a.frontTabKey()
	before := len(fake.sent)
	typeLine(t, a, "/helpers suggest one helper for the flaky tests")
	teamsFlush(t, a)

	disk := helpersDisk(t, a)
	if len(disk) != 1 {
		t.Fatalf("/helpers left %d teams on disk: %+v", len(disk), disk)
	}
	made := disk[0]
	if made.Manager != front || len(made.Members) != 1 || made.Members[0].Key != front {
		t.Fatalf("the team is not this conversation alone, as manager: %+v", made)
	}
	if made.Name != "lab" || made.Parent != "" {
		t.Fatalf("the team was named %q under %q, want the folder's name at the top level", made.Name, made.Parent)
	}
	if active, ok := a.teamActive(); !ok || active.ID != made.ID {
		t.Fatal("the strip was not narrowed to the new team")
	}
	if a.sideKind() != sideKindManager {
		t.Fatal("the column does not read this conversation as the team's manager")
	}
	if hint := a.trafficHint(); !strings.Contains(hint, "manager of lab") {
		t.Fatalf("the composer says %q, not that it talks to the manager", hint)
	}
	if len(fake.sent) != before+1 {
		t.Fatalf("/helpers sent %d messages, want one", len(fake.sent)-before)
	}
	if sent := fake.sent[before]; sent != "suggest one helper for the flaky tests" {
		t.Fatalf("the model was handed %q, not the words that were typed", sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers suggest one helper for the flaky tests" {
		t.Fatalf("the transcript says %q, not what was typed", shown)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersMadeWord, "lab")) {
		t.Fatal("no receipt says the team was made")
	}
}

// A BARE /helpers SAYS ITS ASK OUT LOUD. The model is handed [helpersPrompt],
// and the transcript shows that same sentence after the command, so nothing was
// asked that the person cannot read. The sentence is the person's own kind of
// words: it names no tool and none of codeaf's machinery.
func TestABareHelpersShowsTheAskItSends(t *testing.T) {
	a, fake := helpersApp(t)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	if len(fake.sent) != 1 || fake.sent[0] != helpersPrompt {
		t.Fatalf("a bare /helpers sent %q", fake.sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers "+helpersPrompt {
		t.Fatalf("the transcript says %q, not the ask that was sent", shown)
	}
	for _, internal := range []string{"team_", "tool", "manager", "codeaf", "handle"} {
		if strings.Contains(strings.ToLower(helpersPrompt), internal) {
			t.Errorf("the default ask says %q, which is codeaf's word and not the person's: %q", internal, helpersPrompt)
		}
	}
	for _, want := range []string{"two or three helpers", "Don't start anyone until I agree", "cancel"} {
		if !strings.Contains(helpersPrompt, want) {
			t.Errorf("the default ask does not say %q: %q", want, helpersPrompt)
		}
	}
}

// THE ASK WAITS FOR THE STORE. Until the write that carries the team is back
// the model is sent nothing, so its first step cannot run as anything but the
// manager; a second /helpers meanwhile makes no second team.
func TestHelpersAsksOnlyOnceTheStoreHasTheTeam(t *testing.T) {
	a, fake := helpersApp(t)
	before := len(fake.sent)
	spend(t, a, a.runHelpersCommand(""))
	if len(fake.sent) != before {
		t.Fatal("the ask went out before the store took the team")
	}
	spend(t, a, a.runHelpersCommand(""))
	if !transcriptSays(a, helpersPendingWord) {
		t.Fatal("a second /helpers while the first was saving did not say so")
	}
	teamsFlush(t, a)
	if len(helpersDisk(t, a)) != 1 {
		t.Fatalf("two /helpers made %d teams", len(helpersDisk(t, a)))
	}
	if len(fake.sent) != before+1 {
		t.Fatalf("the store's answer sent %d asks, want one", len(fake.sent)-before)
	}
}

// A TEAM MADE THIS WAY OBEYS THE RULES OF EVERY TEAM. A name already taken is
// not reused; a conversation that already manages a team is not made a second
// team's manager — it is shown its team, and nothing is sent.
func TestHelpersKeepsTheRulesEveryTeamKeeps(t *testing.T) {
	a, fake := helpersApp(t)
	if _, err := a.teamMake("lab", nil); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	var made team
	for _, t := range helpersDisk(t, a) {
		if t.Manager != "" {
			made = t
		}
	}
	if made.ID == "" || strings.EqualFold(made.Name, "lab") {
		t.Fatalf("the new team took a name another team has: %+v", helpersDisk(t, a))
	}

	sent := len(fake.sent)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	if n := len(helpersDisk(t, a)); n != 2 {
		t.Fatalf("a manager's /helpers made another team: %d teams", n)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersManagesWord, made.Name)) {
		t.Fatal("a manager's /helpers did not say which team it manages")
	}
	if len(fake.sent) != sent {
		t.Fatal("a manager's /helpers sent the ask again")
	}
	if active, ok := a.teamActive(); !ok || active.ID != made.ID {
		t.Fatal("a manager's /helpers did not show the team it manages")
	}
}

// THE STORE'S REFUSAL IS FINAL. Another window made this conversation another
// team's manager after this one loaded: the window's own check passes, the
// store refuses with the one-manager rule, the window takes the store's teams
// back, closes the overlay, and the model is asked nothing.
func TestHelpersRefusedByTheStoreAsksNothing(t *testing.T) {
	a, fake := helpersApp(t)
	front := a.frontTabKey()
	elsewhere := teamstore.NewID()
	if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: elsewhere, Name: "orbit"})
		if err := f.AddMember(elsewhere, teamstore.Member{Key: front, File: a.file, Where: a.workspace}); err != nil {
			return err
		}
		return f.SetManager(elsewhere, front)
	}); err != nil {
		t.Fatal(err)
	}
	before := len(fake.sent)
	spend(t, a, a.runHelpersCommand(""))
	teamsFlush(t, a)
	if len(fake.sent) != before {
		t.Fatal("a refused team still asked the model for helpers")
	}
	disk := helpersDisk(t, a)
	if len(disk) != 1 || disk[0].ID != elsewhere {
		t.Fatalf("the store holds %+v, want only the other window's team", disk)
	}
	if _, ok := a.teamActive(); ok {
		t.Fatal("the overlay stayed on a team the store refused")
	}
	if !transcriptSays(a, "a conversation can manage one team and its descendants") {
		t.Fatal("the refusal was not said in the store's words")
	}
}

// WHERE IT CANNOT WORK IT SAYS SO AND MAKES NOTHING: home, which has no
// conversation in front; a connection that holds one conversation at a time,
// where a helper could not open beside its manager; and the welcome screen.
func TestHelpersRefusesWhereItCannotWork(t *testing.T) {
	if got := homeFate("helpers", ""); got != fateAnswers {
		t.Fatalf("the drop-up says /helpers %q on home", got)
	}

	a, fake := helpersApp(t)
	a.shared = true
	spend(t, a, a.runHelpersCommand(""))
	if !transcriptSays(a, helpersShared) || len(helpersDisk(t, a)) != 0 || len(fake.sent) != 0 {
		t.Fatal("a one-conversation connection made a team")
	}

	a, fake = helpersApp(t)
	a.welcome.open, a.welcome.start = true, true
	spend(t, a, a.runHelpersCommand(""))
	if !transcriptSays(a, helpersFreshWord) || len(helpersDisk(t, a)) != 0 || len(fake.sent) != 0 {
		t.Fatal("the welcome screen made a team")
	}
}

// THE TEAMS DROPDOWN IS THERE WITH NO TEAMS, and its last row is `+ New from
// this session`. With teams, that row stays last, under a rule. The dropdown
// says nothing about its keys: no footer of up/down, enter and esc.
func TestTheTeamsDropdownIsThereWithNoTeamsAndEndsWithNewFromThisSession(t *testing.T) {
	a, _ := helpersApp(t)
	if len(a.wall.teams) != 0 {
		t.Fatalf("the fixture has teams: %+v", a.wall.teams)
	}
	if word := a.tabTeamWord(); !strings.Contains(word, "Teams") {
		t.Fatalf("with no teams the strip's chip is %q", word)
	}
	a.openTeamMenu()
	if !a.teamMenu.on {
		t.Fatal("the dropdown did not open with no teams")
	}
	if _, last := helpersMenuRow(t, a); !last {
		t.Fatal("+ New from this session is not the last row")
	}
	frame, _ := menuFrame(t, a)
	if !strings.Contains(frame, helpersRowWord) {
		t.Fatalf("the dropdown does not draw %q:\n%s", helpersRowWord, frame)
	}
	for _, hint := range []string{"up/down move", "enter choose", "esc cancel"} {
		if strings.Contains(frame, hint) {
			t.Errorf("the dropdown still says %q:\n%s", hint, frame)
		}
	}

	if _, err := a.teamMake("harbor", nil); err != nil {
		t.Fatal(err)
	}
	rows := a.teamMenuRows()
	if len(rows) != 3 || rows[0].code != wallPopTeam || !rows[1].rule || rows[2].code != wallPopHelpers {
		t.Fatalf("with a team the rows are %+v, want the team, a rule, and the row", rows)
	}
}

// `+ New from this session` IS /helpers ONE STEP EARLIER. Chosen by the
// keyboard, it puts `/helpers ` and the default ask in the box in place of
// what was there (which goes onto the drafts ring), makes nothing and sends
// nothing; enter then does exactly what typing it would.
func TestNewFromThisSessionPutsHelpersInTheBoxAndEnterSendsIt(t *testing.T) {
	a, fake := helpersApp(t)
	a.input.insert("half a thought")
	a.openTeamMenu()
	picks := teamMenuPicks(a.teamMenuRows())
	for range picks {
		drive(t, a, key("down"))
	}
	drive(t, a, key("enter"))
	if a.teamMenu.on {
		t.Fatal("the dropdown stayed up after its row was chosen")
	}
	if got := a.input.String(); got != "/helpers "+helpersPrompt {
		t.Fatalf("the box holds %q", got)
	}
	if len(a.drafts.entries) == 0 || string(a.drafts.entries[len(a.drafts.entries)-1].text) != "half a thought" {
		t.Fatal("what was in the box was not kept on the drafts ring")
	}
	teamsFlush(t, a)
	if len(helpersDisk(t, a)) != 0 || len(fake.sent) != 0 {
		t.Fatal("choosing the row made a team or sent something before enter")
	}

	drive(t, a, key("enter"))
	teamsFlush(t, a)
	if len(helpersDisk(t, a)) != 1 || len(fake.sent) != 1 || fake.sent[0] != helpersPrompt {
		t.Fatalf("enter did not do what /helpers does: teams %d, sent %q", len(helpersDisk(t, a)), fake.sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers "+helpersPrompt {
		t.Fatalf("the transcript says %q", shown)
	}
}

// WHERE /helpers WOULD REFUSE, THE ROW SAYS SO AND LEAVES THE BOX ALONE: in a
// conversation that already manages a team, choosing it shows that team.
func TestNewFromThisSessionInAManagerSaysWhichTeamAndLeavesTheBox(t *testing.T) {
	a, _ := helpersApp(t)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	made := helpersDisk(t, a)[0]
	spend(t, a, a.teamOverlayChoose(""))
	a.input.insert("keep this")
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	if got := a.input.String(); got != "keep this" {
		t.Fatalf("the row replaced the box in a manager: %q", got)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersManagesWord, made.Name)) {
		t.Fatal("the row did not say which team this conversation manages")
	}
}

// WITH A TEAM SELECTED THERE IS NO `+ New from this session`, and /helpers
// refuses: the team it makes is a top-level team, and made from inside another
// team's overlay it would read as nesting. Nesting is `+ Add subteam` on Teams.
// Nothing is made, nothing is sent, the box is untouched; clearing the
// selection brings the row back.
func TestWithATeamSelectedThereIsNoNewFromThisSession(t *testing.T) {
	a, fake := helpersApp(t)
	harbor, err := a.teamMake("harbor", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	spend(t, a, a.teamActivate(harbor))
	for _, r := range a.teamMenuRows() {
		if r.rule || r.code == wallPopHelpers {
			t.Fatalf("with a team selected the dropdown offers %+v", a.teamMenuRows())
		}
	}
	a.openTeamMenu()
	if frame, _ := menuFrame(t, a); strings.Contains(frame, helpersRowWord) {
		t.Fatalf("with a team selected the dropdown draws %q:\n%s", helpersRowWord, frame)
	}
	a.closeTeamMenu()

	a.input.insert("/helpers")
	drive(t, a, key("enter"))
	teamsFlush(t, a)
	if !transcriptSays(a, fmt.Sprintf(helpersSelectedWord, "harbor")) {
		t.Fatal("/helpers with a team selected did not say why it refused")
	}
	if n := len(helpersDisk(t, a)); n != 1 || len(fake.sent) != 0 {
		t.Fatalf("/helpers with a team selected made %d teams and sent %q", n, fake.sent)
	}

	spend(t, a, a.teamOverlayChoose(""))
	if _, last := helpersMenuRow(t, a); !last {
		t.Fatal("clearing the selection did not bring the row back last")
	}
}

// THE HELPERS IT STARTS OPEN AS TABS IN THE TEAM'S OVERLAY. The manager's
// team_start is carried out by this window behind the manager; the new
// conversation is a member, held open, its tab on the strip, and the strip is
// this team's even if the person had cleared the overlay meanwhile. The person
// stays in front of the manager, with what they were typing.
func TestHelpersStartedOpenAsTabsInTheTeamsOverlay(t *testing.T) {
	a, _ := helpersApp(t)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	made := helpersDisk(t, a)[0]
	front := a.frontTabKey()
	spend(t, a, a.teamOverlayChoose(""))
	if _, ok := a.teamActive(); ok {
		t.Fatal("the overlay did not clear")
	}
	n := 0
	a.start = func(workspace string) (Conversation, error) {
		n++
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: fmt.Sprintf("/tmp/lab/helper-%d.jsonl", n), Workspace: workspace}, nil
	}
	a.input.insert("half a reply")
	for _, handle := range []string{"tests", "docs"} {
		trafficAppend(t, a, made.ID, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: handle, Text: "take the " + handle})
	}
	trafficReadNow(t, a)
	trafficReadNow(t, a)
	if n != 2 {
		t.Fatalf("two starts opened %d conversations", n)
	}
	if active, ok := a.teamActive(); !ok || active.ID != made.ID {
		t.Fatal("the strip is not the team's after its helpers started")
	}
	if a.frontTabKey() != front || a.input.String() != "half a reply" {
		t.Fatal("a start moved the person off the manager or out of their words")
	}
	a.touch()
	row := plain(a.tabsRow(a.width))
	for _, handle := range []string{"tests", "docs"} {
		m, ok := mustTeam(t, a, made.ID).ByHandle(handle)
		if !ok || a.behind[m.Key] == nil {
			t.Fatalf("@%s is not a member held open: %+v", handle, mustTeam(t, a, made.ID).Members)
		}
		if !strings.Contains(row, "@"+handle) {
			t.Fatalf("@%s has no tab on the team's strip: %q", handle, row)
		}
	}
}

// THE MANAGER'S NO PUTS THE CHAT BACK AS IT WAS. The manager's team_dissolve
// writes the store from the engine (internal/teams' Dissolve); the window's
// Traffic read finds the team gone, closes its overlay, and the column is a
// plain chat's again. Nothing else in the window moved.
func TestADissolvedTeamLeavesThePlainChat(t *testing.T) {
	a, _ := helpersApp(t)
	front := a.frontTabKey()
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	made := helpersDisk(t, a)[0]
	tabs := len(a.tabList())
	if err := teamstore.Dissolve(a.profileDir, made.ID, front); err != nil {
		t.Fatal(err)
	}
	trafficReadNow(t, a)
	if _, ok := a.teamByID(made.ID); ok {
		t.Fatal("the window still holds the dissolved team")
	}
	if _, ok := a.teamActive(); ok {
		t.Fatal("the strip is still narrowed to the dissolved team")
	}
	if a.sideKind() != sideKindPlain || a.frontTabKey() != front {
		t.Fatal("the chat in front is not the same plain chat")
	}
	if got := len(a.tabList()); got != tabs {
		t.Fatalf("dissolving changed the strip from %d tabs to %d", tabs, got)
	}
	if _, last := helpersMenuRow(t, a); !last {
		t.Fatal("the dropdown lost its last row")
	}
}
