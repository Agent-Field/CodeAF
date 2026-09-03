package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// The headless door's whole contract is its ENDINGS, so these tests drive
// [runSubharness] rather than the flag parsing above it and assert on the two
// streams and the exit code together. Nothing here reaches a network: every
// program is a scripted [exec.Runner] and every model call that would have been
// made is one the script did not make.

// scriptedRunner is one subharness written for a test: a manifest, a body, and a
// record of the input it was actually handed. The last of those is what makes
// the deoptimization checkable — "the generalist got the ORIGINAL input" is the
// promise the long way rests on, and it can only be tested by asking the
// generalist what it received.
type scriptedRunner struct {
	manifest exec.Manifest
	body     func(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error)
	saw      json.RawMessage
	ran      int
}

func (s *scriptedRunner) Manifest() exec.Manifest { return s.manifest }

func (s *scriptedRunner) Run(ctx context.Context, input json.RawMessage, env exec.Env) (exec.RunResult, error) {
	s.ran++
	s.saw = append(json.RawMessage(nil), input...)
	if s.body == nil {
		return exec.RunResult{Output: json.RawMessage(`{"ok":true}`)}, nil
	}
	return s.body(ctx, input, env)
}

// testManifest is the smallest manifest that validates: a name and the one line
// of purpose every list draws under it.
func testManifest(name, purpose string) exec.Manifest {
	return exec.Manifest{SubharnessInfo: exec.SubharnessInfo{Name: name, Purpose: purpose}}
}

// testOpenManifest is [testManifest] with a ceiling that already reaches a
// shell. The deopt tests need one: the general worker the long way hands to has
// a shell, the files and the web and cannot be narrowed, so a program approved
// under a tighter ceiling than that does not fall back at all
// ([exec.DeoptHeld]). The held case is pinned by its own test below.
func testOpenManifest(name, purpose string) exec.Manifest {
	manifest := testManifest(name, purpose)
	manifest.Whitelist = []string{"bash"}
	return manifest
}

// headlessRegistry is a registry with no provider behind it. The generalist it
// is built with is never run in these tests — where the long way is exercised, a
// scripted runner is registered under the baseline's own name and the lookup
// finds that first, exactly as it finds a compiled-in worker before a bundle.
func headlessRegistry(t *testing.T, runners ...exec.Runner) *exec.Registry {
	t.Helper()
	registry := exec.NewRegistry(exec.NewLinear(nil, nil, nil, 0, 0, 0))
	for _, runner := range runners {
		if err := registry.RegisterRunner(runner); err != nil {
			t.Fatalf("registering %q: %v", runner.Manifest().Name, err)
		}
	}
	return registry
}

// drive runs one headless invocation and hands back both streams and whatever
// the process would have left with.
func drive(t *testing.T, registry *exec.Registry, name string, input string,
	env func(exec.Manifest) exec.Env) (stdout, stderr string, err error) {
	t.Helper()
	var out, errs bytes.Buffer
	err = runSubharness(context.Background(), subharnessRun{
		registry: registry, name: name, input: json.RawMessage(input),
		env: env, journal: &runJournal{}, stdout: &out, stderr: &errs,
	})
	return out.String(), errs.String(), err
}

func TestAFinishedHeadlessRunPrintsWhatItPromisedAndLeavesWithNothingToSay(t *testing.T) {
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{
				Output: json.RawMessage("{\n  \"filed\": 3\n}"),
				Report: "three notes filed, none left over",
			}, nil
		},
	}
	stdout, stderr, err := drive(t, headlessRegistry(t, program), "tidy-notes", `{"folder":"inbox"}`, nil)
	if err != nil {
		t.Fatalf("a finished run should leave with nothing to say, got %v", err)
	}
	if !strings.Contains(stdout, "three notes filed") {
		t.Errorf("the account belongs on stdout, got %q", stdout)
	}
	// The typed output is the LAST line and it is one line, so a caller reading
	// the answer off the end of the stream gets the whole of it.
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if last := lines[len(lines)-1]; last != `{"filed":3}` {
		t.Errorf("the output should be the last line, whole and on one line; got %q", last)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("a run that spent nothing and wrote nothing should say nothing on stderr, got %q", stderr)
	}
}

