package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamMemberUsesExistingContinueHereQuestion(t *testing.T) {
	celltest.Available(t)
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
	enableSurfaceTeamSharing(t, a)
	row := continueRows[0]
	member := teamMember{Key: teamstore.RemoteKey(row.Cell), Word: row.Title}
	a.wall.loaded = true
	a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{member}}}
	a.trafficGo(member.Key)
	ask, ok := a.homeAsking()
	if !ok || ask.question.Head != continueAsk || a.home.armed != machineArm(row.Cell) {
		t.Fatalf("member did not use existing continuation: %+v %v", ask, ok)
	}
}

func TestUnsyncedMemberSaysChatIsOnOtherComputer(t *testing.T) {
	a := teamsPlaceLab(t)
	enableSurfaceTeamSharing(t, a)
	a.devRow.devices = []chatlist.Device{{ID: "a", Name: "My desktop"}}
	cmd, remote := a.teamsRemoteGo(teamMember{Key: teamstore.OtherComputerKey("device:a"), Word: "local chat"})
	if !remote || cmd != nil || !strings.Contains(a.tp.msg, "My desktop and is not shared. Open it there.") {
		t.Fatalf("unsynced member: %q", a.tp.msg)
	}
}

func TestAppliedTeamsRefreshDropdownAndAtListFromFileStamp(t *testing.T) {
	a := teamsPlaceLab(t)
	enableSurfaceTeamSharing(t, a)
	teamsFlush(t, a)
	a.comp.open, a.comp.query = true, "team:alpha"
	a.fillMentions()
	a.comp.rank()
	if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, team{ID: "synced-team", Name: "alpha", Members: []teamMember{{Key: teamstore.RemoteKey("shared-cell"), Word: "storage"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.trafficReadOf(nil, false))
	if len(a.comp.teamHits) != 1 || a.comp.teamHits[0].id != "synced-team" {
		t.Fatal("open @ list missed apply")
	}
	got, ok := a.teamByID("synced-team")
	if !ok || got.Name != "alpha" || len(a.teamTree().Teams) < 1 {
		t.Fatal("surface team model missed apply")
	}
	// Removing the final team must also invalidate a nonempty cached model.
	if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error { f.Teams = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.trafficReadOf(nil, false))
	if len(a.comp.teamHits) != 0 {
		t.Fatal("open @ list kept deleted team")
	}
	if len(a.wall.teams) != 0 {
		t.Fatal("deleted team remained in dropdown model")
	}
}

func TestImportedStartsAndStopsNeverActOnSurface(t *testing.T) {
	a, id, _, _ := trafficApp(t)
	for _, kind := range []string{teamstore.KindStart, teamstore.KindStop} {
		if cmd := a.trafficAct(id, teamstore.Entry{ID: "000000000001", Origin: "other/000000000001", Kind: kind, From: teamstore.FromManager, To: "storage"}); cmd != nil {
			t.Fatal("imported history caused action")
		}
	}
}

// F4 keeps ownership discovery for a remote member while local members take Home's local door.
func TestRemoteTeamMemberFetchesItsContinueRowWhenHomeHasNotReadItYet(t *testing.T) {
	celltest.Available(t)
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
	enableSurfaceTeamSharing(t, a)
	row := continueRows[0]
	a.machineRead.rows, a.machineRead.at = nil, time.Time{}
	member := teamMember{Key: teamstore.RemoteKey(row.Cell), Word: row.Title, SyncID: "cell:" + row.Cell}
	a.wall.loaded = true
	a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{member}}}
	spend(t, a, a.trafficGo(member.Key))
	ask, ok := a.homeAsking()
	if !ok || ask.question.Head != continueAsk {
		t.Fatal("uncached remote team member did not fetch its continuation row")
	}
}

