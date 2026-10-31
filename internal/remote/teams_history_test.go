package remote

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A hosted window uses its engine's mirror and retains the engine's local
// cursor. The ordinary Traffic method remains the local-only compatibility door.
// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestHostedTrafficHistoryKeepsMirrorSeparateAndCursorLocal(t *testing.T) {
	loop, dir := pairedTeamsLoop(t)
	if err := teams.AppendTraffic(dir, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "local"}); err != nil {
		t.Fatal(err)
	}
	first, err := loop.Client.TeamsHistory("alpha", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.WriteTrafficMirror(dir, "alpha", []teams.Entry{{ID: "desktop/1", At: time.Now(), Kind: teams.KindDirective, Text: "history"}}); err != nil {
		t.Fatal(err)
	}
	got, err := loop.Client.TeamsHistory("alpha", first.Cursor, first.Mirror, 100)
	if err != nil || !got.Reset || got.Cursor != first.Cursor || len(got.Entries) != 2 {
		t.Fatal(got, err)
	}
	local, err := loop.Client.TeamsTraffic("alpha", "", 100)
	if err != nil || len(local.Entries) != 1 {
		t.Fatal("compatibility door read mirror", local, err)
	}
}

// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestHostedTeamRefreshDoesNotPersistRepairs(t *testing.T) {
	loop, dir := pairedTeamsLoop(t)
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	first, err := loop.Client.TeamsRead("", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":2,"teams":[{"id":"alpha","name":"alpha","parent":"","members":[],"manager":"","made":"2026-01-01T00:00:00Z"}]}`)
	if err := os.WriteFile(teams.Path(dir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Client.TeamsRead(first.Stamp, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(teams.Path(dir))
	if string(after) != string(raw) {
		t.Fatal("background hosted refresh repaired file")
	}
}

// F1 preserves the existing team surface contract with an explicit pairing gate.
func TestHostedWindowsKeepIndependentMirrorVersions(t *testing.T) {
	first, dir := pairedTeamsLoop(t)
	second, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, ProfileDir: dir, TeamsPaired: func() bool { return true }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := teams.AppendTraffic(dir, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "local"}); err != nil {
		t.Fatal(err)
	}
	one, err := first.Client.TeamsHistory("alpha", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Client.TeamsHistory("alpha", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.WriteTrafficMirror(dir, "alpha", []teams.Entry{{ID: "desktop/1", At: time.Now(), Kind: teams.KindNote, Text: "new mirror"}}); err != nil {
		t.Fatal(err)
	}
	for i, client := range []*Client{first.Client, second.Client} {
		prev := []TeamsTraffic{one, two}[i]
		got, err := client.TeamsHistory("alpha", prev.Cursor, prev.Mirror, 100)
		if err != nil || !got.Reset || len(got.Entries) != 2 {
			t.Fatal("hosted consumer missed refresh", got, err)
		}
	}
}

func pairedTeamsLoop(t *testing.T) (*Loop, string) {
	t.Helper()
	dir := t.TempDir()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, ProfileDir: dir, TeamsPaired: func() bool { return true }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop, dir
}

// F1 keeps repairing reads for an unset preference on an unpaired engine.
func TestUnpairedHostedTeamRefreshRetainsRepairingReads(t *testing.T) {
	loop, dir := teamsLoop(t)
	first, err := loop.Client.TeamsRead("", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":2,"teams":[{"id":"alpha","name":"alpha","members":[{"key":"local","word":"worker"}]}]}`)
	if err := os.WriteFile(teams.Path(dir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loop.Client.TeamsRead(first.Stamp, nil)
	if err != nil || len(got.Teams) != 1 || got.Teams[0].Members[0].Handle == "" {
		t.Fatal("unpaired engine skipped repair", got, err)
	}
	after, _ := os.ReadFile(teams.Path(dir))
	if string(after) == string(raw) {
		t.Fatal("unpaired engine did not persist repair")
	}
}

// F7 hides retained history over the engine boundary when off or unpaired, then restores it on re-enable.
func TestHostedTeamHistoryStopsWithEffectiveSharing(t *testing.T) {
	dir := t.TempDir()
	paired := true
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, ProfileDir: dir, TeamsPaired: func() bool { return paired }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := teams.AppendTraffic(dir, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := teams.WriteTrafficMirror(dir, "alpha", []teams.Entry{{ID: "other/1", Kind: teams.KindNote, Text: "foreign"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(teams.TrafficMirrorPath(dir, "alpha"))
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyTeamsShare)
	cursor, mirror := "", ""
	for _, tc := range []struct {
		paired bool
		choice string
		want   int
	}{{true, "on", 2}, {true, "off", 1}, {false, "on", 1}, {true, "on", 2}} {
		paired = tc.paired
		if err := row.Apply(tc.choice); err != nil {
			t.Fatal(err)
		}
		// R4 carries the engine's live sharing even when its team file is unchanged.
		first, err := loop.Client.TeamsRead("", nil)
		if err != nil {
			t.Fatal(err)
		}
		reading, err := loop.Client.TeamsRead(first.Stamp, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(reading)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Sharing *bool `json:"sharing"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		if state.Sharing == nil || *state.Sharing != (tc.want == 2) {
			t.Fatalf("engine omitted effective sharing: %s", raw)
		}
		got, err := loop.Client.TeamsHistory("alpha", cursor, mirror, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Reset {
			got, err = loop.Client.TeamsHistory("alpha", "", "", 100)
		}
		if err != nil || len(got.Entries) != tc.want {
			t.Fatalf("paired=%v choice=%s history=%+v err=%v", tc.paired, tc.choice, got, err)
		}
		cursor, mirror = got.Cursor, got.Mirror
	}
	after, _ := os.ReadFile(teams.TrafficMirrorPath(dir, "alpha"))
	if string(before) != string(after) {
		t.Fatal("off deleted retained mirror")
	}
}