func TestARunThatDidNotFinishSaysWhyOnStderrAndNeverCallsItAFailure(t *testing.T) {
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Incomplete: "it ran out of room before it was finished"}, nil
		},
	}
	stdout, stderr, err := drive(t, headlessRegistry(t, program), "tidy-notes", `{"folder":"inbox"}`, nil)
	var status exitStatus
	if !errors.As(err, &status) || status != exitIncomplete {
		t.Fatalf("a run that ran and did not finish leaves with %d, got %v", int(exitIncomplete), err)
	}
	if !strings.Contains(stderr, "it ran out of room before it was finished") {
		t.Errorf("the reason belongs on stderr, got %q", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("a run that produced nothing should put nothing on stdout, got %q", stdout)
	}
	// THE VOCABULARY LAW. Work is running, finishing, done, incomplete, or needs
	// your look — and a run that did not finish is none of the words below.
	for _, banned := range []string{"failed", "failure", "error", "aborted"} {
		if strings.Contains(strings.ToLower(stderr), banned) {
			t.Errorf("%q reached a person: %q", banned, stderr)
		}
	}
}

// TestAQuestionWithNoDefaultStopsTheRunAndIsNeverAnApproval is the whole reason
// this door exists. internal/subharness/exec_model.go's gate auto-approves when
// nobody was wired to answer and writes an apology into the trail afterwards;
// this one stops and says which question it stopped at.
func TestAQuestionWithNoDefaultStopsTheRunAndIsNeverAnApproval(t *testing.T) {
	manifest := testManifest("send-digest", "send the weekly digest to the list")
	var journal runJournal
	var progress bytes.Buffer
	env := newHeadlessEnv(nil, nil, approval.Policy{}, manifest, &journal, &progress)

	answer, err := env.Ask(context.Background(), "send it to the whole list?", exec.AskOptions{
		Options: []string{"yes", "no"},
	})
	if err != nil {
		t.Fatalf("a gate with nobody behind it is an answer, not a fault: %v", err)
	}
	if !answer.Unanswered {
		t.Error("a gate that declared no default has to come back unanswered")
	}
	if answer.Text != "" {
		t.Errorf("nothing may be answered on the person's behalf, got %q", answer.Text)
	}
	if answer.TakingOver {
		t.Error("taking over is a person continuing by hand, and there is no person here")
	}
	// It is never an approval, however the word is spelled.
	for _, approved := range []string{"yes", "y", "ok", "approve", "allow", "true"} {
		if strings.EqualFold(answer.Text, approved) {
			t.Fatalf("the gate auto-approved with %q", answer.Text)
		}
	}

	program := &scriptedRunner{
		manifest: manifest,
		body: func(ctx context.Context, _ json.RawMessage, runEnv exec.Env) (exec.RunResult, error) {
			asked, askErr := runEnv.Ask(ctx, "send it to the whole list?", exec.AskOptions{})
			if askErr != nil {
				return exec.RunResult{}, askErr
			}
			if asked.Unanswered {
				return exec.RunResult{
					Incomplete: `it stopped at a question nobody was here to answer: "send it to the whole list?"`,
				}, nil
			}
			return exec.RunResult{Output: json.RawMessage(`{"sent":true}`)}, nil
		},
	}
	stdout, stderr, runErr := drive(t, headlessRegistry(t, program), "send-digest", `{"list":"weekly"}`,
		func(exec.Manifest) exec.Env {
			return newHeadlessEnv(nil, nil, approval.Policy{}, manifest, &journal, &progress)
		})
	var status exitStatus
	if !errors.As(runErr, &status) || status != exitIncomplete {
		t.Fatalf("a run stopped by a question ran and did not finish, so it leaves with %d, got %v",
			int(exitIncomplete), runErr)
	}
	if !strings.Contains(stderr, "send it to the whole list?") {
		t.Errorf("the question it stopped at belongs on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "sent") {
		t.Errorf("nothing was sent and nothing may say it was, got %q", stdout)
	}
}

func TestAQuestionWithADeclaredDefaultIsAnsweredWithIt(t *testing.T) {
	manifest := testManifest("send-digest", "send the weekly digest to the list")
	var journal runJournal
	env := newHeadlessEnv(nil, nil, approval.Policy{}, manifest, &journal, io.Discard)

	answer, err := env.Ask(context.Background(), "which list?", exec.AskOptions{
		Options: []string{"weekly", "monthly"}, Default: "weekly",
	})
	if err != nil {
		t.Fatalf("a declared default is an answer: %v", err)
	}
	if answer.Unanswered {
		t.Error("a gate that declared what it means unattended is answered, not unanswered")
	}
	if answer.Text != "weekly" {
		t.Errorf("the declared answer is the answer, got %q", answer.Text)
	}
	// The row says nobody was here to give it, which is what makes the journal
	// readable afterwards as an account of an unattended run.
	if len(journal.entries) != 1 || journal.entries[0].Call != exec.CallAsk {
		t.Fatalf("the question should be journaled as one ask, got %#v", journal.entries)
	}
	if !strings.Contains(journal.entries[0].Note, "nobody was here") {
		t.Errorf("the row has to say nobody was here to answer it, got %q", journal.entries[0].Note)
	}
}

func TestAnUnknownSubharnessNamesTheTypoAndSaysWhatThereIs(t *testing.T) {
	program := &scriptedRunner{manifest: testManifest("tidy-notes", "file loose notes")}
	baseline := &scriptedRunner{manifest: testManifest(exec.LinearSubharness, "")}
	registry := headlessRegistry(t, program, baseline)

	_, _, err := drive(t, registry, "tidy-note", `{}`, nil)
	if err == nil {
		t.Fatal("a name nothing has is told, never served")
	}
	var status exitStatus
	if errors.As(err, &status) {
		t.Fatalf("a typo could not be made to happen at all, so it leaves through the default: got %v", err)
	}
	if !strings.Contains(err.Error(), `"tidy-note"`) {
		t.Errorf("the message has to name what was typed, got %q", err)
	}
	if !strings.Contains(err.Error(), "tidy-notes") {
		t.Errorf("the message has to say what there is, got %q", err)
	}
	// The generalist resolves by name and is on no list a person picks from.
	if strings.Contains(err.Error(), exec.LinearSubharness) {
		t.Errorf("the baseline is what you get when you pick nothing, not something to pick: %q", err)
	}
}

func TestARunThatNeededACloserLookIsHandledTheLongWayWithTheOriginalInput(t *testing.T) {
	const original = `{"brief":"reconcile the March statement"}`
	program := &scriptedRunner{
		manifest: testOpenManifest("reconcile", "reconcile a bank statement against the ledger"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{FellBack: "this one needs the statement in the repository"}, nil
		},
	}
	generalist := &scriptedRunner{
		manifest: testManifest(exec.LinearSubharness, ""),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{
				Output: json.RawMessage(`{"result":"reconciled by hand"}`),
				Report: "reconciled by hand",
			}, nil
		},
	}
	stdout, stderr, err := drive(t, headlessRegistry(t, program, generalist), "reconcile", original, nil)
	if err != nil {
		t.Fatalf("a long way that finished is a run that finished, got %v", err)
	}
	if generalist.ran != 1 {
		t.Fatalf("the long way is the generalist, run once; it ran %d times", generalist.ran)
	}
	if string(generalist.saw) != original {
		t.Errorf("the long way gets the ORIGINAL input, unchanged; it got %q", generalist.saw)
	}
	if !strings.Contains(stderr, exec.DeoptWord) {
		t.Errorf("the person is told the step needed a closer look, got %q", stderr)
	}
	if !strings.Contains(stderr, "this one needs the statement in the repository") {
		t.Errorf("the program's own reason is carried through, got %q", stderr)
	}
	if !strings.Contains(stdout, "reconciled by hand") {
		t.Errorf("what the long way produced is the answer, got %q", stdout)
	}
}

