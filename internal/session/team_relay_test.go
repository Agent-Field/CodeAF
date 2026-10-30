package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// These tests advance the production watcher explicitly so scheduler timing
// cannot consume a cursor before its carried-file fixture is installed.
func manualRelayTeamClock(c *Config) { c.teamWatchManual = true }

// Exercise the boundary, idle watcher and running-turn stop watcher, all with
// the same mirror. None of these readers may ever consume display history.
func TestTrafficMirrorNeverDeliversStartsQuestionsWakesOrStops(t *testing.T) {
	f := newWakingTeamFixture(t)
	member := teamAgent(t, f, f.web, nil, manualRelayTeamClock)
	manager := teamAgent(t, f, f.manager, nil, manualRelayTeamClock)
	member.teamBoundary()
	manager.teamBoundary()
	at := time.Now()
	var lines []teams.Entry
	for _, kind := range []string{teams.KindDirective, teams.KindStart, teams.KindQuestion, teams.KindStop} {
		entry := teams.Entry{ID: "other/" + kind, At: at, Kind: kind, From: teams.FromManager, To: "web", Member: convKeyOf(t, f.web), Text: "history only"}
		if kind == teams.KindQuestion {
			entry.From, entry.To = "web", teams.ToManager
		}
		lines = append(lines, entry)
	}
	if err := teams.WriteTrafficMirror(f.profile, f.teamID, lines); err != nil {
		t.Fatal(err)
	}
	visible, err := teams.ReadHistory(f.profile, f.teamID, 0)
	if err != nil || len(visible) != 4 {
		t.Fatal(visible, err)
	}
	if member.teamBoundary() != "" || manager.teamBoundary() != "" {
		t.Fatal("delivered mirror")
	}
	memberWakes, stopMember := member.WatchWakes()
	defer stopMember()
	managerWakes, stopManager := manager.WatchWakes()
	defer stopManager()
	member.teamWatchTick(f.profile, at)
	manager.teamWatchTick(f.profile, at)
	manager.teamWatchTick(f.profile, at.Add(teamWakeDelay()))
	for _, wakes := range []<-chan (<-chan Event){memberWakes, managerWakes} {
		select {
		case <-wakes:
			t.Fatal("mirror woke a chat")
		default:
		}
	}
	if len(member.teamStarts(f.profile)) != 0 {
		t.Fatal("mirror started a member")
	}
	if member.running || manager.running {
		t.Fatal("mirror started a turn")
	}
	stopped := false
	member.running = true
	member.teamTurnSerial = 9
	member.cancel = func(error) { stopped = true }
	member.teamStopTick(f.profile, at.Add(-time.Second), 9)
	if stopped || !member.running {
		t.Fatal("mirror stopped the running member")
	}
	member.running = false
	member.cancel = nil
	if local, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0); len(local) != 0 {
		t.Fatal("watch wrote mirror into log")
	}
}

