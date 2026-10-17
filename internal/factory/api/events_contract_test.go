package api

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func TestLiveEventsKeepContractOneAndSmallPayloads(t *testing.T) {
	if Contract != 2 {
		t.Fatal("live events changed the contract")
	}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for _, e := range FixtureLiveEvents(now) {
		if e.Kind == "job.state" {
			var payload live.StatePayload
			if err := json.Unmarshal(e.Data, &payload); err != nil || payload.Job == 0 || payload.State == "" {
				t.Fatalf("bad job state: %s", e.Data)
			}
			seen[e.Kind] = true
			continue
		}
		var payload LiveEvent
		if err := json.Unmarshal(e.Data, &payload); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(payload)
		if err != nil || string(encoded) != string(e.Data) {
			t.Fatalf("payload did not round trip: %s %s %v", e.Data, encoded, err)
		}
		seen[e.Kind] = true
	}
	for _, kind := range []string{"job.state", "needs_you", "question", "approved", "taken", "publishing", "published", "publish_failed", "machine"} {
		if !seen[kind] {
			t.Fatalf("fixture lacks %s", kind)
		}
	}
	for _, kind := range factory.NeedsYouKinds {
		it := factory.Item{ID: 7, State: factory.StateNeedsYou, Question: "continue?", Changed: now}
		switch kind {
		case factory.NeedsFailed:
			it.Failure = &factory.Failure{Code: factory.CodeToolError}
		case factory.NeedsConflict:
			it.State, it.PullRequest = factory.StateShipped, &factory.PullRequest{State: factory.PROpen, Conflict: true}
		case factory.NeedsChangesRequested:
			it.State, it.PullRequest = factory.StateShipped, &factory.PullRequest{State: factory.PROpen, ReviewDecision: "changes_requested"}
		case factory.NeedsApprovePlan:
			it.QKind = factory.QKindApprove
		case factory.NeedsApproveStart:
			it.QKind = factory.QKindAskFirst
		case factory.NeedsSpendingLimit:
			it.QKind = "cap"
		case factory.NeedsOutOfAttempts:
			it.QKind = factory.QKindOutOfAttempts
		case factory.NeedsApproveProof:
			it.State, it.Question = factory.StateLanded, ""
		case factory.NeedsRunnerOffline:
			it.QKind = factory.NeedsRunnerOffline
		case factory.NeedsPublishFailed:
			it.State, it.PublishStatus = factory.StateLanded, &factory.Publishing{State: "failed", Code: "no_push_permission"}
		}
		events := live.Changes(factory.Item{}, it, now)
		found := false
		for _, e := range events {
			if e.Kind != "needs_you" {
				continue
			}
			var p LiveEvent
			if err := json.Unmarshal(e.Data, &p); err != nil {
				t.Fatal(err)
			}
			current := FromItem(it).NeedsYou
			if p.Kind == nil || *p.Kind != kind || p.Job != 7 || p.Since == nil || current == nil || !p.Since.Equal(current.Since) {
				t.Fatalf("need %s: %s", kind, e.Data)
			}
			found = true
		}
		if !found {
			t.Fatalf("missing need %s", kind)
		}
		if kind == factory.NeedsPublishFailed {
			var fields map[string]any
			for _, e := range events {
				if e.Kind == "publish_failed" {
					_ = json.Unmarshal(e.Data, &fields)
				}
			}
			if !reflect.DeepEqual(fields, map[string]any{"job": float64(7), "reason": "no_push_permission"}) {
				t.Fatalf("failure: %v", fields)
			}
		}
	}
}

func TestCommentPublishedEventKeepsNullPR(t *testing.T) {
	events := live.Changes(factory.Item{}, factory.Item{ID: 1, PublishStatus: &factory.Publishing{State: "opened"}}, time.Now())
	for _, e := range events {
		if e.Kind != "published" {
			continue
		}
		if string(e.Data) != `{"job":1,"pr":null}` {
			t.Fatalf("comment receipt: %s", e.Data)
		}
		return
	}
	t.Fatal("no comment publication event")
}
