package session

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// These tests advance the production watcher explicitly so scheduler timing
// cannot consume a cursor before its carried-file fixture is installed.
func manualRelayTeamClock(c *Config) { c.teamWatchManual = true }

// Exercise the boundary, idle watcher and running-turn stop watcher, all with
// the same mirror. None of these readers may ever consume display history.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestTrafficMirrorNeverDeliversStartsQuestionsWakesOrStops(t *testing.T) {
	f := newWakingTeamFixture(t)
	enableSessionTeamSharing(t, f.profile)
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
	visible, err := teams.ReadHistory(f.profile, f.teamID, 0, true)
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
	enableSessionTeamSharing(t, f.profile)
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
	enableSessionTeamSharing(t, f.profile)
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

func enableSessionTeamSharing(t *testing.T, profile string) {
	t.Helper()
	if _, err := identity.Load(home.Dir()); err != nil {
		root := t.TempDir()
		t.Setenv("CODEAF_HOME", root)
		if _, err := identity.Ensure(root); err != nil {
			t.Fatal(err)
		}
	}
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
}

// C9 pins off explicitly so legacy cursors and ownership checks keep their sharing-off contract.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestSharingOffKeepsLegacyCursorBytesAndSkipsEveryOwnershipLookup(t *testing.T) {
	checkSharingDisabledLegacyCursors(t, true)
}

// F1 leaves legacy cursor bytes and delivery unchanged on an unpaired computer with an unset preference.
func TestUnpairedSharingUnsetKeepsLegacyCursorBytesAndSkipsEveryOwnershipLookup(t *testing.T) {
	checkSharingDisabledLegacyCursors(t, false)
}

func checkSharingDisabledLegacyCursors(t *testing.T, explicitOff bool) {
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	ensure := identity.EnsureSolo
	if explicitOff {
		ensure = identity.Ensure
	}
	if _, err := ensure(root); err != nil {
		t.Fatal(err)
	}
	f := newWakingTeamFixture(t)
	if explicitOff {
		sharing, _ := config.NewSettings(config.SettingsOptions{ProfileDir: f.profile}).Row(config.KeyTeamsShare)
		if err := sharing.Apply("off"); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := identity.EnsureSolo(root); err != nil {
			t.Fatal(err)
		}
	}
	a := teamAgent(t, f, f.web, nil, manualRelayTeamClock)
	a.config.TeamComputers = func(context.Context, []teams.Member) (map[string]string, error) {
		t.Fatal("sharing off called ownership lookup")
		return nil, nil
	}
	targets := []teams.Member{{Key: teams.RemoteKey("remote")}, {Key: teams.OtherComputerKey("device:a:chat")}, {Key: "local"}}
	local, unreached := a.teamDeliveryTargets(t.Context(), targets)
	if len(local) != len(targets) || len(unreached) != 0 {
		t.Fatal("sharing off refused legacy delivery")
	}
	for _, device := range []string{"", "foreign-device"} {
		raw, _ := json.Marshal(map[string]any{"read": map[string]string{f.teamID: "999999999999"}, "device": device})
		if err := writeTeamFile(a.teamCursorFile(), raw); err != nil {
			t.Fatal(err)
		}
		a.team.mu.Lock()
		a.team.cursorsRead = false
		a.readTeamCursorsLocked()
		got := a.team.cursors[f.teamID]
		a.saveTeamCursorsLocked()
		a.team.mu.Unlock()
		if got != "999999999999" {
			t.Fatal("sharing off discarded legacy cursor", got)
		}
		saved, err := os.ReadFile(a.teamCursorFile())
		want := `{"read":{"` + f.teamID + `":"999999999999"}}`
		if err != nil || string(saved) != want {
			t.Fatalf("sharing off changed cursor shape: %s %v", saved, err)
		}
	}
}

