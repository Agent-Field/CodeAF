package revision

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A. EVERY POINT IN THE REQUEST'S LIST HAS A LINE IN THE ENDING, in its
// original order and with one of the three words the contract allows.
func TestEveryPointOnTheChecklistIsAnsweredInTheEnding(t *testing.T) {
	points := []store.AcceptancePoint{
		{Behaviour: "alpha remains stable", Quote: "alpha remains stable"},
		{Behaviour: "beta.go keeps its output", Quote: "beta.go keeps its output"},
		{Behaviour: "gamma has a check", Quote: "gamma has a check"},
		{Behaviour: "delta has no check", Quote: "delta has no check"},
	}
	gate := store.DeliveryGate{Exercises: []store.ExercisedPoint{
		{Point: "gamma has a check", Check: "go test ./internal/gamma/"},
		{Point: "delta has no check"},
	}}
	outcomes := AnswerChecklist(points, gate, true, []string{"/work/beta.go"})
	account := ChecklistAccount(outcomes)
	if len(outcomes) != len(points) {
		t.Fatalf("%d points produced %d outcomes: %+v", len(points), len(outcomes), outcomes)
	}
	allowed := map[string]bool{
		PointAnswered: true, PointNotAnswered: true, PointNotReached: true,
	}
	for index, outcome := range outcomes {
		if !allowed[outcome.State] {
			t.Errorf("point %d has state %q", index+1, outcome.State)
		}
		line := fmt.Sprintf("%d. %s — %s", index+1, points[index].Behaviour, outcome.State)
		if !strings.Contains(account, line) {
			t.Errorf("the account has no ordered line %q:\n%s", line, account)
		}
	}
}

// B. THE DELIVERY GATE IS NOT A PRECONDITION FOR ACCOUNTING FOR THE LIST.
func TestARunThatReachedNoGateStillAnswersItsList(t *testing.T) {
	points := []store.AcceptancePoint{
		{Behaviour: "one remains", Quote: "one remains"},
		{Behaviour: "two remains", Quote: "two remains"},
		{Behaviour: "three remains", Quote: "three remains"},
		{Behaviour: "four remains", Quote: "four remains"},
	}
	outcomes := AnswerChecklist(points, store.DeliveryGate{}, false, nil)
	account := ChecklistAccount(outcomes)
	if account == "" {
		t.Fatal("a run with no gate lost its checklist")
	}
	for index, point := range points {
		if !strings.Contains(account, fmt.Sprintf("%d. %s", index+1, point.Behaviour)) {
			t.Errorf("the no-gate account dropped point %d:\n%s", index+1, account)
		}
	}
}

// C. THE ISSUE'S OWN REPRODUCTION: two files were written and two were never
// touched, so the ending names the two files absent from the run's record.
func TestTheItemsNothingTouchedAreNamedWithTheFileTheRunNeverWrote(t *testing.T) {
	points := []store.AcceptancePoint{
		{Behaviour: "repair shaped.go", Quote: "repair internal/shaped/shaped.go"},
		{Behaviour: "update prose.go", Quote: "update prose.go:115"},
		{Behaviour: "repair render.go", Quote: "repair render.go"},
		{Behaviour: "update README.md", Quote: "update README.md"},
	}
	outcomes := AnswerChecklist(points, store.DeliveryGate{}, false,
		[]string{"/work/internal/shaped/shaped.go", "/work/render.go"})

	for _, index := range []int{1, 3} {
		if outcomes[index].State != PointNotAnswered {
			t.Errorf("point %d state = %q, want %q", index+1, outcomes[index].State, PointNotAnswered)
		}
	}
	if outcomes[1].Why != "nothing this run wrote is prose.go" {
		t.Errorf("the missing prose file is said as %q", outcomes[1].Why)
	}
	if outcomes[3].Why != "nothing this run wrote is README.md" {
		t.Errorf("the missing readme file is said as %q", outcomes[3].Why)
	}
	account := ChecklistAccount(outcomes)
	for _, name := range []string{"prose.go", "README.md"} {
		if !strings.Contains(account, "nothing this run wrote is "+name) {
			t.Errorf("the account does not name the untouched %s:\n%s", name, account)
		}
	}
}

// D. WRITING A FILE IS NOT EVIDENCE THAT THE BEHAVIOUR HOLDS.
func TestAPointWhoseFileTheRunChangedIsNotCalledAnswered(t *testing.T) {
	points := []store.AcceptancePoint{{
		Behaviour: "prose.go keeps paragraphs intact", Quote: "change prose.go:115",
	}}
	outcomes := AnswerChecklist(points, store.DeliveryGate{}, false, []string{"/work/Prose.go"})
	if len(outcomes) != 1 {
		t.Fatalf("got %d outcomes, want one", len(outcomes))
	}
	if outcomes[0].State != PointNotReached || outcomes[0].Why != "the run changed prose.go" {
		t.Fatalf("a changed file was settled beyond the evidence: %+v", outcomes[0])
	}
	if strings.Contains(ChecklistAccount(outcomes), "— "+PointAnswered) {
		t.Fatal("a file write was called answered")
	}
}

