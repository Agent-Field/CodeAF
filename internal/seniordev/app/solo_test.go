//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/fullverification"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

// soloEvents decodes the NDJSON the pipeline emitted so a test can assert on
// what the artifact will actually contain, rather than on internal state a
// reader of the run will never see.
func soloEvents(t *testing.T, raw *bytes.Buffer) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			continue
		}
		events = append(events, decoded)
	}
	return events
}

func soloStageEvents(t *testing.T, raw *bytes.Buffer, stage string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, event := range soloEvents(t, raw) {
		if name, _ := event["stage"].(string); name != stage {
			continue
		}
		// The stage name and status live on the envelope; everything the test
		// asserts about lives in data. Flatten so a test reads one map.
		flattened := map[string]any{"status": event["status"]}
		if data, ok := event["data"].(map[string]any); ok {
			for key, value := range data {
				flattened[key] = value
			}
		}
		matched = append(matched, flattened)
	}
	return matched
}

// soloPipeline builds a pipeline over a real git workspace with a stub backend,
// plus the submit freezer wired exactly as runSolo wires it.
func soloPipeline(t *testing.T) (*pipeline, *soloState, string, *bytes.Buffer) {
	t.Helper()
	workspace, base := guardWorkspace(t)
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(&events), Notes: io.Discard,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	t.Cleanup(runner.runtime.Close)
	// A run that reaches submit has written one; tests of submit start there.
	// TestSubmitRefusedWithoutAChecklist removes it to exercise the gate.
	if err := writeFile(filepath.Join(workspace, ".senior-dev", "checklist.md"),
		"- [ ] the thing the request asked for\n"); err != nil {
		t.Fatal(err)
	}
	state := &soloState{baseSHA: base}
	runner.runtime.registry.SetSubmitFreezer(
		func(_ context.Context, submission tool.Submission) (string, error) {
			return runner.soloFreezeWithContext(context.Background(), state, submission)
		},
	)
	return runner, state, base, &events
}

func soloSubmission(reason string) tool.Submission {
	return tool.Submission{
		Reason: reason, Evidence: "make test: exit 0, 41 passed",
		ChecklistSatisfied: true, SessionID: "ses_solo",
	}
}

func TestSoloIntakeWritesTheRequestVerbatim(t *testing.T) {
	// The spec reaches every later stage as a file, never as a paraphrase. A
	// restated request drops the exact identifiers the original names, and the
	// run then ships code that does the right thing under names the request
	// never used.
	runner, _, _, events := soloPipeline(t)
	goal := "Add `expandShorthand(property, value)` to lib/shorthand.js.\n" +
		"It MUST be named exactly that. Trailing spaces matter:   \n\ttabs too."
	if err := runner.soloIntake(goal); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(runner.workspace, ".senior-dev", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != goal {
		t.Fatalf("spec.md was not byte-identical to the request:\n got %q\nwant %q", written, goal)
	}
	captured := soloStageEvents(t, events, "intake")
	if len(captured) != 1 || captured[0]["status"] != "captured" {
		t.Fatalf("intake events = %#v", captured)
	}
}

func TestSubmitFreezesTheTreeAndRefusesASecondSubmission(t *testing.T) {
	// One submission per run. A second one is not a mistake to absorb quietly:
	// the model is telling us it thinks it can still change the answer, and it
	// needs to be told plainly that it cannot.
	runner, state, _, events := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}

	description, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("feature implemented"))
	if err != nil {
		t.Fatalf("first submit refused: %v", err)
	}
	if !strings.Contains(description, "file") {
		t.Fatalf("freeze description = %q", description)
	}
	candidate := state.candidate()
	if candidate == nil || candidate.TreeSHA == "" || candidate.CommitSHA == "" {
		t.Fatalf("candidate = %#v", candidate)
	}
	if candidate.Reason != "feature implemented" || !candidate.ChecklistSatisfied {
		t.Fatalf("the model's claim was not recorded verbatim: %#v", candidate)
	}

	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("actually, this version")); err == nil {
		t.Fatal("a second submission was accepted")
	} else if !strings.Contains(err.Error(), "cannot be replaced") {
		t.Fatalf("second-submit refusal = %v", err)
	}
	if state.candidate().Reason != "feature implemented" {
		t.Fatal("the second submission overwrote the frozen candidate")
	}

	var frozen []map[string]any
	for _, event := range soloStageEvents(t, events, "submit") {
		if event["status"] == "frozen" {
			frozen = append(frozen, event)
		}
	}
	if len(frozen) != 1 {
		t.Fatalf("frozen events = %d, want exactly 1", len(frozen))
	}
	if frozen[0]["evidence"] != "make test: exit 0, 41 passed" {
		t.Fatalf("submit event lost the evidence: %#v", frozen[0])
	}
}

