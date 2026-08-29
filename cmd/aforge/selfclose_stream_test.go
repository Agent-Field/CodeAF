package main

// The line a person watching a headless run sees when a leaf catches its own
// mistake before handing the work over.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A LEAF HELD BACK TO FIX ITS OWN WORK IS STILL RUNNING WHEN THE PERSON EXPECTED
// IT TO BE FINISHED, and that is precisely the fact this register carries
// (FAILSAFE.md clause 3). Without the line, a run that spent three more turns
// closing a finding reads as a run that hung.
func TestTheStreamSaysWhenALeafClosesItsOwnFinding(t *testing.T) {
	var progress strings.Builder
	watch := &settlementWatch{progress: &progress, started: time.Now()}
	payload, err := json.Marshal(store.LeafSelfClose{
		Kinds: []string{"lost public names"},
		Names: []string{"init_file_path", "res_path", "temp_post_req_data_path"},
		Turns: 14, Closed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !watch.narrateOne(
		store.Event{Kind: store.EventLeafSelfClose, Payload: payload},
		store.Node{ID: "task-2", Brief: "persist the feature schema"}, nil) {
		t.Fatal("the closing was not narrated at all")
	}
	said := progress.String()
	for _, phrase := range []string{"↻", "closing its own finding", "lost public names", "3 names"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the line never said %q:\n%s", phrase, said)
		}
	}
	// ✗ IS THE FAULT REGISTER AND A LEAF FIXING ITS OWN WORK IS NOT A FAULT.
	if strings.Contains(said, "✗") {
		t.Errorf("a close was marked as a fault:\n%s", said)
	}

	// AND THE ARM THAT STOOD SAYS NOTHING HERE. The finding lands, the gate says
	// so in its own words, and saying it twice would read as two findings. It is
	// journaled either way, which is where an autopsy reads it.
	progress.Reset()
	stood, err := json.Marshal(store.LeafSelfClose{
		Kinds: []string{"lost public names"}, Names: []string{"res_path"},
		Turns: 19, Why: "its turns were spent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if watch.narrateOne(
		store.Event{Kind: store.EventLeafSelfClose, Payload: stood},
		store.Node{ID: "task-2", Brief: "persist the feature schema"}, nil) {
		t.Fatalf("a finding that stood was narrated as a close:\n%s", progress.String())
	}
}