// E. ONLY THE GATE'S OWN POINT-TO-CHECK MAPPING CAN SAY ANSWERED.
func TestTheGatesOwnMappingIsWhatSaysAnswered(t *testing.T) {
	points := []store.AcceptancePoint{
		{Behaviour: "the parser accepts tabs", Quote: "the parser accepts tabs"},
		{Behaviour: "the parser rejects blanks", Quote: "the parser rejects blanks"},
	}
	gate := store.DeliveryGate{Exercises: []store.ExercisedPoint{
		{Point: "  the parser accepts tabs  ", Check: "go test ./internal/parser/ -run TestTabs"},
		{Point: "the parser rejects blanks"},
	}}
	outcomes := AnswerChecklist(points, gate, true, nil)
	if outcomes[0].State != PointAnswered ||
		outcomes[0].Why != "a check covers it: go test ./internal/parser/ -run TestTabs" {
		t.Errorf("the checked point = %+v", outcomes[0])
	}
	if outcomes[1].State != PointNotAnswered || outcomes[1].Why != "no check exercises it" {
		t.Errorf("the unmapped point = %+v", outcomes[1])
	}
}

// H. THE PERSON'S BLOCK IS BOUNDED AND COUNTS EVERY WHOLE LINE IT OMITS,
// while the machine-shaped slice remains complete.
func TestALongChecklistIsBoundedAndSaysHowManyItCouldNotShow(t *testing.T) {
	points := make([]store.AcceptancePoint, 20)
	for index := range points {
		behaviour := fmt.Sprintf("point %02d %s", index+1, strings.Repeat("long behaviour ", 20))
		points[index] = store.AcceptancePoint{Behaviour: behaviour, Quote: behaviour}
	}
	outcomes := AnswerChecklist(points, store.DeliveryGate{}, false, nil)
	account := ChecklistAccount(outcomes)
	if len(account) > checklistBlockBytes {
		t.Fatalf("account is %d bytes, bound is %d", len(account), checklistBlockBytes)
	}
	if !utf8.ValidString(account) {
		t.Fatal("the bounded account cut through a UTF-8 rune")
	}
	shown := 0
	for index := range points {
		if strings.Contains(account, fmt.Sprintf("\n%d. ", index+1)) {
			shown++
		}
	}
	omitted := len(points) - shown
	if omitted <= 0 {
		t.Fatalf("the long fixture did not exercise the bound:\n%s", account)
	}
	wantLast := fmt.Sprintf("…and %d more on this run's own record.", omitted)
	if !strings.HasSuffix(account, wantLast) {
		t.Fatalf("the final line does not count its omissions; want %q:\n%s", wantLast, account)
	}
	raw, err := json.Marshal(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	var whole []PointOutcome
	if err := json.Unmarshal(raw, &whole); err != nil {
		t.Fatal(err)
	}
	if len(whole) != len(points) {
		t.Fatalf("the machine list has %d rows, want %d", len(whole), len(points))
	}
}

// C, K. FILE WORDS ARE THE SHAPES A PERSON WRITES, not versions, examples,
// numbers, addresses or wildcard patterns that happen to contain a dot.
func TestFileWordsReadsWhatAPersonWritesAndNotWhatTheyDont(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{"prose.go:115", []string{"prose.go"}},
		{"`internal/shaped/shaped.go`", []string{"shaped.go"}},
		{"README.md", []string{"README.md"}},
		{"v0.1.0", nil},
		{"e.g.", nil},
		{"i.e.", nil},
		{"3.14", nil},
		{"a@b.co", nil},
		{"*.go", nil},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			got := fileWords(test.text)
			if strings.Join(got, "|") != strings.Join(test.want, "|") {
				t.Fatalf("fileWords(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

// K. WHERE THE RECORD KNOWS NOTHING MORE, THE LINE ENDS AT ITS STATE.
func TestTheAccountSaysNothingItDoesNotKnow(t *testing.T) {
	outcomes := AnswerChecklist([]store.AcceptancePoint{{
		Behaviour: "paragraphs remain readable", Quote: "paragraphs remain readable",
	}}, store.DeliveryGate{}, false, nil)
	account := ChecklistAccount(outcomes)
	want := "1. paragraphs remain readable — not reached"
	if !strings.HasSuffix(account, want) {
		t.Fatalf("the unknown point does not end at its state:\n%s", account)
	}
	for _, emptyDecoration := range []string{"not reached:", "()", " -"} {
		if strings.Contains(account, emptyDecoration) {
			t.Errorf("the account invented empty decoration %q:\n%s", emptyDecoration, account)
		}
	}
	for _, machinery := range []string{"auditor", "verdict", "verified", "refuted"} {
		if strings.Contains(strings.ToLower(account), machinery) {
			t.Errorf("the account used %q:\n%s", machinery, account)
		}
	}
}