func TestARunThatCouldNotBeMadeToHappenIsAlsoHandledTheLongWay(t *testing.T) {
	const original = `{"brief":"reconcile the March statement"}`
	program := &scriptedRunner{
		manifest: testOpenManifest("reconcile", "reconcile a bank statement against the ledger"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{}, errors.New("the bundle stopped halfway through")
		},
	}
	generalist := &scriptedRunner{
		manifest: testManifest(exec.LinearSubharness, ""),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Output: json.RawMessage(`{"result":"done the long way"}`)}, nil
		},
	}
	stdout, stderr, err := drive(t, headlessRegistry(t, program, generalist), "reconcile", original, nil)
	if err != nil {
		t.Fatalf("a long way that finished is a run that finished, got %v", err)
	}
	if string(generalist.saw) != original {
		t.Errorf("the long way gets the ORIGINAL input, unchanged; it got %q", generalist.saw)
	}
	if !strings.Contains(stderr, exec.DeoptWord) {
		t.Errorf("the person is told the step needed a closer look, got %q", stderr)
	}
	if !strings.Contains(stdout, "done the long way") {
		t.Errorf("what the long way produced is the answer, got %q", stdout)
	}
}

func TestARunWithNothingUnderItToFallBackOnCouldNotBeMadeToHappen(t *testing.T) {
	program := &scriptedRunner{
		manifest: testOpenManifest("reconcile", "reconcile a bank statement against the ledger"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{}, errors.New("the bundle stopped halfway through")
		},
	}
	// There is no fourth worker under the generalist. When the long way cannot be
	// made to happen either, the run leaves through main's own default — which is
	// exit 1, "it could not be made to happen" — and never through the partial.
	generalist := &scriptedRunner{
		manifest: testManifest(exec.LinearSubharness, ""),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{}, errors.New("there was nothing here to hand this to")
		},
	}
	stdout, _, err := drive(t, headlessRegistry(t, program, generalist), "reconcile", `{"brief":"x"}`, nil)
	if err == nil {
		t.Fatal("a run with nothing under it cannot quietly succeed")
	}
	var status exitStatus
	if errors.As(err, &status) {
		t.Fatalf("this one never ran, so it is not a partial: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("nothing was produced and nothing may be printed as though it was, got %q", stdout)
	}
}

