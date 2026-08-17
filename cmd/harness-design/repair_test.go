package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// scripted is a model that says what it was told to say, in order, and records
// what it was asked. It is the smallest thing that can prove the repair turn is
// wired the way the pipeline claims: a rig that repaired by quietly re-running
// the whole design would look identical from the outside except in the bill, and
// the bill is what this whole stage exists to protect.
type scripted struct {
	replies []string
	asked   []chatRequest
}

func (s *scripted) serve(t *testing.T) *chatClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("the rig sent a body that will not decode: %v", err)
		}
		s.asked = append(s.asked, req)
		text := "{}"
		if len(s.asked) <= len(s.replies) {
			text = s.replies[len(s.asked)-1]
		}
		var out chatResponse
		out.Choices = []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string     `json:"content"`
				Reasoning string     `json:"reasoning"`
				ToolCalls []toolCall `json:"tool_calls"`
			} `json:"message"`
		}{{FinishReason: "stop"}}
		out.Choices[0].Message.Content = text
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := newChatClient("test-key", "test/model")
	client.url = server.URL
	return client
}

func (s *scripted) last() chatRequest { return s.asked[len(s.asked)-1] }

// A reply that only needs salvaging must NOT buy a repair turn: the ladder is
// free and the turn is not.
func TestAMangledButSalvageableReplyCostsNoTurn(t *testing.T) {
	model := &scripted{replies: []string{
		"```json\n{“cues”: [“event log”], “justification”: “because”, “harness”: {“id”: {“name”: “x”}},}\n```",
	}}
	chat := model.serve(t)

	salvaged, at, err := jsonReply(context.Background(), chat, []message{{Role: "user", Content: "design it"}}, 100, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if at.repaired {
		t.Error("a salvageable reply spent a repair turn")
	}
	if len(model.asked) != 1 {
		t.Errorf("the model was asked %d times", len(model.asked))
	}
	if at.rung != subharness.SalvageLenient {
		t.Errorf("rung = %q", at.rung)
	}
	if !json.Valid(salvaged.JSON) {
		t.Errorf("salvaged bytes are not JSON: %s", salvaged.JSON)
	}
	if !strings.Contains(at.cost(), "salvaged at") {
		t.Errorf("the stage line would say nothing about it: %q", at.cost())
	}
}

// A reply the ladder cannot fix — cut off mid-object — buys ONE repair turn, and
// that turn carries the broken text and the parser's complaint rather than the
// guide. The whole point is that a punctuation failure must not cost a
// re-derivation.
func TestAnUnsalvageableReplyBuysExactlyOneRepairTurn(t *testing.T) {
	model := &scripted{replies: []string{
		`{"cues": ["event log"], "justification": "because", "harness": {"id": {"nam`,
		`{"cues": ["event log"], "justification": "because", "harness": {"id": {"name": "journal"}}}`,
	}}
	chat := model.serve(t)
	guide := []message{
		{Role: "system", Content: "THE WHOLE DESIGNER GUIDE, twenty-five thousand tokens of it"},
		{Role: "user", Content: "THE GOAL: design it"},
	}

	envelope, _, at, err := designOnce(context.Background(), chat, guide, 100, 0.3)
	// The page is a stub, so the design is refused by the law — but it was
	// DECODED, which is what this test is about.
	if err == nil {
		t.Fatal("a page with no program passed the law")
	}
	if !at.repaired {
		t.Fatal("the reply was not repaired")
	}
	if len(model.asked) != 2 {
		t.Fatalf("the model was asked %d times, want 2", len(model.asked))
	}
	if envelope.Justification != "because" {
		t.Errorf("the repaired envelope did not decode: %+v", envelope)
	}

	repair := model.last()
	whole := ""
	for _, turn := range repair.Messages {
		whole += turn.Content + "\n"
	}
	if strings.Contains(whole, "THE WHOLE DESIGNER GUIDE") {
		t.Error("the repair turn re-sent the guide — it is a transcription job, not a second design")
	}
	if !strings.Contains(whole, `"nam`) {
		t.Error("the repair turn does not carry the model's own broken text")
	}
	if !strings.Contains(whole, "unexpected end of JSON input") {
		t.Errorf("the repair turn does not carry the parser's complaint:\n%s", whole)
	}
	if !strings.Contains(at.cost(), "repair turn") {
		t.Errorf("the stage line would not say a turn was spent: %q", at.cost())
	}
}

// When the repair fails too, the caller gets one error naming both failures and
// spends a real attempt — which is the retry the rig was always going to spend,
// no worse for having tried the cheap thing first.
func TestARepairThatFailsCostsTheAttempt(t *testing.T) {
	model := &scripted{replies: []string{"I'm sorry, I can't.", "I still can't."}}
	chat := model.serve(t)

	_, at, err := jsonReply(context.Background(), chat, []message{{Role: "user", Content: "design it"}}, 100, 0.3)
	if err == nil {
		t.Fatal("prose became JSON")
	}
	if !strings.Contains(err.Error(), "neither the reply nor its repair") {
		t.Errorf("the error does not say what was tried: %v", err)
	}
	if at.raw != "I still can't." {
		t.Errorf("the retry history would carry the wrong text: %q", at.raw)
	}
	if len(model.asked) != 2 {
		t.Errorf("the model was asked %d times, want 2", len(model.asked))
	}
}

// Stage 1.5's turn goes through the same door, and its patch lands on the draft
// the rig already parsed — so a critic whose reply arrived wearing typographic
// quotes still patches the right page.
func TestTheReviewTurnSalvagesAndPatches(t *testing.T) {
	draft := design{Cues: []string{"event log", "session journal"}, Justification: strings.Repeat("x", 500)}
	draftHarness := subharness.Harness{
		Id: subharness.Id{Name: "journal", Desc: "decide on an event log"},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "memo", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "write the memo — ranked"}},
		}},
		Verify: subharness.Verify{Ladder: subharness.VerifyAccept},
		Dyn:    subharness.Dyn{Ladder: subharness.DynFixed},
	}
	model := &scripted{replies: []string{
		"```json\n" + `{"findings": [{"pass": "quality", "text": "the brief is generic"}],
		  "ops": [{"op": "replace_brief", "node": "memo", "text": "write the decision memo, with the migration cost named"},
		          {"op": "replace_brief", "node": "ghost", "text": "..."}],
		  "calls": {"draft": 1, "revised": 1},}` + "\n```",
	}}
	chat := model.serve(t)

	revised, harness, applied, at, err := reviewOnce(context.Background(), chat,
		[]message{{Role: "user", Content: "review it"}}, 100, 0.3, draft, draftHarness)
	if err != nil {
		t.Fatal(err)
	}
	if at.repaired {
		t.Error("a salvageable review spent a repair turn")
	}
	if revised.findings() != 1 {
		t.Errorf("findings = %d", revised.findings())
	}
	if len(applied) != 2 || !applied[0].Applied() || applied[1].Applied() {
		t.Fatalf("op results = %+v", applied)
	}
	node, _ := harness.Program.Node("memo")
	if !strings.Contains(node.Fields.Get("brief"), "migration cost") {
		t.Errorf("the patch did not land: %q", node.Fields.Get("brief"))
	}
	// The draft the rig parsed is untouched, and the critic never restated the
	// justification, so the draft's own stands.
	if got := draftHarness.Program.Nodes[0].Fields.Get("brief"); got != "write the memo — ranked" {
		t.Errorf("the draft was written through: %q", got)
	}
	if revised.design(draft).Justification != draft.Justification {
		t.Error("an omitted justification did not fall back to the draft's")
	}
}
