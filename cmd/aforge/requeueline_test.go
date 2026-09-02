package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ✗ IS THE FAULT REGISTER, AND A REQUEUE IS NOT A FAULT — AND THE LINE SAYS WHY.
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
//
// AND THE ↻ LINE STOOD ON THE LINE ABOVE IT FOR ITS WHY. It printed the count
// and threw the release reason away, so the bound that fired was legible only
// from the ⏳ the leaf happened to write just before — true of every release
// today, and an ordering assumption rather than a guarantee. The line now names
// the bound itself, and it must name it ONCE: the reason's own clause about how
// many turns survived is the count this line has already said in its own words.
func TestARequeueThatHandsOnWorkIsNotSaidAsAFault(t *testing.T) {
	for _, probe := range []struct {
		name     string
		payload  map[string]any
		wantMark string
		wantText string
		// absent is a phrase the line must NOT carry: the turn count said a
		// second time, in the reason's own words.
		absent string
	}{
		{
			name:     "a claim taken back from a worker that stopped answering",
			payload:  map[string]any{"reason": "the worker did not come back within 17m0s and was stopped — 45 turns of its work is recorded, and the next attempt carries on from there", "recorded": 45},
			wantMark: "↻",
			wantText: "picked up again from 45 recorded turns — the worker did not come back within 17m0s and was stopped",
			absent:   "45 turns of its work is recorded",
		},
		{
			name:     "one recorded turn, spelled singular",
			payload:  map[string]any{"reason": "the worker did not come back within 17m0s and was stopped — 1 turn of its work is recorded, and the next attempt carries on from there", "recorded": 1},
			wantMark: "↻",
			wantText: "picked up again from 1 recorded turn — the worker did not come back within 17m0s and was stopped",
			absent:   "1 turn of its work is recorded",
		},
		{
			name: "a leaf that ran out of its room names the bound and its figures",
			payload: map[string]any{"reason": "it was still working when it ran out of its token budget (cost: 178086 of 176834 tokens of billed work) — 3 turns of its work is recorded, and the next attempt carries on from there",
				"recorded": 3},
			wantMark: "↻",
			wantText: "picked up again from 3 recorded turns — it was still working when it ran out of its token budget (cost: 178086 of 176834 tokens of billed work)",
			absent:   "3 turns of its work is recorded",
		},
		{
			name:     "a reason this package did not compose is carried whole",
			payload:  map[string]any{"reason": "the machine it was working on went away", "recorded": 2},
			wantMark: "↻",
			wantText: "picked up again from 2 recorded turns — the machine it was working on went away",
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
			if probe.absent != "" && strings.Contains(line, probe.absent) {
				t.Errorf("the line says the turn count twice:\n\t%s\nwant it not to carry %q",
					strings.TrimSpace(line), probe.absent)
			}
		})
	}
}
