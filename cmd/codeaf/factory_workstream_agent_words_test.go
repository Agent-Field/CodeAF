package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
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
		// NOTHING IN FILES MENTIONS VERSIONS: the agent is never told its text waits in History to be restored.
		for _, never := range []string{"History", "restore", "version"} {
			if strings.Contains(prompt, never) {
				t.Errorf("work=%v: the prompt says %q", work, never)
			}
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
	for _, want := range []string{"mia started you", "Everyone in the workstream can read what you say and do here", "their words change the work", "set the task done", "set the task failed", "ws_task_comment"} {
		if !strings.Contains(p, want) {
			t.Errorf("the task run's prompt does not say %q:\n%s", want, p)
		}
	}
}

// A STEP IN THE AFI PANE IS NEVER A TOOL'S HANDLE. The organization's Afi
// showed "ws_decisions" as a step between "read the files" and "read the tasks"
// (E1 F10); every step reads as words, and one this build has no words for
// still drops the prefix and the underscores.
func TestFactoryAgentStepsNeverShowAToolsHandle(t *testing.T) {
	for _, tool := range []string{session.WSDecisionsTool, session.WSFileCommentTool, session.WSTasksTool, "ws_something_new", "brand_new_tool"} {
		words := factoryAgentStepWords(session.Event{Tool: tool, Args: `{}`})
		if words == "" || strings.Contains(words, "_") || strings.Contains(words, "ws ") {
			t.Errorf("%s reads %q", tool, words)
		}
	}
	if got := factoryAgentStepWords(session.Event{Tool: session.WSDecisionsTool}); got != "read the decisions" {
		t.Errorf("ws_decisions reads %q", got)
	}
}

// A STEER'S STREAM REPORTS ONLY ITS OWN TURN. Session's Steer answers a live
// view of the turn it was typed into, whose lines the run's main stream
// already reports; only what follows the steer falling through into a turn of
// its own is new. Reporting both would draw every line of the task's Working
// conversation twice.
func TestASteersStreamReportsOnlyTheTurnItStarted(t *testing.T) {
	events := make(chan session.Event, 8)
	events <- session.Event{Kind: session.EventSteerAccepted}
	events <- session.Event{Kind: session.EventTextDelta, Text: "already said by the main stream"}
	events <- session.Event{Kind: session.EventSteerFellThrough}
	events <- session.Event{Kind: session.EventTextDelta, Text: "Using the shorter title."}
	close(events)
	var lines []string
	if err := factoryAgentConsume(events, func(kind, text, _ string, _ bool) { lines = append(lines, kind+":"+text) }, true); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "agent:Using the shorter title." {
		t.Fatalf("a steer's stream reported %q", lines)
	}
}

// EVERY ws_* STEP READS AS WORDS IN A TASK'S WORKING CONVERSATION, which
// everyone in the workstream reads. A step with no words of its own fell back
// to the tool's name, and "ws_decisions" was drawn to people (L4 live run).
func TestEveryWorkstreamStepReadsAsWords(t *testing.T) {
	for _, tool := range session.WorkstreamWorkTools() {
		words := factoryAgentStepWords(session.Event{Tool: tool.Name, Args: "{}"})
		if words == "" || strings.Contains(words, "ws_") || strings.Contains(words, "_") {
			t.Errorf("%s reads %q", tool.Name, words)
		}
	}
}

// A STEER IS NEVER ECHOED ONTO THE TASK. Live on 2026-11-17 Afi recorded the
// owner's steer as a request note, which the thread drew as "Afi asked Afi Mia
// asked to keep the headline under eight words": words everyone had already
// read in the Working conversation, said twice and garbled.
func TestATaskRunNeverEchoesASteerOntoTheTask(t *testing.T) {
	p := factoryWorkstreamAgentTaskRun("tk_1", "Draft the post", "mia")
	if !strings.Contains(p, "never repeat them on the task as a note or a comment") {
		t.Errorf("the task run's words do not forbid echoing a steer: %s", p)
	}
}

// ON THE SERVER A BATCH IS A PLAN, NOT A CHAT (org-space DESIGN §12). Before,
// the server's Afi read the shared rule that a batch is named for "the chat
// that runs them" and promised people a chat that cannot exist on a server.
func TestOnTheServerABatchIsAPlan(t *testing.T) {
	p := factoryWorkstreamAgentSystem("Pantry", "priya", true, time.Time{}, "")
	for _, must := range []string{"a batch is a plan, never a chat", "presses Start, which releases all of it", "never say a chat will run it"} {
		if !strings.Contains(p, must) {
			t.Errorf("the server prompt does not say %q", must)
		}
	}
	if strings.Contains(factoryWorkstreamAgentSystem("Pantry", "priya", false, time.Time{}, ""), "a batch is a plan") {
		t.Error("without the task tools there is no batch to speak of")
	}
}

// MY SPACE'S AGENT IS TOLD THE SPACE IS THE PERSON'S ALONE AND HOLDS NO
// TASKS, in the same plain words as a workstream's (OWNER-DECISIONS §6).
func TestMySpaceAgentIsToldTheSpaceIsPrivateAndHoldsNoTasks(t *testing.T) {
	for _, work := range []bool{false, true} {
		p := factoryWorkstreamAgentTurnSystem(serve.AgentTurn{Name: "My space", Asker: "priya", Space: true, Door: nil})
		if work {
			p = factoryMySpaceAgentSystem("priya", true, time.Time{}, "")
		}
		if hit := machineryWords.FindAllString(p, -1); len(hit) > 0 {
			t.Errorf("work=%v: the space prompt says %q", work, hit)
		}
		if !strings.Contains(p, "private to them") || !strings.Contains(p, "the operator included") || !strings.Contains(p, "working with priya") {
			t.Errorf("work=%v: the space prompt must say it is private to the person, the operator included:\n%s", work, p)
		}
		if strings.Contains(p, "You are Afi, the assistant of the organization workstream") {
			t.Errorf("work=%v: a space turn was told it is a shared workstream", work)
		}
	}
	if p := factoryMySpaceAgentSystem("priya", true, time.Time{}, ""); !strings.Contains(p, "There are no tasks here") {
		t.Error("with the work tools, the space prompt must say tasks are refused here")
	}
}
