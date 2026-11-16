package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// machineryWords are the words no person may read from codeaf (CLAUDE.md's
// machinery-vocabulary law, and Workstreams v5's words rule). The server's
// agent says what its prompt says, so the prompt may not carry them either:
// on 2026-11-16 it told a person their workstream was "at stage shaping".
var machineryWords = regexp.MustCompile(`(?i)\b(auditor|verdict|verified|refuted|jobs?|stages?|waves?|go-aheads?|autonomy|proposals?|records?)\b`)

func TestServerAgentPromptKeepsMachineryOut(t *testing.T) {
	for _, work := range []bool{false, true} {
		prompt := factoryWorkstreamAgentSystem("Pantry", "priya", work, time.Time{}, "")
		if hit := machineryWords.FindAllString(prompt, -1); len(hit) > 0 {
			t.Errorf("work=%v: the org workstream prompt says %q:\n%s", work, hit, prompt)
		}
		if !strings.Contains(prompt, "never by an id") {
			t.Errorf("work=%v: the prompt no longer tells the agent to keep ids out of its answers", work)
		}
	}
	if p := factoryWorkstreamAgentSystem("Pantry", "priya", true, time.Time{}, ""); !strings.Contains(p, "private to the one person") || !strings.Contains(p, "talking to priya") {
		t.Error("the prompt must say the conversation is private to the person it names (L1 F2)")
	}
	if !strings.Contains(factoryWorkstreamAgentSystem("Pantry", "priya", true, time.Time{}, ""), "ws_task_create") {
		t.Error("with the task tools, the prompt must send asked-for work to ws_task_create, not to a page")
	}
}

func TestWorkstreamReadNeverSaysTheStage(t *testing.T) {
	ws := factory.OrgWorkstream{ID: "ws_1", Name: "Pantry", Stage: "shaping",
		Pages:     []factory.WorkstreamPage{{ID: "pg_1", Title: "Overview", Kind: "page", BodyMD: "hello"}},
		Decisions: []factory.WorkstreamDecision{{ID: "dc_1", Text: "Ship it", State: "proposed", By: "priya"}}}
	got := session.WorkstreamAnswer(ws)
	if strings.Contains(got, "shaping") || machineryWords.MatchString(got) {
		t.Fatalf("read answers machinery words:\n%s", got)
	}
}

// TestServerAgentKnowsTodaysDate pins L3 F-17: the agent was never told the
// date, took one it read in a page for today and planned a week off it. Every
// turn's prompt now carries the date, in the person's own time zone when
// their app sent one, and says which clock it is when it did not.
func TestServerAgentKnowsTodaysDate(t *testing.T) {
	at := time.Date(2026, 11, 17, 3, 30, 0, 0, time.UTC) // Monday evening in Toronto
	p := factoryWorkstreamAgentSystem("Pantry", "priya", true, at, "America/Toronto")
	if !strings.Contains(p, "Monday 16 November 2026, 22:30 EST") || !strings.Contains(p, "the person's time, America/Toronto") {
		t.Errorf("the prompt does not give the person's date and time:\n%s", p)
	}
	for _, tz := range []string{"", "Not/AZone"} {
		p := factoryWorkstreamAgentSystem("Pantry", "priya", true, at, tz)
		if !strings.Contains(p, at.Local().Format("Monday 2 January 2006")) || !strings.Contains(p, "the server's time") {
			t.Errorf("tz %q: the prompt should fall back to the server's own date and say so:\n%s", tz, p)
		}
	}
	if !strings.Contains(p, "list every change you made") {
		t.Error("the prompt must tell the agent to report every change it made, not only the one asked for (L3 F-10)")
	}
}

// A task run's prompt keeps the same words law, and tells the run that only
// what it does to the task is seen, how to finish, and how to give up.
func TestATaskRunsPromptSaysHowItEnds(t *testing.T) {
	p := factoryWorkstreamAgentTaskRun("tk_1", "Fraction table", "mia")
	if hit := machineryWords.FindAllString(p, -1); len(hit) > 0 {
		t.Errorf("the task run's prompt says %q:\n%s", hit, p)
	}
	for _, want := range []string{"mia started you", "Nobody reads what you say here", "set the task done", "set the task failed", "ws_task_comment"} {
		if !strings.Contains(p, want) {
			t.Errorf("the task run's prompt does not say %q:\n%s", want, p)
		}
	}
}
