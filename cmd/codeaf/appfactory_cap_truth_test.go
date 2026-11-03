package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// The runner's public spending answer and the desk's budget action promise
// the same next cap, including when a round spent past the old cap.
func TestAppFactoryCapQuestionHasOneAmount(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	var sessions appwire.FactorySessionsTaken
	alice.w.must("factory.sessions_take", struct{}{}, &sessions)
	if len(sessions.Changes) != 1 || sessions.Changes[0].Token == "" {
		t.Fatal("fixture has no signed-in session")
	}
	for _, tc := range []struct {
		cap, spent, next float64
		question         string
	}{
		{.01, .01, 5, "budget of $0.01 reached · $0.01 job total spent · raise it to $5, or stop?"},
		{.01, 5.25, 10, "budget of $0.01 reached · $5.25 job total spent · raise it to $10, or stop?"},
		{5, 10, 20, "budget of $5 reached · $10 job total spent · raise it to $20, or stop?"},
		{10, 20, 30, "budget of $10 reached · $20 job total spent · raise it to $30, or stop?"},
		{20, 30, 40, "budget of $20 reached · $30 job total spent · raise it to $40, or stop?"},
		{30, 31, 60, "budget of $30 reached · $31 job total spent · raise it to $60, or stop?"},
		{30, 125, 240, "budget of $30 reached · $125 job total spent · raise it to $240, or stop?"},
	} {
		t.Run(fmt.Sprintf("cap=%g/spent=%g", tc.cap, tc.spent), func(t *testing.T) {
			it := factory.Item{State: factory.StateNeedsYou, Starter: "alice", Cap: tc.cap, QKind: "cap", Stream: &factory.Stream{Spent: tc.spent}}
			question, _ := (*factoryrun.Money)(nil).CapQuestion(it)
			if question != tc.question {
				t.Errorf("question = %q, want %q", question, tc.question)
			}
			// Old saved words must use the current next-cap rule.
			it.Question = "budget reached · more, or stop?"
			saved := f.create(t, it)
			job := alice.job(t, saved.ID)
			if job.SpendingLimit == nil || job.SpendingLimit.NextLimit != tc.next {
				t.Errorf("wire spending limit = %+v, want next %g", job.SpendingLimit, tc.next)
			}
			if len(job.Questions) != 1 || job.Questions[0].NextLimit != tc.next {
				t.Fatalf("question metadata = %+v", job.Questions)
			}
			request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1/jobs/%d/answer", f.URL, saved.ID), strings.NewReader(`{"yes":true}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+sessions.Changes[0].Token)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			answered := alice.job(t, saved.ID)
			continued := it
			changed := factoryrun.NoteContinue(&continued, time.Now())
			if response.StatusCode != 200 || answered.Budget != tc.next {
				t.Errorf("yes status=%d body=%s budget=%g want %g", response.StatusCode, body, answered.Budget, tc.next)
			}
			saved = f.create(t, it)
			if tc.cap == 30 && tc.spent == 31 {
				alice.w.mustFail("factory.job_set_budget", alice.params(t, saved.ID, map[string]any{"budget": 50}), appwire.CodeBadParams)
			}
			alice.act(t, "factory.job_set_budget", saved.ID, map[string]any{"budget": tc.next})
			raised := alice.job(t, saved.ID)
			if !changed || continued.Cap != tc.next || raised.Budget != continued.Cap {
				t.Errorf("yes cap=%g, dock cap=%g, want %g", continued.Cap, raised.Budget, tc.next)
			}
			if factoryrun.NoteContinue(&continued, time.Now()) {
				t.Error("second continue raised the cap again")
			}
		})
	}
}
