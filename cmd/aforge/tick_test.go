package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"golang.org/x/sys/unix"
)

// The door is machinery, not a command, so it must not appear in the list of
// things a person can usefully type — for the same reason `engine` does not.
func TestTickIsAbsentFromTheUsageText(t *testing.T) {
	for _, word := range []string{"aforge tick", " tick "} {
		if strings.Contains(usageText, word) {
			t.Fatalf("the usage text offers %q", word)
		}
	}
}

func TestTickWalksTheItemsAndWritesAWakeLine(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)

	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(standing.Item{
		Words:     "remind me at 6 to leave",
		Workspace: state,
		When:      standing.When{Kind: standing.WhenProbe, Probe: standing.Probe{Command: "gh run list"}},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "leave now"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := runTick(nil); err != nil {
		t.Fatalf("a pass with no runner should still walk: %v", err)
	}
	raw, err := os.ReadFile(store.WakeLogPath())
	if err != nil {
		t.Fatalf("the pass wrote no wake line: %v", err)
	}
	if !strings.Contains(string(raw), "examined=1") {
		t.Fatalf("the wake line is %q", string(raw))
	}
	// Nothing fired, because this build has nothing to fire with.
	if _, err := os.Stat(filepath.Join(store.RunsDir(made.ID), "0001")); !os.IsNotExist(err) {
		t.Fatalf("a build with no runner ran something: %v", err)
	}
}

func TestTickLeavesQuietlyWhenAWindowIsAlreadyKeepingWatch(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(store.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := unix.Flock(int(held.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	// A pass somebody else is already running is a success, not a failure.
	if err := runTick(nil); err != nil {
		t.Fatalf("a held lock was reported as an error: %v", err)
	}
}

func TestTickTakesNoArguments(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if err := runTick([]string{"--now"}); err == nil {
		t.Fatal("an argument was accepted by a door that has none")
	}
}