// Render the real frame after entering through Teams or a chat tab. Inspecting
// home.ask alone would miss a question raised while Home was still hidden.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestMemberGesturesShowContinuationAndBothAnswersFromTeamsAndTabs(t *testing.T) {
	celltest.Available(t)
	for _, gesture := range []string{"teams", "tab"} {
		for _, yes := range []bool{false, true} {
			for _, local := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/yes=%v/local=%v", gesture, yes, local), func(t *testing.T) {
					lab := newHomeLab(t)
					workspace := lab.workspace("project")
					mine := lab.session("project", "0000000000000001", "Mine", workspace, time.Now())
					a := lab.app(mine)
					a.width, a.height = 200, 70
					src := &continuationListing{row: continueRows[0]}
					a.machines = src
					taker := &fakeTaker{on: func(id string) {
						lab.session("project", id, "Port the picker", workspace, time.Now())
						src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here })
					}}
					a.taker = taker
					drain(t, a, a.askMachines())
					m := teamMember{Key: teamstore.RemoteKey(src.row.Cell), Word: src.row.Title, Handle: "worker", SyncID: "cell:" + src.row.Cell}
					if local {
						row := lab.session("project", src.row.Cell, src.row.Title, workspace, time.Now())
						m.File = row
						m.Key = a.convKey(row)
						m.Where = workspace
					}
					a.wall.loaded = true
					a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{m}, Manager: m.Key}}
					a.profileDir = t.TempDir()
					enableSurfaceTeamSharing(t, a)
					if err := teamstore.Update(a.profileDir, func(f *teamstore.File) error { f.Teams = teamsClone(a.wall.teams); return nil }); err != nil {
						t.Fatal(err)
					}
					if gesture == "teams" {
						drain(t, a, a.showPage(pageTeams))
						a.tp.sel = "alpha"
						drain(t, a, a.teamsRead(true))
						if text := teamsFrameText(a); !strings.Contains(text, "Chat is on host-a") {
							t.Fatalf("remote member shown as missing: %s", text)
						}
						target := teamsTargetOf(t, a, teamsActMember, "alpha")
						drain(t, a, a.teamsDo(target))
					} else {
						a.leavePlace()
						drain(t, a, a.tabGo(chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word}))
					}
					frame, _, _ := a.frame()
					if !a.at(pageHome) || !strings.Contains(plain(frame), continueAsk) {
						t.Fatalf("question not on screen:\n%s", plain(frame))
					}
					if a.home.armed != machineArm(src.row.Cell) {
						t.Fatal("wrong armed row")
					}
					selected := a.home.lines[a.home.cursor]
					if selected.remote == nil || selected.remote.Cell != src.row.Cell {
						t.Fatal("remote row not selected")
					}
					before, _ := os.ReadFile(teamstore.Path(a.profileDir))
					if yes {
						confirm(t, a)
						if a.at(pageHome) || len(taker.cells) != 1 {
							t.Fatal("yes did not open continued chat")
						}
					} else {
						drive(t, a, key("2"), key("enter"))
						after, _ := os.ReadFile(teamstore.Path(a.profileDir))
						if string(before) != string(after) {
							t.Fatal("leaving the chat there edited its team")
						}
						if !a.at(pageHome) || len(taker.cells) != 0 || a.file != mine {
							t.Fatal("leave it there changed the chat")
						}
					}
				})
			}
		}
	}
}

