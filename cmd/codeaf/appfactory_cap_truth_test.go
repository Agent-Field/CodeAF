package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// The runner's public spending answer and the desk's budget action promise
// the same next cap, including when a round spent past the old cap.
func TestAppFactoryCapQuestionHasOneAmount(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	for _, tc := range []struct{ cap, spent, next float64 }{
		{.01, .01, 5}, {.01, 5.25, 10}, {5, 10, 20}, {10, 20, 30}, {20, 30, 0}, {30, 31, 0},
	} {
		t.Run(fmt.Sprintf("cap=%g/spent=%g", tc.cap, tc.spent), func(t *testing.T) {
			it := factory.Item{State: factory.StateNeedsYou, Starter: "alice", Cap: tc.cap, QKind: "cap", Stream: &factory.Stream{Spent: tc.spent}}
			question, _ := (*factoryrun.Money)(nil).CapQuestion(it)
			tail := fmt.Sprintf("raise it to $%g, or stop?", tc.next)
			if tc.next == 0 {
				tail = "no higher budget is available; stop this job."
			}
			want := fmt.Sprintf("budget of $%.2f reached · $%.2f spent · %s", tc.cap, tc.spent, tail)
			// Whole-dollar amounts use the same compact spelling as the runner.
			if tc.cap == float64(int(tc.cap)) {
				want = fmt.Sprintf("budget of $%g reached · $%.2f spent · %s", tc.cap, tc.spent, tail)
			}
			if tc.spent == float64(int(tc.spent)) {
				want = fmt.Sprintf("budget of $%g reached · $%g spent · %s", tc.cap, tc.spent, tail)
			}
			if question != want {
				t.Errorf("question = %q, want %q", question, want)
			}
			// Old saved words must not preserve the old doubling behavior.
			it.Question = "budget reached · more, or stop?"
			saved := f.create(t, it)
			job := alice.job(t, saved.ID)
			if job.SpendingLimit == nil || job.SpendingLimit.NextLimit != tc.next {
				t.Errorf("wire spending limit = %+v, want next %g", job.SpendingLimit, tc.next)
			}
			continued := it
			changed := factoryrun.NoteContinue(&continued, time.Now())
			if tc.next == 0 {
				if changed || continued.Cap != tc.cap {
					t.Errorf("exhausted yes changed cap to %g", continued.Cap)
				}
				return
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