func TestSubmitOnAnUnchangedTreeIsRefused(t *testing.T) {
	// A patch containing only probe scripts and a patch containing only
	// documentation are the same failure -- declaring done on a tree that
	// implements nothing -- and submit is the cheapest place in the run to
	// catch it.
	runner, state, _, _ := soloPipeline(t)
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err == nil {
		t.Fatal("submitting an unchanged tree was accepted")
	} else if !strings.Contains(err.Error(), "identical to the base commit") {
		t.Fatalf("refusal = %v", err)
	}
	if state.candidate() != nil {
		t.Fatal("a refused submit still froze something")
	}
}

func TestSubmitIgnoresSeniorDevsOwnArtifactsWhenDecidingSomethingChanged(t *testing.T) {
	// senior-dev writes .senior-dev/ into the workspace it works in: the session
	// database, spec.md, the pinned command. None of that is part of the answer,
	// so a tree whose only content is senior-dev's own bookkeeping is an empty
	// patch.
	//
	// Deciding "did anything change" from the raw tree would therefore accept
	// exactly the submissions worth refusing: the run reports success, and
	// ships nothing.
	runner, state, _, _ := soloPipeline(t)
	if err := runner.soloIntake("write a feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(runner.workspace, ".senior-dev", "pinned.txt"), []byte("make test\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err == nil {
		t.Fatal("a tree containing only senior-dev's own artifacts was accepted as a submission")
	} else if !strings.Contains(err.Error(), "identical to the base commit") {
		t.Fatalf("refusal = %v", err)
	}

	// One real file makes it a real submission, and it is the only one counted.
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatalf("a genuine one-file submission was refused: %v", err)
	}
	if files := state.candidate().PatchFiles; files != 1 {
		t.Fatalf("PatchFiles = %d, want 1 — senior-dev's own artifacts are being counted", files)
	}
}

func TestShipRestoresTheFrozenCandidateWhenTheTreeMovesAfterSubmission(t *testing.T) {
	// Nothing in the pipeline edits after submit, but "nothing should" is not a
	// guarantee, and a run that keeps editing after submitting can leave a tree
	// whose build no longer passes. Post-submission edits are not part of the
	// answer whether they look like improvements or not.
	runner, state, _, events := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "the submitted version\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("implemented")); err != nil {
		t.Fatal(err)
	}
	frozenTree := state.candidate().TreeSHA

	// Something touches the tree after the freeze.
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "a later, unblessed edit\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "debris.tmp"), "scratch\n"); err != nil {
		t.Fatal(err)
	}

	outcome := &soloOutcome{Status: "pass", Frozen: state.candidate()}
	runner.soloRestoreIfDiverged(state, outcome)

	content, err := os.ReadFile(filepath.Join(runner.workspace, "feature.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "the submitted version\n" {
		t.Fatalf("shipped file = %q, want the submitted version", content)
	}
	restored := soloStageEvents(t, events, "ship")
	if len(restored) != 1 || restored[0]["status"] != "restored" {
		t.Fatalf("ship events = %#v", restored)
	}
	if restored[0]["to_tree"] != frozenTree {
		t.Fatalf("restored to %v, want the frozen tree %v", restored[0]["to_tree"], frozenTree)
	}
	if reason, _ := restored[0]["reason"].(string); !strings.Contains(reason, "after submission") {
		t.Fatalf("restore event does not say why: %#v", restored[0])
	}
}

func TestShipLeavesAnUntouchedTreeAlone(t *testing.T) {
	// The complement: when nothing moved, ship must not run a checkout at all.
	// A restore that fires on every run is a restore nobody will believe when
	// it matters.
	runner, state, _, events := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("implemented")); err != nil {
		t.Fatal(err)
	}
	runner.soloRestoreIfDiverged(state, &soloOutcome{Status: "pass", Frozen: state.candidate()})
	shipped := soloStageEvents(t, events, "ship")
	if len(shipped) != 1 || shipped[0]["status"] != "unchanged" {
		t.Fatalf("ship events = %#v, want a single unchanged", shipped)
	}
}

