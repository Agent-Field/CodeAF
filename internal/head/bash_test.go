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
//
// The second transcript, on 2026-08-12, is why the tool is called bash and no
// longer `act`: a story sat on disk, the person said "open the file for me",
// and the head read it with its own eyes and narrated it back — twice — while
// the activity trail shows read, board, read, and never the hand. Nothing about
// the mechanism was wrong. The NAME was, and bash.go argues why a name a model
// already has a prior for beats any amount of prose telling it about a concept
// we invented. TestOpenTheFileForMeEndsInTheHandAndNotInAReading is the
// regression, and it is the one place in this campaign where a case may be
// written down: a test may encode an incident, a prompt may not.

// The command runs, and what the loop is handed is what the command actually
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
		{calls: []ai.ToolCall{beltCall("c1", beltToolBash, map[string]any{"command": "ls"})}},
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

	result, failed := run.execute(beltToolBash, beltArguments(t, map[string]any{
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
		result, failed := run.execute(beltToolBash, beltArguments(t, map[string]any{"command": command}))
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
	if reason, gated := bashGated("rm scratch.txt"); gated {
		t.Fatalf("an ordinary removal inside the workspace was gated: %s", reason)
	}
	if reason, gated := bashGated("open signal.html"); gated {
		t.Fatalf("the sentence that started all of this was gated: %s", reason)
	}
}

// The three bounds, proved without spending a minute and sixteen kilobytes of
// patience on them. The window is a field on the head for exactly this reason.
//
// All three widened when the tool was renamed, because the old numbers were
// arguing the boundary rather than carrying the transport — and the boundary is
// argued in exactly two places that did NOT move: the prompt, and the gate.
func TestTheHandIsBoundedInTimeAndInWhatItHandsBack(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph).WithWorkspace(t.TempDir())
	head.bashWindow = 150 * time.Millisecond
	run := &beltRun{head: head, user: postUser(t, graph, "bash", "run it")}

	started := time.Now()
	slow, failed := run.execute(beltToolBash, beltArguments(t, map[string]any{"command": "sleep 30"}))
	if failed {
		t.Fatalf("a timed-out command came back as a tool failure: %s", slow)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the window did not bound the turn: %s", elapsed)
	}
	if !strings.Contains(slow, "still running") || !strings.Contains(slow, beltToolTask) {
		t.Fatalf("a command too slow for one turn was not redirected to work:\n%s", slow)
	}

	head.bashWindow = 0
	loud, failed := run.execute(beltToolBash, beltArguments(t, map[string]any{
		"command": "yes x | head -c 90000"}))
	if failed {
		t.Fatalf("a loud command came back as a tool failure: %s", loud)
	}
	if len(loud) > bashOutputBytes+1024 {
		t.Fatalf("the output cap did not hold: %d bytes came back", len(loud))
	}
	// A page, never a silent clip: the loop reads this and then speaks, so a cut
	// it cannot see is a cut it will describe as the whole.
	if !strings.Contains(loud, "output cut here") {
		t.Fatalf("output was dropped without saying so:\n%s", loud)
	}

	long, failed := run.execute(beltToolBash, beltArguments(t, map[string]any{
		"command": "echo " + strings.Repeat("x", bashCommandBytes)}))
	if !failed || !strings.Contains(long, beltToolTask) {
		t.Fatalf("a command past the transport bound was accepted: %s", long)
	}
}

// The widening is real and is a mechanism rather than a sentence. These are the
// numbers the last wave's names were standing in for, and they are pinned so a
// future slimming cannot quietly put a judgment back into a constant.
func TestTheHandsBoundsAreTransportAndNotJudgment(t *testing.T) {
	if bashWindow != 60*time.Second {
		t.Errorf("the wall is %s, not the minute a turn is actually worth", bashWindow)
	}
	if bashOutputBytes != 16<<10 {
		t.Errorf("the output window is %d bytes, not the 16KB a real command's evidence needs", bashOutputBytes)
	}
	if bashCommandBytes != 2<<10 {
		t.Errorf("the command bound is %d bytes, which is short enough to be a rule about scripts again", bashCommandBytes)
	}
}

// Where the law lives, now that the tool's NAME carries what its description
// used to have to argue. The description states the tool's nature and nothing
// else; the boundary — time and consequence, with work as the answer — is said
// once, in the prompt. What is pinned on the description is the ABSENCE of a use
// case, because a list of occasions is the emergent-capability failure this
// codebase spent a year unlearning and is exactly what grew here last time.
func TestTheHandsBoundaryIsStatedInThePromptAndNeverAsUseCases(t *testing.T) {
	description := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolBash {
			description = definition.Function.Description
		}
	}
	if description == "" {
		t.Fatal("the belt does not offer the hand at all")
	}
	// Its nature: what it does, and that its own output is the whole of the truth.
	for _, want := range []string{"one shell command", "what it printed and how it exited"} {
		if !strings.Contains(description, want) {
			t.Errorf("the description no longer states %q:\n%s", want, description)
		}
	}
	// And not one occasion. Every one of these was in the description before the
	// slimming, came back as "the canonical examples", and is the shape of the
	// mistake rather than a fix for it.
	for _, forbidden := range []string{
		"open a file", "a URL", "list a directory", "read a small file", "in a browser",
	} {
		if strings.Contains(description, forbidden) {
			t.Errorf("the description grew a use case again — %q:\n%s", forbidden, description)
		}
	}
	if !strings.Contains(orchestratorPrompt, "one instant, reversible command a person at the keyboard would run in two seconds without thinking") {
		t.Error("the head's prompt no longer states the boundary, so the hand is one the model is never told it has")
	}
	if !strings.Contains(orchestratorPrompt, "When unsure, hand it over") {
		t.Error("the prompt lost the tie-breaker, which is the only part of the boundary that decides anything at the margin")
	}
}

