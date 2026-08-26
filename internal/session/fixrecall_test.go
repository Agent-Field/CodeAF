package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// fixTestLane builds one agent, one turn, and the two files under a temp home.
func fixTestLane(t *testing.T) (*episode, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})
	return agent.newEpisode(), bucket
}

func fixCall(tool, field, value string) ai.ToolCall {
	arguments, _ := json.Marshal(map[string]string{field: value})
	return ai.ToolCall{
		ID:       "call-1",
		Type:     "function",
		Function: ai.ToolCallFunction{Name: tool, Arguments: string(arguments)},
	}
}

func fixFailed(text string) toolResult   { return toolResult{text: text, isError: true} }
func fixWorked(text string) toolResult   { return toolResult{text: text} }
func fixBash(command string) ai.ToolCall { return fixCall("bash", "command", command) }

// ── the record loop ─────────────────────────────────────────────────────────

// A failure followed by a success on the same hand records the command that
// succeeded, and the NEXT session meeting that error is told about it.
func TestAFailureThenASuccessRecordsTheCommandThatWorked(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ugrep: error at position 5 (empty (sub)expression)\n\nCommand exited with code 2"

	episode.noteToolOutcome(fixBash("grep -E '(sub)' ."), fixFailed(broken))
	episode.noteToolOutcome(fixBash("grep -F '(sub)' ."), fixWorked("internal/x.go: (sub)"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 {
		t.Fatalf("the fix should have been written down once; got %d", len(document.Entries))
	}
	if document.Entries[0].Fix != "grep -F '(sub)' ." {
		t.Fatalf("the succeeding command is the fix; got %q", document.Entries[0].Fix)
	}
	if document.Entries[0].OK != 1 || document.Entries[0].Failed != 0 {
		t.Fatalf("one confirmation and no failures; got %d/%d", document.Entries[0].OK, document.Entries[0].Failed)
	}
}

// And the line the model reads is exactly this one — once the patch has been
// handed back and seen to work. The first round only ever earns the weaker
// sentence (see [TestTheFooterNeverClaimsAFixWorkedUntilItHasBeenSeenToWork]).
func TestTheLineSaysWhatWorkedAndHowOftenItDid(t *testing.T) {
	episode, _ := fixTestLane(t)
	broken := "ugrep: error at position 5 (empty (sub)expression)\n\nCommand exited with code 2"

	// Three rounds of the same error and the same fix. The first round is the
	// bare pairing; the second and third are the offered patch being taken.
	for i := 0; i < 3; i++ {
		episode.noteToolOutcome(fixBash("grep -E '(sub)' ."), fixFailed(broken))
		episode.noteToolOutcome(fixBash("grep -F '(sub)' ."), fixWorked("found it"))
	}
	annotated := episode.noteToolOutcome(fixBash("grep -E '(sub)' ."), fixFailed(broken))

	want := "this exact error was fixed 2/2 times before · what worked: grep -F '(sub)' ."
	if !strings.HasSuffix(annotated.text, want) {
		t.Fatalf("the result should end with the line:\n%q", annotated.text)
	}
	if !strings.HasPrefix(annotated.text, "ugrep: error at position 5") {
		t.Fatalf("the tool's own output must survive intact:\n%q", annotated.text)
	}
	if !annotated.isError {
		t.Fatal("a failure annotated is still a failure")
	}
	// ONE line, never a menu.
	if got := strings.Count(annotated.text, "this exact error was fixed"); got != fixAdviceLimit {
		t.Fatalf("at most %d lines may be appended; got %d", fixAdviceLimit, got)
	}
}

// A success is silent. Retrieval happens only on an error.
func TestASuccessIsNeverAnnotated(t *testing.T) {
	episode, _ := fixTestLane(t)
	result := episode.noteToolOutcome(fixBash("go build ./..."), fixWorked("ok"))
	if result.text != "ok" {
		t.Fatalf("a successful result must be untouched; got %q", result.text)
	}
}

// An error nothing is known about leaves with the words the tool wrote and
// nothing else. The emptiness of an unknown error is the point.
func TestAnUnknownErrorIsNotAnnotated(t *testing.T) {
	episode, _ := fixTestLane(t)
	result := episode.noteToolOutcome(fixBash("./run"), fixFailed("some brand new error nobody has met"))
	if strings.Contains(result.text, "before") {
		t.Fatalf("nothing should have been appended; got %q", result.text)
	}
}

// An offered patch whose retry hits the same error is counted against it. This
// is the half that keeps the store from ranking a bad patch at the top forever.
func TestAnOfferedPatchThatFailsAgainIsCountedAgainstIt(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ld: symbol(s) not found for architecture arm64\n\nCommand exited with code 1"

	// Earn a patch worth offering.
	for i := 0; i < 3; i++ {
		episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
		episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))
	}
	// It is offered, tried, and the same error comes back.
	offered := episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	if !strings.Contains(offered.text, "make clean && make build") {
		t.Fatalf("the patch should have been offered:\n%q", offered.text)
	}
	episode.noteToolOutcome(fixBash("make clean && make build"), fixFailed(broken))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 {
		t.Fatalf("one patch; got %d", len(document.Entries))
	}
	if document.Entries[0].OK != 3 || document.Entries[0].Failed != 1 {
		t.Fatalf("the patch should read 3/4; got %d/%d", document.Entries[0].OK, document.Entries[0].Failed)
	}
	if document.Failed != 1 {
		t.Fatalf("the injected retry's outcome should be counted; got %d", document.Failed)
	}
}