// F1 preserves observational refresh only while paired sharing is effective.
func TestPairedRefreshClockReadsChangedFileWithoutRepairingOrWritingAnything(t *testing.T) {
	a := teamsPlaceLab(t)
	enableSurfaceTeamSharing(t, a)
	teamsFlush(t, a)
	raw := []byte(`{"version":2,"teams":[{"id":"zz","name":"first","parent":"","members":[],"manager":"","made":"2026-01-01T00:00:00Z"},{"id":"aa","name":"second","parent":"","members":[],"manager":"","made":"2026-01-02T00:00:00Z"}]}`)
	if err := os.WriteFile(teamstore.Path(a.profileDir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.teamsRefreshTick())
	after, _ := os.ReadFile(teamstore.Path(a.profileDir))
	if string(after) != string(raw) {
		t.Fatal("refresh repaired the shared file")
	}
	if len(a.teamsDisk.queue) != 0 {
		t.Fatal("refresh queued a repair")
	}
	if len(a.wall.teams) != 2 || a.wall.teams[0].ID != "zz" {
		t.Fatal("shared refresh changed file order")
	}
}

func TestMirrorTailRefreshKeepsLocalCursorAndShowsHistoryInBothSurfaces(t *testing.T) {
	a, id, _, _ := trafficApp(t)
	enableSurfaceTeamSharing(t, a)
	a.width, a.height = 200, 70
	trafficAppend(t, a, id, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: teamstore.ToRoom, Text: "local tail", At: time.Now().Add(time.Hour)})
	spend(t, a, a.trafficRead())
	localCursor := a.traffic.cursor[id]
	var lines []teamstore.Entry
	words := map[string]string{teamstore.KindDirective: "Send", teamstore.KindStart: "Begin", teamstore.KindQuestion: "Ask", teamstore.KindStop: "End"}
	for _, kind := range []string{teamstore.KindDirective, teamstore.KindStart, teamstore.KindQuestion, teamstore.KindStop} {
		lines = append(lines, teamstore.Entry{ID: "desktop/" + kind, At: time.Now(), Origin: "desktop/" + kind, Kind: kind, From: teamstore.FromManager, To: "storage", Text: words[kind]})
	}
	a.devRow.devices = []chatlist.Device{{ID: "desktop", Name: "My desktop"}}
	if err := teamstore.WriteTrafficMirror(a.profileDir, id, lines); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.trafficRead())
	if a.traffic.cursor[id] != localCursor || len(a.traffic.rows[id]) != 5 {
		t.Fatal("mirror changed local cursor or duplicated history")
	}
	for _, line := range a.traffic.rows[id] {
		if line.Origin != "" && a.trafficComputer(line) != "from My desktop" {
			t.Fatal("history lacks computer name")
		}
	}
	spend(t, a, a.trafficRead())
	if len(a.traffic.rows[id]) != 5 {
		t.Fatal("quiet tick duplicated mirror")
	}
	a.sideToggleThread(sideThreadKey(id, sideGeneral))
	pane := strings.Join(railLines(t, a), "\n")
	for _, want := range []string{"Send", "Ask", "started", "stopped", "from My desktop"} {
		if !strings.Contains(pane, want) {
			t.Fatalf("Traffic did not display %q:\n%s", want, pane)
		}
	}
	spend(t, a, a.tookRoster(rosterMsg{devices: []chatlist.Device{{ID: "desktop", Name: "Renamed desktop"}}}))
	if pane := strings.Join(railLines(t, a), "\n"); !strings.Contains(pane, "from Renamed desktop") {
		t.Fatalf("cached Traffic kept an old computer name: %s", pane)
	}
	if a.traffic.seen[id] != localCursor {
		t.Fatal("mirror changed the local seen position")
	}
	spend(t, a, a.showPage(pageTeams))
	spend(t, a, a.teamsSelect(id))
	spend(t, a, a.teamsRead(true))
	overview := teamsFrameText(a)
	for _, want := range []string{"Send", "Begin", "Ask", "End"} {
		if !strings.Contains(overview, want) {
			t.Fatalf("Teams page did not display %q:\n%s", want, overview)
		}
	}
	a.leavePlace()
	lines[0].ID = "desktop/new-directive"
	lines[0].Text = "New"
	if err := teamstore.WriteTrafficMirror(a.profileDir, id, lines); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.trafficRead())
	if pane := strings.Join(railLines(t, a), "\n"); !strings.Contains(pane, "New") || strings.Contains(pane, "Send") {
		t.Fatalf("same-size older mirror window left stale rows: %s", pane)
	}
	if err := teamstore.WriteTrafficMirror(a.profileDir, id, nil); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.trafficRead())
	if len(a.traffic.rows[id]) != 1 || a.traffic.cursor[id] != localCursor {
		t.Fatal("mirror retirement retained history or changed cursor")
	}
}

