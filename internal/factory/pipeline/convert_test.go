package pipeline

import (
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