// THE CEILING A PERSON APPROVED SURVIVES THE FALLBACK, on this door too. The
// general worker the long way hands to has a shell, the files and the web and
// cannot be narrowed, so a program approved under a tighter ceiling than that is
// not handed to it: the run stops incomplete, in the person's own register, and
// the generalist is never reached.
func TestALongWayThatWouldReachPastTheCeilingIsNotTaken(t *testing.T) {
	program := &scriptedRunner{
		manifest: testManifest("reconcile", "reconcile a bank statement against the ledger"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{FellBack: "this one needs the statement in the repository"}, nil
		},
	}
	generalist := &scriptedRunner{
		manifest: testManifest(exec.LinearSubharness, ""),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Report: "reconciled by hand"}, nil
		},
	}
	stdout, stderr, err := drive(t, headlessRegistry(t, program, generalist), "reconcile",
		`{"brief":"reconcile the March statement"}`, nil)

	var status exitStatus
	if !errors.As(err, &status) || status != exitIncomplete {
		t.Fatalf("a run that stopped without finishing leaves with %d, got %v", int(exitIncomplete), err)
	}
	if generalist.ran != 0 {
		t.Fatalf("a program approved for nothing reached the general worker anyway, %d times", generalist.ran)
	}
	if !strings.Contains(stderr, exec.DeoptHeldWord) {
		t.Errorf("the person was not told why nothing else was tried, got %q", stderr)
	}
	if strings.Contains(stderr, exec.DeoptWord) {
		t.Errorf("the run claimed it was handled the long way, got %q", stderr)
	}
	if !strings.Contains(stderr, "this one needs the statement in the repository") {
		t.Errorf("the program's own reason is carried through, got %q", stderr)
	}
	if strings.Contains(stdout, "reconciled by hand") {
		t.Errorf("the general worker's answer reached stdout for a run it never took: %q", stdout)
	}
}

// TestTheThreeToolRefusalsAreThreeDifferentFacts pins internal/exec/env.go's own
// sentence: a tool not on the whitelist is refused here, a tool not in this
// build is absent, and the two are told apart because they are different facts
// about what the person can fix. The third is this surface's own — a call
// somebody would have had to approve, in a room with nobody in it.
func TestTheThreeToolRefusalsAreThreeDifferentFacts(t *testing.T) {
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	var journal runJournal

	onTheList := testManifest("tidy-notes", "file loose notes")
	onTheList.Whitelist = []string{"sh"}
	env := newHeadlessEnv(nil, exec.NewToolbox(space, "tidy-notes", nil), approval.Policy{},
		onTheList, &journal, io.Discard)

	// Not on the list, though this build has it.
	_, err = env.Tool(context.Background(), "web", map[string]any{"q": "anything"})
	if err == nil || !strings.Contains(err.Error(), "not one of the tools") {
		t.Errorf("a tool the program may not use is refused as such, got %v", err)
	}

	// On the list, and this build does not have it.
	absent := testManifest("tidy-notes", "file loose notes")
	absent.Whitelist = []string{"gmail_send"}
	env = newHeadlessEnv(nil, exec.NewToolbox(space, "tidy-notes", nil), approval.Policy{},
		absent, &journal, io.Discard)
	_, err = env.Tool(context.Background(), "gmail_send", map[string]any{"to": "nobody"})
	if err == nil || !strings.Contains(err.Error(), "does not have it") {
		t.Errorf("a tool this build has not got is absent, not forbidden; got %v", err)
	}

	// On the list, in this build, and it needs somebody to say yes. The zero
	// policy prompts for everything, and there is nobody here to prompt.
	env = newHeadlessEnv(nil, exec.NewToolbox(space, "tidy-notes", nil), approval.Policy{},
		onTheList, &journal, io.Discard)
	_, err = env.Tool(context.Background(), "sh", map[string]any{"cmd": "true"})
	if err == nil || !strings.Contains(err.Error(), "nobody is here to ask") {
		t.Errorf("a call that needed approval is refused, never waved through; got %v", err)
	}

	// A program that named no tools at all spends on the model and nothing else.
	silent := testManifest("tidy-notes", "file loose notes")
	env = newHeadlessEnv(nil, exec.NewToolbox(space, "tidy-notes", nil), approval.Policy{},
		silent, &journal, io.Discard)
	_, err = env.Tool(context.Background(), "sh", map[string]any{"cmd": "true"})
	if err == nil || !strings.Contains(err.Error(), "spends on the model and nothing else") {
		t.Errorf("an empty whitelist is a ceiling of nothing, got %v", err)
	}

	// Every one of them is a call that was made and is on the record.
	if len(journal.entries) != 4 {
		t.Fatalf("every refusal is journaled before its answer returns; got %d rows", len(journal.entries))
	}
	for _, entry := range journal.entries {
		if entry.Call != exec.CallTool || entry.Err == "" {
			t.Errorf("a refused tool call is a tool row carrying what it was refused for, got %#v", entry)
		}
	}
}

