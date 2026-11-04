package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func publishAnywayJob() factory.Item {
	return factory.Item{ID: 3, Repo: "r", Title: "Docs", Assignee: "alice", State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, HeadCommit: strings.Repeat("a", 40), Change: &factory.Change{Files: []factory.ChangedFile{{Path: "docs.md"}}},
		Stages: []factory.Stage{{Name: "inspect", Kind: factory.StageCheck, When: "touches auth"}, {Name: "post", Kind: factory.StagePost, Ask: "pr"}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "inspect", Kind: factory.StageCheck, State: factory.PhaseSkipped, SkipReason: "when touches auth does not match the change files"}, {Name: "post", Kind: factory.StagePost, State: factory.PhaseWaiting, Result: &factory.StageResult{Claims: []factory.Claim{{Medium: "policy", Evidence: "tests pass before anything posts"}}}}}}}
}

func TestPublishAnywayPostRequiresCurrentPersonDecision(t *testing.T) {
	at := time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	policy := []string{"tests pass before anything posts"}
	for _, tc := range []struct {
		name   string
		change func(*factory.Item)
		want   bool
	}{
		{"person", func(*factory.Item) {}, true},
		{"unknown files", func(it *factory.Item) { it.Change = nil; it.Triage.Area = "docs" }, false},
		{"automatic approval", func(it *factory.Item) { it.PublishAnyway.Automatic = true }, false},
		{"files changed", func(it *factory.Item) { it.Change.Files[0].Path = "other.md" }, false},
		{"condition now matches", func(it *factory.Item) { it.Change.Files[0].Path = "auth.go" }, false},
		{"head changed", func(it *factory.Item) { it.HeadCommit = strings.Repeat("b", 40) }, false},
		{"skips changed", func(it *factory.Item) { it.Stream.Phases[0].SkipReason = "different reason" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := publishAnywayJob()
			if !api.CanFor(it, "alice").PublishAnyway {
				t.Fatal("eligible job has no offer")
			}
			if err := factory.DecidePublishAnyway(&it, "alice", at); err != nil {
				t.Fatal(err)
			}
			tc.change(&it)
			src := &fakeSource{}
			res, err, _ := runPost(t, src, policy, it, "pr", nil)
			if err != nil || res.Done != tc.want || (len(src.got) == 1) != tc.want {
				t.Fatalf("published=%v writes=%d err=%v", res.Done, len(src.got), err)
			}
			if tc.want {
				body := src.got[0].Body
				for _, want := range []string{"inspect: skipped", "when touches auth does not match the change files", "Published anyway by @alice at 2026-11-03T15:00:00Z; skipped checks remain unmeasured."} {
					if !strings.Contains(body, want) {
						t.Errorf("PR missing %q: %s", want, body)
					}
				}
				if strings.Contains(body, "Passed locally") || it.Stream.Phases[0].Result != nil || len(it.Proof) != 0 {
					t.Fatal("skip gained measured proof")
				}
			}
		})
	}
}

func TestPublishAnywayPostRefusesOtherUnmetChecks(t *testing.T) {
	for _, state := range []factory.PhaseState{factory.PhaseFailed, factory.PhasePending, factory.PhaseRunning, factory.PhaseDone} {
		t.Run(string(state), func(t *testing.T) {
			it := publishAnywayJob()
			it.Stages = append(it.Stages, factory.Stage{Name: "unit", Kind: factory.StageCheck})
			it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "unit", Kind: factory.StageCheck, State: state})
			if api.CanFor(it, "alice").PublishAnyway {
				t.Fatal("unmet check offered override")
			}
			if err := factory.DecidePublishAnyway(&it, "alice", time.Now()); err == nil {
				t.Fatal("unmet check accepted press")
			}
			src := &fakeSource{}
			res, err, _ := runPost(t, src, []string{"tests pass before anything posts"}, it, "pr", nil)
			if err != nil || res.Done || len(src.got) != 0 || strings.Contains(res.Output, "Publish anyway") {
				t.Fatalf("unmet check escaped: %+v %v", res, err)
			}
		})
	}
}

func TestPublishAnywayNeverAutomaticOrOneGreen(t *testing.T) {
	it := publishAnywayJob()
	it.Autonomy = factory.AutonomyAuto
	it.Approval = &factory.Approval{Automatic: true, Kind: "proof", State: "approved"}
	src := &fakeSource{}
	res, err, _ := runPost(t, src, []string{"tests pass before anything posts"}, it, "pr", nil)
	if err != nil || res.Done || len(src.got) != 0 {
		t.Fatal("automatic approval published a skipped check")
	}
	it.Stream.Phases[1].Result.Claims[0].Evidence = "no pull request without a green check"
	if api.CanFor(it, "alice").PublishAnyway {
		t.Fatal("one-green policy offered override")
	}
	res, err, _ = runPost(t, src, []string{"no pull request without a green check"}, it, "pr", nil)
	if err != nil || res.Done || len(src.got) != 0 {
		t.Fatal("one-green published only skips")
	}
}

