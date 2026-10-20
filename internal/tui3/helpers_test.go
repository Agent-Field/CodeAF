package tui3

import (
	"context"
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

// helpersMakeTeam chooses `+ New from this session` and lets the store take
// the team, as a person's click and the loop after it do. It hands back the
// team as the store holds it.
func helpersMakeTeam(t *testing.T, a *app) team {
	t.Helper()
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	teamsFlush(t, a)
	front := a.frontTabKey()
	for _, made := range helpersDisk(t, a) {
		if made.Manager == front {
			return made
		}
	}
	t.Fatalf("the row made no team this conversation manages: %+v", helpersDisk(t, a))
	return team{}
}

// `+ New from this session` MAKES THE TEAM WHEN IT IS CHOSEN. On the same frame,
// before the store has answered, the strip is the new team's, the column is
// the manager's and the box holds `/helpers` and the default ask, with what was
// there kept on the drafts ring; nothing is sent. Once the store has it, the
// team is on disk with this conversation as manager and only member, and the
// receipt says so. Enter then sends the ask, exactly as it reads.
func TestNewFromThisSessionMakesTheTeamAtOnce(t *testing.T) {
	a, fake := helpersApp(t)
	front := a.frontTabKey()
	a.input.insert("half a thought")
	a.openTeamMenu()
	for range teamMenuPicks(a.teamMenuRows()) {
		drive(t, a, key("down"))
	}
	drive(t, a, key("enter"))
	if a.teamMenu.on {
		t.Fatal("the dropdown stayed up after its row was chosen")
	}
	active, ok := a.teamActive()
	if !ok || active.Manager != front || a.sideKind() != sideKindManager {
		t.Fatal("choosing the row did not show the new team, with this conversation as its manager, at once")
	}
	if hint := a.trafficHint(); !strings.Contains(hint, "manager of lab") {
		t.Fatalf("the composer says %q, not that it talks to the manager", hint)
	}
	if got := a.input.String(); got != "/helpers "+helpersPrompt {
		t.Fatalf("the box holds %q", got)
	}
	if len(a.drafts.entries) == 0 || string(a.drafts.entries[0].text) != "half a thought" {
		t.Fatal("what was in the box was not kept on the drafts ring")
	}
	if len(fake.sent) != 0 {
		t.Fatalf("choosing the row sent %q", fake.sent)
	}

	teamsFlush(t, a)
	disk := helpersDisk(t, a)
	if len(disk) != 1 {
		t.Fatalf("the row left %d teams on disk: %+v", len(disk), disk)
	}
	made := disk[0]
	if made.Manager != front || len(made.Members) != 1 || made.Members[0].Key != front || made.Name != "lab" || made.Parent != "" {
		t.Fatalf("the team is not this conversation alone, as manager, at the top level: %+v", made)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersMadeWord, "lab")) {
		t.Fatal("no receipt says the team was made")
	}
	if len(fake.sent) != 0 {
		t.Fatalf("the store's answer sent %q before enter", fake.sent)
	}

	drive(t, a, key("enter"))
	if len(fake.sent) != 1 || fake.sent[0] != helpersPrompt {
		t.Fatalf("enter sent %q", fake.sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers "+helpersPrompt {
		t.Fatalf("the transcript says %q", shown)
	}
	if n := len(helpersDisk(t, a)); n != 1 {
		t.Fatalf("/helpers made another team: %d", n)
	}
}

// /helpers IN A MANAGER SENDS EXACTLY ITS WORDS. The model is handed what the
// transcript shows after the command and nothing else, and no team is made.
func TestHelpersSendsTheManagerExactlyItsWords(t *testing.T) {
	a, fake := helpersApp(t)
	helpersMakeTeam(t, a)
	a.input.reset()
	typeLine(t, a, "/helpers suggest one helper for the flaky tests")
	teamsFlush(t, a)
	if len(fake.sent) != 1 || fake.sent[0] != "suggest one helper for the flaky tests" {
		t.Fatalf("the model was handed %q", fake.sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers suggest one helper for the flaky tests" {
		t.Fatalf("the transcript says %q", shown)
	}
	if n := len(helpersDisk(t, a)); n != 1 {
		t.Fatalf("/helpers made a team: %d on disk", n)
	}
}

// THE DEFAULT ASK IS ONE SENTENCE IN THE PERSON'S WORDS. It names no tool and
// none of codeaf's machinery, and it says what happens next rather than laying
// down a rule a memory could keep.
func TestTheDefaultHelpersAskIsOneSentenceInThePersonsWords(t *testing.T) {
	if strings.Count(helpersPrompt, ".") != 1 || !strings.HasSuffix(helpersPrompt, ".") {
		t.Fatalf("the default ask is not one sentence: %q", helpersPrompt)
	}
	for _, internal := range []string{"team_", "tool", "manager", "codeaf", "handle", "don't", "until"} {
		if strings.Contains(strings.ToLower(helpersPrompt), internal) {
			t.Errorf("the default ask says %q: %q", internal, helpersPrompt)
		}
	}
	for _, want := range []string{"two or three helpers", "yes", "cancel"} {
		if !strings.Contains(helpersPrompt, want) {
			t.Errorf("the default ask does not say %q: %q", want, helpersPrompt)
		}
	}
}

// /helpers BEFORE THE STORE HAS THE TEAM IS HELD, NOT LOST. The ask waits for
// the write that carries the team, so the model's first step finds the
// conversation already a manager; it is sent once, after the receipt. (The
// command is run directly: the loop that carries a key also carries the write,
// in the same message.)
func TestHelpersBeforeTheStoreHasTheTeamIsHeld(t *testing.T) {
	a, fake := helpersApp(t)
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	spend(t, a, a.runHelpersCommand(""))
	if len(fake.sent) != 0 {
		t.Fatal("the ask went out before the store took the team")
	}
	teamsFlush(t, a)
	if len(fake.sent) != 1 || fake.sent[0] != helpersPrompt {
		t.Fatalf("the store's answer sent %q", fake.sent)
	}
	if shown := lastPersonLine(t, a); shown != "/helpers "+helpersPrompt {
		t.Fatalf("the transcript says %q", shown)
	}
	if n := len(helpersDisk(t, a)); n != 1 {
		t.Fatalf("%d teams on disk", n)
	}
}

// /helpers MAKES NO TEAM. In a conversation that manages none — with no team
// selected, or seen as an ordinary member of the team selected — it changes
// nothing, sends nothing, and says where the team is made. On home it answers
// the same way.
func TestHelpersInAConversationThatManagesNoTeamSaysWhereTheTeamIsMade(t *testing.T) {
	if got := homeFate("helpers", ""); got != fateAnswers {
		t.Fatalf("the drop-up says /helpers %q on home", got)
	}
	a, fake := helpersApp(t)
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	if !transcriptSays(a, helpersNotManagerWord) || len(helpersDisk(t, a)) != 0 || len(fake.sent) != 0 {
		t.Fatal("/helpers in a conversation in no team made a team or sent something")
	}

	harbor, err := a.teamMake("harbor", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	spend(t, a, a.teamActivate(harbor))
	a.entries = nil
	typeLine(t, a, "/helpers")
	teamsFlush(t, a)
	if !transcriptSays(a, helpersNotManagerWord) || len(fake.sent) != 0 {
		t.Fatal("/helpers in a member of the selected team, not its manager, did not refuse")
	}
	if got := mustTeam(t, a, harbor); got.Manager != "" {
		t.Fatalf("/helpers gave the selected team a manager: %q", got.Manager)
	}
}

// THE ROW KEEPS THE RULES EVERY TEAM KEEPS. A name already taken is not reused;
// in a conversation that already manages a team it makes no second one,
// shows the one it manages, and leaves the box alone.
func TestNewFromThisSessionKeepsTheRulesEveryTeamKeeps(t *testing.T) {
	a, _ := helpersApp(t)
	if _, err := a.teamMake("lab", nil); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	made := helpersMakeTeam(t, a)
	if strings.EqualFold(made.Name, "lab") {
		t.Fatalf("the new team took a name another team has: %+v", helpersDisk(t, a))
	}

	spend(t, a, a.teamOverlayChoose(""))
	a.input.setText("keep this")
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	teamsFlush(t, a)
	if n := len(helpersDisk(t, a)); n != 2 {
		t.Fatalf("the row in a manager made another team: %d teams", n)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersManagesWord, made.Name)) {
		t.Fatal("the row did not say which team this conversation manages")
	}
	if got := a.input.String(); got != "keep this" {
		t.Fatalf("the row replaced the box in a manager: %q", got)
	}
	if active, ok := a.teamActive(); !ok || active.ID != made.ID {
		t.Fatal("the row in a manager did not show the team it manages")
	}
}

// THE STORE'S REFUSAL IS FINAL. Another window made this conversation another
// team's manager after this one loaded: the window's own check passes, the
// store refuses with the one-manager rule, the window takes the store's teams
// back, closes the overlay, gives the box back, and the model is asked nothing.
func TestNewFromThisSessionRefusedByTheStorePutsTheWindowBack(t *testing.T) {
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
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	teamsFlush(t, a)
	if len(fake.sent) != 0 {
		t.Fatal("a refused team still sent something")
	}
	disk := helpersDisk(t, a)
	if len(disk) != 1 || disk[0].ID != elsewhere {
		t.Fatalf("the store holds %+v, want only the other window's team", disk)
	}
	if active, ok := a.teamActive(); ok && active.ID != elsewhere {
		t.Fatal("the overlay stayed on a team the store refused")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the box still holds the refused team's ask: %q", got)
	}
	if !transcriptSays(a, "a conversation can manage one team and its descendants") {
		t.Fatal("the refusal was not said in the store's words")
	}
}

// WHERE THE ROW CANNOT WORK IT SAYS SO AND MAKES NOTHING: a connection that
// holds one conversation at a time, where a helper could not open beside its
// manager, and the welcome screen.
func TestNewFromThisSessionRefusesWhereItCannotWork(t *testing.T) {
	a, fake := helpersApp(t)
	a.shared = true
	row, _ := helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
	if !transcriptSays(a, helpersShared) || len(helpersDisk(t, a)) != 0 || len(fake.sent) != 0 {
		t.Fatal("a one-conversation connection made a team")
	}

	a, fake = helpersApp(t)
	a.welcome.open, a.welcome.start = true, true
	row, _ = helpersMenuRow(t, a)
	spend(t, a, a.teamMenuDo(row))
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

// WITH A TEAM SELECTED THERE IS NO `+ New from this session`: the team it
// makes is a top-level team, and made from inside another team's overlay it
// would read as nesting. Nesting is `+ Add subteam` on Teams. Clearing the
// selection brings the row back.
func TestWithATeamSelectedThereIsNoNewFromThisSession(t *testing.T) {
	a, _ := helpersApp(t)
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
	made := helpersMakeTeam(t, a)
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
	a.input.setText("half a reply")
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
	made := helpersMakeTeam(t, a)
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

// THE 💡 TIP IS SAID WHERE THE ROW WOULD ACT, AND ONLY THERE: after the first
// exchange, with no team selected, in a conversation that manages none; never
// before a word was said, never with a team on the strip, never in a manager.
// Choosing the row retires it for good.
func TestTheHelpersTipIsArmedWhereTheRowWouldAct(t *testing.T) {
	var row notice
	for _, n := range notices {
		if n.id == "helpers-team" {
			row = n
		}
	}
	if row.id == "" || !strings.HasPrefix(row.text, helpersRowWord) || row.retire != eventHelpersUsed {
		t.Fatalf("the tip is %+v", row)
	}
	a, _ := helpersApp(t)
	a.turn = 0
	if row.armed(a) {
		t.Fatal("the tip is armed before the first exchange")
	}
	a.turn = 1
	if !row.armed(a) {
		t.Fatal("the tip is not armed in a spoken-to conversation with no team")
	}
	harbor, err := a.teamMake("harbor", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	spend(t, a, a.teamActivate(harbor))
	if row.armed(a) {
		t.Fatal("the tip is armed with a team selected")
	}
	spend(t, a, a.teamOverlayChoose(""))
	if !row.armed(a) {
		t.Fatal("clearing the team did not arm the tip again")
	}
	helpersMakeTeam(t, a)
	if !a.notices.retired(row.id) {
		t.Fatal("choosing the row did not retire its tip")
	}
	spend(t, a, a.teamOverlayChoose(""))
	if row.armed(a) {
		t.Fatal("the tip is armed in a conversation that already manages a team")
	}
}

// wordNamingAgent is the fake conversation with the naming ask: it answers name
// (or err), and keeps what it was asked.
type wordNamingAgent struct {
	*fakeAgent
	name   string
	err    error
	titles []string
	words  int
}

func (n *wordNamingAgent) NameTeamWords(_ context.Context, titles []string, words int) (string, error) {
	n.titles, n.words = append([]string(nil), titles...), words
	return n.name, n.err
}

// groupNamingAgent has only the group's ask, as an engine from before the word
// limit does: up to three words, which the surface cuts to two.
type groupNamingAgent struct {
	*fakeAgent
	name string
}

func (n *groupNamingAgent) NameTeam(context.Context, []string) (string, error) { return n.name, nil }

// THE TEAM IS NAMED AFTER THE CONVERSATION. The click makes it under the
// folder's name at once; the naming role is asked for two words over this
// conversation's title, and the team takes the answer, cleaned — on disk, on
// the strip, and in the receipt, which says the final name once.
func TestNewFromThisSessionNamesTheTeamAfterTheConversation(t *testing.T) {
	a, fake := helpersApp(t)
	namer := &wordNamingAgent{fakeAgent: fake, name: "Parser Port"}
	a.agent = namer
	made := helpersMakeTeam(t, a)
	if len(namer.titles) != 1 || namer.titles[0] != "Shipping the parser" || namer.words != helpersNameWords {
		t.Fatalf("the namer was asked %q for %d words", namer.titles, namer.words)
	}
	if made.Name != "parser port" {
		t.Fatalf("the team on disk is called %q, want the conversation's name", made.Name)
	}
	if active, ok := a.teamActive(); !ok || active.Name != "parser port" {
		t.Fatal("the strip does not show the team's new name")
	}
	if !transcriptSays(a, fmt.Sprintf(helpersMadeWord, "parser port")) {
		t.Fatal("the receipt does not name the team by its final name")
	}
	if transcriptSays(a, fmt.Sprintf(helpersNamedWord, "parser port")) {
		t.Fatal("the name was said twice")
	}
}

// AN OLDER ENGINE'S THREE WORDS ARE CUT TO TWO.
func TestAGroupNamersAnswerIsCutToTwoWords(t *testing.T) {
	a, fake := helpersApp(t)
	a.agent = &groupNamingAgent{fakeAgent: fake, name: "guitar pickup history"}
	if made := helpersMakeTeam(t, a); made.Name != "guitar pickup" {
		t.Fatalf("the team is called %q", made.Name)
	}
}

// A NAME THAT ARRIVES AFTER THE RECEIPT IS SAID ON ITS OWN LINE.
func TestALateNameIsSaid(t *testing.T) {
	a, _ := helpersApp(t)
	made := helpersMakeTeam(t, a)
	a.helpersNamed(a.helpersName.gen, "guitar pickups", nil)
	teamsFlush(t, a)
	if got := mustTeam(t, a, made.ID).Name; got != "guitar pickups" {
		t.Fatalf("the team is called %q", got)
	}
	if disk := helpersDisk(t, a); disk[0].Name != "guitar pickups" {
		t.Fatalf("the name did not reach the disk: %q", disk[0].Name)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersNamedWord, "guitar pickups")) {
		t.Fatal("a late name was not said")
	}
}

// THE ANSWER NEVER OVERWRITES A CHOICE OR TAKES A NAME. The team keeps the
// name it has when the person renamed it here, another window renamed it in
// the store, another team has the suggested name, the ask failed, the answer
// is not a name, or a newer ask replaced it; a team that is gone stays gone.
func TestTheSuggestedNameNeverOverwritesAChoice(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, a *app, id string) (gen int, answer string, err error)
		want  string
	}{
		{"renamed here", func(t *testing.T, a *app, id string) (int, string, error) {
			if err := a.teamRename(id, "mine"); err != nil {
				t.Fatal(err)
			}
			teamsFlush(t, a)
			return a.helpersName.gen, "guitar pickups", nil
		}, "mine"},
		{"renamed in another window", func(t *testing.T, a *app, id string) (int, string, error) {
			if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error {
				f.Teams[teamIndex(f.Teams, id)].Name = "theirs"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return a.helpersName.gen, "guitar pickups", nil
		}, "theirs"},
		{"name taken", func(t *testing.T, a *app, id string) (int, string, error) {
			if _, err := a.teamMake("guitar pickups", nil); err != nil {
				t.Fatal(err)
			}
			teamsFlush(t, a)
			return a.helpersName.gen, "Guitar Pickups", nil
		}, "lab"},
		{"ask failed", func(t *testing.T, a *app, id string) (int, string, error) {
			return a.helpersName.gen, "guitar pickups", fmt.Errorf("no model")
		}, "lab"},
		{"not a name", func(t *testing.T, a *app, id string) (int, string, error) {
			return a.helpersName.gen, "   ", nil
		}, "lab"},
		{"an older ask", func(t *testing.T, a *app, id string) (int, string, error) {
			return a.helpersName.gen - 1, "guitar pickups", nil
		}, "lab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := helpersApp(t)
			made := helpersMakeTeam(t, a)
			gen, answer, err := c.setup(t, a, made.ID)
			a.helpersNamed(gen, answer, err)
			teamsFlush(t, a)
			for _, team := range helpersDisk(t, a) {
				if team.ID == made.ID && team.Name != c.want {
					t.Fatalf("the team is called %q on disk, want %q", team.Name, c.want)
				}
			}
			if got := mustTeam(t, a, made.ID).Name; got != c.want {
				t.Fatalf("the window calls the team %q, want %q", got, c.want)
			}
		})
	}

	a, _ := helpersApp(t)
	made := helpersMakeTeam(t, a)
	if err := teamstore.Dissolve(a.profileDir, made.ID, made.Manager); err != nil {
		t.Fatal(err)
	}
	trafficReadNow(t, a)
	a.helpersNamed(a.helpersName.gen, "guitar pickups", nil)
	teamsFlush(t, a)
	if len(helpersDisk(t, a)) != 0 {
		t.Fatal("naming a team that was gone brought it back")
	}
}