func TestTerminalEventIsEmittedOnceAndSaysWhyTheRunEnded(t *testing.T) {
	// "Why did it exit?" is a question the event stream has to answer without
	// a log. A decision that reports only through a log line is lost with the
	// log.
	runner, state, _, events := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("auto-toc rule implemented and green")); err != nil {
		t.Fatal(err)
	}
	outcome := &soloOutcome{Status: "pass", Frozen: state.candidate(), Nudges: 1}
	runner.soloTerminal(outcome, "submitted and verified")

	// Asserted on the payload that reaches the run's terminal event
	// (type=="terminal"), not on a stage event named "terminal": a terminal
	// that carries nothing but a cost cannot answer "did it submit?".
	_ = events
	data := outcome.TerminalData
	if data == nil {
		t.Fatal("no terminal payload was recorded")
	}
	for _, key := range []string{
		"reason", "submission_reason", "submission_evidence",
		"checklist_satisfied", "patch_bytes", "frozen_tree", "nudges",
	} {
		if _, ok := data[key]; !ok {
			t.Fatalf("terminal payload is missing %q: %#v", key, data)
		}
	}
	if data["status"] != "pass" || data["submitted"] != true {
		t.Fatalf("terminal payload = %#v", data)
	}
}

func TestAnUnsubmittedRunSaysSoRatherThanClaimingAnAttempt(t *testing.T) {
	// A run that never submitted did not finish. Reporting it as anything else
	// turns "done" into "whatever the tree looked like when the budget
	// expired".
	runner, state, _, events := soloPipeline(t)
	outcome := &soloOutcome{Status: "fail"}
	runner.soloShip(context.Background(), state, outcome, nil)

	if outcome.Status != "unsubmitted" {
		t.Fatalf("status = %q, want unsubmitted", outcome.Status)
	}
	_ = events
	data := outcome.TerminalData
	if data == nil || data["submitted"] != false {
		t.Fatalf("terminal payload = %#v", data)
	}
	if reason, _ := data["reason"].(string); !strings.Contains(reason, "without calling submit") {
		t.Fatalf("terminal reason = %q", reason)
	}
}

func soloTestVerification(exit int, dead bool) projectVerificationResult {
	entrypoint := fullverification.Entrypoint{
		Kind: fullverification.KindBuild, Command: "cargo build", Source: "Cargo.toml",
	}
	command := map[string]any{
		"cmd": entrypoint.Command, "exit": float64(exit),
		"tail": "error: could not compile `widget`",
	}
	if dead {
		command["suite_dead"] = true
	}
	result := projectVerificationResult{Commands: []any{command}}
	if exit != 0 {
		result.Failed = &entrypoint
		result.Failure = "cargo build failed"
	}
	return result
}

func TestUnsubmittedDeadTreeRestoresExactStartingTree(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "preexisting.txt"), "keep me\n"); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloCaptureStart(state); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "broken.rs"), "does not compile\n"); err != nil {
		t.Fatal(err)
	}
	runner.verifyForTest = func(context.Context) projectVerificationResult {
		return soloTestVerification(101, true)
	}
	outcome := &soloOutcome{Status: "fail", TerminalTrigger: "nudge-cap"}
	runner.soloShip(context.Background(), state, outcome, nil)

	if outcome.RestoreSource != "starting-tree" || !outcome.SuiteDead {
		t.Fatalf("finalization = %#v", outcome)
	}
	if _, err := os.Stat(filepath.Join(runner.workspace, "broken.rs")); !os.IsNotExist(err) {
		t.Fatalf("suite-dead file survived restore: %v", err)
	}
	kept, err := os.ReadFile(filepath.Join(runner.workspace, "preexisting.txt"))
	if err != nil || string(kept) != "keep me\n" {
		t.Fatalf("starting untracked file was not restored exactly: %q, %v", kept, err)
	}
}

func TestUnsubmittedOrdinaryFailureKeepsLiveTree(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	if err := runner.soloCaptureStart(state); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "solution.js"), "working solution\n"); err != nil {
		t.Fatal(err)
	}
	runner.verifyForTest = func(context.Context) projectVerificationResult {
		result := soloTestVerification(1, false)
		result.Commands[0].(map[string]any)["tail"] = "1 failed; 126 passed"
		return result
	}
	outcome := &soloOutcome{Status: "fail", TerminalTrigger: "landing-window"}
	runner.soloShip(context.Background(), state, outcome, nil)

	if outcome.RestoreSource != "" || outcome.SuiteDead {
		t.Fatalf("ordinary failure triggered rollback: %#v", outcome)
	}
	if got, err := os.ReadFile(filepath.Join(runner.workspace, "solution.js")); err != nil || string(got) != "working solution\n" {
		t.Fatalf("live solution was not preserved: %q, %v", got, err)
	}
}