// F7 removes retained mirror history from status, prompt digests and team mentions while off or unpaired.
func TestSessionTeamHistoryStopsWithEffectiveSharing(t *testing.T) {
	f := newWakingTeamFixture(t)
	enableSessionTeamSharing(t, f.profile)
	a := teamAgent(t, f, f.manager, nil, manualRelayTeamClock)
	if err := teams.WriteTrafficMirror(f.profile, f.teamID, []teams.Entry{{ID: "other/1", At: time.Now(), Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "foreign history"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(teams.TrafficMirrorPath(f.profile, f.teamID))
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: f.profile}).Row(config.KeyTeamsShare)
	for _, tc := range []struct {
		paired bool
		choice string
		want   bool
	}{{true, "on", true}, {true, "off", false}, {false, "on", false}, {true, "on", true}} {
		a.config.TeamsPaired = func() bool { return tc.paired }
		if err := row.Apply(tc.choice); err != nil {
			t.Fatal(err)
		}
		status, failed, err := a.teamStatusTool(t.Context(), json.RawMessage(`{}`))
		if err != nil || failed {
			t.Fatal(status, failed, err)
		}
		for _, got := range []string{status, a.teamDigest(f.profile), mentionTeamBlock(f.profile, f.manager, "harbor", time.Now(), a.teamsSharing())} {
			if strings.Contains(got, "foreign history") != tc.want {
				t.Fatalf("paired=%v choice=%s history=%s", tc.paired, tc.choice, got)
			}
		}
	}
	after, _ := os.ReadFile(teams.TrafficMirrorPath(f.profile, f.teamID))
	if string(before) != string(after) {
		t.Fatal("session readers changed retained mirror")
	}
}

// R7/F7 removes cached mirror digests from the outgoing request after off or unpairing.
func TestOutgoingTeamDigestStopsWithEffectiveSharing(t *testing.T) {
	for _, unpair := range []bool{false, true} {
		t.Run(fmt.Sprintf("unpair=%v", unpair), func(t *testing.T) {
			f := newWakingTeamFixture(t)
			enableSessionTeamSharing(t, f.profile)
			var paired atomic.Bool
			paired.Store(true)
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
			}}
			a := teamAgent(t, f, f.manager, completer, func(c *Config) { manualRelayTeamClock(c); c.TeamsPaired = func() bool { return paired.Load() } })
			if err := teams.WriteTrafficMirror(f.profile, f.teamID, []teams.Entry{{ID: "other/1", At: time.Now(), Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "foreign digest marker"}}); err != nil {
				t.Fatal(err)
			}
			a.refreshTeamDigest(t.Context())
			lane, err := a.Submit(t.Context(), "how is the team?")
			if err != nil {
				t.Fatal(err)
			}
			collect(t, lane)
			if !strings.Contains(userTextIn(completer.request(0)), "foreign digest marker") {
				t.Fatal("enabled request lacked mirror digest")
			}
			if unpair {
				paired.Store(false)
			} else {
				row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: f.profile}).Row(config.KeyTeamsShare)
				if err := row.Apply("off"); err != nil {
					t.Fatal(err)
				}
			}
			lane, err = a.Submit(t.Context(), "and now?")
			if err != nil {
				t.Fatal(err)
			}
			collect(t, lane)
			if got := userTextIn(completer.request(1)); strings.Contains(got, "foreign digest marker") {
				t.Fatalf("outgoing request retained mirror digest: %s", got)
			}
		})
	}
}

// R7/F7 rejects a background digest after off and on were adopted while its result was in flight.
func TestTeamDigestDropsBackgroundResultAcrossSharingChanges(t *testing.T) {
	f := newWakingTeamFixture(t)
	enableSessionTeamSharing(t, f.profile)
	var paired atomic.Bool
	paired.Store(true)
	var calls atomic.Int32
	computed, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a := teamAgent(t, f, f.manager, nil, func(c *Config) {
		manualRelayTeamClock(c)
		c.TeamsPaired = func() bool {
			state := paired.Load()
			if calls.Add(1) == 2 {
				close(computed)
				<-release
			}
			return state
		}
	})
	if err := teams.WriteTrafficMirror(f.profile, f.teamID, []teams.Entry{{ID: "other/1", Kind: teams.KindNote, Text: "stale mirror digest"}}); err != nil {
		t.Fatal(err)
	}
	go func() { defer close(done); a.refreshTeamDigest(t.Context()) }()
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(release) }); <-done }()
	select {
	case <-computed:
	case <-time.After(5 * time.Second):
		t.Fatal("digest did not finish computing")
	}
	for _, state := range []bool{false, true} {
		paired.Store(state)
		a.drainSteering(nil)
	}
	a.mu.Lock()
	if a.teamDigestText != "" {
		t.Error("changed sharing kept cached digest")
	}
	a.mu.Unlock()
	// The held callback returns its original on reading, so only the adopted
	// generation can reject this result after the off/on transition.
	releaseOnce.Do(func() { close(release) })
	<-done
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.teamDigestText != "" {
		t.Fatal("late background result revived stale digest")
	}
}
