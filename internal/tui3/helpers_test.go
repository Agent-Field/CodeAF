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

// /helpers MAKES A TEAM WITH THIS CONVERSATION AS ITS MANAGER, and only then
// asks it for helpers. The team is on disk with the conversation as manager and
// only member; the strip is narrowed to it; the column is the manager's; the
// transcript says `/helpers …` and the model was handed the ask, naming both the
// verb that grows the team and the one that takes it back.
func TestHelpersMakesThisConversationTheManagerAndAsksForHelpers(t *testing.T) {
	a, fake := helpersApp(t)
	front := a.frontTabKey()
	before := len(fake.sent)
	typeLine(t, a, "/helpers the flaky tests")
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
	sent := fake.sent[before]
	if sent != helpersAskOf("lab", "the flaky tests") {
		t.Fatalf("the model was handed %q", sent)
	}
	for _, want := range []string{"team_start", "team_dissolve", "Start nobody until I agree", "the flaky tests"} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the ask does not say %q: %q", want, sent)
		}
	}
	if shown := lastPersonLine(t, a); shown != "/helpers the flaky tests" {
		t.Fatalf("the transcript says %q, not what was typed", shown)
	}
	if !transcriptSays(a, fmt.Sprintf(helpersMadeWord, "lab")) {
		t.Fatal("no receipt says the team was made")
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

// THE COLUMN'S DOOR IS /helpers PRESSED. A chat in no team has `+ Make team`
// beside its Tasks word; pressing it makes the team and asks, as typing does;
// and the chat, now a manager, has the Traffic word there instead.
func TestTheColumnsMakeTeamDoorIsHelpers(t *testing.T) {
	a, fake := helpersApp(t)
	line, row := a.sideHeadRow(40)
	if !strings.Contains(plain(line), helpersDoorWord) {
		t.Fatalf("a chat in no team has no %q in its column's header: %q", helpersDoorWord, plain(line))
	}
	var door *sideDoor
	for i := range row.doors {
		if row.doors[i].act.kind == sideActHelpers {
			door = &row.doors[i]
		}
	}
	if door == nil || !strings.HasPrefix(door.hint, helpersDoorHint) {
		t.Fatalf("the header's doors are %+v", row.doors)
	}
	before := len(fake.sent)
	cmd, _ := a.sidePress(row, door.span.from)
	spend(t, a, cmd)
	teamsFlush(t, a)
	if len(helpersDisk(t, a)) != 1 || len(fake.sent) != before+1 || lastPersonLine(t, a) != "/helpers" {
		t.Fatal("pressing the door did not do what /helpers does")
	}
	line, _ = a.sideHeadRow(40)
	if strings.Contains(plain(line), helpersDoorWord) || !strings.Contains(plain(line), sideTrafficWord) {
		t.Fatalf("the manager's header is %q", plain(line))
	}
}

// THE MANAGER'S NO PUTS THE CHAT BACK AS IT WAS. The manager's team_dissolve
// writes the store from the engine (internal/teams' Dissolve); the window's
// Traffic read finds the team gone, closes its overlay, and the column is a
// plain chat's again, with its door. Nothing else in the window moved.
func TestADissolvedTeamLeavesThePlainChatAndItsDoor(t *testing.T) {
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
	line, _ := a.sideHeadRow(40)
	if !strings.Contains(plain(line), helpersDoorWord) {
		t.Fatalf("the plain chat's door did not come back: %q", plain(line))
	}
}

// A NARROW COLUMN LOSES THE DOOR, and keeps the key that hides it.
func TestANarrowColumnLosesTheMakeTeamDoorFirst(t *testing.T) {
	a, _ := helpersApp(t)
	line, _ := a.sideHeadRow(sideColsMin - 6)
	if strings.Contains(plain(line), helpersDoorWord) {
		t.Fatalf("the door was squeezed into %q", plain(line))
	}
}
