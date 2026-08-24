package main

import (
	"strings"
	"testing"
)

// The target is read the way the ssh door reads its own, because it is the same
// idea said about a different kind of address.
func TestTheAtTargetSplitsOnTheFirstColon(t *testing.T) {
	for _, one := range []struct {
		typed, name, workspace string
	}{
		{"otter-lamp-42", "otter-lamp-42", ""},
		{"otter-lamp-42:", "otter-lamp-42", ""},
		{"otter-lamp-42:code/app", "otter-lamp-42", "code/app"},
		{"otter-lamp-42:/srv/code/app", "otter-lamp-42", "/srv/code/app"},
		{"  otter-lamp-42:code  ", "otter-lamp-42", "code"},
	} {
		name, workspace, err := parseAtTarget(one.typed)
		if err != nil {
			t.Fatalf("%q: %v", one.typed, err)
		}
		if name != one.name || workspace != one.workspace {
			t.Fatalf("%q read as %q and %q", one.typed, name, workspace)
		}
	}
}

// A REFUSAL NAMES THE SHAPE IT WANTED. Somebody who typed the wrong thing needs
// an example, not a complaint.
func TestAnEmptyOrHeadlessAtTargetIsRefusedWithAnExample(t *testing.T) {
	_, _, err := parseAtTarget("   ")
	if err == nil || !strings.Contains(err.Error(), "otter-lamp-42") {
		t.Fatalf("an empty --at said %v", err)
	}
	if _, _, err := parseAtTarget(":code/app"); err == nil || !strings.Contains(err.Error(), "no machine in front of the colon") {
		t.Fatalf("a headless --at said %v", err)
	}
}

// The two flags that build a session cannot travel, and are refused rather than
// quietly ignored — the same law the ssh door states, for the same reason.
func TestTheFlagsThatBuildASessionCannotTravelOverAt(t *testing.T) {
	err := atLaunch{target: "otter-lamp-42", yolo: true, noCompact: true}.check()
	if err == nil {
		t.Fatal("--yolo and --no-compact travelled over --at")
	}
	said := err.Error()
	for _, want := range []string{"--no-compact", "--yolo", "otter-lamp-42", "settings panel"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal does not mention %q: %q", want, said)
		}
	}
	if err := (atLaunch{target: "otter-lamp-42"}).check(); err != nil {
		t.Fatalf("a launch with neither flag was refused: %v", err)
	}
}

// WITH NO RELAY SET UP, BOTH DOORS SAY SO IN ONE SENTENCE AND NAME THE ROAD
// THAT DOES WORK. The relay is a service, this build does not assume one is
// running, and a person who meets this must not be left guessing whether their
// network is broken.
func TestWithNoRelayBothDoorsSayWhatIsWrongAndWhatToDoInstead(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv("AFORGE_RELAY", "")

	err := openChatV3At(atLaunch{target: "otter-lamp-42"})
	if err == nil {
		t.Fatal("--at opened a connection with no relay set up")
	}
	said := err.Error()
	for _, want := range []string{"no relay is set up", "AFORGE_RELAY", "--host over ssh", "otter-lamp-42"} {
		if !strings.Contains(said, want) {
			t.Fatalf("--at with no relay said %q, which does not mention %q", said, want)
		}
	}
	if strings.Contains(said, "\n") {
		t.Fatalf("--at with no relay said more than one line: %q", said)
	}

	err = runServe(nil)
	if err == nil {
		t.Fatal("aforge serve started with no relay set up")
	}
	said = err.Error()
	for _, want := range []string{"no relay is set up", "AFORGE_RELAY", "--host"} {
		if !strings.Contains(said, want) {
			t.Fatalf("aforge serve with no relay said %q, which does not mention %q", said, want)
		}
	}
}

// `aforge devices` on a machine nothing has paired with is one sentence and an
// invitation, not a heading over an empty table.
func TestDevicesOnAFreshMachineSaysNothingIsPaired(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	if err := runDevices(nil); err != nil {
		t.Fatal(err)
	}
	if err := runDevices([]string{"revoke"}); err == nil {
		t.Fatal("revoke with no name was accepted")
	}
	if err := runDevices([]string{"revoke", "laptop"}); err == nil {
		t.Fatal("revoking a device nothing has paired was accepted")
	}
	if err := runDevices([]string{"nonsense"}); err == nil {
		t.Fatal("an unknown word was accepted")
	}
}
