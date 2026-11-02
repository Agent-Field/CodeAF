package pair

import (
	"bytes"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
	"os"
	"path/filepath"
	"testing"
)

func TestTeamsReplaceMovesLocalTeamsAsideAndKeepsTraffic(t *testing.T) {
	home := t.TempDir()
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: home}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(home, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "alpha", Name: "local"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	before := teamstest.JSON(t, home)
	if err := teams.AppendTraffic(home, "alpha", teams.Entry{Kind: teams.KindNote, From: teams.FromYou, To: teams.ToRoom, Text: "keep history"}); err != nil {
		t.Fatal(err)
	}
	traffic, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	if err := os.WriteFile(filepath.Join(home, "teams-sync.ledger"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "teams-traffic-sync.ledger"), []byte(`"2026-01-01T00:00:00Z"`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceState(home, home); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(teams.Path(home) + ".unreadable-*[0-9]")
	if len(aside) != 1 {
		t.Fatal("local teams were not moved aside")
	}
	// app/v3 keeps teams in an SQLite store (#2066): what was set aside is
	// read back as a store, with its WAL beside it, in a profile of its own.
	kept := t.TempDir()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(aside[0]+suffix, teams.Path(kept)+suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(teams.TrafficPath(home, "alpha"))
	if !bytes.Equal(before, teamstest.JSON(t, kept)) || !bytes.Equal(traffic, after) {
		t.Fatal("replace discarded local history")
	}
	if _, err := os.Stat(teams.Path(home)); !os.IsNotExist(err) {
		t.Fatal("replace kept the store in place")
	}
	if _, err := os.Stat(filepath.Join(home, "teams-traffic-sync.ledger")); !os.IsNotExist(err) {
		t.Fatal("replace retained Traffic rate clock")
	}
	if _, err := os.Stat(filepath.Join(home, "teams-sync.ledger")); !os.IsNotExist(err) {
		t.Fatal("ledger survived replace")
	}
}

func TestTeamsReplacementWithSharingOffPreservesTeamsAndLedgers(t *testing.T) {
	home := t.TempDir()
	if err := teams.Update(home, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "alpha", Name: "local"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	paths := []string{teams.Path(home), filepath.Join(home, "teams-sync.ledger"), filepath.Join(home, "teams-traffic-sync.ledger")}
	for _, path := range paths[1:] {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(raw)
	}
	if err := replaceState(home, home); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != before[path] {
			t.Fatalf("replacement changed %s: %v", path, err)
		}
	}
}
