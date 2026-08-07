package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The trigger table. Every true here is a question a person would ask while
// learning what their employee is; every false is a sentence that must keep the
// routing it has today.
func TestSelfQuestionTriggerOpensOnQuestionsAndNotOnWork(t *testing.T) {
	for _, message := range []string{
		"what can you do",
		"how does the boost model work",
		"why did you ask me to confirm that cancel",
		"what is a charter?",
		"what happens every day while I'm gone",
		"how does the daily rail work",
		"can you see images?",
		"tell me about yourself",
	} {
		if !selfQuestionCued(message) {
			t.Fatalf("%q did not open the loop", message)
		}
	}
	for _, message := range []string{
		"build me a parser",
		"cancel the queued ones",
		"can you build me a parser?",
		"write the launch note and save it as launch.md",
		"draft the summary with the better model",
		"find out which dependencies changed licence",
		"thanks!",
		"",
	} {
		if selfQuestionCued(message) {
			t.Fatalf("%q opened the loop", message)
		}
	}
}

// The whole point, end to end: a question about aforge with nothing live at
// all, answered out of the manual, journalling nothing.
func TestSelfQuestionAnswersFromTheManualWithNothingLiveAndNoCommand(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("m1", beltToolManual, map[string]any{
			"q": "what happens while I am gone every day"})}},
		{text: "While you're away I keep a standing watch every five minutes, fire the goals you ratified, practice on my own $2 a day, and fold it all into one card when you come back."},
	}}
	session := "self-question"
	user := postUser(t, graph, session, "what happens every day while I'm gone?")
	head := New(client, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "standing watch") {
		t.Fatalf("reply was not grounded in the manual: %q", reply.Body)
	}
	if reply.CommandSeq != 0 {
		t.Fatalf("a pure answer carried command seq %d", reply.CommandSeq)
	}
	if commands, err := graph.PendingCommands(20); err != nil || len(commands) != 0 {
		t.Fatalf("a pure answer journalled commands: %+v err=%v", commands, err)
	}
	if calls, tooled := client.counts(); tooled == 0 || calls != tooled {
		t.Fatalf("self-question did not stay in the belt: calls=%d tooled=%d", calls, tooled)
	}
}

// The manual tool is a read: a wrong page name is a correctable error, and a
// question the manual does not cover names the pages instead of inventing one.
func TestManualToolReadsPagesAndFailsLoudlyOnAnUnknownOne(t *testing.T) {
	graph := openHeadStore(t)
	run := &beltRun{head: New(nil, graph), user: store.Message{Body: "how does boost work"}}

	whole, failed := run.manual(map[string]any{"page": "daily-rhythm"})
	if failed || !strings.Contains(whole, "standing watch") {
		t.Fatalf("whole-page read failed=%v: %q", failed, firstLine(whole))
	}
	missing, failed := run.manual(map[string]any{"page": "the-page-of-lies"})
	if !failed || !strings.Contains(missing, "daily-rhythm") {
		t.Fatalf("unknown page failed=%v: %q", failed, missing)
	}
	searched, failed := run.manual(map[string]any{})
	if failed || !strings.Contains(searched, "boost") {
		t.Fatalf("query fell back to the user's words badly: failed=%v %q", failed, firstLine(searched))
	}
	if run.acted {
		t.Fatal("reading the manual recorded an action")
	}
}

// The work arm is untouched: a control sentence with live jobs still reaches
// the graph tools, and the manual does not get in its way.
func TestWorkMessageWithLiveJobsStillGetsTheWorkBelt(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{calls: []ai.ToolCall{beltCall("c2", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"research"}})}},
		{text: "Stopped the market research and left the rest alone."},
	}}
	session := "still-work"
	user := postUser(t, graph, session, "kill everything except the finance one and the scans")
	head := New(client, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.CommandSeq == 0 {
		t.Fatalf("belt work lost its command receipt: %q", reply.Body)
	}
	commands, err := graph.PendingCommands(20)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCancel {
		t.Fatalf("work belt journalled %+v err=%v", commands, err)
	}
}

// Completeness. A capability that lands without a page becomes something aforge
// improvises about, so the registries the recognizers actually dispatch on are
// checked against the pages on every build.
func TestManualCoversEveryCapabilityTheHeadDispatchesOn(t *testing.T) {
	wanted := map[string]string{}
	for _, definition := range beltDefinitions() {
		wanted[definition.Function.Name] = "belt tool"
	}
	for _, class := range classVocabulary {
		wanted[class] = "status class"
	}
	for _, kind := range []store.CommandKind{
		store.CommandSplice, store.CommandAmend, store.CommandCancel,
		store.CommandPause, store.CommandResume, store.CommandRestart,
		store.CommandReprioritize, store.CommandRedirect, store.CommandExpedite,
		store.CommandCharterRatify, store.CommandServiceRestart,
		store.CommandStandingWatchEnable,
	} {
		for _, word := range strings.Split(string(kind), "_") {
			wanted[word] = "command kind"
		}
	}
	wanted[urgencyCue] = "recognizer"
	wanted[ModelSlotBoost] = "model slot"
	wanted[routeReflexKind] = "routing"
	for _, word := range []string{"charter", "service", "notebook", "practice", "vision"} {
		wanted[word] = "recognizer"
	}
	for term, source := range wanted {
		if !manual.Mentions(term) {
			t.Fatalf("no manual page mentions %q (%s)", term, source)
		}
	}
}
