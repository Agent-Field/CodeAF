package jsrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE PIN: a hand-written bundle runs, spends through the ledger, journals every
// door it used, and produces the shape it promised.
//
// PRD §14 names exactly this as Phase 1's acceptance — "a JS bundle authored by
// hand runs from all three doors, spends through the ledger" — and this is the
// runtime's half of it. Everything else in this file is one way that can go
// wrong.
func TestAHandWrittenBundleProducesItsDeclaredOutput(t *testing.T) {
	const program = `
log("looking at the brief");
function run(input) {
  var summary = ai("summarise", { brief: input.brief });
  var notes = tool("read", { path: "notes.md" });
  remember("this one always arrives as a brief");
  log("finishing up");
  return { summary: summary, notes: notes };
}
`
	env := &scriptedEnv{
		answers: map[string]exec.Answer{
			"summarise": {Text: "four lines about the launch", Spend: exec.Spend{Model: "a-model", Calls: 1, Input: 900, Output: 120}},
		},
		tools: map[string]exec.ToolResult{
			"read": {Text: "the notes", Spend: exec.Spend{Input: 40}},
		},
	}
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise the brief."},
	}
	result, journal := runToy(t, bundle, env, `{"brief":"launch week"}`)

	if !result.Finished() {
		t.Fatalf("the run did not finish: incomplete=%q fell back=%q", result.Incomplete, result.FellBack)
	}
	var produced struct {
		Summary string `json:"summary"`
		Notes   string `json:"notes"`
	}
	if err := json.Unmarshal(result.Output, &produced); err != nil {
		t.Fatalf("the output is not what it promised: %v", err)
	}
	if produced.Summary != "four lines about the launch" || produced.Notes != "the notes" {
		t.Fatalf("the output carried %+v", produced)
	}
	if result.Report != "finishing up" {
		t.Fatalf("the report should be the last thing it said, got %q", result.Report)
	}

	// THE LEDGER IS THE SUM OF THE JOURNAL, which is the only reason the two can
	// never disagree — so the test adds the entries up itself rather than
	// trusting the total.
	var summed exec.Spend
	for _, entry := range journal.entries {
		summed.Add(entry.Spend)
	}
	if summed != result.Spend {
		t.Fatalf("the ledger says %+v and its own journal says %+v", result.Spend, summed)
	}
	if result.Spend.Input != 940 || result.Spend.Output != 120 || result.Spend.Calls != 1 {
		t.Fatalf("the run spent %+v", result.Spend)
	}
	if result.Spend.Model != "a-model" {
		t.Fatalf("the run ran on %q", result.Spend.Model)
	}
}

// Every host call is written down, before its answer went back, in the order it
// was made — the journal is the progress feed, the resume point and the evidence
// a later revision is argued from, and all three want the same ordering.
func TestTheJournalCarriesEveryHostCallInOrder(t *testing.T) {
	const program = `
function run(input) {
  log("started");
  var said = ai("summarise", input);
  tool("read", { path: "notes.md" });
  remember("a note");
  recall("");
  log("done");
  return { summary: said };
}
`
	env := &scriptedEnv{}
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise."},
	}
	result, journal := runToy(t, bundle, env, `{"brief":"anything"}`)
	if !result.Finished() {
		t.Fatalf("the run did not finish: %+v", result)
	}
	want := []string{
		"1:log:",
		"2:ai:summarise",
		"3:tool:read",
		"4:remember:a note",
		"5:recall:",
		"6:log:",
	}
	got := journal.calls()
	if len(got) != len(want) {
		t.Fatalf("the journal has %d rows, wanted %d: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("row %d is %q, wanted %q", index+1, got[index], want[index])
		}
	}
	if note := journal.entries[0].Note; note != "started" {
		t.Fatalf("a log row carries what it said, got %q", note)
	}
}

// A tool the manifest did not list is refused HERE, and the refusal never
// reaches the Env — the whitelist is a fact about this subharness, not a policy
// the capability surface enforces on its behalf.
func TestAToolOutsideTheWhitelistNeverReachesTheEnv(t *testing.T) {
	const program = `
function run(input) {
  try {
    tool("write", { path: "/etc/passwd" });
  } catch (e) {
    return { summary: String(e.message || e) };
  }
  return { summary: "it went through" };
}
`
	env := &scriptedEnv{}
	bundle := Bundle{Manifest: toyManifest(), Program: program}
	result, journal := runToy(t, bundle, env, `{"brief":"x"}`)

	var produced struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(result.Output, &produced); err != nil {
		t.Fatalf("the output is not readable: %v", err)
	}
	if !strings.Contains(produced.Summary, `may not call "write"`) {
		t.Fatalf("the refusal said %q", produced.Summary)
	}
	if !strings.Contains(produced.Summary, "the tools it may use are read") {
		t.Fatalf("the refusal should say what it may use, got %q", produced.Summary)
	}
	for _, call := range env.seen {
		if strings.HasPrefix(call, "tool:") {
			t.Fatalf("a refused tool call reached the Env: %v", env.seen)
		}
	}
	if len(journal.entries) == 0 || journal.entries[0].Err == "" {
		t.Fatalf("the refusal was not journaled: %+v", journal.entries)
	}
}

