package pair

import (
	"bytes"
	"errors"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F3 preserves the described replacement behavior and recovery receipts.
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
	if _, err := replaceState(home, home, func() bool { return true }); err != nil {
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

// C9 pins off explicitly so replacement still preserves local teams and sharing ledgers.
// F3 preserves the described replacement behavior and recovery receipts.
func TestTeamsReplacementWithSharingOffPreservesTeamsAndLedgers(t *testing.T) {
	home := t.TempDir()
	sharing, _ := config.NewSettings(config.SettingsOptions{ProfileDir: home}).Row(config.KeyTeamsShare)
	if err := sharing.Apply("off"); err != nil {
		t.Fatal(err)
	}
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
	if _, err := replaceState(home, home, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != before[path] {
			t.Fatalf("replacement changed %s: %v", path, err)
		}
	}
}

// F3 returns the preserved file through real adoption and names it in the receipt.
func TestPairReplacementReceiptNamesPreservedTeamsFile(t *testing.T) {
	home := t.TempDir()
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: home}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if err := teams.Update(home, func(f *teams.File) error { f.Teams = []teams.Team{{ID: "local", Name: "harbor"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	before := teamstest.JSON(t, home)
	other, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	joined, err := adopt(Joining{Home: home, Replace: true, TeamsPaired: func() bool { return true }}, Grant{Identity: other})
	if err != nil {
		t.Fatal(err)
	}
	keptProfile := t.TempDir()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(joined.TeamsAside+suffix, teams.Path(keptProfile)+suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	kept := teamstest.JSON(t, keptProfile)
	if !bytes.Equal(before, kept) {
		t.Fatal("receipt did not identify original team file", joined, err)
	}
	if !strings.Contains(joined.Sentence(), "This computer's teams were set aside to "+joined.TeamsAside+".") {
		t.Fatal("receipt omitted teams recovery path", joined.Sentence())
	}
}

func TestInterruptedReplacementRetainsTeamsReceiptOnResume(t *testing.T) {
	home := t.TempDir()
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: home}).Row(config.KeyTeamsShare)
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if err := teams.Save(home, []teams.Team{{ID: "local", Name: "harbor"}}); err != nil {
		t.Fatal(err)
	}
	other, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("interrupted after cleanup")
	j := Joining{Home: home, Replace: true, TeamsPaired: func() bool { return true }, After: func(step string) error {
		if step == "staged" {
			return interrupted
		}
		return nil
	}}
	joined, err := adopt(j, Grant{Identity: other})
	if !errors.Is(err, interrupted) || joined.TeamsAside == "" {
		t.Fatalf("interruption lost receipt: %+v, %v", joined, err)
	}
	j.After = nil
	resumed, err := adopt(j, Grant{Identity: other})
	if err != nil || resumed.TeamsAside != joined.TeamsAside {
		t.Fatalf("resume lost receipt: %+v, %v", resumed, err)
	}
	if _, err := os.Stat(resumed.TeamsAside); err != nil {
		t.Fatal(err)
	}
}
