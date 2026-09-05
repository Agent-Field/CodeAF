package session

// A HAND THAT ONLY READS, TESTED THROUGH REAL HANDS.
//
// The door used to refuse an empty scope, so a caller that wanted four readers —
// four sources, four files to compare — had to declare writable paths it never
// meant to touch. Everything here is driven through the scripted provider for
// fork_test.go's reason: the claim is about what a hand can actually DO, and a
// hand that carried `write` while the struct said read-only would pass any
// assertion about the struct.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// forkReadingPartJSON is a hand that declares the empty scope: the key is there
// and it names nothing.
func forkReadingPartJSON(role string) string {
	return `{"role":"` + role + `","scope":[]}`
}

// AN EMPTY SCOPE IS A READER, AND IT HAS NO WAY TO WRITE.
//
// The belt is the mechanism and the scope is not: an empty write scope is
// UNRESTRICTED at the guard (orchestrate.go), so a read-only hand that kept its
// writers would be the least bounded hand in the building.
func TestAHandThatDeclaresAnEmptyScopeReadsAndCannotWrite(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	seed, system := agent.forkSeed()

	hand, err := agent.newHandAgent(forkPart{Role: "read the sources", scopeGiven: true},
		seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hand.Close() })

	byName := map[string]bare.Tool{}
	for _, tool := range hand.beltTools() {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"read", "grep", "find", "ls", "bash"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("a reading hand has no %q, so it cannot look at anything", name)
		}
	}
	// Every verb that could put something on disk, named rather than inferred.
	for _, name := range []string{"edit", "write", "generate_image", "generate_video",
		"edit_video", "speak", "generate_music", "fork", "propose_task", "divide_work"} {
		if _, ok := byName[name]; ok {
			t.Errorf("a reading hand carries %q", name)
		}
	}

	// AND ITS BASH IS THE SAME ORIENTATION POLICY, unchanged by this: it looks,
	// and it is not a sandbox.
	for _, refused := range []string{"go build ./...", "sed -i s/a/b/ x.go", "rm -rf ."} {
		text, isError, err := byName["bash"].Execute(context.Background(),
			[]byte(`{"command":`+mustQuote(refused)+`}`))
		if err != nil {
			t.Fatalf("%q: the refusal was an error, not a result: %v", refused, err)
		}
		if !isError || !strings.HasPrefix(text, "refused:") {
			t.Fatalf("a reading hand ran %q: %q", refused, text)
		}
	}
}

// AND THE PAGE IT READS SAYS SO. The tail names the belt's own tools, so a hand
// with no writers is never told it has any.
func TestAReadingHandIsNeverToldItMayWrite(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	seed, system := agent.forkSeed()

	hand, err := agent.newHandAgent(forkPart{Role: "read the sources", scopeGiven: true},
		seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hand.Close() })

	hand.mu.Lock()
	page := hand.system
	hand.mu.Unlock()
	for _, verb := range []string{"`edit`", "`write`"} {
		if strings.Contains(page, verb) {
			t.Errorf("a reading hand's page offers it %s:\n%s", verb, page)
		}
	}

	charge := forkCharge(0, forkArguments{Parts: []forkPart{{Role: "read the sources", scopeGiven: true}}})
	if !strings.Contains(charge, "YOU WRITE NOTHING") {
		t.Errorf("a reading hand's charge does not say it writes nothing:\n%s", charge)
	}
	if strings.Contains(charge, "YOU MAY WRITE") {
		t.Errorf("a reading hand's charge tells it what it may write:\n%s", charge)
	}
}

// TWO READERS ON THE SAME FILES, AND NEITHER IS REFUSED. This is the shape the
// change is for: the same history, several lanes, one conversation.
func TestTwoReadingHandsMayInspectTheSameFiles(t *testing.T) {
	completer := newForkCompleter(2)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "shared.md"), []byte("one fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkReadingPartJSON("what the file says about cost"),
				forkReadingPartJSON("what the file says about time"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn == 1 {
			// BOTH HANDS READ THE SAME PATH. Two writers here would have been
			// refused at the door; two readers claim nothing.
			return toolResponse("look", "read", `{"path":"shared.md"}`), nil
		}
		return textResponse("read it"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "look at it two ways"))
	handsAreHome(t, agent)

	roster := theForkRoster(t, completer)
	if !strings.Contains(roster, forkOutLead) {
		t.Fatalf("the fork did not open two reading hands:\n%s", roster)
	}
	for hand := 1; hand <= 2; hand++ {
		if turns := completer.handTurns(hand); turns < 2 {
			t.Errorf("hand %d made %d requests, so it never read the result of its own look", hand, turns)
		}
	}
}

