package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFollowPRProgressIsPlainOrderedBoundedAndStopsWithTheRound(t *testing.T) {
	for _, ending := range []string{"cancel", "manual", "round", "stopped"} {
		t.Run(ending, func(t *testing.T) {
			now := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
			talk := newFakeTalk()
			g := newRig(t, nil, func(o *Options) { o.Talk = talk; o.Clock = func() time.Time { return now } })
			st := chat("followpr")
			id := g.add("repair", st)
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.Talk = "job-manager"
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "followpr", State: factory.PhaseRunning, Round: 1}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &loopCtl{id: id, ctx: ctx}
			lp := g.r.loop()
			if lp.followProgress(c, chat("plan"), 0, 1) != nil {
				t.Fatal("unrelated stages acquired a progress feed")
			}
			progress := lp.followProgress(c, st, 0, 1)
			progress("I found the failing dependency check.")
			progress("I found the failing dependency check.")
			progress("Too soon to repeat the tool narration.")
			now = now.Add(followProgressEvery)
			progress("I updated the dependency and am running the focused checks.\n\nLong worker-only detail.")
			lines := talk.lines("job-manager")
			if len(lines) != 2 || strings.Contains(strings.Join(lines, " "), "worker-only") {
				t.Fatalf("progress: %q", lines)
			}
			for _, line := range lines {
				if step, _ := talk.stepOf("job-manager", line); step != nil {
					t.Fatal("progress was folded into the running step instead of plain prose")
				}
			}
			it, _ := g.st.Get(id)
			if len(it.Stream.Log) != 2 {
				t.Fatalf("updates not retained in order: %+v", it.Stream.Log)
			}
			now = now.Add(followProgressEvery)
			if ending == "cancel" {
				cancel()
			} else {
				if err := g.st.Update(id, func(it *factory.Item) error {
					switch ending {
					case "manual":
						it.Manual = true
					case "round":
						it.Stream.Phases[0].Round = 2
					case "stopped":
						it.State = factory.StateNew
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			progress("A stale callback must never report work still underway.")
			if len(talk.lines("job-manager")) != 2 {
				t.Fatal("late progress escaped after the round stopped or changed")
			}
		})
	}
}

func TestFollowPRProgressStartNamesTheObservedTriggerAndReadiness(t *testing.T) {
	j := Job{Stage: chat("followpr"), Item: factory.Item{PullRequest: &factory.PullRequest{CheckRuns: []factory.CheckRun{{Name: "npm audit", State: "failure"}}}}}
	if !strings.Contains(followProgressStart(j), "npm audit") {
		t.Fatal("CI trigger was not named")
	}
	j.Item.PullRequest.CheckRuns = nil
	j.Item.PullRequest.Feedback = "a review finding"
	if !strings.Contains(followProgressStart(j), "Assessing the review") {
		t.Fatal("feedback trigger missing")
	}
	j.Item.FollowUp = &factory.PRFollowUp{Kind: "readiness"}
	if !strings.Contains(followProgressStart(j), "before marking it ready") {
		t.Fatal("readiness was misreported as a repair")
	}
	if followProgressBrief(Job{Stage: chat("plan")}) != "" {
		t.Fatal("other briefs changed")
	}
	for _, word := range []string{"ordinary assistant", "verification", "service", "stage_result"} {
		if !strings.Contains(followProgressBrief(j), word) {
			t.Fatal("missing progress instruction: " + word)
		}
	}
}

func TestFollowPRProgressTextStaysBriefWithoutProtocolOrEmptyNoise(t *testing.T) {
	for _, text := range []string{"", "done.", "OK!", "```json\n{}", "{\"done\":true}", "<｜DSML｜invoke>"} {
		if followProgressText(text) != "" {
			t.Fatalf("noise relayed: %q", text)
		}
	}
	if n := len([]rune(followProgressText(strings.Repeat("é", summaryMost*3)))); n > summaryMost {
		t.Fatalf("unbounded summary: %d", n)
	}
}

func TestFollowPRProgressReachesTheConversationSpecWithoutChangingTheReport(t *testing.T) {
	var updates []string
	j := Job{Item: factory.Item{ID: 12}, Stage: chat("followpr"), Round: 1, Progress: func(text string) { updates = append(updates, text) }}
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		if c.Spec.Progress == nil {
			return
		}
		c.Spec.Progress("The regression test is running.")
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Checked the finding against current code"}})
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), j)
	if err != nil || !res.Done || res.Repair == nil || len(updates) != 1 || !strings.Contains(maker.Opened()[0].Spec.Brief, "ordinary assistant") {
		t.Fatalf("report=%+v updates=%q err=%v", res, updates, err)
	}
}

func TestFollowPRProgressPublicationMessagesRequireConfirmedOutcomes(t *testing.T) {
	for _, mode := range []string{"initial", "repair", "failed"} {
		t.Run(mode, func(t *testing.T) {
			g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix the issue"})
			talk := newFakeTalk()
			g.r.opts.Talk = talk
			old := mustGit(t, g.dir, "rev-parse", "HEAD")
			mustGit(t, g.dir, "add", "ledger.go")
			mustGit(t, g.dir, "commit", "-q", "-m", "verified repair")
			head := mustGit(t, g.dir, "rev-parse", "HEAD")
			pr := factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: old}
			if mode != "initial" {
				g.r.opts.Source = func(string) factory.Source { return ciPublishSource{g.src, pr} }
			}
			if mode == "failed" {
				g.r.opts.Push = func(string, string) error { return errors.New("permission denied") }
			}
			if err := g.st.Update(g.id, func(it *factory.Item) error {
				it.Talk = "manager"
				it.HeadCommit = head
				if mode != "initial" {
					it.PullRequest = &pr
					it.FollowUp = &factory.PRFollowUp{Kind: "pr", HeadSHA: old, State: "waiting_approval"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Publish(g.id, testTrailer+" "+head); err != nil {
				t.Fatal(err)
			}
			g.settle(t)
			lines := strings.Join(talk.lines("manager"), "\n")
			switch mode {
			case "initial":
				if !strings.Contains(lines, "PR available:") || strings.Contains(lines, "PR updated:") {
					t.Fatal(lines)
				}
			case "repair":
				if !strings.Contains(lines, "PR updated:") || !strings.Contains(lines, "Waiting for GitHub CI") {
					t.Fatal(lines)
				}
			case "failed":
				if !strings.Contains(lines, "PR update was not confirmed:") || strings.Contains(lines, "PR updated:") {
					t.Fatal(lines)
				}
			}
		})
	}
}

func TestFollowPRProgressStaysWithItsJob(t *testing.T) {
	talk := newFakeTalk()
	g := newRig(t, nil, func(o *Options) { o.Talk = talk })
	st := chat("followpr")
	for _, name := range []string{"issue-58", "issue-59"} {
		id := g.add(name, st)
		if err := g.st.Update(id, func(it *factory.Item) error {
			it.State = factory.StateRunning
			it.Talk = name
			it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: st.Name, State: factory.PhaseRunning, Round: 1}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g.r.loop().followProgress(&loopCtl{id: id, ctx: ctx}, st, 0, 1)("Investigating " + name)
	}
	for _, name := range []string{"issue-58", "issue-59"} {
		lines := talk.lines(name)
		if len(lines) != 1 || lines[0] != "Follow PR: Investigating "+name {
			t.Fatalf("job identity mixed: %s %q", name, lines)
		}
	}
}
