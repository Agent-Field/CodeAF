package main

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestWhySelfPrintsTodaysRealReceipts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "why-self.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "practice-parser", Brief: "Practice parser recovery", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginSelf, Intent: "Practice parser recovery"}); err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordFact("practice-parser", "repo:/work/parser", store.FactLesson,
		"check the recovery token before advancing")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "practice-parser", Cost: 0.31}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("practice-parser", "why-test")
	if err != nil || !ok {
		t.Fatalf("claim ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "learned recovery"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runWhyTo([]string{"self", "--db", path}, &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	printed := output.String()
	for _, want := range []string{"TRIED", "COST", "LEARNED", "Practice parser recovery", "$0.31", "facts #"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("why self output missing %q:\n%s", want, printed)
		}
	}
	if !strings.Contains(printed, "#"+strconv.FormatInt(fact.Seq, 10)) {
		t.Fatalf("why self output omitted fact #%d:\n%s", fact.Seq, printed)
	}
}

// TestAskingWhyAboutAnIdThatIsNotThereIsNotASuccess is row 27, and it is the
// difference between "not found" and "found, and empty".
//
// `aforge why bogus-node-id` printed its sentence and returned nil, so a script
// asking whether an id exists read exit 0 and concluded that it did. The
// sentence tells a PERSON which of the two silences they have; nothing told a
// caller. `aforge logs` took this same one-line change over the same emptiness.
func TestAskingWhyAboutAnIdThatIsNotThereIsNotASuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "why-miss.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	err = runWhyTo([]string{"bogus-node-id", "--db", path}, &output, time.Now())
	if code := exitCodeOf(err); code == 0 {
		t.Errorf("`aforge why bogus-node-id` left with 0, so a script reads it as an id that exists and has nothing in it — "+
			"want a non-zero exit beside the sentence:\n%s", output.String())
	}
	// The sentence is still printed: the exit is for the script, the words are
	// for the person, and taking either away costs the other nothing.
	if !strings.Contains(output.String(), "has no transcript") {
		t.Errorf("the miss stopped saying which of the two silences it is:\n%s", output.String())
	}
}