func TestPublishAnywayOfferRecognizesPolicyPunctuation(t *testing.T) {
	it := publishAnywayJob()
	it.Stream.Phases[1].Result.Claims[0].Evidence = "Tests  pass before anything posts."
	if !api.CanFor(it, "alice").PublishAnyway {
		t.Fatal("valid every-check policy lost offer")
	}
}

func TestPublishAnywayRerunRequiresANewPress(t *testing.T) {
	it := publishAnywayJob()
	if err := factory.DecidePublishAnyway(&it, "alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := StepControl(&it, "inspect", true, "recheck files", time.Now()); err != nil {
		t.Fatal(err)
	}
	it.Change.Files[0].Path = "other.md"
	it.Stream.Phases[0].State = factory.PhaseSkipped
	it.Stream.Phases[0].SkipReason = "when touches auth does not match the change files"
	src := &fakeSource{}
	res, err, _ := runPost(t, src, []string{"tests pass before anything posts"}, it, "pr", nil)
	if err != nil || res.Done || len(src.got) != 0 || it.PublishAnyway != nil {
		t.Fatal("rerun retained earlier person press")
	}
}

func TestPublishAnywayPostKeepsMeasuredPassBesideSkip(t *testing.T) {
	it := publishAnywayJob()
	it.Stages = append(it.Stages, factory.Stage{Name: "unit", Kind: factory.StageCheck})
	it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "unit", Kind: factory.StageCheck, State: factory.PhaseDone, Result: &factory.StageResult{Done: true}})
	if err := factory.DecidePublishAnyway(&it, "alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{}
	res, err, _ := runPost(t, src, []string{"tests pass before anything posts"}, it, "pr", []factory.StageResult{{Done: true}})
	if err != nil || !res.Done || len(src.got) != 1 {
		t.Fatalf("measured pass plus skip did not publish: %+v %v", res, err)
	}
}

func TestPublishAnywayPostUsesServerReceiptWithEmptyLocalLedger(t *testing.T) {
	serverStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverRecord, err := record.Open(serverStore, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localRecord, err := record.Open(localStore, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	it := publishAnywayJob()
	if err := factory.DecidePublishAnyway(&it, "alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := serverRecord.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "alice", Commit: it.HeadCommit}); err != nil {
		t.Fatal(err)
	}
	trailer, err := serverRecord.TrailerFor(context.Background(), it.ID, it.HeadCommit)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, receipt string
		want          bool
	}{
		{"server receipt", trailer, true},
		{"wrong head", strings.ReplaceAll(trailer, it.HeadCommit, strings.Repeat("b", 40)), false},
		{"wrong person", strings.ReplaceAll(trailer, "@alice", "@bob"), false},
		{"malformed", "not a receipt", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := it
			approval := *it.PublishAnyway
			copy.PublishAnyway = &approval
			copy.PublishAnyway.Words = tc.receipt
			src := &fakeSource{}
			ex := NewPostExecutorWith(PostOptions{Record: localRecord, Source: func(string) factory.Source { return src }, Policy: func(factory.Item) []string { return []string{"tests pass before anything posts"} }})
			res, err := ex.Run(context.Background(), Job{Item: copy, Stage: factory.Stage{Name: "post", Kind: factory.StagePost, Ask: "pr"}})
			if (err == nil && res.Done) != tc.want || (len(src.got) == 1) != tc.want {
				t.Fatalf("receipt accepted=%v, err=%v writes=%d", res.Done, err, len(src.got))
			}
		})
	}
}

// A person's Stop stands until they start the job; Publish anyway isn't offered past it.
func TestPublishAnywayIsNotOfferedOnAStoppedJob(t *testing.T) {
	it := publishAnywayJob()
	if !factory.OffersPublishAnyway(it) {
		t.Fatal("fixture should be offered Publish anyway before Stop")
	}
	factory.MarkPersonStopped(&it)
	if factory.OffersPublishAnyway(it) {
		t.Fatal("a stopped job was offered Publish anyway")
	}
	if err := factory.DecidePublishAnyway(&it, "alice", time.Now()); err == nil {
		t.Fatal("a stopped job accepted Publish anyway")
	}
}
