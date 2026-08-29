package bare

// The symbol-level half, wired end to end through a real leaf.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// igelSourceBase is the shape igel s11 stood in: a public class carrying its
// paths as CLASS attributes.
const igelSourceBase = `class Igel:
    available_commands = ("fit", "evaluate")
    results_path = configs.get("results_path")
    description_file = configs.get("description_file")
    model = None

    def fit(self, **kwargs):
        return None
`

// igelSourceAfter is what the run left: the class attributes gone, instance
// attributes of the same words set in __init__ instead.
const igelSourceAfter = `class Igel:
    available_commands = ("fit", "evaluate")
    model = None

    def __init__(self, **cli_args):
        _cfg = _make_configs()
        self.results_path = _cfg["results_path"]
        self.description_file = _cfg["description_file"]

    def fit(self, **kwargs):
        return None
`

// A LEAF WHOSE SUITE CAME BACK GREENER STILL SAYS WHAT IT DELETED.
//
// This is igel s11 end to end: the project's own reading of the finished tree
// improves — nothing red, more checks named — while the work removes public
// class attributes no check in the project touches. The check-level half is
// telling the truth and answering a different question; the symbol-level half is
// the one that answers this one.
func TestALeafSaysWhatPublicNamesItDeleted(t *testing.T) {
	stage := stageSuite(t)
	root := stage.workspace.Root()
	source := filepath.Join(root, "igel", "igel.py")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(igelSourceBase), 0o644); err != nil {
		t.Fatal(err)
	}
	stage.says(t, "test_fit PASSED\n", 0)
	worker := New(stage.workspace, "model", "key", "http://127.0.0.1:1", 90*time.Minute)

	task := exec.Task{Goal: t.Name()}
	reading, _ := worker.photographBefore(context.Background(), task)
	if !reading.Taken {
		t.Fatal("the first reading was not taken")
	}
	if len(reading.Surface) == 0 {
		t.Fatal("the baseline read no public surface at all")
	}

	// The work: the attributes come off the class, and the suite gets BETTER.
	if err := os.WriteFile(source, []byte(igelSourceAfter), 0o644); err != nil {
		t.Fatal(err)
	}
	stage.says(t, "test_fit PASSED\ntest_init PASSED\n", 0)
	outcome := &exec.Outcome{Artifacts: []string{source}}
	worker.photographAfter(context.Background(), task, reading, true, false, outcome)

	if len(outcome.Regressed) != 0 {
		t.Fatalf("the check-level half claimed a regression it cannot have seen: %v",
			outcome.Regressed)
	}
	held := strings.Join(outcome.Removed, " ")
	for _, name := range []string{"Igel.results_path", "Igel.description_file"} {
		if !strings.Contains(held, name) {
			t.Errorf("%s was deleted and the leaf said nothing: %v", name, outcome.Removed)
		}
	}
	for _, kept := range []string{"Igel.model", "Igel.fit", "Igel.available_commands"} {
		if strings.Contains(held, kept) {
			t.Errorf("%s survived and was reported removed: %v", kept, outcome.Removed)
		}
	}
	// A leaf that removed nothing says nothing, which reads downstream as no
	// claim rather than as an acquittal.
	if err := os.WriteFile(source, []byte(igelSourceBase), 0o644); err != nil {
		t.Fatal(err)
	}
	quiet := &exec.Outcome{Artifacts: []string{source}}
	worker.photographAfter(context.Background(), task, reading, true, false, quiet)
	if len(quiet.Removed) != 0 {
		t.Errorf("a leaf that removed nothing raised %v", quiet.Removed)
	}
}