func TestUnsubmittedDeadTreeRestoresLatestCoherentCheckpoint(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	if err := runner.soloCaptureStart(state); err != nil {
		t.Fatal(err)
	}
	feature := filepath.Join(runner.workspace, "feature.rs")
	if err := writeFile(feature, "partial but coherent\n"); err != nil {
		t.Fatal(err)
	}
	runner.verifyForTest = func(context.Context) projectVerificationResult {
		return soloTestVerification(1, false)
	}
	if result := runner.soloCheckUnsubmitted(
		context.Background(), state, time.Second, "nudge",
	); result == nil {
		t.Fatal("coherent checkpoint verification did not run")
	}
	if err := writeFile(feature, "mid-edit and suite-dead\n"); err != nil {
		t.Fatal(err)
	}
	runner.verifyForTest = func(context.Context) projectVerificationResult {
		return soloTestVerification(101, true)
	}
	outcome := &soloOutcome{Status: "fail", TerminalTrigger: "nudge-cap"}
	runner.soloShip(context.Background(), state, outcome, nil)

	if outcome.RestoreSource != "coherent-checkpoint" {
		t.Fatalf("restore source = %q, want coherent checkpoint", outcome.RestoreSource)
	}
	if got, err := os.ReadFile(feature); err != nil || string(got) != "partial but coherent\n" {
		t.Fatalf("coherent work was not restored: %q, %v", got, err)
	}
}

func TestSoloLandingReserveScalesWithoutConsumingShortRuns(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		90 * time.Minute: 12 * time.Minute,
		7 * time.Minute:  56 * time.Second,
		1 * time.Minute:  15 * time.Second,
	}
	for limit, want := range cases {
		if got := soloLandingReserve(limit); got != want {
			t.Errorf("reserve(%s) = %s, want %s", limit, got, want)
		}
	}
}

func TestUnsubmittedFindingsCarryFactsNotEncouragement(t *testing.T) {
	// The nudge exists to correct a specific mechanical belief, so it has to
	// carry what senior-dev can see for itself. On a clean tree the first
	// thing it must say is that there is no change at all -- the failure mode
	// where a run believes it implemented something it never wrote.
	runner, _, base, _ := soloPipeline(t)
	// The finding under test is about a MISSING checklist, so remove the one
	// soloPipeline provides.
	if err := os.Remove(filepath.Join(runner.workspace, ".senior-dev", "checklist.md")); err != nil {
		t.Fatal(err)
	}
	findings := runner.soloUnsubmittedFindings(base)
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "nothing has been implemented") {
		t.Fatalf("findings on an empty tree = %#v", findings)
	}
	if !strings.Contains(joined, "no pinned command") {
		t.Fatalf("findings do not mention the missing pinned command: %#v", findings)
	}
	if !strings.Contains(joined, "checklist.md") {
		t.Fatalf("findings do not mention the missing checklist: %#v", findings)
	}

	if err := os.WriteFile(
		filepath.Join(runner.workspace, ".senior-dev", "pinned.txt"),
		[]byte("pnpm exec jest auto-toc\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if pinned := runner.readPinnedCommand(); pinned != "pnpm exec jest auto-toc" {
		t.Fatalf("pinned command = %q", pinned)
	}
}

