package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
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

// keyless is a machine that has never been given an API key, whatever the
// shell running the tests exports. Empty reads as unset everywhere the profile
// is resolved (internal/config's APIKeyAt), and the state root is already a
// temporary directory, so there is no persisted key either.
func keyless(t *testing.T) {
	t.Helper()
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
}

// The whole door, on a machine that has never been given a key: `aforge tick`
// builds its pass, takes the lock, walks the one item there is, looks at the
// world, and cannot buy the judgment that would decide whether to fire. THE
// WALK STILL HAPPENS AND THE WAKE LINE STILL SAYS SO — a pass that will mostly
// do nothing is not allowed to demand credentials before it finds out
// (chatv3_standing.go's v3StandingTicker builds keyless).
//
// THE MACHINE IS KEYLESS ON PURPOSE AND THE TEST DOES NOT SKIP. Both variables
// are emptied rather than merely left alone, so this is the same walk on a
// developer's shell as on the CI runner — and so a key that IS exported can
// never turn the last step below into a real model call somebody pays for.
//
// The probe is a command every machine has rather than the `gh run list` this
// was written with, so that what the walk reports is this build's own doing and
// not whether the box has the GitHub CLI installed.
func TestTickWalksTheItemsAndWritesAWakeLine(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	keyless(t)

	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(standing.Item{
		Words:     "remind me at 6 to leave",
		Workspace: state,
		When:      standing.When{Kind: standing.WhenProbe, Probe: standing.Probe{Command: "echo nothing to report"}},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "leave now"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := runTick(nil); err != nil {
		t.Fatalf("a pass on a machine with no key should still walk: %v", err)
	}
	raw, err := os.ReadFile(store.WakeLogPath())
	if err != nil {
		t.Fatalf("the pass wrote no wake line: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	// One item was reached, and the look that could not be paid for is one
	// error. `checked=0` is the honest half: the walk never got an answer about
	// the world, so it must not claim it checked anything.
	for _, field := range []string{"examined=1", "checked=0", "errors=1", "fired=0", "said=0"} {
		if !strings.Contains(line, field) {
			t.Fatalf("the wake line does not say %s: %q", field, line)
		}
	}
	// AND THE ITEM'S OWN ROW SAYS WHY, in the one sentence a card shows: the
	// refusal comes from the sentinel's client at the moment it is asked, not
	// from the door that built the pass.
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	const wanted = "could not check: no API key: this session has not been given one yet"
	if after.LastCheckLine != wanted {
		t.Fatalf("the item's row reads %q, wanted %q", after.LastCheckLine, wanted)
	}
	// Nothing fired: a firing needs a yes, and nobody was able to say one.
	if _, err := os.Stat(filepath.Join(store.RunsDir(made.ID), "0001")); !os.IsNotExist(err) {
		t.Fatalf("a pass that could not judge ran something anyway: %v", err)
	}
}

// The pass that does the least there is: a window already holds the tick lock,
// so this one declines it and leaves. NOTHING IS READ AND NOTHING IS SPENT, and
// again no key is set — a pass that will do nothing costs nothing and needs
// nothing, which is why the door builds keyless.
func TestTickLeavesQuietlyWhenAWindowIsAlreadyKeepingWatch(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	keyless(t)
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
