package api

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The acts doors take the caller's expectation as the job's version in a
// plain number, with 0 for none, or as the object every other door takes.
func TestActsExpectIsAVersionNumberOrAnExpectation(t *testing.T) {
	for body, want := range map[string]int{
		`{"acts":[],"by":"you","expect":0}`:                               0,
		`{"acts":[],"by":"you"}`:                                          0,
		`{"acts":[],"by":"you","expect":12}`:                              12,
		`{"acts":[],"by":"you","expect":{"version":7}}`:                   7,
		`{"acts":[],"by":"you","expect":{"state":"running","version":3}}`: 3,
	} {
		var req ActsRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		guard := req.Expect.Guard()
		switch {
		case want == 0 && guard != nil:
			t.Errorf("%s: an expectation of nothing guards %+v", body, guard)
		case want != 0 && (guard == nil || guard.Version == nil || *guard.Version != want):
			t.Errorf("%s: guard %+v, want version %d", body, guard, want)
		}
	}
	var req ActsRequest
	if err := json.Unmarshal([]byte(`{"acts":[],"by":"you","expect":"soon"}`), &req); err == nil {
		t.Fatal("an expectation that is neither was taken")
	}
	var state ActsApproveRequest
	if err := json.Unmarshal([]byte(`{"expect":{"state":"running"}}`), &state); err != nil || state.Expect.Guard() == nil || state.Expect.Guard().State != "running" {
		t.Fatalf("a state alone: %+v, %v", state.Expect, err)
	}
}

// A job's acts cross the wire whole, and a job with none says nothing.
func TestActsCrossTheWire(t *testing.T) {
	it := factory.Item{ID: 7, State: factory.StateShipped, Acts: []factory.PublicAct{
		{ID: "act_1", Kind: factory.ActKindReply, Thread: "inline:501", Text: "Renamed it.\n\n" + factory.PublicActFooter, By: factory.ActByMainAgent, State: factory.ActProposed, At: at},
		{ID: "act_2", Kind: factory.ActKindRerequestReview, Reviewers: []string{"bob"}, By: factory.ActByYou, State: factory.ActDone, At: at, DoneAt: at, Result: "Asked @bob to review again."},
	}}
	j := FromItem(it)
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Acts []map[string]any `json:"acts"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || len(wire.Acts) != 2 {
		t.Fatalf("acts on the wire: %s, %v", raw, err)
	}
	if wire.Acts[0]["kind"] != "reply" || wire.Acts[0]["state"] != "proposed" || wire.Acts[0]["thread"] != "inline:501" || wire.Acts[1]["result"] != "Asked @bob to review again." {
		t.Fatalf("acts %v", wire.Acts)
	}
	if _, has := wire.Acts[0]["done_at"]; has {
		t.Fatalf("an act that is not over says when it ended: %v", wire.Acts[0])
	}
	back := ToItem(j)
	if len(back.Acts) != 2 || back.Acts[1].Reviewers[0] != "bob" || !back.Acts[1].DoneAt.Equal(at) {
		t.Fatalf("back %+v", back.Acts)
	}
	// The job and the item it came from share no list.
	j.Acts[1].Reviewers[0] = "carol"
	if it.Acts[1].Reviewers[0] != "bob" {
		t.Fatal("the wire job shares the item's reviewers")
	}
	none, _ := json.Marshal(FromItem(factory.Item{ID: 8}))
	var bare map[string]any
	_ = json.Unmarshal(none, &bare)
	if _, has := bare["acts"]; has {
		t.Fatal("a job with no acts carries acts")
	}
}