func TestSoloPromptCarriesTheMechanicsSeniorDevReads(t *testing.T) {
	// The run instruction carries mechanics only: the three files senior-dev
	// reads or refuses over, and the one way the run ends. A silent edit that
	// drops one of them breaks a code path no build catches -- submit refuses
	// without the checklist, and readPinnedCommand has no other writer.
	prompt := buildSoloPrompt("Add expandShorthand to lib/shorthand.js", "", ".senior-dev/checklist.md")
	for _, mechanic := range []string{
		"Add expandShorthand to lib/shorthand.js", // the request, verbatim and first
		".senior-dev/spec.md",                     // the spec is a file, not a memory
		".senior-dev/pinned.txt",                  // readPinnedCommand's only writer
		".senior-dev/checklist.md",                // soloFreeze refuses without it
		"[ ] ",                                    // the form soloChecklistItem counts
		"submit",                                  // the run's only ending
		"Nothing else ends it",                    // and it is the only one
		"refuses",                                 // a refusal is not the end of the run
	} {
		if !strings.Contains(prompt, mechanic) {
			t.Errorf("solo prompt no longer contains %q", mechanic)
		}
	}
	if strings.Contains(prompt, "acceptance contract") ||
		strings.Contains(prompt, "contract.json") {
		t.Error("the prompt names an acceptance contract, which nothing in the run reads")
	}
	// The prompt states mechanics, not history and not pacing: no anecdote
	// from a past run, no fraction of wall to aim for, and no instruction about
	// when to start editing.
	for _, regression := range []string{
		"Finishing early", "winning", "A run that shipped",
		"A run that implemented", "read-only", "Do not skip ahead",
	} {
		if strings.Contains(prompt, regression) {
			t.Errorf("solo prompt carries history or pacing advice again: %q", regression)
		}
	}
}

func TestNudgeEscalatesOnTheLastAttempt(t *testing.T) {
	// The bound has to be visible to the model. A nudge loop the model cannot
	// see the end of is one it can keep deferring.
	early := soloNudge(1, []string{"git status is clean"})
	last := soloNudge(soloMaxNudges, []string{"git status is clean"})
	if strings.Contains(early, "last prompt") {
		t.Error("the first nudge already claims to be the last")
	}
	if !strings.Contains(last, "last prompt") {
		t.Error("the final nudge does not say it is final")
	}
	if !strings.Contains(early, "git status is clean") {
		t.Error("the nudge dropped the findings")
	}
	// What an ignored nudge actually does, stated as solo_ship.go does it: the
	// run is recorded unsubmitted and the live tree is what it leaves behind.
	// It is NOT true that such a run ships nothing.
	if !strings.Contains(early, "unsubmitted") {
		t.Error("the nudge does not say what happens if it is ignored")
	}
	if strings.Contains(early, "ships nothing") {
		t.Error("the nudge claims an unsubmitted run ships nothing, which it does not")
	}
}

// TestSubmitRefusedWithoutAChecklist pins the one checklist refusal there is.
// A run that writes no checklist at all has nothing to check its work against
// and has not finished; such runs ship mid-edit trees that break pre-existing
// tests.
func TestSubmitRefusedWithoutAChecklist(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(runner.workspace, ".senior-dev", "checklist.md")); err != nil {
		t.Fatal(err)
	}
	_, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done"))
	if err == nil {
		t.Fatal("submit was accepted with no checklist")
	}
	if !strings.Contains(err.Error(), "checklist.md") {
		t.Fatalf("refusal = %q, want it to name the missing checklist", err)
	}
	if state.candidate() != nil {
		t.Fatal("a refused submission froze a candidate")
	}
}

// TestSubmitCountsTicksButDoesNotGateOnThem is the other half. Models
// routinely claim checklist_satisfied without ticking a box, so gating on
// ticks would refuse most submissions, verified passes included. The ticks are
// COUNTED and recorded next to the model's claim, and the gap between them is
// left visible rather than resolved into a refusal.
func TestSubmitCountsTicksButDoesNotGateOnThem(t *testing.T) {
	runner, state, _, events := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	checklist := "# Checklist\n\n- [ ] one\n- [x] two\n[ ] three\nnot an item\n"
	if err := writeFile(filepath.Join(runner.workspace, ".senior-dev", "checklist.md"), checklist); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatalf("submit refused despite a present checklist: %v", err)
	}
	submitted := soloStageEvents(t, events, "submit")
	if len(submitted) != 1 {
		t.Fatalf("submit events = %d, want 1", len(submitted))
	}
	if got := submitted[0]["checklist_items"]; got != float64(3) && got != 3 {
		t.Fatalf("checklist_items = %v (%T), want 3", got, got)
	}
	if got := submitted[0]["checklist_ticked"]; got != float64(1) && got != 1 {
		t.Fatalf("checklist_ticked = %v (%T), want 1", got, got)
	}
	// The claim and the observation are both present and both unreconciled.
	if _, ok := submitted[0]["checklist_satisfied"]; !ok {
		t.Fatal("the model's own claim is no longer recorded alongside the count")
	}
}

