package serve_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestTakeoverAndResumeKeepWorkAndSpend(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Assignee: "sam", Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}, Stream: recordedRun()})
	f.post(t, it, "control", `{"mode":"take_over"}`, 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.Manual || cur.State != factory.StateNeedsYou || cur.Stream.Spent != 1.25 || cur.Stream.Phases[1].Chat != "old-chat" {
		t.Fatalf("takeover lost work: %+v", cur)
	}
	f.post(t, cur, "control", `{"mode":"resume_pipeline","words":"Verify my changes"}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Manual || cur.State != factory.StateQueued || cur.Stream.Spent != 1.25 || cur.Stream.Phases[1].Chat != "old-chat" || cur.Notes[len(cur.Notes)-1] != "Verify my changes" {
		t.Fatalf("resume lost context: %+v", cur)
	}
}
