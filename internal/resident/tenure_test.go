package resident

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestProbationProposalDoesNotSpliceUntilPendingApprovalAndPromotesAfterGreen(t *testing.T) {
	t.Setenv("AFORGE_TENURE_AFTER", "1")
	graph := openStore(t)
	charter := residentTestCharter(t, "probation-approval", store.CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3,
	}, false)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}

	compileCalls, planCalls := 0, 0
	reconciler := New(graph,
		func(_ context.Context, instruction, _ string) (Compiled, error) {
			compileCalls++
			return Compiled{Goal: instruction}, nil
		},
		func(_ context.Context, compiled Compiled) (store.Subtree, error) {
			planCalls++
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID: "approved-probation-job", Brief: compiled.Goal, Stage: 1,
			}}}, nil
		}).WithWatchEngine(0, func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
		return SentinelVerdict{Yes: true, Line: "condition is green"}, nil
	})
	stored, _, _ := graph.Charter(charter.ID)
	watchNow := stored.NextDue.Add(time.Second)
	reconciler.now = func() time.Time { return watchNow }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pass.Proposed != 1 || pass.Fired != 0 || compileCalls != 0 || planCalls != 0 {
		t.Fatalf("probation pass=%+v compile=%d plan=%d", pass, compileCalls, planCalls)
	}
	if _, found, err := graph.Node("approved-probation-job"); err != nil || found {
		t.Fatalf("proposal spliced work found=%t err=%v", found, err)
	}
	stored, _, _ = graph.Charter(charter.ID)
	if !stored.WakePending || !stored.SentinelYes || stored.Autonomy != store.CharterProbation {
		t.Fatalf("proposal wake = %+v", stored)
	}
	messages, err := graph.Messages("charter-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	question := findTenureQuestion(t, messages, charter.ID)
	if !strings.Contains(question.Body, "I would have done") || !strings.Contains(question.Body, "approve?") || len(question.Options) != 4 {
		t.Fatalf("proposal question = %+v", question)
	}

	command, err := graph.RequestCommand(store.Command{
		SessionID: "charter-session", Kind: store.CommandCharterFire, Target: charter.ID,
		Instruction: "wake:" + formatWakeSeq(stored.WakeSeq),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved := commandBySeq(t, graph, command.Seq)
	if resolved.Status != store.CommandApplied || compileCalls != 1 || planCalls != 1 {
		t.Fatalf("approval command=%+v compile=%d plan=%d", resolved, compileCalls, planCalls)
	}
	stored, _, _ = graph.Charter(charter.ID)
	if stored.GreenFirings != 0 || stored.Autonomy != store.CharterProbation {
		t.Fatalf("unverified firing advanced ladder: %+v", stored)
	}
	job, found, err := graph.Node("approved-probation-job")
	if err != nil || !found || job.Provenance.Origin != store.OriginTrigger || job.Provenance.CharterID != charter.ID {
		t.Fatalf("approved job=%+v found=%t err=%v", job, found, err)
	}
	claim, won, err := graph.Claim(job.ID, "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "independently verified green"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, _, _ = graph.Charter(charter.ID)
	if stored.Autonomy != store.CharterTenured || stored.GreenFirings != 1 {
		t.Fatalf("verified promotion = %+v", stored)
	}
}

func TestProbationAlwaysAllowPromotesAndNeverPauses(t *testing.T) {
	for _, test := range []struct {
		name       string
		kind       store.CommandKind
		wantStatus store.CharterStatus
		wantGrade  store.CharterAutonomy
		wantFired  bool
	}{
		{name: "not now", kind: store.CommandCharterDecline, wantStatus: store.CharterActive, wantGrade: store.CharterProbation},
		{name: "always allow", kind: store.CommandCharterAlways, wantStatus: store.CharterActive, wantGrade: store.CharterTenured, wantFired: true},
		{name: "never", kind: store.CommandCharterNever, wantStatus: store.CharterPaused, wantGrade: store.CharterProbation},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			charter := residentTestCharter(t, "probation-"+strings.ReplaceAll(test.name, " ", "-"), store.CharterRails{
				PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3,
			}, true)
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			reconciler := New(graph, nil, nil).WithWatchEngine(0,
				func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
					return SentinelVerdict{Yes: true}, nil
				})
			stored, _, _ := graph.Charter(charter.ID)
			watchNow := stored.NextDue.Add(time.Second)
			reconciler.now = func() time.Time { return watchNow }
			if pass, err := reconciler.WatchOnce(context.Background()); err != nil || pass.Proposed != 1 {
				t.Fatalf("proposal pass=%+v err=%v", pass, err)
			}
			stored, _, _ = graph.Charter(charter.ID)
			command, err := graph.RequestCommand(store.Command{
				SessionID: "charter-session", Kind: test.kind, Target: charter.ID,
				Instruction: "wake:" + formatWakeSeq(stored.WakeSeq),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if resolved := commandBySeq(t, graph, command.Seq); resolved.Status != store.CommandApplied {
				t.Fatalf("command = %+v", resolved)
			}
			stored, _, _ = graph.Charter(charter.ID)
			if stored.Status != test.wantStatus || stored.Autonomy != test.wantGrade || stored.WakePending {
				t.Fatalf("charter after %s = %+v", test.name, stored)
			}
			if fired := countTenureEvents(t, graph, store.EventCharterFired) > 0; fired != test.wantFired {
				t.Fatalf("fired=%t want %t", fired, test.wantFired)
			}
			if test.kind == store.CommandCharterAlways && countTenureEvents(t, graph, store.EventCharterPromoted) != 1 {
				t.Fatal("always allow did not journal promotion")
			}
			if test.kind == store.CommandCharterNever && countTenureEvents(t, graph, store.EventCharterFiringDeclined) != 1 {
				t.Fatal("never did not journal decline")
			}
			if test.kind == store.CommandCharterDecline && countTenureEvents(t, graph, store.EventCharterFiringDeclined) != 1 {
				t.Fatal("not now did not journal decline")
			}
		})
	}
}

func findTenureQuestion(t *testing.T, messages []store.Message, charterID string) store.Message {
	t.Helper()
	for _, message := range messages {
		if message.NodeID == charterID && len(message.Options) > 0 {
			return message
		}
	}
	t.Fatal("probation question not found")
	return store.Message{}
}

func countTenureEvents(t *testing.T, graph *store.Store, kind store.EventKind) int {
	t.Helper()
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func formatWakeSeq(seq int64) string {
	return strconv.FormatInt(seq, 10)
}