// clearGitIdentity strips every source of a git committer identity for the
// duration of the test: the repo config, the global and system files, and the
// GIT_* / EMAIL environment. A container image that ships no git config is a
// normal case, not an exotic one.
func clearGitIdentity(t *testing.T, workspace string) {
	t.Helper()
	for _, key := range []string{"user.name", "user.email"} {
		// --unset returns 5 when the key is already absent; that is fine.
		_ = gitRun(workspace, "config", "--unset", key)
	}
	// Unsetting is not enough on a developer machine: git happily invents
	// user@hostname when the hostname has a domain, and only refuses when it
	// cannot (a container yields an identity like 'root@0123abcd.(none)').
	// useConfigOnly makes that refusal unconditional, so the test reproduces
	// the container's condition on any host.
	if err := gitRun(workspace, "config", "user.useConfigOnly", "true"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL",
	} {
		t.Setenv(name, "") // registers restoration
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("HOME", t.TempDir())
}

// The freeze must not depend on the container having a git identity.
//
// If workspaceGit shelled plain `git` while eager-commit went through
// attribution.GitArgv, every wip(edit) commit would work and the one commit
// that decides what ships would fail with
//
//	could not record the tree: git commit-tree …: exit status 128:
//	Author identity unknown … unable to auto-detect email address
//
// leaving the model to run `git config user.email …` and submit again.
func TestFreezeRecordsTheTreeWithoutAConfiguredGitIdentity(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	clearGitIdentity(t, workspace)

	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(discardWriter{}), Notes: discardWriter{},
	})
	t.Cleanup(runner.runtime.Close)

	if err := writeFile(filepath.Join(workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	treeSHA, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatalf("capturing the tree failed: %v", err)
	}

	// Guard the guard: if this environment can still resolve an identity, the
	// assertion below would pass whether or not the fix is present.
	bare := exec.Command("git", "commit-tree", treeSHA, "-m", "identity probe")
	bare.Dir = workspace
	if out, bareErr := bare.CombinedOutput(); bareErr == nil {
		t.Fatalf("test environment still has a git identity, so it cannot detect the defect: %s", out)
	}

	commitSHA, err := runner.soloCommitTree(treeSHA, "the candidate")
	if err != nil {
		t.Fatalf("soloCommitTree needs a configured git identity: %v", err)
	}
	if commitSHA == "" {
		t.Fatal("soloCommitTree returned an empty commit")
	}
	recorded, err := runner.recorder.(*gitRecorder).git("rev-parse", commitSHA+"^{tree}")
	if err != nil || recorded != treeSHA {
		t.Fatalf("frozen commit points at %q (err %v); want tree %q", recorded, err, treeSHA)
	}
}

func TestCurrentTreeSHAIncludesTrackedIgnoredFiles(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	// currentTreeSHA only needs a workspace. Avoid starting the durable runtime,
	// which intentionally creates untracked .senior-dev state unrelated to this
	// exact-index regression.
	runner := &pipeline{workspace: workspace, recorder: newGitRecorder(workspace, func(string) {})}
	if err := writeFile(filepath.Join(workspace, ".gitignore"), "tracked-ignored.txt\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "tracked-ignored.txt"), "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", ".gitignore"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "-f", "tracked-ignored.txt"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "track an ignored file"); err != nil {
		t.Fatal(err)
	}

	headTree, err := runner.recorder.(*gitRecorder).git("rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := runner.currentTreeSHA()
	if err != nil || unchanged != headTree {
		t.Fatalf("unchanged tree = %q (err %v), want HEAD tree %q", unchanged, err, headTree)
	}
	if err := writeFile(filepath.Join(workspace, "tracked-ignored.txt"), "modified\n"); err != nil {
		t.Fatal(err)
	}
	modified, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	if modified == headTree {
		t.Fatal("tracked-but-ignored modification was absent from the captured tree")
	}
	content, err := runner.recorder.(*gitRecorder).git("show", modified+":tracked-ignored.txt")
	if err != nil || content != "modified" {
		t.Fatalf("captured ignored file = %q (err %v), want modified", content, err)
	}
}

// A restore must remove files ADDED after the checkpoint, not only revert
// edits. Overlay checkout cannot: every model-written file is tracked by
// eager-commit, so probe debris (a scratch test file the model added)
// survives `checkout --force <commit> -- .` + `clean -fd`, and the "restored"
// tree is not the checkpoint. The runs whose debris breaks the suite are
// exactly the ones that need this to work.
func TestRestoreRemovesFilesAddedAfterTheCheckpoint(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(discardWriter{}), Notes: discardWriter{},
	})
	t.Cleanup(runner.runtime.Close)

	if err := writeFile(filepath.Join(workspace, "feature.txt"), "good state\n"); err != nil {
		t.Fatal(err)
	}
	wantTree, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	commitSHA, err := runner.soloRecordTree(wantTree, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}

	// The debris: a file added AND tracked after the checkpoint, the way
	// eager-commit tracks everything the model writes.
	if err := writeFile(filepath.Join(workspace, "probe.test.js"), "debris\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "probe.test.js"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "wip(edit): probe.test.js"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "feature.txt"), "broken state\n"); err != nil {
		t.Fatal(err)
	}

	if err := runner.soloRestoreTree(commitSHA, wantTree); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "probe.test.js")); !os.IsNotExist(err) {
		t.Fatalf("added file survived the restore (stat err %v)", err)
	}
	got, err := runner.currentTreeSHA()
	if err != nil || got != wantTree {
		t.Fatalf("restored tree %q (err %v), want %q", got, err, wantTree)
	}
}

