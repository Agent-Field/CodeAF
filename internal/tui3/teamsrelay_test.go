package tui3

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamMemberUsesExistingContinueHereQuestion(t *testing.T) {
	celltest.Available(t)
	a := continueHome(t, &fakeTaker{}, continueRows, 140)
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
	a.devRow.devices = []chatlist.Device{{ID: "a", Name: "My desktop"}}
	cmd, remote := a.teamsRemoteGo(teamMember{Key: teamstore.OtherComputerKey("device:a"), Word: "local chat"})
	if !remote || cmd != nil || !strings.Contains(a.tp.msg, "My desktop and is not shared. Open it there.") {
		t.Fatalf("unsynced member: %q", a.tp.msg)
	}
}

func TestAppliedTeamsRefreshDropdownAndAtListFromFileStamp(t *testing.T) {
	a := teamsPlaceLab(t)
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

func TestTeamMemberFetchesItsContinueRowWhenHomeHasNotReadItYet(t *testing.T) {
	celltest.Available(t)
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%v", local), func(t *testing.T) {
			a := continueHome(t, &fakeTaker{}, continueRows, 140)
			row := continueRows[0]
			a.machineRead.rows, a.machineRead.at = nil, time.Time{}
			member := teamMember{Key: teamstore.RemoteKey(row.Cell), Word: row.Title, SyncID: "cell:" + row.Cell}
			if local {
				member.Key, member.File = "/old/local/transcript.jsonl", "/old/local/transcript.jsonl"
			}
			a.wall.loaded = true
			a.wall.teams = []team{{ID: "alpha", Name: "alpha", Members: []teamMember{member}}}
			spend(t, a, a.trafficGo(member.Key))
			ask, ok := a.homeAsking()
			if !ok || ask.question.Head != continueAsk {
				t.Fatal("uncached team member did not fetch its continuation row")
			}
		})
	}
}

// Render the real frame after entering through Teams or a chat tab. Inspecting
// home.ask alone would miss a question raised while Home was still hidden.
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

func TestUnpairedRefreshClockReadsChangedFileWithoutRepairingOrWritingAnything(t *testing.T) {
	a := teamsPlaceLab(t)
	teamsFlush(t, a)
	raw := []byte(`{"version":2,"teams":[{"id":"zz","name":"first","parent":"","members":[],"manager":"","made":"2026-01-01T00:00:00Z"},{"id":"aa","name":"second","parent":"","members":[],"manager":"","made":"2026-01-02T00:00:00Z"}]}`)
	if err := os.WriteFile(teamstore.Path(a.profileDir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	spend(t, a, a.teamsRefreshTick())
	after, _ := os.ReadFile(teamstore.Path(a.profileDir))
	if string(after) != string(raw) {
		t.Fatal("refresh repaired the unpaired file")
	}
	if len(a.teamsDisk.queue) != 0 {
		t.Fatal("refresh queued a repair")
	}
	if len(a.wall.teams) != 2 || a.wall.teams[0].ID != "zz" {
		t.Fatal("unpaired refresh changed file order")
	}
}

func TestMirrorTailRefreshKeepsLocalCursorAndShowsHistoryInBothSurfaces(t *testing.T) {
	a, id, _, _ := trafficApp(t)
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
