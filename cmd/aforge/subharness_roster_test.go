package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE ROSTER IS A FILTER OVER REGISTRATION, AND EVERYTHING ELSE FOLLOWS FROM
// THE ABSENCE.
//
// A worker left off the roster is not registered, and this is the test that the
// four readers of a registration all agree it is gone: the menu the compiler
// chooses from, the predicate the escalation ladder climbs by, the constructor
// the leaf is built from, and the flag a person types. None of them was taught
// about rosters — that is the whole point of doing it at registration.
func TestARosterInstallsOnlyTheWorkersItNames(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.ForgetSubharnesses()

	var stderr bytes.Buffer
	installWorkerRoster("bare", &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("a roster of known workers said something: %q", stderr.String())
	}

	if exec.KnownSubharness("swe") {
		t.Fatal("a worker left off the roster is still known")
	}
	if !exec.KnownSubharness("bare") {
		t.Fatal("a worker on the roster was not installed")
	}
	menu := exec.MenuText()
	if strings.Contains(menu, "swe") {
		t.Fatalf("the compiler is still offered the worker that is not here:\n%s", menu)
	}
	if !strings.Contains(menu, "bare") {
		t.Fatalf("the roster's own worker is off the menu:\n%s", menu)
	}

	// The flag says the sentence it has always said for a worker this build does
	// not have, without knowing why this one is missing.
	var flagged bytes.Buffer
	if got := resolveSubharnessFlag("swe", &flagged); got != "" {
		t.Fatalf("--subharness swe resolved to %q with swe off the roster", got)
	}
	if !strings.Contains(flagged.String(), `no subharness named "swe"`) ||
		!strings.Contains(flagged.String(), "this build has: bare") {
		t.Fatalf("the note does not say what happened: %q", flagged.String())
	}

	// And the leaf that names it anyway is built by the generalist rather than
	// by the worker the roster removed.
	build := leafBuild{settings: config.Config{ProfileDir: t.TempDir()}, maxTurns: 1, maxTokens: 1000}
	if worker := executorFor("swe", build); worker.Subharness() != exec.LinearSubharness {
		t.Fatalf("a leaf naming an uninstalled worker was built as %q", worker.Subharness())
	}
}

// The default roster is the whole build, and it is the shape every profile that
// has said nothing keeps having. This is the test that adding the row changed
// nothing for anybody who does not use it.
func TestTheDefaultRosterIsEveryWorkerThisBuildHas(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.ForgetSubharnesses()

	var stderr bytes.Buffer
	installWorkerRoster("", &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("the default roster said something: %q", stderr.String())
	}
	for _, info := range buildWorkers() {
		if !exec.KnownSubharness(info.Name) {
			t.Fatalf("%q is in this build and was not installed by the default roster", info.Name)
		}
	}
}

// A roster that places no worker leaves the generalist, and says so about the
// names it could not place. Naming the generalist is not an error — it is the
// plainest way somebody writes "only the ordinary worker" — and it installs
// nothing because the generalist was never a registration in the first place.
func TestARosterThatPlacesNoWorkerLeavesTheGeneralist(t *testing.T) {
	defer exec.ForgetSubharnesses()

	for _, testCase := range []struct {
		roster string
		note   string
	}{
		{roster: exec.LinearSubharness},
		{roster: "not-a-worker", note: `no worker named "not-a-worker"`},
	} {
		exec.ForgetSubharnesses()
		var stderr bytes.Buffer
		installWorkerRoster(testCase.roster, &stderr)
		if len(exec.Subharnesses()) != 0 {
			t.Fatalf("roster %q installed %v", testCase.roster, exec.Subharnesses())
		}
		if !exec.GeneralistSubharness(exec.SubharnessFor("").Name) {
			t.Fatalf("roster %q left no generalist to take the work", testCase.roster)
		}
		if testCase.note == "" {
			if stderr.Len() != 0 {
				t.Fatalf("roster %q said %q", testCase.roster, stderr.String())
			}
			continue
		}
		if !strings.Contains(stderr.String(), testCase.note) {
			t.Fatalf("roster %q said %q, want it to name the word", testCase.roster, stderr.String())
		}
		// The note has to be usable: it names what this build actually has, so
		// somebody with a typo can fix the line without going hunting.
		for _, info := range buildWorkers() {
			if !strings.Contains(stderr.String(), info.Name) {
				t.Fatalf("the note does not name %q: %q", info.Name, stderr.String())
			}
		}
	}
}

// The roster reaches the profile the way every other row does — the settings
// registry writes it, [config.WorkersAt] reads it back, and installation obeys
// it — so `"work.workers": "bare"` in a config.json is the whole of turning the
// specialist off for that profile.
func TestTheRosterIsReadFromTheProfileTheSettingsRowWrites(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.ForgetSubharnesses()

	dir := t.TempDir()
	raw, err := json.Marshal(map[string]string{config.KeyWorkers: "bare"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AFORGE_PROFILE_DIR", dir)

	installSubharnesses()
	if exec.KnownSubharness("swe") {
		t.Fatal(`a profile holding "work.workers": "bare" still installed swe`)
	}
	if !exec.KnownSubharness("bare") {
		t.Fatal(`a profile holding "work.workers": "bare" did not install bare`)
	}

	// And the row a person opens reports the whole build rather than the
	// filtered result, or the worker they turned off would have no name they
	// could type to turn it back on.
	row, found := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyWorkers)
	if !found {
		t.Fatal("the roster has no settings row")
	}
	if got := row.Value(); got != "bare" {
		t.Fatalf("the row reads %q, want what the profile holds", got)
	}
	for _, info := range buildWorkers() {
		if !strings.Contains(row.Receipt(), info.Name) {
			t.Fatalf("the row's receipt does not name %q: %q", info.Name, row.Receipt())
		}
	}
}