// An empty whitelist has its own sentence, because it is a different fact: not
// "you named the wrong tool" but "this one spends only on the model".
func TestASubharnessWithNoWhitelistSaysItSpendsOnlyOnTheModel(t *testing.T) {
	manifest := toyManifest()
	manifest.Whitelist = nil
	const program = `
function run(input) {
  try { tool("read", {}); } catch (e) { return { summary: String(e.message || e) }; }
  return { summary: "it went through" };
}
`
	result, _ := runToy(t, Bundle{Manifest: manifest, Program: program}, &scriptedEnv{}, `{}`)
	if !strings.Contains(string(result.Output), "spends only on the model") {
		t.Fatalf("the refusal said %s", result.Output)
	}
}

// ai() takes the NAME of a prompt asset, never the prompt itself, and the
// refusal says what to do instead. This is the enforcement of the one rule that
// keeps prompts diffable, reviewable and revisable.
func TestAPromptWrittenInlineIsRefusedWithSomethingToDoAboutIt(t *testing.T) {
	const program = `
function run(input) {
  try {
    ai("Summarise the following brief in four lines.", input);
  } catch (e) {
    return { summary: String(e.message || e) };
  }
  return { summary: "it went through" };
}
`
	env := &scriptedEnv{}
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise."},
	}
	result, _ := runToy(t, bundle, env, `{"brief":"x"}`)

	message := string(result.Output)
	if !strings.Contains(message, "not the prompt itself") {
		t.Fatalf("the refusal said %s", message)
	}
	if !strings.Contains(message, "prompts/") {
		t.Fatalf("the refusal should say where the prompt goes, got %s", message)
	}
	for _, call := range env.seen {
		if strings.HasPrefix(call, "ai:") {
			t.Fatalf("an inline prompt reached the Env: %v", env.seen)
		}
	}
}

// A ref that is simply not there is a different mistake from an inline prompt,
// and it is answered with the names the bundle actually has.
func TestAMissingPromptNamesTheOnesTheBundleHas(t *testing.T) {
	const program = `
function run(input) {
  try { ai("summarize", input); } catch (e) { return { summary: String(e.message || e) }; }
  return { summary: "it went through" };
}
`
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise.", "triage": "Triage."},
	}
	result, _ := runToy(t, bundle, &scriptedEnv{}, `{}`)
	message := string(result.Output)
	if !strings.Contains(message, `no prompt called \"summarize\"`) {
		t.Fatalf("the refusal said %s", message)
	}
	if !strings.Contains(message, "summarise, triage") {
		t.Fatalf("the refusal should list what is there, got %s", message)
	}
}

// The three ways an author writes the same asset are one asset.
func TestAPromptRefIsTheSameAssetHoweverItIsSpelled(t *testing.T) {
	for _, spelling := range []string{"summarise", "summarise.md", "prompts/summarise.md"} {
		program := "function run(input) { return { summary: ai(" + `"` + spelling + `"` + ", input) }; }"
		bundle := Bundle{
			Manifest: toyManifest(),
			Program:  program,
			Prompts:  map[string]string{"summarise": "Summarise."},
		}
		env := &scriptedEnv{answers: map[string]exec.Answer{"summarise": {Text: "ok"}}}
		result, _ := runToy(t, bundle, env, `{}`)
		if !result.Finished() {
			t.Fatalf("%q did not resolve: %+v", spelling, result)
		}
		if len(env.seen) != 1 || env.seen[0] != "ai:summarise" {
			t.Fatalf("%q reached the Env as %v", spelling, env.seen)
		}
	}
}