// A retry that fails DIFFERENTLY is a step forward, not a verdict on the patch.
func TestARetryThatFailsDifferentlyDoesNotBlameThePatch(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ld: symbol(s) not found for architecture arm64\n\nCommand exited with code 1"

	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))
	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixFailed("internal/x.go:4:2: undefined: foo"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	for _, entry := range document.Entries {
		if entry.Fix == "make clean && make build" && entry.Failed != 0 {
			t.Fatalf("a different error must not be blamed on the patch; got %d", entry.Failed)
		}
	}
	if document.Failed != 0 {
		t.Fatalf("no injected retry has failed here; got %d", document.Failed)
	}
}

// The offered patch succeeding is the store's own win, and it is counted only
// when the patch that was offered is the patch that ran.
func TestAnOfferedPatchThatWorksIsCountedForIt(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ld: symbol(s) not found for architecture arm64\n\nCommand exited with code 1"

	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))
	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if document.Worked != 1 {
		t.Fatalf("the offered patch worked once; got %d", document.Worked)
	}

	// And a retry that succeeded with something ELSE is neither a win nor a loss
	// for the advice — it is the line being read and set aside.
	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("cargo build"), fixWorked("built"))
	document = readFixDocument(filepath.Join(bucket, fixesFileName))
	if document.Worked != 1 || document.Failed != 0 {
		t.Fatalf("an ignored line is no verdict; got worked=%d failed=%d", document.Worked, document.Failed)
	}
}

// The lane is per HAND: a bash failure is not answered by a grep success.
func TestOneHandsFailureIsNotAnsweredByAnothersSuccess(t *testing.T) {
	episode, bucket := fixTestLane(t)
	episode.noteToolOutcome(fixBash("make build"), fixFailed("ld: symbol(s) not found for architecture arm64"))
	episode.noteToolOutcome(fixCall("grep", "pattern", "func main"), fixWorked("main.go: func main"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 0 {
		t.Fatalf("nothing should have been recorded; got %+v", document.Entries)
	}
}

// The lane dies with the turn: a new episode answers for nothing the last one
// saw. A fix credited across half an hour and four subjects is not evidence.
func TestTheLaneDoesNotOutliveItsTurn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})

	first := agent.newEpisode()
	first.noteToolOutcome(fixBash("make build"), fixFailed("ld: symbol(s) not found for architecture arm64"))

	second := agent.newEpisode()
	second.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 0 {
		t.Fatalf("a new turn answers for nothing the last one saw; got %+v", document.Entries)
	}
}