// TestTheBundleMemoryIsAbsentRatherThanQuietlyDropped pins the store lane's seam.
// A note accepted and thrown away would teach a program it had learned something
// it had not, so both doors answer the contract's own typed absence.
func TestTheBundleMemoryIsAbsentRatherThanQuietlyDropped(t *testing.T) {
	var journal runJournal
	env := newHeadlessEnv(nil, nil, approval.Policy{},
		testManifest("tidy-notes", "file loose notes"), &journal, io.Discard)

	if err := env.Remember(context.Background(), "briefs here always arrive as a PDF"); !errors.Is(err, exec.ErrNotWired) {
		t.Errorf("remember has nothing behind it yet and says so, got %v", err)
	}
	if _, err := env.Recall(context.Background(), ""); !errors.Is(err, exec.ErrNotWired) {
		t.Errorf("recall has nothing behind it yet and says so, got %v", err)
	}
}

func TestTheInputIsRequiredAndHasToBeJSON(t *testing.T) {
	if _, err := readSubharnessInput("", strings.NewReader("")); err == nil {
		t.Error("a run with no input has been handed nothing to do")
	}
	if _, err := readSubharnessInput("-", strings.NewReader("   \n")); err == nil {
		t.Error("an empty input is nothing to do either")
	}
	if _, err := readSubharnessInput("-", strings.NewReader("not json at all")); err == nil {
		t.Error("input that is not JSON cannot be a typed front door")
	}
	material, err := readSubharnessInput("-", strings.NewReader(`{"folder":"inbox"}`))
	if err != nil {
		t.Fatalf("piped input: %v", err)
	}
	if string(material) != `{"folder":"inbox"}` {
		t.Errorf("the input reaches the run as it was written, got %q", material)
	}
}

// TestTheLedgerIsTheJournalsOwnSum keeps the one law that makes a receipt
// trustworthy: there is no counter beside the journal, so the two cannot
// disagree. An unreported run draws nothing rather than $0.00.
func TestTheLedgerIsTheJournalsOwnSum(t *testing.T) {
	journal := &runJournal{}
	_ = exec.Record(journal, exec.JournalEntry{Call: exec.CallAI, Spend: exec.Spend{
		Model: "a/model", Calls: 1, Input: 900, Output: 120, CostUSD: 0.0031,
	}})
	_ = exec.Record(journal, exec.JournalEntry{Call: exec.CallAI, Spend: exec.Spend{
		Model: "a/model", Calls: 1, Input: 400, Output: 60, CostUSD: 0.0012,
	}})
	ledger := journal.Ledger()
	if ledger.Calls != 2 || ledger.Input != 1300 || ledger.Output != 180 {
		t.Errorf("the ledger is the sum of the rows, got %#v", ledger)
	}
	if journal.entries[1].Seq != 2 {
		t.Errorf("the rows are numbered from one within a run, got %d", journal.entries[1].Seq)
	}

	var said bytes.Buffer
	sayLedger(&said, ledger)
	if !strings.Contains(said.String(), "$0.0043") || !strings.Contains(said.String(), "a/model") {
		t.Errorf("what a run cost is said once, from the journal; got %q", said.String())
	}

	var silent bytes.Buffer
	sayLedger(&silent, exec.Spend{Calls: 3})
	if silent.Len() != 0 {
		t.Errorf("a provider that said nothing draws nothing, got %q", silent.String())
	}
}