// AN UNCAUGHT ERROR IS A FALLBACK AND NOT A FAILED TASK. The run does not error;
// it comes back saying it needed the long way, and the caller — never this
// package — runs the general worker.
func TestAnUncaughtErrorFallsBackInsteadOfFailingTheTask(t *testing.T) {
	const program = `
function run(input) {
  return { summary: input.brief.somethingThatIsNotThere.atAll };
}
`
	bundle := Bundle{Manifest: toyManifest(), Program: program}
	result, journal := runToy(t, bundle, &scriptedEnv{}, `{"brief":"x"}`)

	if result.FellBack == "" {
		t.Fatalf("an uncaught error should fall back, got %+v", result)
	}
	if result.Incomplete != "" {
		t.Fatalf("a fallback is not an incomplete run: %q", result.Incomplete)
	}
	if len(result.Output) != 0 {
		t.Fatalf("a fallback produced no output, got %s", result.Output)
	}
	if !strings.Contains(result.FellBack, "stopped partway through") {
		t.Fatalf("the fallback said %q", result.FellBack)
	}
	if strings.Contains(result.FellBack, "\n") {
		t.Fatalf("the fallback is one line, got %q", result.FellBack)
	}
	// The deopt is on the record; repeated ones are the signal a revision is
	// worth proposing (PRD §12).
	last := journal.entries[len(journal.entries)-1]
	if last.Call != exec.CallLog || last.Note != result.FellBack {
		t.Fatalf("the deopt was not journaled: %+v", last)
	}
}

// The whole authoring error story: the compiler's own message, its own line
// number, verbatim. There is no salvage ladder and there must not be one.
func TestASyntaxErrorCarriesItsLineNumber(t *testing.T) {
	const program = "var a = 1;\nfunction run(input) {\n  return { summary: ;\n}\n"
	_, err := New(Bundle{Manifest: toyManifest(), Program: program, Source: "program.js"})
	if err == nil {
		t.Fatal("almost-JavaScript was accepted")
	}
	message := err.Error()
	if !strings.Contains(message, "program.js") {
		t.Fatalf("the error does not say which file: %q", message)
	}
	if !strings.Contains(message, "Line 3:") {
		t.Fatalf("the error does not carry its line number: %q", message)
	}
	if !strings.Contains(message, "SyntaxError") {
		t.Fatalf("the compiler's own message did not survive: %q", message)
	}
}

// A run that ends without the shape it promised is INCOMPLETE — never done with
// a strange message, which is the only ending the old one-string program form
// could offer.
func TestAnAnswerMissingAPromisedFieldIsIncomplete(t *testing.T) {
	const program = `function run(input) { return { notes: "only the notes" }; }`
	result, _ := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, &scriptedEnv{}, `{}`)
	if result.Finished() {
		t.Fatalf("a run missing its promise should not be finished: %+v", result)
	}
	if !strings.Contains(result.Incomplete, "summary") {
		t.Fatalf("the reason should name what is missing, got %q", result.Incomplete)
	}
}

// A program that returns nothing at all has not finished either.
func TestAProgramThatProducesNothingIsIncomplete(t *testing.T) {
	const program = `function run(input) { log("thinking"); }`
	result, _ := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, &scriptedEnv{}, `{}`)
	if result.Finished() || result.Incomplete == "" {
		t.Fatalf("nothing produced should be incomplete: %+v", result)
	}
	if result.Report != "thinking" {
		t.Fatalf("the report survives an incomplete run, got %q", result.Report)
	}
}

// A bundle with no `run` function is a script whose own value is the answer, so
// the shortest honest bundle is one expression.
func TestAScriptWithoutARunFunctionAnswersWithItsOwnValue(t *testing.T) {
	const program = `({ summary: "the whole program is this" })`
	result, _ := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, &scriptedEnv{}, `{}`)
	if !result.Finished() {
		t.Fatalf("the script did not finish: %+v", result)
	}
	if !strings.Contains(string(result.Output), "the whole program is this") {
		t.Fatalf("the output is %s", result.Output)
	}
}

// remember() keeps a note in the SUBHARNESS's own memory when its store opened
// one, and falls through to the Env when it did not.
func TestMemoryPrefersTheBundlesOwnDoor(t *testing.T) {
	const program = `function run(input) { remember("briefs arrive as PDFs"); return { summary: "kept" }; }`
	memory := &scriptedMemory{}
	env := &scriptedEnv{}
	bundle := Bundle{Manifest: toyManifest(), Program: program, Memory: memory}
	if _, _ = runToy(t, bundle, env, `{}`); len(memory.kept) != 1 {
		t.Fatalf("the bundle's own memory kept %+v", memory.kept)
	}
	for _, call := range env.seen {
		if strings.HasPrefix(call, "remember:") {
			t.Fatalf("the note went to the session's memory as well: %v", env.seen)
		}
	}

	env = &scriptedEnv{}
	if _, _ = runToy(t, Bundle{Manifest: toyManifest(), Program: program}, env, `{}`); len(env.notes) != 1 {
		t.Fatalf("with no bundle memory the note should fall through, saw %v", env.seen)
	}
}

