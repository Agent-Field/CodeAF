package main

// THE WINDOW POLLS THE FLOOR IT DRAWS, HOST OR NOT.
//
// On 2026-10-08 a window with the factory attached to its workspace's session
// host, a daemon started two days earlier from an older binary with no factory
// poll. The window took the host road, which never started a poll of its own,
// so the three repositories the owner watched were never read. These tests hold
// the host road to the same start the in-process launch makes, and the window
// to saying once when the engine it is attached to is another build.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func TestTheHostRoadStartsTheFloorsPollAndTriage(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	type call struct {
		root, workspace, profile string
	}
	var calls []call
	stock := factoryFloorHere
	t.Cleanup(func() { factoryFloorHere = stock })
	factoryFloorHere = func(st *store.Store, workspace, profileDir string) {
		if st == nil {
			t.Error("the host road started the floor's work with no store")
			return
		}
		calls = append(calls, call{root: st.Root(), workspace: workspace, profile: profileDir})
	}

	profile := t.TempDir()
	workspace := t.TempDir()
	var options tui3.Options
	localDoors(&options, remote.Welcome{Workspace: workspace, ProfileDir: profile}, config.Config{ProfileDir: profile})

	if options.Factory.Load == nil {
		t.Fatal("the host road drew no floor")
	}
	if len(calls) != 1 {
		t.Fatalf("the host road started the floor's poll and triage %d times, want once", len(calls))
	}
	got := calls[0]
	if filepath.Clean(got.root) != filepath.Clean(v3FactoryRoot()) {
		t.Fatalf("poll started over %q, the floor drawn is %q", got.root, v3FactoryRoot())
	}
	if got.workspace != workspace || got.profile != profile {
		t.Fatalf("poll started for workspace %q profile %q, want %q %q", got.workspace, got.profile, workspace, profile)
	}
}

func TestTheWindowSaysOnceWhenTheEngineIsAnotherBuild(t *testing.T) {
	const ws = "/srv/app"
	cases := []struct {
		name    string
		welcome remote.Welcome
		mine    string
		rev     string
		says    bool
	}{
		{"same identity", remote.Welcome{Identity: "abc12345", Build: "abc12345 built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", false},
		{"another identity", remote.Welcome{Identity: "0ld0ld00", Build: "0ld0ld00 built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", true},
		{"same source built later", remote.Welcome{Identity: "abc12345", Build: "abc12345 built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", false},
		{"older engine, another revision", remote.Welcome{Build: "0ld0ld00 built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", true},
		{"older engine, same revision", remote.Welcome{Build: "abc12345 built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", false},
		{"older engine, dirty", remote.Welcome{Build: "0ld0ld00 (dirty) built 2026-10-06 09:00", Workspace: ws}, "abc12345", "abc12345", false},
		{"older engine, names nothing", remote.Welcome{Workspace: ws}, "abc12345", "abc12345", false},
		{"older engine, dev", remote.Welcome{Build: "dev", Workspace: ws}, "abc12345", "abc12345", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			note := hostBuildNote(c.welcome, c.mine, c.rev)
			if (note != "") != c.says {
				t.Fatalf("note = %q, want said=%v", note, c.says)
			}
			if !c.says {
				return
			}
			for _, want := range []string{
				"this workspace's engine is another build (" + c.welcome.Build + ")",
				"it keeps running your chats; restart it to match",
				"codeaf engine --stop --workspace " + ws,
			} {
				if !strings.Contains(note, want) {
					t.Fatalf("note %q does not say %q", note, want)
				}
			}
		})
	}
}
