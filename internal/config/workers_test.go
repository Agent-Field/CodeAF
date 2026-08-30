package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The roster is a SET FILTER and nothing more, and this is the whole of its
// grammar: blank is everything, names are matched case-insensitively over
// commas or spaces, the build's own order is kept whatever order they were
// typed in, and a name nobody knows is answered rather than refused.
func TestWorkerRosterReadsALineAgainstWhatTheBuildKnows(t *testing.T) {
	known := []string{"linear", "swe", "bare"}
	for _, testCase := range []struct {
		line    string
		kept    []string
		unknown []string
	}{
		{line: "", kept: known},
		{line: "   ", kept: known},
		{line: "bare", kept: []string{"bare"}},
		{line: "BARE", kept: []string{"bare"}},
		{line: "bare, swe", kept: []string{"swe", "bare"}},
		{line: "bare swe", kept: []string{"swe", "bare"}},
		{line: "bare,bare", kept: []string{"bare"}},
		{line: "linear", kept: []string{"linear"}},
		{line: "reviewer", unknown: []string{"reviewer"}},
		{line: "bare, reviewer, reviewer", kept: []string{"bare"}, unknown: []string{"reviewer"}},
	} {
		kept, unknown := WorkerRoster(testCase.line, known)
		if strings.Join(kept, ",") != strings.Join(testCase.kept, ",") {
			t.Errorf("WorkerRoster(%q) kept %v, want %v", testCase.line, kept, testCase.kept)
		}
		if strings.Join(unknown, ",") != strings.Join(testCase.unknown, ",") {
			t.Errorf("WorkerRoster(%q) could not place %v, want %v", testCase.line, unknown, testCase.unknown)
		}
	}
}

// The resolution order, said out loud: the environment pin, then what the
// profile holds, then nothing at all — and nothing at all is the default that
// means every worker.
func TestWorkersAtResolvesTheEnvironmentThenTheProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	if got := WorkersAt(dir); got != "" {
		t.Fatalf("a profile nobody wrote reads %q, want the default", got)
	}
	if err := writeText(dir, KeyWorkers, "bare"); err != nil {
		t.Fatal(err)
	}
	if got := WorkersAt(dir); got != "bare" {
		t.Fatalf("the persisted roster reads %q", got)
	}
	t.Setenv(EnvWorkers, "swe")
	if got := WorkersAt(dir); got != "swe" {
		t.Fatalf("the environment pin reads %q", got)
	}
}

// The row reports the build's own workers rather than a list written here, and
// a build that installed no catalog says nothing rather than "none" — unknown
// and zero are not the same fact.
func TestTheRosterRowReportsTheBuildsOwnWorkers(t *testing.T) {
	t.Cleanup(func() { UseInstalledWorkers(nil) })

	UseInstalledWorkers(nil)
	row, found := NewSettings(SettingsOptions{ProfileDir: t.TempDir()}).Row(KeyWorkers)
	if !found {
		t.Fatal("the roster has no settings row")
	}
	if got := row.Receipt(); got != "" {
		t.Fatalf("a build with no catalog reports %q", got)
	}

	UseInstalledWorkers(func() []string { return []string{"swe", "bare"} })
	row, _ = NewSettings(SettingsOptions{ProfileDir: t.TempDir()}).Row(KeyWorkers)
	for _, want := range []string{"bare", "swe"} {
		if !strings.Contains(row.Receipt(), want) {
			t.Fatalf("the receipt %q does not name %q", row.Receipt(), want)
		}
	}
	// And the row a person has not written reads as the default in its own
	// words rather than as a blank.
	if got := row.Value(); got != "every worker installed" {
		t.Fatalf("an unwritten roster reads %q", got)
	}
}
