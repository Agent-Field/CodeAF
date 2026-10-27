package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func followItem() Item {
	head := strings.Repeat("a", 40)
	it := Item{Kind: KindIssue, Repo: "acme/api", Num: 12, State: StateShipped, Publish: "pr", HeadCommit: head, Cap: 3,
		PullRequest: &PullRequest{Number: 7, State: PROpen, HeadSHA: head, ChecksRead: true, Checks: PRChecks{Failed: 1}},
		Change:      &Change{Files: []ChangedFile{{Path: "ledger.go"}}}, Stream: &Stream{Phases: []Phase{{Name: "proof", State: PhaseDone}}}}
	WatchPublishedPR(&it)
	return it
}

func TestDefaultCIFollowUpPersistsAndNeverRepeatsTheSameHead(t *testing.T) {
	it := followItem()
	now := time.Now()
	if !QueueCIFollowUp(&it, now) || it.State != StateQueued || len(it.Stream.Phases) != 2 || it.FollowUp.Attempt != 1 {
		t.Fatalf("not queued: %+v", it)
	}
	data, _ := json.Marshal(it)
	var restored Item
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if QueueCIFollowUp(&restored, now) {
		t.Fatal("queued while already active")
	}
	restored.State = StateShipped
	WatchPublishedPR(&restored)
	if QueueCIFollowUp(&restored, now) {
		t.Fatal("same red head repeated after restart")
	}
	restored.HeadCommit = strings.Repeat("b", 40)
	restored.PullRequest.HeadSHA = restored.HeadCommit
	if !QueueCIFollowUp(&restored, now) || restored.FollowUp.Attempt != 2 {
		t.Fatal("second head did not queue")
	}
	restored.State = StateShipped
	WatchPublishedPR(&restored)
	restored.FollowUp.Attempt = restored.FollowUp.Limit
	restored.HeadCommit = strings.Repeat("c", 40)
	restored.PullRequest.HeadSHA = restored.HeadCommit
	if !QueueCIFollowUp(&restored, now) || restored.FollowUp.State != "needs_you" || restored.State != StateShipped {
		t.Fatal("attempt limit did not escalate")
	}
}

func TestCIFollowUpNeverRunsAgainstUnknownOrChangingWork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Item)
	}{
		{"pending", func(i *Item) { i.PullRequest.Checks.Pending = 1 }},
		{"unread", func(i *Item) { i.PullRequest.ChecksRead = false }},

		{"dropped", func(i *Item) { i.DoneHow = "dropped" }},
		{"active", func(i *Item) { i.State = StateRunning }},
		{"merge queued", func(i *Item) { i.Merging = &MergeRun{State: MergeQueued} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := followItem()
			tc.change(&it)
			state, phases := it.State, len(it.Stream.Phases)
			// The follow-up may say what it waits for (its reason is
			// monitoring, saved as a change); it never queues work.
			QueueCIFollowUp(&it, time.Now())
			if it.State != state || len(it.Stream.Phases) != phases || (it.FollowUp != nil && (it.FollowUp.State == "queued" || it.FollowUp.State == "running")) {
				t.Fatalf("unexpected follow-up: state=%s phases=%d follow=%+v", it.State, len(it.Stream.Phases), it.FollowUp)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*Item)
	}{

		{"spending cap", func(i *Item) { i.Stream.Spent = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := followItem()
			tc.change(&it)
			if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "needs_you" {
				t.Fatal("did not escalate")
			}
			if len(it.Stream.Phases) != 2 {
				t.Fatal("queued work despite blocker")
			}
		})
	}
}

func TestClosedPRDoesNotKeepAskingForAnObsoleteCIRepair(t *testing.T) {
	it := followItem()
	it.FollowUp.State = "needs_you"
	it.PullRequest.State = PRClosed
	if JobState(it) != "done" {
		t.Fatal("closed PR still asks for repair")
	}
}