// Only the hands whose defining argument is an INSTRUCTION keep a lane. read's
// commonest failure normalizes to one signature every missing file shares, and
// the patch under it would be one arbitrary path.
func TestAHandWhoseArgumentIsAPathKeepsNoLane(t *testing.T) {
	episode, bucket := fixTestLane(t)
	missing := "Error reading file: open /Users/x/notes.txt: no such file or directory"
	result := episode.noteToolOutcome(fixCall("read", "path", "/Users/x/notes.txt"), fixFailed(missing))
	if strings.Contains(result.text, "before") {
		t.Fatalf("read should not be annotated; got %q", result.text)
	}
	episode.noteToolOutcome(fixCall("read", "path", "/Users/x/notes.md"), fixWorked("hello"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 0 {
		t.Fatalf("read keeps no lane; got %+v", document.Entries)
	}
}

// The project's file is consulted before the machine's, through the whole live
// path rather than through the shelf alone.
func TestTheLiveLoopConsultsTheProjectBeforeTheMachine(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")
	broken := "ld: symbol(s) not found for architecture arm64"
	signature, _ := fixSignature("bash", broken)

	project := newFixStore(filepath.Join(bucket, fixesFileName))
	project.confirm(signature, "the project answer")
	global := newFixStore(filepath.Join(root, "v3", fixesFileName))
	for i := 0; i < 9; i++ {
		global.confirm(signature, "the machine answer")
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})
	annotated := agent.newEpisode().noteToolOutcome(fixBash("make build"), fixFailed(broken))
	if !strings.Contains(annotated.text, "the project answer") {
		t.Fatalf("the project should win even against a better-worn machine answer:\n%q", annotated.text)
	}
}

// A confirmation lands in BOTH files, which is the only way the machine's store
// ever learns anything.
func TestAConfirmationLandsInBothFiles(t *testing.T) {
	episode, bucket := fixTestLane(t)
	root := filepath.Dir(filepath.Dir(bucket))
	episode.noteToolOutcome(fixBash("make build"), fixFailed("ld: symbol(s) not found for architecture arm64"))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))

	for _, path := range []string{
		filepath.Join(bucket, fixesFileName),
		filepath.Join(root, fixesFileName),
	} {
		document := readFixDocument(path)
		if len(document.Entries) != 1 || document.Entries[0].Fix != "make clean && make build" {
			t.Fatalf("%s should hold the fix; got %+v", path, document.Entries)
		}
	}
}

// ── through the real chokepoint ─────────────────────────────────────────────
//
// Everything above drives [episode.noteToolOutcome] directly. This one drives
// the belt: a real bash call that fails, a real one that works, through
// [Agent.executeTool] — because the whole argument for wiring the sidecar at
// that chokepoint is that no execution can slip past it, and a unit test of the
// annotation would pass just as well if nothing ever called it.
func TestTheChokepointLearnsFromARealToolCall(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})

	hub := newEventHub()
	defer hub.close()
	episode := agent.newEpisode()

	broke := agent.runToolsWarm(context.Background(), episode, []ai.ToolCall{
		fixBash("echo 'ld: symbol(s) not found for architecture arm64' >&2; exit 1"),
	}, hub, nil)
	if len(broke) != 1 || !broke[0].isError {
		t.Fatalf("the first call should have failed: %+v", broke)
	}
	agent.runToolsWarm(context.Background(), episode, []ai.ToolCall{
		fixBash("true"),
	}, hub, nil)

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 || document.Entries[0].Fix != "true" {
		t.Fatalf("the chokepoint should have learned the fix; got %+v", document.Entries)
	}

	// And the next occurrence of that failure comes back annotated, out of the
	// belt rather than out of a helper.
	again := agent.runToolsWarm(context.Background(), agent.newEpisode(), []ai.ToolCall{
		fixBash("echo 'ld: symbol(s) not found for architecture arm64' >&2; exit 1"),
	}, hub, nil)
	// Nothing has yet been offered and taken, so the line is the weaker of the
	// two shapes: the pairing that was watched, and not a claim it is a cure.
	if !strings.Contains(again[0].text, "this exact error came up here before · what ran next and it went away: true") {
		t.Fatalf("the failed result should carry the line:\n%q", again[0].text)
	}
}

