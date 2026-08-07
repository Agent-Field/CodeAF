package head

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

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