// A HAND THAT REACHES FOR A WRITE IT WAS NEVER GIVEN CHANGES NOTHING, and the
// tree is the assertion rather than the refusal text.
func TestAReadingHandCannotMutateTheWorkingCopy(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkReadingPartJSON("read the notes"),
				forkReadingPartJSON("read the data"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 1 {
			return textResponse("nothing to do"), nil
		}
		switch turn {
		case 1:
			return toolResponse("w", "write", `{"path":"notes.md","content":"x"}`), nil
		case 2:
			return toolResponse("s", "bash", `{"command":"sed -i s/a/b/ notes.md"}`), nil
		}
		return textResponse("it could not"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "read it"))
	handsAreHome(t, agent)

	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); err == nil {
		t.Fatal("a reading hand wrote a file")
	}
	if reports := theHandReports(t, completer); anyContains(reports, "wrote notes.md") {
		t.Errorf("a reading hand's report claims a write:\n%s", strings.Join(reports, "\n--\n"))
	}
}

// A MISSING KEY IS A SLIP AND IS REFUSED. Only an explicit [] asks for a reader,
// because reading an omission as read-only turns a typo into a hand that
// silently does half its work.
func TestAHandWithNoScopeKeyIsRefused(t *testing.T) {
	workspace := t.TempDir()
	for _, spelling := range []string{
		`{"parts":[{"role":"one"},{"role":"two","scope":[]}]}`,
		`{"parts":[{"role":"one","scope":null},{"role":"two","scope":[]}]}`,
	} {
		_, problem := parseForkArguments(workspace, []byte(spelling))
		if !strings.Contains(problem, "gives no scope") {
			t.Errorf("%s was not refused for its missing scope: %q", spelling, problem)
		}
	}

	// And a scope that names nothing but blanks is still its own refusal.
	_, problem := parseForkArguments(workspace, []byte(`{"parts":[{"role":"one","scope":["  "]},`+
		`{"role":"two","scope":["b"]}]}`))
	if !strings.Contains(problem, "names no path") {
		t.Errorf("a scope of blanks was not refused: %q", problem)
	}
}

// A MIXED FORK IS VALID: some hands write, some only read.
func TestAForkMayMixWritingAndReadingHands(t *testing.T) {
	workspace := t.TempDir()
	parsed, problem := parseForkArguments(workspace, []byte(forkCall(
		forkPartJSON("edit the adapters", "adapters"),
		forkReadingPartJSON("read the docs"),
	)))
	if problem != "" {
		t.Fatalf("a mixed fork was refused: %s", problem)
	}
	if parsed.Parts[0].readOnly() {
		t.Error("the writing hand came back read-only")
	}
	if !parsed.Parts[1].readOnly() {
		t.Errorf("the reading hand kept a scope: %v", parsed.Parts[1].Scope)
	}
	// A reader claims nothing, so it cannot collide with the writer beside it.
	if _, collides := forkScopesCollide(parsed.Parts[0].Scope, parsed.Parts[1].Scope); collides {
		t.Error("a reading hand collided with a writing one")
	}
}

// AND THE WRITERS ARE UNCHANGED: overlapping scopes are still refused.
func TestOverlappingWritingScopesAreStillRefusedBesideReaders(t *testing.T) {
	workspace := t.TempDir()
	_, problem := parseForkArguments(workspace, []byte(forkCall(
		forkPartJSON("the parser", "src"),
		forkPartJSON("one file of it", "src/parser.go"),
		forkReadingPartJSON("read the docs"),
	)))
	if !strings.Contains(problem, "both claim") {
		t.Errorf("two writing hands sharing a path were admitted: %q", problem)
	}
}

// AND AN ORDINARY EDITING HAND STILL WORKS, end to end, with its writers on the
// belt and its write on disk.
func TestAWritingHandStillEditsItsOwnScope(t *testing.T) {
	completer := newForkCompleter(0)
	workspace := t.TempDir()

	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkReadingPartJSON("read the docs"),
			)), nil
		},
	}
	completer.callerTail = keepWorking(completer, "done")
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index == 1 && turn == 1 {
			return toolResponse("in", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
		}
		return textResponse("hand done"), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)

	if _, err := os.Stat(filepath.Join(workspace, "adapters", "mine.md")); err != nil {
		t.Fatalf("a writing hand could not write inside its own scope: %v", err)
	}
	if reports := theHandReports(t, completer); !anyContains(reports, "wrote adapters/mine.md") {
		t.Errorf("no report names what the writing hand wrote:\n%s", strings.Join(reports, "\n--\n"))
	}
}