// Typing in a team chat another computer holds is a message to that chat, not a
// Traffic line, so it must take exactly the door the same chat takes outside any
// team (continue.go's ownership read and its question). Team sharing must not put
// a refusal in front of that door, lose the words or write Traffic.
func TestTypingInATeamChatHeldElsewhereTakesTheSameDoorAsAnyChat(t *testing.T) {
	celltest.Available(t)
	type outcome struct {
		draft  string
		asking bool
		notes  []string
	}
	send := func(inTeam bool) outcome {
		lab := newHomeLab(t)
		workspace := lab.workspace("project")
		row := continueRows[0]
		file := lab.session("project", row.Cell, row.Title, workspace, time.Now())
		a := lab.app(file)
		a.profileDir = t.TempDir()
		enableSurfaceTeamSharing(t, a)
		a.machines = chatlist.Static{row}
		drain(t, a, a.askMachines())
		a.leavePlace()
		if inTeam {
			key := a.convKey(file)
			m := teamMember{Key: key, File: file, Word: row.Title, Handle: "worker", SyncID: "cell:" + row.Cell}
			a.wall.loaded = true
			a.wall.activeID = "alpha"
			a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{m}, Manager: key}}
		}
		a.input.setText("please do this here")
		spend(t, a, a.enterCheckedLine(false))
		_, asking := a.homeAsking()
		got := outcome{draft: a.input.String(), asking: asking}
		for _, e := range a.entries {
			if e.kind == entryNote {
				got.notes = append(got.notes, plain(e.text))
			}
		}
		if lines, _ := teamstore.ReadTraffic(a.profileDir, "alpha", "", 0); len(lines) != 0 {
			t.Fatalf("typing in the chat wrote Traffic: %+v", lines)
		}
		return got
	}
	alone, team := send(false), send(true)
	if team.draft != "please do this here" {
		t.Fatalf("the draft was lost: %q", team.draft)
	}
	if !reflect.DeepEqual(alone, team) {
		t.Fatalf("team membership changed what typing in a held chat does:\nalone %+v\nteam  %+v", alone, team)
	}
}

func enableSurfaceTeamSharing(t *testing.T, a *app) {
	t.Helper()
	a.teamsPaired = func() bool { return true }
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	refreshSurfaceTeamSharing(t, a)
}

// refreshSurfaceTeamSharing runs the preference read beside the simulated loop.
func refreshSurfaceTeamSharing(t *testing.T, a *app) {
	t.Helper()
	a.refreshProfileUI()
	drive(t, a, runCmd(a.takeTeamsSharingCmd())...)
}

