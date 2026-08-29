package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ✗ IS THE FAULT REGISTER, AND A REQUEUE IS NOT A FAULT.
//
// The headless stream said `✗ … picked up again — …` for every release that
// carried a reason, and once exhaustion started requeueing (exec.Requeued) the
// commonest of those became the ordinary end of a leaf that ran out of its
// room — announced one line under the ⏳ that had just said, correctly, that
// nothing had failed. Two marks, three lines apart, disagreeing about the same
// event.
//
// The register is chosen from the fact and not from the words: the release
// carries how many recorded turns it hands on, which is the same count the next
// claim's resume seed is built from.
func TestARequeueThatHandsOnWorkIsNotSaidAsAFault(t *testing.T) {
	for _, probe := range []struct {
		name     string
		payload  map[string]any
		wantMark string
		wantText string
	}{
		{
			name:     "a leaf that ran out of its room",
			payload:  map[string]any{"reason": "the worker did not come back within 17m0s and was stopped — 45 turns of its work is recorded, and the next one carries on from there", "recorded": 45},
			wantMark: "↻",
			wantText: "picked up again from 45 recorded turns",
		},
		{
			name:     "one recorded turn, spelled singular",
			payload:  map[string]any{"reason": "the worker did not come back within 17m0s and was stopped — 1 turn of its work is recorded, and the next one carries on from there", "recorded": 1},
			wantMark: "↻",
			wantText: "picked up again from 1 recorded turn",
		},
		{
			name:     "a claim taken back over a worker that never answered",
			payload:  map[string]any{"reason": "no sign of life for 24m3s"},
			wantMark: "✗",
			wantText: "picked up again — no sign of life for 24m3s",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			body, err := json.Marshal(probe.payload)
			if err != nil {
				t.Fatal(err)
			}
			var page bytes.Buffer
			watch := &settlementWatch{progress: &page, started: time.Now()}
			node := store.Node{ID: "task-2", Title: "Core engine"}
			if !watch.narrateOne(store.Event{NodeID: node.ID, Kind: store.EventNodeReleased, Payload: body},
				node, []store.Node{node}) {
				t.Fatal("the release said nothing at all")
			}
			line := page.String()
			if !strings.Contains(line, probe.wantMark) {
				t.Errorf("the line is in the wrong register:\n\t%s\nwant the %s mark", strings.TrimSpace(line), probe.wantMark)
			}
			if !strings.Contains(line, probe.wantText) {
				t.Errorf("the line reads\n\t%s\nwant it to say %q", strings.TrimSpace(line), probe.wantText)
			}
			if probe.wantMark == "↻" && strings.Contains(line, "✗") {
				t.Errorf("a requeue was marked as a fault:\n\t%s", strings.TrimSpace(line))
			}
		})
	}
}
