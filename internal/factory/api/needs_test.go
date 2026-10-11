package api

import (
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestNeedsYouKindsAreTheSix(t *testing.T) {
	want := []string{
		"approve_plan", "approve_proof", "question", "spending_limit", "out_of_attempts", "publish_failed",
	}
	if !reflect.DeepEqual(factory.NeedsYouKinds, want) {
		t.Fatalf("kinds = %v", factory.NeedsYouKinds)
	}
	seen := map[string]bool{}
	for _, kind := range factory.NeedsYouKinds {
		if seen[kind] {
			t.Fatalf("duplicate kind %s", kind)
		}
		seen[kind] = true
	}
}

func TestNeedsYouKindsQuestionsAndWhoMayAnswer(t *testing.T) {
	when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	step := &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "review", At: when}}}
	cases := []struct {
		name string
		item factory.Item
		kind string
		who  string
		form string
	}{
		{"plan", factory.Item{ID: 1, State: factory.StateNeedsYou, Question: "ready?", QKind: factory.QKindApprove, Changed: when}, factory.NeedsApprovePlan, factory.WhoJobPerson, ""},
		{"proof", factory.Item{ID: 2, State: factory.StateLanded, Changed: when, Stream: &factory.Stream{Ended: when}}, factory.NeedsApproveProof, factory.WhoJobPerson, ""},
		{"publish", factory.Item{ID: 3, State: factory.StateLanded, PublishError: "protected", Changed: when}, factory.NeedsPublishFailed, factory.WhoJobPerson, ""},
		{"cap", factory.Item{ID: 4, State: factory.StateNeedsYou, Question: "budget?", QKind: "cap", Cap: 5, Starter: "raj", Changed: when, Stream: &factory.Stream{Spent: 5, Cur: 0, Phases: []factory.Phase{{Name: "write"}}}}, factory.NeedsSpendingLimit, factory.WhoJobPerson, ""},
		{"rounds", factory.Item{ID: 5, State: factory.StateNeedsYou, Question: "try once more, send it back, or cancel the job?", QKind: factory.QKindOutOfAttempts, Starter: "raj", Stream: step}, factory.NeedsOutOfAttempts, factory.WhoJobPerson, factory.AskOptions},
		{"text", factory.Item{ID: 6, State: factory.StateNeedsYou, Question: "which file?", QKind: "step", Stream: step}, factory.NeedsQuestion, factory.WhoAnyoneWithRepo, factory.QuestionText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := FromItem(tc.item)
			if j.NeedsYou == nil || j.NeedsYou.Kind != tc.kind || j.NeedsYou.WhoCanAnswer != tc.who {
				t.Fatalf("needs_you = %+v", j.NeedsYou)
			}
			if tc.form == "" {
				if j.NeedsYou.Question != nil {
					t.Fatalf("question = %+v", j.NeedsYou.Question)
				}
				return
			}
			q := j.NeedsYou.Question
			if q == nil || q.ID != "q"+itoa(tc.item.ID) || q.Kind != tc.form || q.WhoCanAnswer != tc.who || q.Text == "" {
				t.Fatalf("question = %+v", q)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestSpendingLimitIsTheDoubledCap(t *testing.T) {
	j := FromItem(factory.Item{
		ID: 4, State: factory.StateNeedsYou, Question: "budget of $5 reached", QKind: "cap", Cap: 5, Starter: "raj",
		Stream: &factory.Stream{Spent: 5, Cur: 0, Phases: []factory.Phase{{Name: "write"}}},
	})
	if j.SpendingLimit == nil || j.SpendingLimit.Limit != 5 || j.SpendingLimit.Spent != 5 || j.SpendingLimit.NextLimit != 10 || j.SpendingLimit.Currency != "USD" {
		t.Fatalf("spending = %+v", j.SpendingLimit)
	}
	if j.NeedsYou == nil || j.NeedsYou.Kind != factory.NeedsSpendingLimit || j.NeedsYou.Question != nil {
		t.Fatalf("needs_you = %+v", j.NeedsYou)
	}
	if len(j.Questions) != 1 || j.Questions[0].Kind != "cap" || j.Questions[0].NextLimit != 10 {
		t.Fatalf("legacy cap question = %+v", j.Questions)
	}
}

func TestOutOfAttemptsIsAQuestionAndFailureIsNot(t *testing.T) {
	rounds := FromItem(factory.Item{
		ID: 5, State: factory.StateNeedsYou, Starter: "@Raj",
		Question: "review is not clean after 2 rounds: 2 findings · try once more, send it back, or cancel the job?",
		QKind:    factory.QKindOutOfAttempts,
		Stream:   &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "review"}}},
	})
	q := rounds.NeedsYou.Question
	if q == nil || q.Kind != "options" || q.Asks != "raj" || q.FromStep != "review" || q.WhoCanAnswer != factory.WhoJobPerson {
		t.Fatalf("rounds question = %+v", q)
	}
	if !reflect.DeepEqual(q.Options, []string{"try_once_more", "send_back", "cancel_job"}) {
		t.Fatalf("options = %v", q.Options)
	}
	when := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	failed := FromItem(factory.Item{
		ID: 8, State: factory.StateNew,
		Failure: &factory.Failure{Step: "write", Code: factory.CodeToolError, Detail: "the worktree is gone", Next: factory.FailureStopped, At: when},
	})
	if failed.NeedsYou != nil || len(failed.Questions) != 0 {
		t.Fatalf("a failure became a question: %+v %+v", failed.NeedsYou, failed.Questions)
	}
	if failed.Failure == nil || failed.Failure.Code != factory.CodeToolError || failed.Failure.Next != factory.FailureStopped || !failed.Failure.At.Equal(when) {
		t.Fatalf("failure = %+v", failed.Failure)
	}
	back := ToItem(failed)
	if back.Failure == nil || back.Failure.Detail != "the worktree is gone" || back.Failure.Step != "write" {
		t.Fatalf("failure round trip = %+v", back.Failure)
	}
}