// FIX4 F1: Update uses the adopted profile snapshot; a direct file write is
// not a setting adoption and cannot arm the sharing clock on the surface loop.
// C9 pins off explicitly so this still tests profile adoption after the default changes.
func TestTeamSharingClockUsesOnlyAdoptedProfileSettings(t *testing.T) {
	a, _ := sheetApp(t)
	a.teamsPaired = func() bool { return true }
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
	if !ok {
		t.Fatal("missing sharing setting")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	refreshSurfaceTeamSharing(t, a)
	a.wall.loaded = true
	if cmd := a.teamsRefreshArm(); cmd != nil || a.teamsDisk.refreshing {
		t.Fatal("the disabled profile armed the sharing clock")
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []tea.Msg{struct{}{}, key("x"), key("backspace"), teamsRefreshTickMsg{}} {
		a.Update(msg)
		if a.teamsDisk.refreshing {
			t.Fatal("the surface loop read an unadopted sharing setting")
		}
	}
	refreshSurfaceTeamSharing(t, a)
	a.Update(struct{}{})
	if !a.teamsDisk.refreshing {
		t.Fatal("profile adoption did not arm the sharing clock")
	}
	reopened := newApp(context.Background(), Options{
		Agent: &fakeAgent{}, Workspace: "/tmp/lab", UsageLedger: labLedger(),
		ProfileDir: a.profileDir, TeamsPaired: a.teamsPaired,
		BashBackgroundAfterSeconds: config.DefaultBashBackgroundAfter,
		Env:                        envOf(map[string]string{"TERM": "xterm-256color"}),
	})
	reopened.wall.loaded = true
	if cmd := reopened.teamsRefreshArm(); cmd == nil || !reopened.teamsDisk.refreshing {
		t.Fatal("boot did not adopt the enabled sharing setting")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if cmd := a.teamsRefreshTick(); cmd == nil || !a.teamsDisk.refreshing {
		t.Fatal("the sharing tick read an unadopted file change")
	}
	spend(t, a, a.settle())
	if cmd := a.teamsRefreshTick(); cmd != nil || a.teamsDisk.refreshing {
		t.Fatal("turn completion did not adopt the disabled sharing setting")
	}
}

// C9 pins off explicitly so local shortcuts still exercise the sharing-off path.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestSharingOffStopsRefreshAndKeepsLocalChatShortcuts(t *testing.T) {
	celltest.Available(t)
	checkSharingDisabledShortcuts(t, true)
}

// F1 preserves the refresh clock and local shortcuts with an unset preference and no pairing.
func TestUnpairedSharingUnsetStopsRefreshAndKeepsLocalChatShortcuts(t *testing.T) {
	checkSharingDisabledShortcuts(t, false)
}

func checkSharingDisabledShortcuts(t *testing.T, explicitOff bool) {
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
	if explicitOff {
		sharing, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
		if err := sharing.Apply("off"); err != nil {
			t.Fatal(err)
		}
	}
	refreshSurfaceTeamSharing(t, a)
	row := continueRows[0]
	member := teamMember{Key: a.frontTabKey(), File: a.file, Word: row.Title, SyncID: "cell:" + row.Cell}
	a.wall.loaded = true
	a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{member}}}
	if cmd := a.teamsRefreshArm(); cmd != nil || a.teamsDisk.refreshing {
		t.Fatal("sharing off armed a refresh clock")
	}
	a.teamsDisk.refreshing = true
	if cmd := a.teamsRefreshTick(); cmd != nil || a.teamsDisk.refreshing {
		t.Fatal("sharing off retained a refresh clock")
	}
	for _, gesture := range []func() tea.Cmd{
		func() tea.Cmd { return a.mentionChatPress(member.Key) },
		func() tea.Cmd { return a.trafficGo(member.Key) },
		func() tea.Cmd { return a.tabGo(chatTab{key: member.Key, file: member.File}) },
	} {
		a.leavePlace()
		gesture()
		if _, ok := a.homeAsking(); ok {
			t.Fatal("sharing off replaced a local shortcut with an ownership question")
		}
	}
	if got := a.teamsElsewhere(member); got != "" {
		t.Fatal("sharing off used directory ownership", got)
	}
	enableSurfaceTeamSharing(t, a)
	if _, remote := a.teamsRemoteFront(member.Key); !remote {
		t.Fatal("enabling did not enable cached ownership")
	}
}

// C1, C5 and C9 keep the visible default and explanation accurate in General.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestSettingsGeneralHasLocalTeamSharingChoiceAndSearch(t *testing.T) {
	a, _ := sheetApp(t)
	a.teamsPaired = func() bool { return true }
	refreshSurfaceTeamSharing(t, a)
	a.openSettings()
	cursorTo(t, a, config.KeyTeamsShare)
	item, ok := a.sheet.current()
	if !ok || item.row.Key != config.KeyTeamsShare || item.meta.tab != tabGeneral || item.meta.label != "share teams" || item.meta.widget != widgetCycle {
		t.Fatal("sharing row not on General", item)
	}
	if item.row.Value() != "on" {
		t.Fatal("sharing must start on", item.row.Value())
	}
	for _, want := range []string{"teams", "members and settings", "recent Traffic as read-only history", "paired computers", "On by default", "turning it off on either computer stops sharing"} {
		if !strings.Contains(item.meta.about, want) {
			t.Fatal("sharing explanation omitted", want, item.meta.about)
		}
	}
	if shown := strings.Join(settingsDetails(a, 90), " "); !strings.Contains(shown, "either computer") {
		t.Fatal("sharing explanation hidden on General", shown)
	}
	a.wall.loaded = true
	chooseSheetValue(t, a, "off")
	if item.row.Value() != "off" || a.teamsRefreshTick() != nil || a.teamsDisk.refreshing {
		t.Fatal("saving the General choice did not stop default sharing immediately")
	}
	chooseSheetValue(t, a, "on")
	if item.row.Value() != "on" || !a.teamsDisk.refreshing {
		t.Fatal("saving the General choice did not enable sharing immediately")
	}
	chooseSheetValue(t, a, "off")
	if item.row.Value() != "off" || a.teamsRefreshTick() != nil || a.teamsDisk.refreshing {
		t.Fatal("saving the General choice did not stop sharing immediately")
	}
	a.sheet.build()
	if current, ok := a.sheet.current(); !ok || current.row.Key != config.KeyTeamsShare {
		t.Fatal("rebuilding General moved focus away from the sharing setting")
	}
	if itemAt(searchItems(t, a, "share teams"), config.KeyTeamsShare) < 0 {
		t.Fatal("sharing setting not searchable")
	}
}