func TestForeignDeliveryCursorStartsAtThisComputersOpeningAndDeliversOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	if _, err := identity.Ensure(root); err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(root)
	if err != nil {
		t.Fatal(err)
	}
	f := newWakingTeamFixture(t)
	// A line from long before the chat arrived is history; the directive sent
	// a second before the engine opened is the one a rouse opened it for, which
	// a conversation meeting a team for the first time is always handed.
	appendTraffic(t, f, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "long before arrival", At: time.Now().Add(-time.Hour)})
	appendTraffic(t, f, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "the rousing directive", At: time.Now().Add(-time.Second)})
	if err := os.MkdirAll(filepath.Join(filepath.Dir(f.web), ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	a := teamAgent(t, f, f.web, nil, manualRelayTeamClock)
	raw, _ := json.Marshal(map[string]any{"device": "device-a", "read": map[string]string{f.teamID: "999999999999"}})
	if err := writeTeamFile(a.teamCursorFile(), raw); err != nil {
		t.Fatal(err)
	}
	if got := a.teamBoundary(); !strings.Contains(got, "the rousing directive") || strings.Contains(got, "long before arrival") {
		t.Fatalf("a continued chat must start as a chat meeting the team: %q", got)
	}
	if got := a.teamBoundary(); got != "" {
		t.Fatalf("rousing directive delivered twice: %q", got)
	}
	appendTraffic(t, f, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "next local directive"})
	if got := a.teamBoundary(); !strings.Contains(got, "next local directive") {
		t.Fatalf("lost next line: %q", got)
	}
	if got := a.teamBoundary(); got != "" {
		t.Fatalf("duplicate: %q", got)
	}
	saved, _ := os.ReadFile(a.teamCursorFile())
	if !strings.Contains(string(saved), dev.ID()) {
		t.Fatal("no cursor device")
	}
	solo := t.TempDir()
	t.Setenv("CODEAF_HOME", solo)
	if _, err := identity.EnsureSolo(solo); err != nil {
		t.Fatal(err)
	}
	a.saveTeamCursorsLocked()
	saved, _ = os.ReadFile(a.teamCursorFile())
	expected := `{"read":{"` + f.teamID + `":"000000000003"}}`
	if string(saved) != expected {
		t.Fatalf("unpaired cursor encoding changed: %s", saved)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	legacy := teamAgent(t, f, f.web, nil, manualRelayTeamClock)
	if got := legacy.teamBoundary(); got != "" {
		t.Fatalf("a legacy cursor without a device was discarded: %q", got)
	}

}

func TestRemoteTeamSendsStartsPostsAndRousesRefuseBeforeTraffic(t *testing.T) {
	f := newWakingTeamFixture(t)
	a := teamAgent(t, f, f.manager, nil, manualRelayTeamClock)
	a.config.TeamComputers = func(_ context.Context, members []teams.Member) (map[string]string, error) {
		names := map[string]string{}
		for _, m := range members {
			if m.Handle == "web" {
				names[m.Key] = "desktop"
			}
		}
		return names, nil
	}
	checks := []struct {
		run  func(context.Context, json.RawMessage) (string, bool, error)
		args string
	}{
		{a.teamSendTool, `{"to":"web","text":"work","kind":"directive"}`},
		{a.teamStartTool, `{"handle":"web","brief":"work"}`},
		{a.teamStopTool, `{"handle":"web"}`},
	}
	for _, c := range checks {
		said, failed, err := c.run(t.Context(), json.RawMessage(c.args))
		if err != nil || !failed || !strings.Contains(said, "desktop") || !strings.Contains(said, "message would not reach") {
			t.Fatalf("%q %v %v", said, failed, err)
		}
	}
	file, _ := teams.Snapshot(f.profile)
	m, _ := file.Teams[0].ByHandle("web")
	if said := a.teamRouse(f.profile, file.Teams[0], true, []teams.Member{m}, ""); !strings.Contains(said, "desktop") {
		t.Fatal(said)
	}
	member := teamAgent(t, f, f.web, nil, manualRelayTeamClock)
	member.config.TeamComputers = func(_ context.Context, members []teams.Member) (map[string]string, error) {
		names := map[string]string{}
		for _, m := range members {
			names[m.Key] = "laptop"
		}
		return names, nil
	}
	said, failed, err := member.teamPostTool(t.Context(), json.RawMessage(`{"to":"manager","text":"reply"}`))
	if err != nil || !failed || !strings.Contains(said, "laptop") {
		t.Fatal(said, failed, err)
	}
	said, failed, err = member.teamRaiseTool(t.Context(), json.RawMessage(`{"question":"which format?","parties":["@parser"],"options":[{"label":"one","consequence":"use one"},{"label":"two","consequence":"use two"}]}`))
	if err != nil || !failed || !strings.Contains(said, "laptop") {
		t.Fatal("remote decider received a conflict receipt", said, failed, err)
	}
	local, _ := teams.ReadTraffic(f.profile, f.teamID, "", 0)
	if len(local) != 0 {
		t.Fatal("refusal wrote Traffic", local)
	}
}