func TestAskWithoutAnIssueAndFromAnIssue(t *testing.T) {
	when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	local := FromItem(factory.Item{
		ID: 8, Title: "meter crashes", Kind: factory.KindIssue, Origin: factory.OriginTerminal, Num: 1540,
		Ask: &factory.JobAsk{Text: "Written in the terminal.", By: "santosh", At: when, Source: factory.AskTUI},
	})
	if local.Ask == nil || local.Ask.Issue != nil || local.Ask.Source != "tui" || local.Ask.Text != "Written in the terminal." || local.Ask.By != "santosh" || local.Ask.At == nil || !local.Ask.At.Equal(when) {
		t.Fatalf("local ask = %+v", local.Ask)
	}
	if back := ToItem(local); back.Ask == nil || back.Ask.Source != factory.AskTUI || back.Ask.Text != "Written in the terminal." {
		t.Fatalf("local ask round trip = %+v", back.Ask)
	}
	issue := FromItem(factory.Item{
		ID: 1, Kind: factory.KindIssue, Origin: factory.OriginForge, Num: 1538,
		URL: "https://github.com/agentfield/codeaf/issues/1538", Title: "budget caps per task", Body: "Cap each task.",
	})
	if issue.Ask == nil || issue.Ask.Issue == nil || issue.Ask.Issue.Number != 1538 || issue.Ask.Issue.URL == "" || issue.Ask.Issue.Title != "budget caps per task" || issue.Ask.Text != "Cap each task." || issue.Ask.Source != "" {
		t.Fatalf("issue ask = %+v", issue.Ask)
	}
	if back := ToItem(issue); back.Ask != nil {
		t.Fatalf("derived issue ask was stored: %+v", back.Ask)
	}
}

func TestQueueGroupsByNeedsYouKind(t *testing.T) {
	when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s := factory.Snapshot{Items: []factory.Item{
		{ID: 3, State: factory.StateRunning},
		{ID: 1, State: factory.StateNeedsYou, Question: "ready?", QKind: factory.QKindApprove, Changed: when},
		{ID: 9, State: factory.StateLanded, Stream: &factory.Stream{Ended: when}},
		{ID: 4, State: factory.StateNeedsYou, Question: "budget?", QKind: "cap", Cap: 5, Changed: when, Stream: &factory.Stream{Spent: 5}},
		{ID: 2, State: factory.StateNeedsYou, Question: "ready too?", QKind: factory.QKindApprove, Changed: when},
	}}
	q := FromSnapshot(s, "me")
	var got []NeedsYouGroup
	for _, g := range q.NeedsYou {
		got = append(got, g)
	}
	want := []NeedsYouGroup{
		{Kind: factory.NeedsApprovePlan, Jobs: []int{1, 2}},
		{Kind: factory.NeedsApproveProof, Jobs: []int{9}},
		{Kind: factory.NeedsSpendingLimit, Jobs: []int{4}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %+v", got)
	}
	if len(FromSnapshot(factory.Snapshot{Items: []factory.Item{{ID: 3, State: factory.StateRunning}}}, "me").NeedsYou) != 0 {
		t.Fatal("a queue with nothing waiting grouped it")
	}
}

func TestFixtureNeedsYouAndAsks(t *testing.T) {
	q := FromSnapshot(factory.Fixture(at), "me")
	var plan, proof Job
	var sawPlan, sawProof bool
	for _, j := range q.Jobs {
		switch j.ID {
		case 1:
			plan, sawPlan = j, true
		case 9:
			proof, sawProof = j, true
		case 8:
			if j.Ask == nil || j.Ask.Issue != nil || j.Ask.Source != factory.AskTUI || j.Ask.Text == "" {
				t.Fatalf("job 8 ask = %+v", j.Ask)
			}
		case 6:
			if j.Ask == nil || j.Ask.Issue == nil || j.Ask.Issue.Number != 31 {
				t.Fatalf("job 6 ask = %+v", j.Ask)
			}
		}
	}
	if !sawPlan || plan.NeedsYou == nil || plan.NeedsYou.Kind != factory.NeedsApprovePlan {
		t.Fatalf("job 1 = %+v", plan.NeedsYou)
	}
	if !sawProof || proof.NeedsYou == nil || proof.NeedsYou.Kind != factory.NeedsApproveProof || !proof.Ready {
		t.Fatalf("job 9 = %+v ready %v", proof.NeedsYou, proof.Ready)
	}
	if len(q.Jobs) != 10 {
		t.Fatalf("jobs = %d", len(q.Jobs))
	}
	if len(q.NeedsYou) != 2 || q.NeedsYou[0].Kind != factory.NeedsApprovePlan || q.NeedsYou[1].Kind != factory.NeedsApproveProof {
		t.Fatalf("fixture groups = %+v", q.NeedsYou)
	}
}
