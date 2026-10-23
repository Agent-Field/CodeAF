package run

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recoveringPRSource struct {
	prSource
	fresh factory.PullRequest
	err   error
	reads int
}

func (s *recoveringPRSource) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	s.reads++
	return s.fresh, s.err
}

func TestFollowPRRefreshPrecedesPaidWorkAndPreservesTerminalState(t *testing.T) {
	for _, event := range []string{"head and feedback", "merged", "closed", "access lost"} {
		t.Run(event, func(t *testing.T) {
			src := &recoveringPRSource{fresh: factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, HeadSHA: strings.Repeat("b", 40), Feedback: "Please handle cancellation", ChecksRead: true}}
			if event == "merged" {
				src.fresh.State = factory.PRMerged
			}
			if event == "closed" {
				src.fresh.State = factory.PRClosed
			}
			if event == "access lost" {
				src.err = errors.New("403 forbidden")
			}
			g := newRig(t, nil, func(o *Options) { o.Source = func(string) factory.Source { return src } })
			id := g.add("existing PR", chat(factory.FollowPRStep))
			_ = g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.HeadCommit = strings.Repeat("a", 40)
				it.PullRequest = &factory.PullRequest{Number: 7, URL: src.fresh.URL, State: factory.PROpen, HeadSHA: it.HeadCommit}
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: it.HeadCommit, Attempt: 2, Limit: 3}
				it.Stream = &factory.Stream{Spent: 3, Phases: []factory.Phase{{Name: factory.FollowPRStep, State: factory.PhaseRunning}}}
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &loopCtl{id: id, ctx: ctx, cancel: cancel, carry: "old-snapshot-chat"}
			ok := g.r.loop().refreshPRRound(c, 0)
			got, _ := g.st.Get(id)
			if src.reads != 1 {
				t.Fatal("paid work could proceed without a fresh read")
			}
			switch event {
			case "head and feedback":
				if !ok || got.HeadCommit == src.fresh.HeadSHA || got.FollowUp.HeadSHA != src.fresh.HeadSHA || c.carry != "" || !strings.Contains(freshFollowContext(got), "Please handle cancellation") || got.Stream.Spent != 3 || got.FollowUp.Attempt != 2 {
					t.Fatal("fresh facts reset work, spend, bounds or accepted an unverified head")
				}
			case "merged", "closed":
				if ok || got.FollowUp.State != src.fresh.State || got.Stream.Phases[0].State != factory.PhaseDone {
					t.Fatal("terminal PR continued paid repair")
				}
			case "access lost":
				if ok || got.Failure == nil {
					t.Fatal("access failure silently continued work")
				}
			}
		})
	}
}

func TestFollowPRReconciliationPreservesExternalAndLocalFiles(t *testing.T) {
	remote := gitRig(t)
	clone := filepath.Join(t.TempDir(), "clone")
	mustGit(t, remote, "clone", "--no-local", remote, clone)
	if err := os.WriteFile(filepath.Join(remote, "external.txt"), []byte("another contributor's work"), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, remote, "add", "external.txt")
	mustGit(t, remote, "commit", "-qm", "external work")
	head := mustGit(t, remote, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(clone, "local.txt"), []byte("retained local work"), 0600); err != nil {
		t.Fatal(err)
	}
	g := newRig(t, nil, nil)
	it := factory.Item{PullRequest: &factory.PullRequest{Number: 7}, FollowUp: &factory.PRFollowUp{HeadSHA: head}}
	if err := g.r.loop().reconcilePR(it, clone); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"external.txt", "local.txt"} {
		if _, err := os.Stat(filepath.Join(clone, name)); err != nil {
			t.Fatalf("reconciliation lost %s", name)
		}
	}
	if got := mustGit(t, clone, "rev-parse", "HEAD"); got != head {
		t.Fatal("remote head was not reconciled")
	}
}

func TestHeldPRObservationCanAdvanceOnlyTheSameAttemptAndPR(t *testing.T) {
	for _, mode := range []string{"current", "other PR", "newer service head", "wrong parent"} {
		t.Run(mode, func(t *testing.T) {
			old, newHead := strings.Repeat("a", 40), strings.Repeat("b", 40)
			it := factory.Item{State: factory.StateRunning, HeadCommit: old, PullRequest: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, HeadSHA: old}, FollowUp: &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: old, Attempt: 2}, Stream: &factory.Stream{Spent: 3}}
			pr := *it.PullRequest
			pr.HeadSHA = newHead
			f := *it.FollowUp
			f.HeadSHA, f.ObservedFrom = newHead, old
			switch mode {
			case "other PR":
				pr.Number = 8
			case "newer service head":
				it.PullRequest.HeadSHA = strings.Repeat("c", 40)
			case "wrong parent":
				f.ObservedFrom = "unknown"
			}
			err := ApplyReport(&it, factory.RunReport{PR: &pr, FollowUp: &f}, time.Now())
			if mode != "current" {
				if err == nil {
					t.Fatal("stale or unrelated observation accepted")
				}
				return
			}
			if err != nil || it.FollowUp.HeadSHA != newHead || it.HeadCommit != old || it.Stream.Spent != 3 || it.FollowUp.Attempt != 2 {
				t.Fatalf("fresh observation changed checked evidence or attempt: %v", err)
			}
		})
	}
}

func TestPendingDecisionIdentitySurvivesTheWorkerReportBoundary(t *testing.T) {
	now := time.Now().UTC()
	cur := factory.Item{ID: 7, State: factory.StateNeedsYou, Question: "plan asks: Merge both?", QKind: "step", Asking: &factory.Asked{Stage: "plan", Question: "Merge both?", With: factory.AskedYou, At: now}, Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseWaiting, Round: 1}}}}
	prev := cur
	prev.State = factory.StateRunning
	prev.Question = ""
	prev.Asking = nil
	report, ok := Diff(prev, cur)
	if !ok {
		t.Fatal("no question report")
	}
	roundTrip := api.ToWorkerReport(api.FromRunReport(report))
	stored := prev
	if err := ApplyReport(&stored, roundTrip, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if factory.PendingQuestionID(stored) != factory.PendingQuestionID(cur) || !stored.Asking.At.Equal(now) {
		t.Fatal("wire formatting or arrival time changed the decision identity")
	}
}