// C9 pins off explicitly so this still checks the original local Traffic reader.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestSharingOffUsesOnlyTheOriginalTrafficReadDoor(t *testing.T) {
	checkSharingDisabledTrafficDoor(t, true)
}

// F1 keeps the original Traffic reader when the preference is unset on an unpaired computer.
func TestUnpairedSharingUnsetUsesOnlyTheOriginalTrafficReadDoor(t *testing.T) {
	checkSharingDisabledTrafficDoor(t, false)
}

func checkSharingDisabledTrafficDoor(t *testing.T, explicitOff bool) {
	a, id, _, _ := trafficApp(t)
	if explicitOff {
		sharing, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
		if err := sharing.Apply("off"); err != nil {
			t.Fatal(err)
		}
	}
	refreshSurfaceTeamSharing(t, a)
	a.tp.sel = id
	seam := a.teamsSeam()
	seam.DisplayTraffic = func(string, string, string, int) ([]teamstore.Entry, string, string, bool, error) {
		t.Error("sharing off called the new history door")
		return nil, "", "", false, nil
	}
	a.teamsDisk.door = seam
	trafficReadNow(t, a)
	spend(t, a, a.teamsRead(false))
}

// F7 drops cached mirror rows on adoption and rejects a late pre-off history response.
func TestSharingStopsDropCachedAndInFlightMirrorHistory(t *testing.T) {
	for _, unpair := range []bool{false, true} {
		t.Run(fmt.Sprintf("unpair=%v", unpair), func(t *testing.T) {
			a, id, _, _ := trafficApp(t)
			enableSurfaceTeamSharing(t, a)
			local := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindNote, Text: "local"}
			mirror := teamstore.Entry{ID: "other/1", Origin: "other/1", Kind: teamstore.KindNote, Text: "foreign"}
			a.teamsTakeInteractions(id, []teamstore.Entry{local, mirror})
			if len(a.traffic.rows[id]) != 2 {
				t.Fatal("fixture did not adopt history")
			}
			if unpair {
				a.teamsPaired = func() bool { return false }
			} else {
				row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
				if err := row.Apply("off"); err != nil {
					t.Fatal(err)
				}
			}
			refreshSurfaceTeamSharing(t, a)
			if got := a.traffic.rows[id]; len(got) != 1 || got[0].ID != local.ID {
				t.Fatalf("retained cached mirror: %+v", got)
			}
			a.teamsTakeInteractions(id, []teamstore.Entry{mirror})
			if len(a.traffic.rows[id]) != 1 {
				t.Fatal("late overview response revived mirror")
			}
			a.traffic.cursor = map[string]string{id: trafficFromStart}
			a.trafficTake([]trafficGot{{trafficJob: trafficJob{id: id, after: trafficFromStart}, entries: []teamstore.Entry{mirror}, reset: true}}, nil, a.traffic.stamp, a.traffic.edits, false)
			if got := a.traffic.rows[id]; len(got) != 1 || got[0].ID != local.ID {
				t.Fatal("late history response erased local rows", got)
			}
			for _, e := range a.traffic.rows[id] {
				if e.Origin != "" {
					t.Fatal("late live response revived mirror")
				}
			}
		})
	}
}

type offlineTeamSource struct{ calls int }

func (s *offlineTeamSource) Rows(context.Context) ([]chatlist.Row, error) {
	s.calls++
	return nil, fmt.Errorf("relay offline")
}