// An Env that asks for the bundle's prompts is handed them once, before the
// first call — the ref is what the journal records, and the text is what the
// model has to see.
func TestAnEnvThatAsksForThePromptsIsHandedThem(t *testing.T) {
	const program = `function run(input) { return { summary: ai("summarise", input) }; }`
	env := &scriptedEnv{}
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise the brief."},
	}
	runToy(t, bundle, env, `{}`)
	if env.prompts["summarise"] != "Summarise the brief." {
		t.Fatalf("the Env was handed %v", env.prompts)
	}
}

// Nobody there and no default declared stops the run AT ITS QUESTION. The
// program does not get to guess on somebody's behalf, and neither does the host.
func TestNobodyThereToAnswerStopsTheRunAtItsQuestion(t *testing.T) {
	const program = `
function run(input) {
  var choice = ask("Send it now?", { options: ["yes", "no"] });
  return { summary: choice };
}
`
	result, _ := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, &scriptedEnv{}, `{}`)
	if result.Finished() {
		t.Fatalf("an unanswered question should stop the run: %+v", result)
	}
	if !strings.Contains(result.Incomplete, "Send it now?") {
		t.Fatalf("the reason should name the question, got %q", result.Incomplete)
	}
}

// Taking over ends the run where it stands, with what they typed as its report —
// a person watching something go slightly wrong wants to continue by hand.
func TestTakingOverEndsTheRunWithWhatTheyTyped(t *testing.T) {
	const program = `
function run(input) {
  ask("Send it now?", {});
  return { summary: "it carried on" };
}
`
	env := &scriptedEnv{asks: map[string]exec.AskAnswer{
		"Send it now?": {Text: "I will do the last bit myself", TakingOver: true},
	}}
	result, _ := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, env, `{}`)
	if result.Finished() {
		t.Fatalf("taking over ends the run: %+v", result)
	}
	if result.Report != "I will do the last bit myself" {
		t.Fatalf("their note should be the report, got %q", result.Report)
	}
	if !strings.Contains(result.Incomplete, "took it from here") {
		t.Fatalf("the reason said %q", result.Incomplete)
	}
}

// A door that fails is an ordinary exception the program may have a plan B for.
func TestADoorThatFailsIsCatchableByTheProgram(t *testing.T) {
	const program = `
function run(input) {
  try { tool("read", {}); } catch (e) { return { summary: "plan B" }; }
  return { summary: "plan A" };
}
`
	env := &scriptedEnv{failTool: context.DeadlineExceeded}
	result, journal := runToy(t, Bundle{Manifest: toyManifest(), Program: program}, env, `{}`)
	if !strings.Contains(string(result.Output), "plan B") {
		t.Fatalf("the program could not catch its own failed tool: %s", result.Output)
	}
	if journal.entries[0].Err == "" {
		t.Fatalf("a call that failed is still a call that happened: %+v", journal.entries[0])
	}
}

// A run with nobody watching is a real case: the journal is nil and nothing
// complains.
func TestARunNobodyIsWatchingStillRuns(t *testing.T) {
	const program = `function run(input) { log("quietly"); return { summary: "done" }; }`
	runner, err := New(Bundle{Manifest: toyManifest(), Program: program})
	if err != nil {
		t.Fatalf("this bundle would not compile: %v", err)
	}
	result, err := runner.Run(context.Background(), json.RawMessage(`{}`), &scriptedEnv{})
	if err != nil || !result.Finished() {
		t.Fatalf("an unwatched run should still finish: %+v %v", result, err)
	}
}

// Input that is not JSON is the one thing left, after New has compiled the
// program, that means the run could not be MADE to happen.
func TestInputThatIsNotJSONIsAnError(t *testing.T) {
	runner, err := New(Bundle{Manifest: toyManifest(), Program: `function run(i) { return {summary:"x"}; }`})
	if err != nil {
		t.Fatalf("this bundle would not compile: %v", err)
	}
	if _, err := runner.Run(context.Background(), json.RawMessage(`{not json`), &scriptedEnv{}); err == nil {
		t.Fatal("input that is not JSON was accepted")
	}
}

// A bundle with nothing in it is refused at load, in the prose its author needs.
func TestABundleWithNoProgramIsRefusedAtLoad(t *testing.T) {
	if _, err := New(Bundle{Manifest: toyManifest()}); err == nil {
		t.Fatal("an empty bundle was accepted")
	} else if !strings.Contains(err.Error(), "nothing here to run") {
		t.Fatalf("the refusal said %q", err.Error())
	}
}