func TestATurnKilledByADroppedStreamIsRetriedInTheSameSession(t *testing.T) {
	// One dropped stream must not end the run: the run layer owns the only
	// retry and resumes the persisted session.
	runner, state, _, events := soloPipeline(t)
	outcome := soloOutcome{}
	turns := 0
	runner.turnForTest = func(_ context.Context, _, prompt string) (turnResult, error) {
		turns++
		switch turns {
		case 1:
			return turnResult{}, errors.New("stream error: unexpected EOF")
		case 2:
			if prompt != soloRecoveryPrompt() ||
				!strings.Contains(prompt, "failed and is not in context") {
				t.Fatalf("retry prompt does not say what happened: %q", prompt)
			}
			if err := writeFile(filepath.Join(runner.workspace, "fix.go"), "package fix\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
				t.Fatalf("freeze during the retried turn: %v", err)
			}
			return turnResult{}, nil
		default:
			t.Fatalf("turn %d should not run", turns)
			return turnResult{}, nil
		}
	}
	if err := runner.soloConverse(context.Background(), "fix it", state, &outcome); err != nil {
		t.Fatalf("converse: %v", err)
	}
	if outcome.TerminalTrigger != "submitted" || outcome.Nudges != 0 {
		t.Fatalf("trigger %q nudges %d, want submitted with 0 nudges",
			outcome.TerminalTrigger, outcome.Nudges)
	}
	var retried []map[string]any
	for _, event := range soloStageEvents(t, events, "implement") {
		if event["status"] == "transport-retry" {
			retried = append(retried, event)
		}
	}
	if len(retried) != 1 || retried[0]["class"] != "unexpected-eof" ||
		retried[0]["retry"] != float64(1) ||
		retried[0]["max_retries"] != float64(soloMaxRecoveryRetries) ||
		retried[0]["delay_ms"] != float64(5_000) {
		t.Fatalf("transport-retry events = %#v", retried)
	}
}

func TestExhaustedTransportRetriesStillGetALandingTurnAndAnHonestError(t *testing.T) {
	runner, state, _, events := soloPipeline(t)
	outcome := soloOutcome{}
	turns := 0
	status := uint64(503)
	runner.turnForTest = func(context.Context, string, string) (turnResult, error) {
		turns++
		return turnResult{}, &modelTurnError{
			kind: "APIError", message: "provider down", statusCode: &status,
			responseBody: `{"metadata":{"error_type":"provider_unavailable"}}`,
		}
	}
	err := runner.soloConverse(context.Background(), "fix it", state, &outcome)
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Fatalf("converse err = %v, want the original provider error", err)
	}
	if outcome.TerminalTrigger != "turn-error" {
		t.Fatalf("trigger %q, want turn-error", outcome.TerminalTrigger)
	}
	// 1 original turn + 3 transport retries + 1 landing turn.
	if turns != 5 {
		t.Fatalf("model turns = %d, want 5", turns)
	}
	retries, landings := 0, 0
	for _, event := range soloStageEvents(t, events, "implement") {
		if event["status"] == "transport-retry" {
			retries++
			if event["class"] != "provider-5xx" || event["http_status"] != float64(503) ||
				event["provider_code"] != "provider_unavailable" ||
				event["max_retries"] != float64(soloMaxRecoveryRetries) {
				t.Fatalf("structured retry event = %#v", event)
			}
		}
	}
	for _, event := range soloStageEvents(t, events, "landing") {
		if event["status"] == "repair-turn" {
			landings++
		}
	}
	if retries != soloMaxRecoveryRetries || landings != 1 {
		t.Fatalf("retries=%d landings=%d, want %d and 1",
			retries, landings, soloMaxRecoveryRetries)
	}
}

