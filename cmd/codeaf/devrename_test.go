package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// A computer that is not paired with anything keeps the name and says nobody
// needs telling yet; it never reaches for the sync service.
func TestRenameOnAComputerWithNoFleetKeepsTheNameHere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	t.Setenv("CODEAF_SYNC_URL", "off")

	said, err := renameThisDevice(context.Background(), dir, "  atlas ")
	if err != nil {
		t.Fatal(err)
	}
	if said != pair.RenamedAloneLine("atlas") || devname.Name(dir) != "atlas" || deviceName() != "atlas" {
		t.Fatalf("said %q, name %q, deviceName %q", said, devname.Name(dir), deviceName())
	}
}

func TestARefusedRenameKeepsTheOldNameAndSaysWhy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	if _, err := devname.Set(dir, "atlas"); err != nil {
		t.Fatal(err)
	}
	for typed, want := range map[string]error{"": devname.ErrEmpty, strings.Repeat("x", 33): devname.ErrTooLong, "a\nb": devname.ErrControl} {
		if _, err := renameThisDevice(context.Background(), dir, typed); !errors.Is(err, want) {
			t.Errorf("rename to %q: %v, want %v", typed, err, want)
		}
	}
	if devname.Name(dir) != "atlas" {
		t.Fatalf("a refused rename changed the name to %q", devname.Name(dir))
	}
}

func TestDevicesRenameTakesSeveralWordsAndAnEmptyNameIsUsage(t *testing.T) {
	celltest.Available(t)
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	t.Setenv("CODEAF_SYNC_URL", "off")
	if _, err := captureStdout(t, func() error { return runDevices([]string{"rename", "kitchen", "laptop"}) }); err != nil {
		t.Fatal(err)
	}
	if devname.Name(dir) != "kitchen laptop" {
		t.Fatalf("name = %q", devname.Name(dir))
	}
	if err := runDevices([]string{"rename"}); err == nil || !strings.Contains(err.Error(), "usage: codeaf devices rename") {
		t.Fatalf("an empty rename ended with %v", err)
	}
}

// M7: a join validates --name before reaching a relay and saves it only after
// success, so a refusal leaves the computer unchanged.
func TestM7PairNameFlagWaitsForSuccess(t *testing.T) {
	celltest.Available(t)
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	t.Setenv("CODEAF_SYNC_URL", "off")

	err := newPairDoor(strings.NewReader(""), &pairScreenText{}).run(context.Background(), []string{"--name", "atlas", "42-715-302"})
	if !errors.Is(err, pair.ErrSyncOff) {
		t.Fatalf("the pairing ended with %v, want it to reach the relay check", err)
	}
	if _, chosen := devname.Chosen(dir); chosen {
		t.Fatal("M7 a refused join kept --name")
	}

	err = newPairDoor(strings.NewReader(""), &pairScreenText{}).run(context.Background(), []string{"--name", "bad\x00name", "42-715-302"})
	if !errors.Is(err, devname.ErrControl) {
		t.Fatalf("a bad name ended with %v", err)
	}
	if _, chosen := devname.Chosen(dir); chosen {
		t.Fatal("M7 a bad name changed the saved name")
	}
}

// A person at a terminal who has never named this computer is asked once, with
// the host name filled in; Enter keeps it and writes nothing, a name is kept, and
// a bad one is refused and asked again.
func TestM7JoinDefersANameUntilPaired(t *testing.T) {
	cases := []struct {
		typed, want string
		chosen      bool
	}{
		{"\n", devname.Default(), false},
		{"host-a\n", "host-a", true},
		{"bad\x00\ngood name\n", "good name", true},
	}
	for _, c := range cases {
		dir := t.TempDir()
		t.Setenv("CODEAF_HOME", dir)
		out := &pairScreenText{}
		d := pairDoor{term: newTerminal(strings.NewReader(c.typed), out), asksName: true}
		if err := d.naming(context.Background(), "", nil, false); err != nil {
			t.Fatal(err)
		}
		if _, chosen := devname.Chosen(dir); chosen {
			t.Fatal("M7 name was written before pairing")
		}
		want := c.want
		if !c.chosen {
			want = ""
		}
		if d.name != want {
			t.Errorf("typed %q: pending name %q, want %q", c.typed, d.name, want)
		}
		if !strings.Contains(out.String(), pair.NamePrompt(devname.Default())) {
			t.Errorf("typed %q: the question was not asked:\n%s", c.typed, out.String())
		}
	}
}

// The question is for a join that has no name yet: not for the device that shows
// a code or approves, not when the computer already has a name, and not when
// nobody is at a terminal.
func TestTheNameQuestionIsAskedOnlyWhereItMeansSomething(t *testing.T) {
	asked := func(door pairDoor, args []string, showing bool) bool {
		out := &pairScreenText{}
		door.term = newTerminal(strings.NewReader("\n"), out)
		_ = door.naming(context.Background(), "", args, showing)
		return strings.Contains(out.String(), "Press Enter to keep that")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	key, err := pair.NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	token := pair.LinkRef{Code: "k7m2q9xd", Key: key}.Token()
	person := pairDoor{asksName: true}
	if !asked(person, nil, false) || !asked(person, []string{"42-715-302"}, false) {
		t.Fatal("a join with no name was not asked")
	}
	if asked(person, nil, true) || asked(person, []string{"approve", token}, false) || asked(person, []string{token}, false) {
		t.Fatal("a door that shows a code or approves was asked for a name")
	}
	if asked(pairDoor{}, nil, false) {
		t.Fatal("a run with nobody at a terminal was asked")
	}
	if _, err := devname.Set(home.Dir(), "atlas"); err != nil {
		t.Fatal(err)
	}
	if asked(person, nil, false) {
		t.Fatal("a computer that already has a name was asked again")
	}
}
