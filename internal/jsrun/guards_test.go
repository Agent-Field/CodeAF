package jsrun

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// A CHECK THAT DOES NOT PASS IS A FALLBACK, NOT A FAILED TASK. The work still
// has to get done — by the general worker, with the original input — and the
// caller does that. Nothing in this package runs `linear`.

const guardedProgram = `function run(input) { return { summary: "the fast path ran" }; }`

func guardedBundle(guards []exec.Guard, look Look) Bundle {
	manifest := toyManifest()
	manifest.Guards = guards
	return Bundle{Manifest: manifest, Program: guardedProgram, Look: look}
}

// The author's own account of what the check was for is the sentence the person
// reads, because somebody who knew why the check existed wrote it.
func TestAFileThatIsNotThereFallsBackInTheAuthorsWords(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{{
		Kind:    exec.GuardFile,
		File:    "docs/brief.md",
		Because: "this one needs the brief in the repository",
	}}, scriptedLook{files: map[string]bool{}})

	result, journal := runToy(t, bundle, &scriptedEnv{}, `{}`)
	if result.FellBack != "it needed a closer look — this one needs the brief in the repository" {
		t.Fatalf("the fallback said %q", result.FellBack)
	}
	if len(result.Output) != 0 {
		t.Fatalf("the fast path ran anyway: %s", result.Output)
	}
	if result.Incomplete != "" {
		t.Fatalf("a fallback is not an incomplete run: %q", result.Incomplete)
	}
	if len(journal.entries) != 1 || journal.entries[0].Note != result.FellBack {
		t.Fatalf("the deopt was not journaled: %+v", journal.entries)
	}
	// The vocabulary law reaches here especially: "needed a closer look", never
	// "guard failed".
	if strings.Contains(strings.ToLower(result.FellBack), "guard") {
		t.Fatalf("machinery vocabulary reached a person: %q", result.FellBack)
	}
}

// A check with nothing written about it still says what the fact was, rather
// than falling back silently.
func TestACheckWithNoReasonStillNamesTheFact(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{{Kind: exec.GuardTool, Tool: "browse"}},
		scriptedLook{belt: map[string]bool{"read": true}})
	result, _ := runToy(t, bundle, &scriptedEnv{}, `{}`)
	if !strings.Contains(result.FellBack, "the browse tool is not on the belt") {
		t.Fatalf("the fallback said %q", result.FellBack)
	}
}

// The checks that pass let the fast path run, which is the whole point of having
// them.
func TestChecksThatPassLetTheFastPathRun(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{
		{Kind: exec.GuardFile, File: "docs/brief.md"},
		{Kind: exec.GuardTool, Tool: "read"},
		{Kind: exec.GuardField, Field: "brief", Pattern: "launch"},
	}, scriptedLook{
		files: map[string]bool{"docs/brief.md": true},
		belt:  map[string]bool{"read": true},
	})
	result, _ := runToy(t, bundle, &scriptedEnv{}, `{"brief":"Launch week for the new thing"}`)
	if !result.Finished() {
		t.Fatalf("every check passed and the run still did not happen: %+v", result)
	}
}

// The field check is a plain substring test that ignores case — a manifest is
// written by somebody typing a word they expect to see, not a pattern they have
// debugged.
func TestTheFieldCheckIsAPlainSubstringTest(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{{Kind: exec.GuardField, Field: "brief", Pattern: "flake"}}, nil)
	if result, _ := runToy(t, bundle, &scriptedEnv{}, `{"brief":"A FLAKE in the test suite"}`); !result.Finished() {
		t.Fatalf("a substring in another case should match: %+v", result)
	}
	if result, _ := runToy(t, bundle, &scriptedEnv{}, `{"brief":"launch week"}`); result.FellBack == "" {
		t.Fatalf("a field that does not match should fall back: %+v", result)
	}
	if result, _ := runToy(t, bundle, &scriptedEnv{}, `{}`); result.FellBack == "" {
		t.Fatalf("a field that is not there should fall back: %+v", result)
	}
}

// A CHECK THAT CANNOT BE MADE DOES NOT PASS. With no way to look at the world,
// the safe answer to "I could not tell" is the long way.
func TestACheckThatCannotBeMadeDoesNotPass(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{{Kind: exec.GuardFile, File: "docs/brief.md"}}, nil)
	result, _ := runToy(t, bundle, &scriptedEnv{}, `{}`)
	if result.FellBack == "" {
		t.Fatalf("an uncheckable precondition let the fast path run: %+v", result)
	}
}

// The one rung that costs is asked last, and only when everything free has
// already passed — there is no reason to pay for a question a free check has
// already settled.
func TestTheFreeChecksAreMadeBeforeTheOneThatCosts(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{
		{Kind: exec.GuardJudgement, Question: "Is this a marketing brief?"},
		{Kind: exec.GuardFile, File: "docs/brief.md"},
	}, scriptedLook{files: map[string]bool{}})

	env := &scriptedEnv{}
	result, _ := runToy(t, bundle, env, `{}`)
	if result.FellBack == "" {
		t.Fatalf("the missing file should have settled it: %+v", result)
	}
	if len(env.seen) != 0 {
		t.Fatalf("the model was asked anyway: %v", env.seen)
	}
}

// The judgement is one small question and one word back, and it is journaled and
// billed like any other model call.
func TestTheJudgementAsksOnceAndIsBilledForIt(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{{
		Kind:     exec.GuardJudgement,
		Question: "Is this a marketing brief?",
		Because:  "this one is only for marketing work",
	}}, nil)

	yes := &scriptedEnv{answers: map[string]exec.Answer{
		"Is this a marketing brief?": {Text: "yes", Spend: exec.Spend{Calls: 1, Input: 60, Output: 1}},
	}}
	result, journal := runToy(t, bundle, yes, `{"brief":"launch week"}`)
	if !result.Finished() {
		t.Fatalf("a yes should let the fast path run: %+v", result)
	}
	if result.Spend.Input != 60 {
		t.Fatalf("the question was not billed: %+v", result.Spend)
	}
	if len(journal.entries) == 0 || journal.entries[0].Call != exec.CallAI {
		t.Fatalf("the question was not journaled: %+v", journal.entries)
	}

	no := &scriptedEnv{answers: map[string]exec.Answer{
		"Is this a marketing brief?": {Text: "no"},
	}}
	if result, _ := runToy(t, bundle, no, `{}`); result.FellBack != "it needed a closer look — this one is only for marketing work" {
		t.Fatalf("a no said %q", result.FellBack)
	}

	// ANYTHING BUT YES IS A NO — including a model that would not answer at all.
	broken := &scriptedEnv{failAI: errBrokenDoor{}}
	if result, _ := runToy(t, bundle, broken, `{}`); result.FellBack == "" {
		t.Fatalf("a question that could not be asked let the fast path run: %+v", result)
	}
}

type errBrokenDoor struct{}

func (errBrokenDoor) Error() string { return "nothing answered" }

// A subharness may ask the model at most one question before it runs, and a
// bundle that asks more is refused at load rather than quietly skipping a check.
func TestABundleThatAsksTooMuchBeforeItRunsIsRefused(t *testing.T) {
	bundle := guardedBundle([]exec.Guard{
		{Kind: exec.GuardJudgement, Question: "Is this marketing?"},
		{Kind: exec.GuardJudgement, Question: "Is it urgent?"},
	}, nil)
	_, err := New(bundle)
	if err == nil {
		t.Fatal("a bundle with two paid checks was accepted")
	}
	if !strings.Contains(err.Error(), "at most 1") {
		t.Fatalf("the refusal said %q", err.Error())
	}
}
