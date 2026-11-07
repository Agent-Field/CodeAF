package pipeline

import (
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A FACTORY COPY SAVED THROUGH ENCODE DECIDES A START THE WAY ITS TEXT DOES.
// Encode once wrote max_cost, size.issue and default_budget into a file that
// never said them, and the explicit max_cost then held every job with no cost
// history at "no cost history yet · max_cost $5 asks first".
func TestEncodeWritesNoKeyTheFileNeverSaid(t *testing.T) {
	text := "pipeline:\n  quick: [write, test, proof]\n"
	f, err := Parse(text)
	if err != nil || len(f.Problems) > 0 {
		t.Fatalf("parse: %v %v", err, f.Problems)
	}
	enc, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_cost", "max_size", "size:", "start:", "pr_lines", "default_budget", "never", "ask_first", "labels"} {
		if strings.Contains(string(enc), key) {
			t.Errorf("encoded copy says %s:\n%s", key, enc)
		}
	}
	back, err := Parse(string(enc))
	if err != nil {
		t.Fatal(err)
	}
	it := factory.Item{ID: 1, Kind: factory.KindIssue, Title: "x"}
	if a, b := f.DecideAdmitted(it, nil, Measured{}), back.DecideAdmitted(it, nil, Measured{}); a.Start != b.Start || len(a.Reasons) != len(b.Reasons) {
		t.Fatalf("text decides %+v, its encoding %+v", a, b)
	}
	if d := back.DecideAdmitted(it, nil, Measured{}); !d.Start {
		t.Fatalf("an unsized job with no history under a copy that says nothing of cost: %+v", d)
	}
	// A key the file did say survives, even at its default value.
	said, _ := Parse("start:\n  max_cost: 5\n  never: []\n")
	enc, _ = Encode(said)
	if !strings.Contains(string(enc), "max_cost: 5") || !strings.Contains(string(enc), "never: []") {
		t.Fatalf("a said key was dropped:\n%s", enc)
	}
}

// A SAVED FILE IS WHAT ITS AUTHOR WROTE. A named pipeline with one step of
// its own encodes to that pipeline and that step: no shipped step left as
// shipped, no default pipeline, no `fixed:` or `rules: []` nobody wrote.
// Every save path (the AI preview, a pipeline PR, the factory's copy) once
// wrote all eleven shipped steps around the two lines a person changed.
func TestEncodeWritesOnlyThePipelinesAndStepsTheAuthorWrote(t *testing.T) {
	text := "pipeline:\n  docs: [write, docscheck, review]\nsteps:\n  docscheck:\n    prompt: check every exported name has a doc comment\n    until: done\n    max: 2\n"
	f, err := Parse(text)
	if err != nil || len(f.Problems) > 0 {
		t.Fatalf("parse: %v %v", err, f.Problems)
	}
	enc, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	got := string(enc)
	for _, absent := range []string{"plan:", "proof:", "security:", "issue:", "fixed:", "rules:", "make the change"} {
		if strings.Contains(got, absent) {
			t.Errorf("encoded file says %q:\n%s", absent, got)
		}
	}
	for _, present := range []string{"docs:", "docscheck:", "check every exported name"} {
		if !strings.Contains(got, present) {
			t.Errorf("encoded file lost %q:\n%s", present, got)
		}
	}
	back, err := Parse(got)
	if err != nil || len(back.Problems) > 0 {
		t.Fatalf("re-parse: %v %v", err, back.Problems)
	}
	if !slices.Equal(back.Pipeline["docs"], f.Pipeline["docs"]) || !slices.Equal(back.Pipeline[factory.KindIssue], f.Pipeline[factory.KindIssue]) || back.Steps["write"] != f.Steps["write"] {
		t.Fatalf("round trip changed the file: %v / %v", back.Pipeline, f.Pipeline)
	}
	// A shipped step the author changed is written, and an empty file stays a file.
	changed, _ := Parse("steps:\n  write:\n    prompt: make the change, smaller\n")
	if enc, _ := Encode(changed); !strings.Contains(string(enc), "make the change, smaller") {
		t.Fatalf("a changed shipped step was dropped:\n%s", enc)
	}
	if enc, err := Encode(Defaults()); err != nil {
		t.Fatal(err)
	} else if back, err := Parse(string(enc)); err != nil || len(back.Problems) > 0 {
		t.Fatalf("defaults encode to %q, which reads back as %v %v", enc, err, back.Problems)
	}
}
