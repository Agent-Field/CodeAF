package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// A LEAF THAT CLOSED ITS OWN FINDING AND A RUN WHERE NOBODY LOOKED MUST NOT BE
// THE SAME SILENCE (FAILSAFE.md clause 4). Both arms are journaled — the close
// that was taken and the finding that stood — and both carry the names, because
// `lost: 3` said in three places and never once saying WHICH three is exactly
// what made igel s14 unreadable.
func TestALeafsOwnClosingIsJournaledOnBothArms(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	if err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "task-2", Brief: "persist the feature schema", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "persist the feature schema"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordLeafSelfClose("task-2", LeafSelfClose{
		Kinds: []string{"lost public names"},
		Names: []string{"init_file_path", "res_path", "temp_post_req_data_path"},
		Turns: 14, Closed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordLeafSelfClose("task-2", LeafSelfClose{
		Kinds: []string{"lost public names"}, Names: []string{"temp_post_req_data_path"},
		Turns: 19, Why: "it had already closed lost public names once",
	}); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events(0, 200)
	if err != nil {
		t.Fatal(err)
	}
	var closings []LeafSelfClose
	for _, event := range events {
		if event.Kind != EventLeafSelfClose {
			continue
		}
		var record LeafSelfClose
		if err := json.Unmarshal(event.Payload, &record); err != nil {
			t.Fatal(err)
		}
		closings = append(closings, record)
	}
	if len(closings) != 2 {
		t.Fatalf("journaled %d closings, want both arms", len(closings))
	}
	if !closings[0].Closed || len(closings[0].Names) != 3 || closings[0].Turns != 14 {
		t.Errorf("the taken arm is %+v", closings[0])
	}
	if closings[1].Closed || closings[1].Why == "" {
		t.Errorf("the arm that stood does not say it stood, or why: %+v", closings[1])
	}
}

// A ROW NAMING NO FINDING IS REFUSED. This record exists to say what a leaf
// found against itself; "it found nothing" is what the surface, unbound and
// verification rows beside it already say, and a third spelling of it would be
// a third answer to one question.
func TestAClosingWithNoFindingIsRefused(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	if err := store.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "task-2", Brief: "work", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "work"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordLeafSelfClose("task-2", LeafSelfClose{Closed: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a closing about nothing was accepted: %v", err)
	}
	if err := store.RecordLeafSelfClose("  ", LeafSelfClose{
		Kinds: []string{"unbound names"},
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a closing filed against nothing was accepted: %v", err)
	}
}