// F4 gives local members Home's immediate local opening path and retains known-remote refusals.
func TestLocalTeamMemberOpeningNeverAddsAnOwnershipRequest(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	enableSurfaceTeamSharing(t, a)
	src := &offlineTeamSource{}
	a.machines = src
	a.machineRead.rows = nil
	a.machineRead.at = time.Time{}
	member := teamMember{Key: a.frontTabKey(), File: a.file, SyncID: "cell:local"}
	if cmd, handled := a.teamsRemoteGo(member); cmd != nil || handled {
		t.Fatal("local member waited for ownership")
	}
	if src.calls != 0 {
		t.Fatal("local member asked relay", src.calls)
	}
	a.machineRead.down = true
	a.machineRead.rows = []chatlist.Row{{Cell: "local", Status: chatlist.Running, Device: "desktop", DeviceID: "remote"}}
	if _, handled := a.teamsRemoteGo(member); !handled {
		t.Fatal("known remote holder was opened locally")
	}
	remote := teamMember{Key: teamstore.RemoteKey("elsewhere"), Word: "remote"}
	spend(t, a, a.trafficGo(remote.Key))
	// The direct remote boundary must also refuse an unavailable member.
	cmd, handled := a.teamsRemoteGo(remote)
	if !handled {
		t.Fatal("remote member was treated as local")
	}
	spend(t, a, cmd)
	if len(a.entries) == 0 || a.entries[len(a.entries)-1].text != a.chatNotice(a.machineRead.unavailableWord()) {
		t.Fatal("remote member had no relay refusal")
	}
}

// F1 keeps @ completion identical to explicit off on an unpaired computer with an unset preference.
func TestUnpairedSharingUnsetKeepsTheOriginalCompletionCatalog(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	baseline := a.mentionChatsExcept("")
	a.wall.teams = append(a.wall.teams, team{ID: "foreign", Members: []teamMember{{Key: "team-only", Word: "only in teams"}}})
	refreshSurfaceTeamSharing(t, a)
	if got := a.mentionChatsExcept(""); !reflect.DeepEqual(got, baseline) {
		t.Fatal("unpaired default added team-only completion", got)
	}
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	refreshSurfaceTeamSharing(t, a)
	if got := a.mentionChatsExcept(""); !reflect.DeepEqual(got, baseline) {
		t.Fatal("explicit off catalog changed", got)
	}
}

// F1 adopts pairing events off-loop and rejects a pre-off preference snapshot.
func TestTeamSharingAdoptsPairingChangesWithoutLoopReads(t *testing.T) {
	a, _ := sheetApp(t)
	paired := false
	calls := 0
	a.teamsPaired = func() bool { calls++; return paired }
	refreshSurfaceTeamSharing(t, a)
	a.wall.loaded = true
	if a.teamsShare || a.teamsRefreshArm() != nil {
		t.Fatal("unpaired default armed sharing")
	}
	paired = true
	run := newPairRun(a.ctx)
	defer run.cancel()
	a.pair.start(run)
	before := calls
	cmd := a.tookPair(pairMsg{run: run, event: pairEnded{ok: true}})
	if before != calls {
		t.Fatal("pairing adoption read the gate on the loop")
	}
	drive(t, a, runCmd(cmd)...)
	if !a.teamsShare {
		t.Fatal("pairing did not adopt sharing")
	}
	old := runCmd(a.refreshTeamsSharing())
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyTeamsShare)
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	refreshSurfaceTeamSharing(t, a)
	drive(t, a, old...)
	if a.teamsShare {
		t.Fatal("old snapshot re-enabled sharing")
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	refreshSurfaceTeamSharing(t, a)
	if !a.teamsShare {
		t.Fatal("fixture did not resume sharing")
	}
	a.teamsRefreshArm()
	paired = false
	drive(t, a, runCmd(a.refreshTeamsSharing())...)
	if a.teamsShare || a.teamsDisk.refreshing {
		t.Fatal("unpairing retained sharing")
	}
}

