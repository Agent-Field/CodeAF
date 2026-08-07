package head

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestProbationApprovalFromQuestionDockFiresCharter(t *testing.T) {
	graph := openHeadStore(t)
	charter, _, _ := proposeHeadTestFiring(t, graph, "dock-approval", "charter-origin")
	questions, err := graph.PendingQuestions("live-dock", 0)
	if err != nil || len(questions) != 1 {
		t.Fatalf("pending questions = %+v err=%v", questions, err)
	}
	question := questions[0]
	if surfaced, err := graph.SurfaceQuestionForSession(question.Seq, "live-dock"); err != nil {
		t.Fatal(err)
	} else if surfaced.SessionID != "live-dock" {
		t.Fatalf("dock surfaced question into %q", surfaced.SessionID)
	}
	user, err := graph.PostMessage(store.Message{
		SessionID: "live-dock", Role: store.RoleUser, Body: "yes", QuestionSeq: question.Seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if client.callCount() != 0 {
		t.Fatal("dock approval fell through to provider")
	}
	resolved, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || resolved.Status != store.QuestionAnswered {
		t.Fatalf("resolved question = %+v found=%t err=%v", resolved, found, err)
	}
	if err := headTestFiringReconciler(graph, charter.ID).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countHeadTenureEvents(t, graph, store.EventCharterFired); got != 1 {
		t.Fatalf("charter firing events = %d, want 1", got)
	}
}

func TestProbationMessageOptionResolvesDockQuestion(t *testing.T) {
	graph := openHeadStore(t)
	_, _, proposal := proposeHeadTestFiring(t, graph, "message-approval", "charter-origin")
	user, err := graph.PostMessage(store.Message{
		SessionID: "live-message", Role: store.RoleUser, Body: "approve this time",
		QuestionSeq: proposal.QuestionSeq,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	resolved, found, err := graph.AgentQuestionBySeq(proposal.QuestionSeq)
	if err != nil || !found || resolved.Status != store.QuestionAnswered || resolved.AnswerMessageSeq != user.Seq {
		t.Fatalf("resolved question = %+v found=%t err=%v", resolved, found, err)
	}
	if pending, err := graph.PendingQuestions("live-message", 0); err != nil || len(pending) != 0 {
		t.Fatalf("resolved proposal remained in dock: %+v err=%v", pending, err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterFire {
		t.Fatalf("approval commands = %+v err=%v", commands, err)
	}
}

func TestEmptySessionProbationProposalIsAnswerableFromLiveSession(t *testing.T) {
	graph := openHeadStore(t)
	charter, wakeSeq, proposal := proposeHeadTestFiring(t, graph, "neutral-proposal", "")
	if proposal.SessionID != "" {
		t.Fatalf("proposal session = %q, want neutral", proposal.SessionID)
	}
	user, err := graph.PostMessage(store.Message{SessionID: "live-session", Role: store.RoleUser, Body: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if client.callCount() != 0 {
		t.Fatal("neutral proposal fell through to provider")
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Target != charter.ID ||
		commands[0].Instruction != "wake:"+strconv.FormatInt(wakeSeq, 10) {
		t.Fatalf("neutral proposal commands = %+v err=%v", commands, err)
	}
	resolved, found, err := graph.AgentQuestionBySeq(proposal.QuestionSeq)
	if err != nil || !found || resolved.Status != store.QuestionAnswered {
		t.Fatalf("neutral question = %+v found=%t err=%v", resolved, found, err)
	}
}

func TestRacingDockAndMessageAnswersFireProbationWakeExactlyOnce(t *testing.T) {
	graph := openHeadStore(t)
	charter, _, proposal := proposeHeadTestFiring(t, graph, "racing-approval", "charter-origin")
	if _, err := graph.SurfaceQuestionForSession(proposal.QuestionSeq, "dock-session"); err != nil {
		t.Fatal(err)
	}
	messageAnswer, err := graph.PostMessage(store.Message{
		SessionID: "message-session", Role: store.RoleUser, Body: "approve this time",
		QuestionSeq: proposal.QuestionSeq,
	})
	if err != nil {
		t.Fatal(err)
	}
	dockAnswer, err := graph.PostMessage(store.Message{
		SessionID: "dock-session", Role: store.RoleUser, Body: "yes", QuestionSeq: proposal.QuestionSeq,
	})
	if err != nil {
		t.Fatal(err)
	}

	head := New(&fakeClient{}, graph)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, answer := range []store.Message{messageAnswer, dockAnswer} {
		answer := answer
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- head.answer(context.Background(), answer)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("racing answer: %v", err)
		}
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterFire {
		t.Fatalf("racing approval commands = %+v err=%v", commands, err)
	}
	if err := headTestFiringReconciler(graph, charter.ID).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countHeadTenureEvents(t, graph, store.EventCharterFired); got != 1 {
		t.Fatalf("racing charter firing events = %d, want 1", got)
	}
	resolved, found, err := graph.AgentQuestionBySeq(proposal.QuestionSeq)
	if err != nil || !found || resolved.Status != store.QuestionAnswered {
		t.Fatalf("racing question = %+v found=%t err=%v", resolved, found, err)
	}
}

func TestProbationQuestionChoicesUsePendingCharterCommands(t *testing.T) {
	for _, test := range []struct {
		reply    string
		wantKind store.CommandKind
	}{
		{reply: "yes", wantKind: store.CommandCharterFire},
		{reply: "no", wantKind: store.CommandCharterDecline},
		{reply: "always allow", wantKind: store.CommandCharterAlways},
		{reply: "never", wantKind: store.CommandCharterNever},
	} {
		t.Run(test.reply, func(t *testing.T) {
			graph := openHeadStore(t)
			charter, err := store.NewCharter("probation-choice", "Keep releases documented.", store.WatchSpec{
				Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
			}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
				store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
				store.Ratification{Origin: store.OriginUser, SessionID: "tenure-head", Evidence: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			wakeSeq, err := graph.BeginCharterWake(charter.ID, time.Now(), "release found", store.CharterWatchState{
				NextDue: time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.RecordSentinelCheck(charter.ID, store.SentinelCheck{WakeSeq: wakeSeq, Yes: true}); err != nil {
				t.Fatal(err)
			}
			if posted, err := graph.ProposeCharterFiring(charter.ID, wakeSeq, charter.Action.Template); err != nil || !posted {
				t.Fatalf("proposal posted=%t err=%v", posted, err)
			}
			user, err := graph.PostMessage(store.Message{SessionID: "tenure-head", Role: store.RoleUser, Body: test.reply})
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{}
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if client.callCount() != 0 {
				t.Fatal("structured probation choice fell through to provider")
			}
			pending, err := graph.PendingCommands(0)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending commands=%+v err=%v", pending, err)
			}
			if pending[0].Kind != test.wantKind || pending[0].Target != charter.ID ||
				pending[0].Instruction != "wake:"+strconv.FormatInt(wakeSeq, 10) {
				t.Fatalf("probation command = %+v", pending[0])
			}
		})
	}
}

func proposeHeadTestFiring(t *testing.T, graph *store.Store, id, charterSession string) (store.Charter, int64, store.Message) {
	t.Helper()
	charter, err := store.NewCharter(id, "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: charterSession, Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	wakeSeq, err := graph.BeginCharterWake(charter.ID, time.Now(), "release found", store.CharterWatchState{
		NextDue: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSentinelCheck(charter.ID, store.SentinelCheck{WakeSeq: wakeSeq, Yes: true}); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.ProposeCharterFiring(charter.ID, wakeSeq, charter.Action.Template); err != nil || !posted {
		t.Fatalf("proposal posted=%t err=%v", posted, err)
	}
	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.NodeID == charter.ID && message.QuestionSeq != 0 && len(message.Options) == 4 {
			return charter, wakeSeq, message
		}
	}
	t.Fatal("proposal message not found")
	return store.Charter{}, 0, store.Message{}
}

func headTestFiringReconciler(graph *store.Store, charterID string) *resident.Reconciler {
	return resident.New(graph,
		func(_ context.Context, instruction, _ string) (resident.Compiled, error) {
			return resident.Compiled{Goal: instruction}, nil
		},
		func(_ context.Context, compiled resident.Compiled) (store.Subtree, error) {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID: charterID + "-approved-job", Brief: compiled.Goal, Stage: 1,
			}}}, nil
		})
}

func countHeadTenureEvents(t *testing.T, graph *store.Store, kind store.EventKind) int {
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
