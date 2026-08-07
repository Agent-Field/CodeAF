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