// The one principle that replaced a year of case law, and the only sentence in
// the prompt that speaks to the failure of 2026-08-12. It is a frame rather than
// a rule: it names no occasion, forbids nothing, and generalises to every hand
// this head will ever grow, because the question it answers — whose world does
// this change? — is the question every one of them differs on.
func TestThePromptFramesTheHandsByWhoseWorldTheyChange(t *testing.T) {
	for _, phrase := range []string{
		"Your hands differ by whose world they change",
		"a read changes what you know",
		"bash changes what they experience on their own machine right now",
		"work changes the world durably",
	} {
		if !strings.Contains(orchestratorPrompt, phrase) {
			t.Errorf("the prompt no longer says %q", phrase)
		}
	}
	// And the frame is stated ONCE. A principle restated per-tool is case law
	// growing back under a better name.
	if count := strings.Count(orchestratorPrompt, "whose world"); count != 1 {
		t.Errorf("the whose-world frame is stated %d times; it is a principle and belongs in exactly one place", count)
	}
}

// THE REGRESSION, 2026-08-12. A story had been written to the workspace; the
// person said "open the file for me"; the head read the file with its own eyes
// and answered "The file is open in front of me" — which was true of the head
// and false of the person, who was looking at an unchanged screen. The trail
// shows read, board, read. The hand was never reached for.
//
// What this proves is the ROUTE, end to end: a delivered file in context, the
// hand called with the command a person would have typed, the command actually
// running on their side of the glass, and the loop being handed what it did
// rather than what it meant to do. Which tool a live model PICKS is not
// something a scripted client can prove — the name is what moves that, and
// bash.go argues why — but every part of the route the pick lands on is here,
// and a break in any of it would have produced the same transcript.
//
// The real `open` is shimmed onto PATH. A test that launched the person's
// document viewer would be a test that did to CI exactly what the product is
// supposed to do to them, and the shim also makes the side effect legible:
// the file it leaves behind is the proof that something happened OUT THERE
// rather than in the head's own reading.
func TestOpenTheFileForMeEndsInTheHandAndNotInAReading(t *testing.T) {
	graph := openHeadStore(t)
	workspace := t.TempDir()
	story := filepath.Join(workspace, "carrohaven_story.md")
	if err := os.WriteFile(story, []byte("# Carrohaven\n\nThe harbour went quiet.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The file is in context the way the live one was: work that finished and
	// recorded it. This is what made a reading tool look like a plausible answer.
	spliceSurgeryJob(t, graph, "task-1061", "Write the Carrohaven story", "write me the carrohaven story")
	completeNodeWith(t, graph, "task-1061", "The story is written.\nFiles:\n"+story)

	shimmedOpen(t, workspace)

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBash, map[string]any{
			"command": "open carrohaven_story.md"})}},
		{text: "Opened it — it should be on your screen now."},
	}}
	session := "carrohaven"
	user := postUser(t, graph, session, "okay open the book for me")
	if err := New(client, graph).WithWorkspace(workspace).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	// It happened in THEIR world. The shim is the only thing that could have
	// written this, and the head's own reading of the story could not have.
	if _, err := os.Stat(filepath.Join(workspace, "opened.marker")); err != nil {
		t.Fatalf("the command never reached the person's side of the glass: %v", err)
	}

	handed := beltToolMessages(client)
	if len(handed) != 1 {
		t.Fatalf("the loop was handed %d tool results, want the one command's: %v", len(handed), handed)
	}
	// And the loop was told what happened rather than left to infer it — the
	// exact hallucination the tool result exists to close.
	for _, want := range []string{"carrohaven_story.md", "exited 0", workspace} {
		if !strings.Contains(handed[0], want) {
			t.Fatalf("the result never told the loop %q:\n%s", want, handed[0])
		}
	}

	// The person watching the turn sees a sentence about their own machine, in
	// the words they would have typed themselves.
	gloss := toolGloss(beltToolBash, beltCall("c", "x", map[string]any{
		"command": "open carrohaven_story.md"}).Function.Arguments)
	if gloss != "running «open carrohaven_story.md»" {
		t.Fatalf("the activity line reads %q", gloss)
	}
}

// shimmedOpen puts a harmless `open` in front of the real one for the length of
// one test, and has it leave a mark in the workspace so the act of running it is
// observable rather than merely unobjected-to.
func shimmedOpen(t *testing.T, workspace string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"opened $1\"\n: > " + filepath.Join(workspace, "opened.marker") + "\n"
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
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