// The patch is read whole rather than as a gloss: a shell command cut off after
// its first line is advice that would not run.
func TestAMultiLineCommandIsRecordedWhole(t *testing.T) {
	episode, bucket := fixTestLane(t)
	episode.noteToolOutcome(fixBash("make build"), fixFailed("ld: symbol(s) not found for architecture arm64"))
	episode.noteToolOutcome(fixBash("make clean\nmake build"), fixWorked("built"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 || document.Entries[0].Fix != "make clean make build" {
		t.Fatalf("the whole command should be kept on one line; got %+v", document.Entries)
	}
}

// ── what a fix memory may learn from (fixblame.go) ──────────────────────────

// A DOOR SAYING NO IS NOT AN ERROR A COMMAND FIXED. The checker's reading-only
// bash refuses a command outright; whatever the model types next is simply the
// next thing it typed, and filing it as the cure is how the store came to hold
// `pwd` as the answer to a refusal.
func TestARefusalTeachesTheStoreNothing(t *testing.T) {
	episode, bucket := fixTestLane(t)
	refusal, allowed := auditRefusal("bash run_tests.sh", auditReadCommands)
	if allowed {
		t.Fatal("this command has to be refused for the test to be about anything")
	}

	annotated := episode.noteToolOutcome(fixBash("bash run_tests.sh"), fixFailed(refusal))
	if annotated.text != refusal {
		t.Fatalf("a refusal must leave with the door's own words and nothing else:\n%q", annotated.text)
	}
	episode.noteToolOutcome(fixBash("pwd"), fixWorked("/work"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 0 {
		t.Fatalf("nothing a door refused may be learned from; got %+v", document.Entries)
	}
}

// AND NEITHER IS A PROGRAM THIS MACHINE DOES NOT HAVE. There is no git in that
// container and there was never going to be: the next command is a route
// around an absence, not a patch anybody can be handed later.
func TestAnAbsentProgramTeachesTheStoreNothing(t *testing.T) {
	for _, absent := range []string{
		"bash: git: command not found",
		"sh: 1: git: not found",
		`exec: "git": executable file not found in $PATH`,
	} {
		episode, bucket := fixTestLane(t)
		annotated := episode.noteToolOutcome(fixBash("git status"), fixFailed(absent))
		if annotated.text != absent {
			t.Fatalf("%q should leave untouched; got %q", absent, annotated.text)
		}
		episode.noteToolOutcome(fixBash("pwd"), fixWorked("/work"))

		document := readFixDocument(filepath.Join(bucket, fixesFileName))
		if len(document.Entries) != 0 {
			t.Fatalf("%q must teach nothing; got %+v", absent, document.Entries)
		}
	}
}

// THE OTHER HALF OF THE LAW: a failure the model really did cause, followed by
// the command that made the same failure go away, is still recorded. The gate
// above must not be a gate on everything.
func TestAGenuineFailureFollowedByTheCommandThatEndedItIsStillLearned(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ugrep: error at position 5 (empty (sub)expression)"

	episode.noteToolOutcome(fixBash("grep -E '(sub)' ."), fixFailed(broken))
	episode.noteToolOutcome(fixBash("grep -F '(sub)' ."), fixWorked("internal/x.go: (sub)"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 || document.Entries[0].Fix != "grep -F '(sub)' ." {
		t.Fatalf("a real failure and its real answer should be kept; got %+v", document.Entries)
	}
	// And the same failure, again, comes back with the line on it.
	again := episode.noteToolOutcome(fixBash("grep -E '(sub)' ."), fixFailed(broken))
	if !strings.Contains(again.text, "grep -F '(sub)' .") {
		t.Fatalf("the answer should have been offered back:\n%q", again.text)
	}
}

// A REFUSAL IS NOT ANSWERED EITHER. Advice about how to get past a door is
// advice about a command that never ran, so the refusal leaves as the door
// wrote it however much the store knows about that signature.
func TestARefusalIsNeverAnsweredWithAdvice(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")

	refusal, _ := auditRefusal("bash run_tests.sh", auditReadCommands)
	signature, keyed := fixSignature("bash", refusal)
	if !keyed {
		t.Fatal("the refusal has to normalize to something for this test to mean anything")
	}
	// Plant the very entry the measured run had, by the back door the live path
	// is now forbidden from taking.
	store := newFixStore(filepath.Join(bucket, fixesFileName))
	for i := 0; i < 5; i++ {
		store.confirmAdvised(signature, "pwd")
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})
	annotated := agent.newEpisode().noteToolOutcome(fixBash("bash run_tests.sh"), fixFailed(refusal))
	if annotated.text != refusal {
		t.Fatalf("a refusal must never be annotated, however well worn the entry:\n%q", annotated.text)
	}
}

// THE FOOTER SAYS ONLY WHAT WAS SEEN TO WORK. A pairing the store merely
// watched is offered in the weaker words; the claim that something WORKED waits
// until the patch has been handed back and taken.
func TestTheFooterNeverClaimsAFixWorkedUntilItHasBeenSeenToWork(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ld: symbol(s) not found for architecture arm64"

	// One pairing watched, nothing offered yet.
	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 1 || document.Entries[0].Worked != 0 {
		t.Fatalf("a watched pairing is not yet a fix that worked; got %+v", document.Entries)
	}

	offered := episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	if strings.Contains(offered.text, "what worked") {
		t.Fatalf("nothing has been seen to work, so the line may not say it did:\n%q", offered.text)
	}
	if !strings.Contains(offered.text, "this exact error came up here before · what ran next and it went away: make clean && make build") {
		t.Fatalf("the weaker line is what an unproven patch earns:\n%q", offered.text)
	}

	// The offer is taken and the error goes away. NOW it has worked.
	episode.noteToolOutcome(fixBash("make clean && make build"), fixWorked("built"))
	document = readFixDocument(filepath.Join(bucket, fixesFileName))
	if document.Entries[0].Worked != 1 || document.Entries[0].OK != 2 {
		t.Fatalf("the taken offer should read worked=1 ok=2; got %+v", document.Entries[0])
	}

	proven := episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	if !strings.Contains(proven.text, "this exact error was fixed 1/1 times before · what worked: make clean && make build") {
		t.Fatalf("a patch seen to work earns the stronger line:\n%q", proven.text)
	}
}

// The number in the footer is the OFFERS, never the pairings. An entry that was
// watched nine times and taken once says 1/1, because nine watched pairings are
// nine coincidences until one of them is put to the test.
func TestTheFooterCountsOffersTakenAndNotPairingsWatched(t *testing.T) {
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")
	store := testStore(t, time.Now())
	for i := 0; i < 9; i++ {
		store.confirm(signature, "make clean && make build")
	}
	store.confirmAdvised(signature, "make clean && make build")

	found := store.consult(signature)
	if len(found) != 1 {
		t.Fatalf("the patch should be offered; got %d", len(found))
	}
	line := fixAnnotate("boom", []fixAdvice{{
		patch:  found[0].Fix,
		ok:     found[0].ok(),
		worked: found[0].worked(),
		failed: found[0].failed(),
	}})
	if !strings.HasSuffix(line, "this exact error was fixed 1/1 times before · what worked: make clean && make build") {
		t.Fatalf("ten pairings and one taken offer reads 1/1; got:\n%q", line)
	}
}

// THE VOICE IS PINNED TO THE LIVE DOORS. The recogniser keys on the one word
// this package's doors refuse with; a door reworded out from under it would
// otherwise go back to teaching the store nonsense, silently.
func TestEveryLiveDoorStillSpeaksTheRefusalTheSidecarKnows(t *testing.T) {
	refused := []string{}
	for _, command := range []string{"bash run_tests.sh", "cargo test && rm -rf .", ""} {
		text, ok := auditRefusal(command, auditReadCommands)
		if ok {
			t.Fatalf("%q should have been refused", command)
		}
		refused = append(refused, text)
	}
	refused = append(refused, refusal("refused in a task: rm -rf / — nobody to ask").text)
	refused = append(refused, "Unknown tool: nosuchtool")

	for _, text := range refused {
		if !blamelessFailure(text) {
			t.Errorf("the sidecar no longer recognises a door's refusal, so it will learn from it:\n%q", text)
		}
	}

	// And an ordinary failure is still ordinary — the recogniser must not be a
	// gate on everything a tool ever says.
	for _, ordinary := range []string{
		"ugrep: error at position 5 (empty (sub)expression)",
		"ld: symbol(s) not found for architecture arm64",
		"Error reading file: open /x/notes.txt: no such file or directory",
		// "not found" is in half the errors ever written, and most of them ARE
		// failures a command fixes. Only the shells' own ending counts.
		"go: module github.com/x@v1.2.3 not found in the module cache",
		"grep: no matches found",
	} {
		if blamelessFailure(ordinary) {
			t.Errorf("an ordinary failure must still be learned from: %q", ordinary)
		}
	}
}