func TestTransientTurnErrorSeparatesTransportFromDecisions(t *testing.T) {
	for _, tc := range []struct {
		err   error
		class string
	}{
		{fmt.Errorf("stream: %w", io.ErrUnexpectedEOF), "unexpected-eof"},
		{errors.New("Post \"https://x\": read: connection reset by peer"), "connection-reset"},
		{errors.New("write: broken pipe"), "broken-pipe"},
		{errors.New("net/http: TLS handshake timeout"), "tls-handshake-timeout"},
		{errors.New("http2: server sent GOAWAY and closed the connection"), "http2-goaway"},
		{errors.New("SSE read timed out"), "sse-read-timeout"},
		{errors.New("fetch failed: getaddrinfo EAI_AGAIN"), "fetch-failed"},
		{errors.New("Upstream error: provider_unavailable; retry after 2s"), "provider-unavailable"},
		{errors.New("Service unavailable"), "provider-unavailable"},
		{errors.New("You can retry your request, or contact support"), "provider-retry-requested"},
		{context.Canceled, ""},
		{context.DeadlineExceeded, ""},
		{fmt.Errorf("turn: %w", context.Canceled), ""},
		{errors.New("assistant error: invalid request"), ""},
		{errors.New("status 400: bad request"), ""},
		{nil, ""},
	} {
		info, transient := transientTurnError(tc.err)
		if info.Class != tc.class || transient != (tc.class != "") {
			t.Errorf("transientTurnError(%v) = %q,%v; want %q", tc.err, info.Class, transient, tc.class)
		}
	}
}

func TestTransientTurnErrorUsesStructuredProviderStatusAndExcludesQuota(t *testing.T) {
	status503 := uint64(503)
	providerFailure := &modelTurnError{
		kind:         "APIError",
		message:      "Upstream error",
		statusCode:   &status503,
		responseBody: `{"error":{"metadata":{"error_type":"provider_unavailable"}}}`,
	}
	info, transient := transientTurnError(providerFailure)
	if !transient || info.Class != "provider-5xx" || info.StatusCode == nil ||
		*info.StatusCode != 503 || info.ProviderCode != "provider_unavailable" {
		t.Fatalf("structured 503 classification = %#v,%v", info, transient)
	}

	status429 := uint64(429)
	quota := &modelTurnError{
		kind: "APIError", message: "insufficient_quota: billing limit reached",
		statusCode: &status429,
	}
	if info, transient := transientTurnError(quota); transient || info.Class != "" {
		t.Fatalf("quota classification = %#v,%v; want terminal", info, transient)
	}
}

func TestSubmitRefusalsAreCountableEvents(t *testing.T) {
	// A refusal that travels only as tool-call error text cannot be counted
	// without opening a log. Every refusal is an event with a reason class.
	runner, state, _, events := soloPipeline(t)

	// Refusal 1: nothing changed.
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("empty")); err == nil {
		t.Fatal("an unchanged tree must refuse")
	}
	// Refusal 2: a change but no checklist.
	if err := writeFile(filepath.Join(runner.workspace, "fix.go"), "package fix\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(runner.workspace, ".senior-dev", "checklist.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("no checklist")); err == nil {
		t.Fatal("a missing checklist must refuse")
	}
	// A successful freeze, then refusal 3: a second submission.
	if err := writeFile(filepath.Join(runner.workspace, ".senior-dev", "checklist.md"),
		"- [x] done\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("real")); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("again")); err == nil {
		t.Fatal("a second submission must refuse")
	}

	var classes []string
	for _, event := range soloStageEvents(t, events, "submit") {
		if event["status"] == "refused" {
			class, _ := event["reason_class"].(string)
			if detail, _ := event["detail"].(string); detail == "" {
				t.Fatalf("refusal %q carries no detail", class)
			}
			classes = append(classes, class)
		}
	}
	want := []string{"empty-tree", "no-checklist", "already-submitted"}
	if strings.Join(classes, ",") != strings.Join(want, ",") {
		t.Fatalf("refusal classes = %v, want %v", classes, want)
	}
}
