package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestIssueOnlyUpgradeErasesStandalonePRJobsAndKeepsLinkedPRs(t *testing.T) {
	s := newLocalSetup(t)
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	issue, err := floor.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Num: 59, Title: "keep issue", State: factory.StateShipped, Publish: "pr", PullRequest: &factory.PullRequest{Number: 65, State: factory.PROpen, URL: "https://github.com/acme/api/pull/65"}, FollowUp: &factory.PRFollowUp{Kind: "ci", State: "watching", Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := floor.ForgetPRJob(issue.ID); err == nil {
		t.Fatal("cleanup accepted an issue job")
	}
	pr, err := floor.Create(factory.Item{Kind: factory.KindPR, Repo: "acme/api", Num: 65, Title: "erase PR job", State: factory.StateQueued, Assignee: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	chat, _ := jobConversation(t)
	if err := markFactoryChat(floor, pr, chat); err != nil {
		t.Fatal(err)
	}
	if err := floor.Update(pr.ID, func(it *factory.Item) error { it.Talk = chat; return nil }); err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(filepath.Join(floor.Root(), "workers", "99999999", "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mirror.Save(pr); err != nil {
		t.Fatal(err)
	}
	orphan, _ := jobConversation(t)
	if err := markFactoryChat(mirror, pr, orphan); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(floor.Root(), "work", "api-65")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "local.txt"), []byte("PR scratch work"), 0600)
	history, err := record.Open(floor, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range []factory.Item{issue, pr} {
		if _, err := history.Append(context.Background(), record.Event{Kind: record.Started, Job: it.ID}); err != nil {
			t.Fatal(err)
		}
	}
	floor.SetMarks([]int{pr.ID}, true)
	floor.SetBusy(pr.ID, "reading")
	desk, err := lease.Open(floor, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.Pull(lease.Worker{ID: "worker", Handle: "owner"}); err == nil {
		t.Fatal("legacy PR job got a worker lease")
	}
	for n := 0; n < 2; n++ {
		if err := forgetFactoryPRJobs(context.Background(), floor, s.backend.localProfile()); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{floor.ItemPath(pr.ID), mirror.ItemPath(pr.ID), filepath.Dir(chat), filepath.Dir(orphan), work} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("retained old PR job data: %s (%v)", p, err)
		}
	}
	got, err := floor.Get(issue.ID)
	if err != nil || got.PullRequest == nil || got.PullRequest.Number != 65 || got.FollowUp.State != "watching" {
		t.Fatalf("issue PR tracking changed: %+v %v", got, err)
	}
	events, err := history.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Job == pr.ID {
			t.Fatal("PR job event retained")
		}
	}
	data, err := os.ReadFile(filepath.Join(floor.Root(), "live-events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var replay struct{ Events []live.Event }
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	for _, e := range replay.Events {
		if e.Job == pr.ID {
			t.Fatal("PR job replay payload retained")
		}
	}
	snap, err := factory.LocalSeam(floor, time.Now()).Load()
	if err != nil {
		t.Fatal(err)
	}
	queue := api.FromSnapshot(snap, "")
	if len(queue.Jobs) != 1 || queue.Jobs[0].PR.Number != 65 {
		t.Fatal("issue's linked PR disappeared")
	}
	next, err := floor.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Title: "new issue"})
	if err != nil || next.ID <= pr.ID {
		t.Fatalf("erased PR ID was reused: %d %v", next.ID, err)
	}
}

func TestIssueOnlyUpgradeRemovesWorkForOrphanWorkerPRSnapshot(t *testing.T) {
	s := newLocalSetup(t)
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(filepath.Join(floor.Root(), "workers", "99999999", "remote"))
	if err != nil {
		t.Fatal(err)
	}
	pr, err := mirror.Create(factory.Item{Kind: factory.KindPR, Repo: "acme/api", Num: 81, Title: "old remote PR"})
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(floor, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := history.Append(context.Background(), record.Event{Kind: record.Started, Job: pr.ID}); err != nil {
		t.Fatal(err)
	}
	// Worker work is on the main floor, not alongside its mirror JSON.
	work := filepath.Join(floor.Root(), "work", "api-81")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("scratch"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := forgetFactoryPRJobs(context.Background(), floor, s.backend.localProfile()); err != nil {
		t.Fatal(err)
	}
	events, err := history.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 0 {
		t.Fatalf("orphan worker history retained: %v %v", events, err)
	}
	for _, p := range []string{work, mirror.ItemPath(pr.ID)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("worker PR retained %s: %v", p, err)
		}
	}
}
