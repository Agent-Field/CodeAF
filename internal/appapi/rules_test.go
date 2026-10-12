package appapi

import (
	"fmt"
	"testing"
	"time"
)

func TestIdleAgentSelectionUsesMessagesAndNeverSelectsRunningWork(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		at            time.Time
		running, want bool
	}{
		{"unknown", time.Time{}, false, false},
		{"recent", now.Add(-IdleAgentAfter + time.Nanosecond), false, false},
		{"boundary", now.Add(-IdleAgentAfter), false, true},
		{"old", now.Add(-IdleAgentAfter - time.Hour), false, true},
		{"running", now.Add(-IdleAgentAfter - time.Hour), true, false},
		{"future", now.Add(time.Hour), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AgentIsIdle(tc.at, tc.running, now); got != tc.want {
				t.Fatalf("idle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuickTaskProposalBoundaryAndTestRuns(t *testing.T) {
	var files []string
	for n := 0; n < 30; n++ {
		files = append(files, fmt.Sprintf("file-%d.go", n))
	}
	if QuickTaskNeedsProposal(files, false) {
		t.Fatal("30 files require a full task")
	}
	if QuickTaskNeedsProposal(append(files, files[0]), false) {
		t.Fatal("a repeated path counted as another file")
	}
	if !QuickTaskNeedsProposal(append(files, "another.go"), false) {
		t.Fatal("31 files did not require a full task")
	}
	if !QuickTaskNeedsProposal(nil, true) {
		t.Fatal("a test run did not require a full task")
	}
}

func TestAppRuleDefaultsMatchTheProductContract(t *testing.T) {
	if DefaultTaskProposalSeconds != 12 || DefaultTaskCapUSD != 2 || DefaultDayCapUSD != 20 || DefaultStandingRunUSD != 0.40 {
		t.Fatal("app spending or proposal defaults drifted")
	}
	if QuestionClock != 15*time.Minute || ProcessOutputRetention != 7*24*time.Hour || MaxProcessOutputBytes != 10<<20 {
		t.Fatal("question clock or process retention defaults drifted")
	}
	if ScopeThisChat != "this_chat" || ScopeThisCrew != "this_crew" || ScopeEverywhere != "everywhere" || ScopeNotHere != "not_here" {
		t.Fatal("standing scope spellings drifted")
	}
}
