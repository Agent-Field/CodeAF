package head

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The transcript this file exists for, on 2026-08-11: "open signal.html in a
// browser" was answered with "I can't open a browser for you — that's on your
// side". The head was telling the truth; that is the indictment. What is tested
// here is that it now has the hand, that the hand's OUTPUT is what reaches the
// loop, and that the two things which make giving it hands safe are code rather
// than persuasion.

// The act runs, and what the loop is handed is what the command actually
// printed. This is the hallucination class the tool result closes: the head once
// said "The site is open — I've launched it" over a job that had found nothing,
// and a hand whose outcome the model infers rather than reads would have made
// that cheaper rather than rarer.
func TestAnActRunsAndItsOwnOutputIsWhatTheLoopIsToldHappened(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "signal.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolAct, map[string]any{"command": "ls"})}},
		{text: "signal.html is there."},
	}}
	session := "act"
	user := postUser(t, graph, session, "what's in the workspace?")
	if err := New(client, graph).WithWorkspace(workspace).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	// The tool message is the ground truth, and it has to carry both halves: how
	// the command ended and what it said.
	handed := beltToolMessages(client)
	if len(handed) != 1 {
		t.Fatalf("the loop was handed %d tool results, want the one act's: %v", len(handed), handed)
	}
	if !strings.Contains(handed[0], "signal.html") {
		t.Fatalf("what the command printed never reached the loop:\n%s", handed[0])
	}
	if !strings.Contains(handed[0], "exited 0") {
		t.Fatalf("the loop was not told how the command ended:\n%s", handed[0])
	}
	if !strings.Contains(handed[0], workspace) {
		t.Fatalf("the loop was not told where it ran — the wrong directory is half the failure:\n%s", handed[0])
	}
}

// A command that RAN and exited non-zero is not a failure of this tool. It is a
// fact about the world, and the loop has to read it as one rather than as an
// error to apologise for — the exit status is exactly the thing the head was
// inventing before it had one to read.
func TestANonZeroExitIsReportedAsWhatHappenedRatherThanAsAToolFailure(t *testing.T) {
	graph := openHeadStore(t)
	run := &beltRun{head: New(nil, graph).WithWorkspace(t.TempDir()),
		user: postUser(t, graph, "act", "is there a manifest?")}

	result, failed := run.execute(beltToolAct, beltArguments(t, map[string]any{
		"command": "cat manifest.json"}))
	if failed {
		t.Fatalf("a command that ran and exited non-zero was reported as a tool failure: %s", result)
	}
	if !strings.Contains(result, "exited 1") {
		t.Fatalf("the exit status is not in what the loop reads:\n%s", result)
	}
	if !run.acted {
		t.Fatal("an act that touched the world left no record of having acted")
	}
}

// The floor, which is code and not the description. Both refusals REDIRECT: a
// person who asked for something is owed the route that can do it, so the tool
// result names spawn rather than being a wall.
func TestAConsequenceGatedCommandIsRefusedWithASpawnRedirect(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	run := &beltRun{head: New(nil, graph).WithWorkspace(workspace),
		user: postUser(t, graph, "act", "send it")}

	for name, command := range map[string]string{
		"the sentence gate reads the shell line too": "touch landed.txt; echo email the report",
		"privileges are never an instant act":        "sudo touch landed.txt",
		"nor is taking the machine down":             "shutdown -h now",
		"nor is recursive removal outside here":      "rm -rf ~/Documents",
	} {
		result, failed := run.execute(beltToolAct, beltArguments(t, map[string]any{"command": command}))
		if !failed {
			t.Fatalf("%s: %q was allowed to run: %s", name, command, result)
		}
		if !strings.Contains(result, beltToolTask) {
			t.Fatalf("%s: the refusal is a wall rather than a redirect: %s", name, result)
		}
	}
	// Refused means nothing ran, not that the shell ran and was tidied up after.
	if _, err := os.Stat(filepath.Join(workspace, "landed.txt")); err == nil {
		t.Fatal("a gated command was executed before it was refused")
	}
	if run.acted {
		t.Fatal("a refusal was recorded as an act")
	}
	// And the floor is a floor rather than a filter: deleting a file inside the
	// workspace is exactly the small reversible thing this tool is for.
	if reason, gated := actGated("rm scratch.txt"); gated {
		t.Fatalf("an ordinary removal inside the workspace was gated: %s", reason)
	}
	if reason, gated := actGated("open signal.html"); gated {
		t.Fatalf("the sentence that started all of this was gated: %s", reason)
	}
}

