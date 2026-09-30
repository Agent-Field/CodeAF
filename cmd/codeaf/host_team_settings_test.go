package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// THE SETTINGS WRITE CROSSES WHEN THE ENGINE SAYS SO. An edit through the
// seam lands in the engine's profile. An engine that does not say
// TeamSettings keeps the door nil, which is the Teams tab staying read-only.
func TestHostTeamsCarriesASettingsWriteOnlyWhenTheEngineSaysSo(t *testing.T) {
	far := t.TempDir()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: &quietAgent{}, ProfileDir: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	welcome := loop.Client.Welcome()
	seam := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if seam.ApplyDefault == nil || seam.Defaults == nil {
		t.Fatal("this engine's seam cannot read or change team defaults")
	}
	d, err := seam.ApplyDefault("teams.cap_usd_day", "6")
	if err != nil || d.CapUSDDay != 6 {
		t.Fatalf("apply: %+v, %v", d, err)
	}
	if teamstore.DefaultsAt(far).CapUSDDay != 6 {
		t.Fatal("the engine profile did not take the cap")
	}
	welcome.TeamSettings = false
	older := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if older.ApplyDefault != nil {
		t.Fatal("an older engine was handed the settings write")
	}
	if older.Defaults == nil {
		t.Fatal("an engine with the delegation doors lost the read")
	}
}
