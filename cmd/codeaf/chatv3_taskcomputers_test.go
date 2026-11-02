package main

import (
	"context"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// The absence law is applied at the actual conversation configuration door.
func TestConversationTaskComputersAreAbsentInUnpairedSoloAndSyncOffHomes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	for _, kind := range []string{"unpaired", "solo", "off"} {
		t.Run(kind, func(t *testing.T) {
			profile := t.TempDir()
			if kind == "solo" {
				if _, err := identity.EnsureSolo(profile); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "off" {
				if _, err := identity.Ensure(profile); err != nil {
					t.Fatal(err)
				}
				t.Setenv("CODEAF_SYNC_URL", "off")
			}
			if got := v3TaskComputers(profile); got != nil {
				t.Fatalf("%s offered task computers", kind)
			}
		})
	}
}

// Contract 6 selects from the signed pairing roster, including unsupported
// Windows rows and excluding this computer and revoked device certificates.
func TestConversationTaskComputersUsePairingNamesAndRevocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	profile := t.TempDir()
	id, err := identity.Ensure(profile)
	if err != nil {
		t.Fatal(err)
	}
	self, err := identity.LoadDevice(profile)
	if err != nil {
		t.Fatal(err)
	}
	state := directory.NewMemory(time.Now)
	client := state.For(self.ID())
	key := directory.MetadataKey(id.CellKey())
	var revoked string
	for _, item := range []struct {
		name, platform string
		revoke         bool
	}{{"chat computer", "linux", false}, {"work computer", "darwin", false}, {"Windows computer", "windows", false}, {"removed computer", "linux", true}} {
		dev := self
		if item.name != "chat computer" {
			dev, err = identity.NewDevice(id)
			if err != nil {
				t.Fatal(err)
			}
		}
		name, err := directory.SealName(key, item.name)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1, Name: name, Platform: item.platform}); err != nil {
			t.Fatal(err)
		}
		if item.revoke {
			revoked = dev.ID()
			if err := client.Revoke(context.Background(), dev.ID()); err != nil {
				t.Fatal(err)
			}
		}
	}
	server := httptest.NewServer(directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return state, nil }))
	defer server.Close()
	t.Setenv("CODEAF_SYNC_URL", server.URL)
	computers := v3TaskComputers(profile)
	if runtime.GOOS == "windows" {
		if computers != nil {
			t.Fatal("Windows origin offered remote tasks")
		}
		return
	}
	if computers == nil {
		t.Fatal("paired home has no task computers")
	}
	rows, err := computers.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("paired computers = %+v", rows)
	}
	for _, row := range rows {
		if row.Device == self.ID() || row.Device == revoked {
			t.Fatalf("non-target listed: %+v", row)
		}
		if row.Name == "Windows computer" && row.CanTakeTasks {
			t.Fatal("Windows target can take tasks")
		}
	}
}
