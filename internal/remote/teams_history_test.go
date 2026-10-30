package remote

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A hosted window uses its engine's mirror and retains the engine's local
// cursor. The ordinary Traffic method remains the local-only compatibility door.
func TestHostedTrafficHistoryKeepsMirrorSeparateAndCursorLocal(t *testing.T) {
	loop, dir := teamsLoop(t)
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

func TestHostedTeamRefreshDoesNotPersistRepairs(t *testing.T) {
	loop, dir := teamsLoop(t)
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

func TestHostedWindowsKeepIndependentMirrorVersions(t *testing.T) {
	first, dir := teamsLoop(t)
	second, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: &fakeAgent{model: "m"}, ProfileDir: dir}, nil }})
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
