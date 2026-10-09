package tui3

import (
	"fmt"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamsGlobalManagerCardCreationAndRecoveryStayReachable(t *testing.T) {
	a, _, _ := menuApp(t)
	for _, withRoot := range []bool{false, true} {
		if withRoot {
			if err := a.teamEdit(func(f *teamstore.File) error { f.MakeRoot(a.now()); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		for _, width := range []int{12, 35, 100} {
			d := &teamsDraw{a: a}
			rows := a.teamsGlobalManagerCard(d, width, 0)
			found := false
			for _, tg := range d.targets {
				if tg.act == teamsActRootManager {
					found = true
				} else {
					t.Fatalf("absent manager exposes extra control: %+v", tg)
				}
				if tg.x0 < 0 || tg.x1 > width {
					t.Fatalf("target outside card: %+v", tg)
				}
			}
			if !found {
				t.Fatal("creation unavailable")
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("card overflows %d: %s", width, row)
				}
			}
		}
		d := &teamsDraw{a: a}
		rail := a.teamsRail(d, 30, len(a.teamsRailRows()))
		if strings.Contains(plain(strings.Join(rail, "\n")), teamGlobalManagerSlotWord) {
			t.Fatal("duplicate sidebar creation remains")
		}
		a.teamsRailWindow(&teamsDraw{a: a}, 30, 1)
	}
}

func TestTeamsGlobalManagerCardLinksPreviewAndDeletesOnlyItsConversation(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 120, 40
	key, file := a.frontTabKey(), a.file
	var rootID string
	if err := a.teamEdit(func(f *teamstore.File) error {
		rootID = f.MakeRoot(a.now())
		if err := f.AddMember(rootID, teamstore.Member{Key: key, File: file, Word: "Global conversation", Handle: "global"}); err != nil {
			return err
		}
		return f.SetManager(rootID, key)
	}); err != nil {
		t.Fatal(err)
	}
	a.tp.previews = map[string]teamsPreview{key: {text: "Actual saved update from the global manager"}}
	d := &teamsDraw{a: a}
	text := plain(strings.Join(a.teamsGlobalManagerCard(d, 90, 3), "\n"))
	for _, word := range []string{"Global manager", "@global", "Actual saved update", "+ Add chat", "Settings"} {
		if !strings.Contains(text, word) {
			t.Fatalf("missing %s: %s", word, text)
		}
	}
	for _, word := range []string{teamGlobalManagerSlotWord, "Choose AI manager"} {
		if strings.Contains(text, word) {
			t.Fatalf("populated global card has %s", word)
		}
	}
	a.teamChooseManagerOpen(rootID)
	if a.tmembers.on {
		t.Fatal("global manager chooser is still available")
	}
	var deletion teamsTarget
	for _, tg := range d.targets {
		if tg.act == teamsActDeleteGlobalManager {
			deletion = tg
		}
	}
	// Every saved-preview row uses the same conversation and overlay as its alias.
	links := 0
	for _, tg := range d.targets {
		if tg.act == teamsActMember {
			links++
			if tg.id != rootID || tg.arg != key {
				t.Fatal("wrong conversation link")
			}
		}
	}
	if links < 2 || deletion.arg != key {
		t.Fatal("preview or deletion door absent")
	}
	called := false
	a.deleteConversation = func(got string, choices map[string]string, _ map[string][]string) error {
		called = true
		if got != file || len(choices) != 1 {
			t.Fatal("deletion scope changed")
		}
		if choice, ok := choices[rootID]; !ok || choice != "" {
			t.Fatal("global-manager removal not authorized")
		}
		return nil
	}
	a.teamsDo(deletion)
	if !a.cdelete.on || a.cdelete.cursor != 0 {
		t.Fatal("deletion did not confirm with cancel default")
	}
	a.conversationDeleteChoose(0)
	if called {
		t.Fatal("cancel deleted conversation")
	}
	a.teamsDo(deletion)
	drain(t, a, a.conversationDeleteChoose(1))
	if !called || a.cdelete.on {
		t.Fatal("global-manager deletion blocked")
	}
}

func TestTeamsGlobalManagerDeletionDoesNotPermitOrdinaryManagerDeletion(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 120, 40
	key := a.frontTabKey()
	a.wall.teams = append(a.wall.teams, team{ID: "global-root", Root: true, Name: teamstore.RootName, Manager: key, Members: []teamMember{{Key: key, File: a.file}}})
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == parent {
			a.wall.teams[i].Manager = key
		}
	}
	called := false
	a.deleteConversation = func(string, map[string]string, map[string][]string) error { called = true; return nil }
	a.conversationDeleteOpen(a.file, "conflicting manager")
	if cmd := a.conversationDeleteChoose(1); cmd != nil || called || a.cdelete.message == "" {
		t.Fatal("conflicting ordinary manager deletion permitted")
	}
}

func TestTeamsGlobalManagerDeletionRefreshesWithoutOtherHeldMembers(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 120, 40
	a.profileDir = t.TempDir()
	key, file := a.frontTabKey(), a.file
	a.behind = nil
	var id string
	if err := a.teamEdit(func(f *teamstore.File) error {
		id = f.MakeRoot(a.now())
		if err := f.AddMember(id, teamstore.Member{Key: key, File: file, Handle: "global"}); err != nil {
			return err
		}
		return f.SetManager(id, key)
	}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	a.deleteConversation = func(_ string, choices map[string]string, _ map[string][]string) error {
		return teamstore.Update(a.profileDir, func(f *teamstore.File) error { return f.RemoveConversation(key, choices, a.now()) })
	}
	a.conversationDeleteOpen(file, "global")
	drain(t, a, a.conversationDeleteChoose(1))
	root, ok := a.teamsRoot()
	if !ok || root.Manager != "" || root.Holds(key) {
		t.Fatal("deleted global manager remained in loaded tree")
	}
	a.tp.sel = teamsAllRow
	a.page = pageTeams
	text := plain(strings.Join(a.teamsGlobalManagerCard(&teamsDraw{a: a}, 100, 0), "\n"))
	if strings.Contains(text, "+ Add chat") || strings.Contains(text, "Settings") || strings.Contains(text, "Choose AI manager") {
		t.Fatal("deleted manager card retained controls")
	}
	if !strings.Contains(text, teamGlobalManagerSlotWord) || strings.Contains(text, "Conversation unavailable") {
		t.Fatalf("creation did not return after deletion: %s", text)
	}
}

func TestTeamsGlobalManagerCreationUsesRootOverlayFromOrdinaryTeam(t *testing.T) {
	for _, withRoot := range []bool{false, true} {
		a, parent, _ := teamsPlaceLabIDs(t)
		var id string
		if withRoot {
			if err := a.teamEdit(func(f *teamstore.File) error { id = f.MakeRoot(a.now()); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		teamsFlush(t, a)
		a.tp.sel = parent
		file := fmt.Sprintf("/tmp/global-manager-%t.jsonl", withRoot)
		a.start = func(workspace string) (Conversation, error) {
			return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: workspace}, nil
		}
		drain(t, a, a.teamsRootManagerStart())
		root, ok := a.teamsRoot()
		if !ok || withRoot && root.ID != id || root.Manager != a.convKey(file) {
			t.Fatalf("global manager not created on correct root: withRoot=%v root=%+v ok=%v front=%s file=%s note=%s", withRoot, root, ok, a.frontTabKey(), a.file, a.tp.msg)
		}
		a.teamOverlaySync()
		if a.teamViews.id != root.ID || a.frontTabKey() != root.Manager || a.at(pageTeams) {
			t.Fatal("new global manager opened wrong overlay or conversation")
		}
	}
}

func TestTeamsGlobalManagerCardReportsFollowCurrentRouting(t *testing.T) {
	a, parent, child := menuApp(t)
	var id string
	key := a.frontTabKey()
	if err := a.teamEdit(func(f *teamstore.File) error {
		id = f.MakeRoot(a.now())
		if err := f.AddMember(id, teamstore.Member{Key: key, File: a.file}); err != nil {
			return err
		}
		tm, _ := f.Team(child)
		if err := f.SetManager(child, tm.Members[0].Key); err != nil {
			return err
		}
		return f.SetManager(id, key)
	}); err != nil {
		t.Fatal(err)
	}
	root, _ := a.teamsRoot()
	// A later explicit reporting change must be reflected without stale labels.
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == child {
			for j := range a.wall.teams[i].Members {
				a.wall.teams[i].Members[j].Home = false
				a.wall.teams[i].Members[j].Independent = true
			}
		}
		if a.wall.teams[i].ID == id {
			for j := range a.wall.teams[i].Members {
				if a.wall.teams[i].Members[j].Key != root.Manager {
					a.wall.teams[i].Members[j].Home = false
					a.wall.teams[i].Members[j].Independent = true
				}
			}
		}
	}
	d := &teamsDraw{a: a}
	text := plain(strings.Join(a.teamsGlobalManagerCard(d, 100, 0), "\n"))
	tm, _ := a.teamByID(child)
	other, _ := a.teamByID(parent)
	if strings.Contains(text, "Reports: "+tm.Name) || strings.Contains(text, "Reports: "+other.Name) {
		t.Fatal("card invented reporting relationships")
	}
}

func TestTeamsGlobalManagerCreationRefusesConcurrentLeaderAndKeepsNewChat(t *testing.T) {
	for _, withRoot := range []bool{false, true} {
		a, _, _ := teamsPlaceLabIDs(t)
		if withRoot {
			if err := a.teamEdit(func(f *teamstore.File) error { f.MakeRoot(a.now()); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		teamsFlush(t, a)
		newFile := fmt.Sprintf("/tmp/concurrent-global-%t.jsonl", withRoot)
		a.start = func(workspace string) (Conversation, error) {
			return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: newFile, Workspace: workspace}, nil
		}
		// Fold creation without driving the automatic queued write, so the
		// other window can land exactly between the optimistic and disk edits.
		for _, msg := range runCmd(a.teamsRootManagerStart()) {
			answer, ok := msg.(doorMsg)
			if !ok {
				t.Fatalf("unexpected creation answer: %T", msg)
			}
			answer.fold(true)
		}
		// Another window's assignment lands before this window's queued edit.
		var actualID string
		if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error {
			actualID = f.MakeRoot(a.now())
			if err := f.AddMember(actualID, teamstore.Member{Key: "other-global", File: "/tmp/other-global.jsonl"}); err != nil {
				return err
			}
			return f.SetManager(actualID, "other-global")
		}); err != nil {
			t.Fatal(err)
		}
		teamsFlush(t, a)
		stored, err := teamstore.Load(a.profileDir)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := stored.Root()
		loaded, ok := a.teamsRoot()
		if !ok || actual.Manager != "other-global" || loaded.Manager != actual.Manager || loaded.ID != actualID {
			t.Fatalf("concurrent manager overwritten or optimistic role retained: withRoot=%v stored=%+v loaded=%+v", withRoot, actual, loaded)
		}
		if a.frontTabKey() != a.convKey(newFile) || a.teamViews.id != "" {
			t.Fatal("new unassigned conversation lost or wrong overlay retained")
		}
	}
}

func TestMissingGlobalManagerUsesOnlyTheCreationState(t *testing.T) {
	a, _, _ := menuApp(t)
	var rootID string
	if err := a.teamEdit(func(f *teamstore.File) error { rootID = f.MakeRoot(a.now()); return f.SetManager(rootID, "missing") }); err != nil {
		t.Fatal(err)
	}
	a.tp.previews = map[string]teamsPreview{"missing": {missing: true}}
	d := &teamsDraw{a: a}
	text := plain(strings.Join(a.teamsGlobalManagerCard(d, 100, 0), "\n"))
	if !strings.Contains(text, "Optional") || !strings.Contains(text, teamGlobalManagerSlotWord) {
		t.Fatal("missing creation state")
	}
	if len(d.targets) != 1 || d.targets[0].act != teamsActRootManager {
		t.Fatal("missing manager exposes other controls")
	}
	if strings.Contains(text, "Conversation unavailable") || strings.Contains(text, "idle") {
		t.Fatal("missing manager still shows stale conversation")
	}

}

func TestEmptyTeamsPageOffersGlobalCreationOnlyInItsCard(t *testing.T) {
	a := placeApp(t)
	a.width, a.height = 110, 24
	drive(t, a, key("alt+2"))
	text := teamsFrameText(a)
	if !strings.Contains(text, "Global manager") || !strings.Contains(text, teamGlobalManagerSlotWord) {
		t.Fatal("empty Teams page hides global card")
	}
	rows := strings.Split(text, "\n")
	for _, target := range a.tp.targets {
		if target.hidden || !target.pane || (target.act != teamsActOrganize && target.act != teamsActNewTeam) {
			continue
		}
		if target.y < 0 || target.y >= len(rows) {
			t.Fatal("empty-page action outside frame")
		}
		label := "Organize"
		if target.act == teamsActNewTeam {
			label = "New AI team"
		}
		if !strings.Contains(ansi.Cut(rows[target.y], target.x0, target.x1), label) {
			t.Fatalf("empty-page %s hit misses visible label: %+v", label, target)
		}
	}
	creation := 0
	for _, target := range a.tp.targets {
		if target.act != teamsActRootManager {
			continue
		}
		creation++
		if !target.pane || target.hidden || target.y < placeHeadRows || target.y >= a.height-placeBareFootRows {
			t.Fatalf("creation not visible in card: %+v", target)
		}
		hit, ok := a.teamsTargetAt(target.x0+1, target.y)
		if !ok || hit.act != teamsActRootManager {
			t.Fatal("creation hit misses its button")
		}
	}
	if creation != 1 {
		t.Fatalf("creation buttons=%d", creation)
	}
}