// The two bounds, proved without spending ten seconds and four kilobytes of
// patience on them. The window is a field on the head for exactly this reason.
func TestAnActIsBoundedInTimeAndInWhatItHandsBack(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph).WithWorkspace(t.TempDir())
	head.actWindow = 150 * time.Millisecond
	run := &beltRun{head: head, user: postUser(t, graph, "act", "run it")}

	started := time.Now()
	slow, failed := run.execute(beltToolAct, beltArguments(t, map[string]any{"command": "sleep 30"}))
	if failed {
		t.Fatalf("a timed-out command came back as a tool failure: %s", slow)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the window did not bound the turn: %s", elapsed)
	}
	if !strings.Contains(slow, "still running") || !strings.Contains(slow, beltToolTask) {
		t.Fatalf("a command too slow to be an instant act was not said to be one:\n%s", slow)
	}

	head.actWindow = 0
	loud, failed := run.execute(beltToolAct, beltArguments(t, map[string]any{
		"command": "yes x | head -c 60000"}))
	if failed {
		t.Fatalf("a loud command came back as a tool failure: %s", loud)
	}
	if len(loud) > actOutputBytes+1024 {
		t.Fatalf("the output cap did not hold: %d bytes came back", len(loud))
	}
	if !strings.Contains(loud, "output cut here") {
		t.Fatalf("output was dropped without saying so:\n%s", loud)
	}

	// A script is a deliverable, and a deliverable is work.
	long, failed := run.execute(beltToolAct, beltArguments(t, map[string]any{
		"command": "echo " + strings.Repeat("x", actCommandBytes)}))
	if !failed || !strings.Contains(long, beltToolTask) {
		t.Fatalf("a command the length of a script was accepted: %s", long)
	}
}

// The law lives in the tool description, which is this package's doctrine and
// not a stylistic preference: the boundary a model actually reads is the one
// resent with every turn. What is pinned is the boundary's SHAPE — time and
// consequence, with spawn as the answer — never a list of blessed programs,
// because a topic filter is the emergent-capability failure this codebase spent
// a year unlearning.
func TestTheActBoundaryIsStatedWhereTheModelReadsIt(t *testing.T) {
	description := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolAct {
			description = definition.Function.Description
		}
	}
	if description == "" {
		t.Fatal("the belt does not offer act at all")
	}
	for _, want := range []string{
		"instant, reversible command",
		"two seconds without thinking",
		"produces a deliverable",
		"use task",
		"When unsure, task",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("the act description no longer states %q:\n%s", want, description)
		}
	}
	if !strings.Contains(orchestratorPrompt, "one instant, reversible command a person at the keyboard would run in two seconds without thinking") {
		t.Error("the head's prompt no longer states the boundary, so the hand is one the model is never told it has")
	}
	if !strings.Contains(orchestratorPrompt, "When unsure, hand it over") {
		t.Error("the prompt lost the tie-breaker, which is the only part of the boundary that decides anything at the margin")
	}
}

// beltToolMessages is every tool result the loop was handed, in order. The tool
// message is the only thing the reply may be built from, so an assertion about
// what the head KNOWS is an assertion about these.
func beltToolMessages(client *beltClient) []string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	results := make([]string, 0, 2)
	for _, message := range client.seen {
		if message.Role != "tool" {
			continue
		}
		var text strings.Builder
		for _, part := range message.Content {
			text.WriteString(part.Text)
		}
		results = append(results, text.String())
	}
	return results
}