// F1 retains the original repairing read door with an unset preference and no pairing.
func TestUnpairedLocalTeamRefreshRetainsRepairingReads(t *testing.T) {
	dir := t.TempDir()
	watch := &teamstore.Watch{}
	seam := localTeams(dir, watch, nil)
	_, stamp, _ := seam.Load(nil)
	raw := []byte(`{"version":2,"teams":[{"id":"alpha","name":"harbor","members":[{"key":"local","word":"worker"}]}]}`)
	if err := os.WriteFile(teamstore.Path(dir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, _, same, err := seam.ReadSince(stamp, nil)
	if err != nil || same || len(got) != 1 || got[0].Members[0].Handle == "" {
		t.Fatal("unpaired read skipped repair", got, err)
	}
	after, _ := os.ReadFile(teamstore.Path(dir))
	if string(after) == string(raw) {
		t.Fatal("unpaired read did not persist repair")
	}
}

// R6/F1 keeps pairing reads outside Update for both a Settings save and a completed turn.
func TestSettingsSaveAndTurnCompletionReadPairingOffLoop(t *testing.T) {
	for _, turn := range []bool{false, true} {
		t.Run(fmt.Sprintf("turn=%v", turn), func(t *testing.T) {
			a, _ := sheetApp(t)
			calls, updating := 0, false
			a.teamsPaired = func() bool {
				calls++
				if updating {
					t.Error("pairing callback ran on the update loop")
				}
				return true
			}
			var cmd tea.Cmd
			updating = true
			if turn {
				_, cmd = a.Update(streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
			} else {
				a.openSettings()
				cursorTo(t, a, config.KeyTeamsShare)
				a.sheetKey(key("enter"))
				_, cmd = a.Update(key("enter"))
			}
			updating = false
			if calls != 0 {
				t.Fatal("update read pairing")
			}
			drive(t, a, runCmd(cmd)...)
			if calls == 0 {
				t.Fatal("update never scheduled pairing adoption")
			}
		})
	}
}

// R4/F1/F7 uses the engine's sharing for history, mirror adoption and the refresh clock over any remote seam.
func TestHostedTeamDisplayFollowsEngineSharing(t *testing.T) {
	for _, viewerPaired := range []bool{false, true} {
		t.Run(fmt.Sprintf("viewerPaired=%v", viewerPaired), func(t *testing.T) {
			a, id, _, _ := trafficApp(t)
			a.host = "engine"
			a.teamsPaired = func() bool { t.Error("hosted display read viewer pairing"); return viewerPaired }
			engine := t.TempDir()
			enginePaired := !viewerPaired
			if err := teamstore.Save(engine, a.wall.teams); err != nil {
				t.Fatal(err)
			}
			if err := teamstore.WriteTrafficMirror(engine, id, []teamstore.Entry{{ID: "engine/1", Kind: teamstore.KindNote, Text: "engine mirror"}}); err != nil {
				t.Fatal(err)
			}
			seam := localTeams(engine, &teamstore.Watch{}, func() bool { return enginePaired })
			seam.Sharing = func() (bool, error) { return config.TeamsSharingAt(engine, func() bool { return enginePaired }), nil }
			displayReads, history := 0, seam.DisplayTraffic
			seam.DisplayTraffic = func(team, after, version string, limit int) ([]teamstore.Entry, string, string, bool, error) {
				displayReads++
				return history(team, after, version, limit)
			}
			a.teamsDisk.door = seam
			for _, sharing := range []bool{!viewerPaired, viewerPaired, !viewerPaired} {
				enginePaired = sharing
				before := displayReads
				spend(t, a, a.trafficRead())
				if (displayReads > before) != sharing {
					t.Fatalf("engine sharing=%v history requests=%d", sharing, displayReads-before)
				}
				if a.teamsShare != sharing || a.teamsDisk.refreshing != sharing {
					t.Fatalf("window did not follow engine sharing=%v: sharing=%v refreshing=%v", sharing, a.teamsShare, a.teamsDisk.refreshing)
				}
				found := false
				for _, entry := range a.traffic.rows[id] {
					found = found || entry.Text == "engine mirror"
				}
				if found != sharing {
					t.Fatalf("engine sharing=%v mirror=%v", sharing, found)
				}
			}
		})
	}
}
