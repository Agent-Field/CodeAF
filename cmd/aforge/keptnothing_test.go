package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestARefusedRunThatWroteNothingNamesTheDirectory proves C1: a refused run
// that kept nothing says so and names the absolute directory where it worked.
func TestARefusedRunThatWroteNothingNamesTheDirectory(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	script := newScriptedBrain(t)
	script.stall = true
	defer script.close()

	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		timeout: 2 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("timeout exit = %v, want exit status 3\nstderr:\n%s", err, stderr.String())
	}
	want := fmt.Sprintf(keptNothing, workspace)
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("the refused run did not name its empty workspace:\n%s", stdout.String())
	}
	entries, readErr := os.ReadDir(workspace)
	if readErr != nil {
		t.Fatalf("read workspace: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("the run said it wrote nothing, but %s contains %v", workspace, entries)
	}
}

// TestTheSameSentenceIsInTheJSONAnswer proves C2 and C3: both answer spellings
// carry the stdout sentence and the machine record always names the workspace.
func TestTheSameSentenceIsInTheJSONAnswer(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	script := newScriptedBrain(t)
	script.stall = true
	defer script.close()

	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		asJSON: true, timeout: 2 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("timeout exit = %v, want exit status 3\nstderr:\n%s", err, stderr.String())
	}
	var outcome struct {
		Answer      string `json:"answer"`
		Deliverable string `json:"deliverable"`
		Workspace   string `json:"workspace"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stdout.String()), &outcome); unmarshalErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", unmarshalErr, stdout.String())
	}
	want := fmt.Sprintf(keptNothing, workspace)
	if !strings.Contains(outcome.Answer, want) {
		t.Fatalf("answer does not carry the closing sentence: %q", outcome.Answer)
	}
	if outcome.Deliverable != outcome.Answer {
		t.Fatalf("deliverable %q differs from answer %q", outcome.Deliverable, outcome.Answer)
	}
	if outcome.Workspace != workspace {
		t.Fatalf("workspace = %q, want %q", outcome.Workspace, workspace)
	}
}

// TestARunThatWroteFilesIsNeverToldNothingWasKept proves C4: a non-empty file
// record keeps the empty-tree sentence out of the answer.
func TestARunThatWroteFilesIsNeverToldNothingWasKept(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	script := newScriptedBrain(t)
	script.writeFile = true
	// The invented gap leaves its finding standing after the leaf writes, so
	// this run is refused with a real file and only the artifact guard can keep
	// the empty-tree sentence out of its answer.
	script.inventedGap = true
	defer script.close()

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: t.TempDir(),
		asJSON: true, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitIncomplete {
		t.Fatalf("file-producing refusal exit = %v, want exit status %d\nstderr:\n%s",
			err, int(exitIncomplete), stderr.String())
	}
	var outcome struct {
		Answer string   `json:"answer"`
		Files  []string `json:"files"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stdout.String()), &outcome); unmarshalErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", unmarshalErr, stdout.String())
	}
	if strings.Contains(stdout.String(), keptNothingOpening(t)) {
		t.Fatalf("a run with files claimed it kept nothing: %s", stdout.String())
	}
	if len(outcome.Files) == 0 {
		t.Fatalf("the scripted write produced no files: %s", stdout.String())
	}
}

// TestACleanRunSaysNothingAboutTheTree proves C5: a done run stays quiet about
// the filesystem whether or not its file record is empty.
func TestACleanRunSaysNothingAboutTheTree(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	script := newScriptedBrain(t)
	defer script.close()

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: t.TempDir(),
		asJSON: true, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("errand: %v\nstderr:\n%s", err, stderr.String())
	}
	var outcome struct {
		Stop   stopReason `json:"stop"`
		Answer string     `json:"answer"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stdout.String()), &outcome); unmarshalErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", unmarshalErr, stdout.String())
	}
	if outcome.Stop != stopDone {
		t.Fatalf("stop = %q, want %q", outcome.Stop, stopDone)
	}
	if strings.Contains(stdout.String(), keptNothingOpening(t)) {
		t.Fatalf("a clean run discussed the empty tree: %s", stdout.String())
	}
}

// TestAQuestionKeepsItsEmptyAnswer proves C6: a standing question remains in
// blocked_on and the tree sentence never replaces its deliberately empty answer.
func TestAQuestionKeepsItsEmptyAnswer(t *testing.T) {
	workspace := t.TempDir()
	outcome := headlessOutcome{
		Artifacts: []string{}, Nodes: 1, BlockedOn: unanswerableQuestion,
		workspace: workspace, stop: stopQuestion,
	}
	outcome.Deliverable = groundedInTheTree(outcome)

	var stdout, stderr strings.Builder
	err := reportErrand(doRequest{asJSON: true, stdout: &stdout, stderr: &stderr}, outcome)
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitUnanswered {
		t.Fatalf("blocked exit = %v, want exit status 4", err)
	}
	var decoded struct {
		Answer    string `json:"answer"`
		BlockedOn string `json:"blocked_on"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stdout.String()), &decoded); unmarshalErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", unmarshalErr, stdout.String())
	}
	if decoded.Answer != "" {
		t.Fatalf("the question's answer is not empty: %q", decoded.Answer)
	}
	if decoded.BlockedOn != unanswerableQuestion {
		t.Fatalf("blocked_on = %q, want %q", decoded.BlockedOn, unanswerableQuestion)
	}
	if strings.Contains(stdout.String(), keptNothingOpening(t)) {
		t.Fatalf("the tree sentence replaced a standing question: %s", stdout.String())
	}
}

// TestARunThatCouldNotStartCarriesNoWorkspace proves C8: a failedErrand JSON
// object keeps the workspace key present and empty and makes no tree claim.
func TestARunThatCouldNotStartCarriesNoWorkspace(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note", database: filepath.Join(blocked, "graph.db"),
		asJSON: true, timeout: 10 * time.Second, stdout: &stdout, stderr: &stderr,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitCannotRun {
		t.Fatalf("failed errand exit = %v, want exit status 1", err)
	}
	var decoded map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(stdout.String()), &decoded); unmarshalErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", unmarshalErr, stdout.String())
	}
	rawWorkspace, present := decoded["workspace"]
	if !present {
		t.Fatalf("failed errand omitted workspace: %s", stdout.String())
	}
	var workspace string
	if unmarshalErr := json.Unmarshal(rawWorkspace, &workspace); unmarshalErr != nil {
		t.Fatalf("workspace is not a string: %v", unmarshalErr)
	}
	if workspace != "" {
		t.Fatalf("failed errand workspace = %q, want empty", workspace)
	}
	if strings.Contains(stdout.String(), keptNothingOpening(t)) {
		t.Fatalf("a run that never opened a workspace made a tree claim: %s", stdout.String())
	}
}

// keptNothingOpening gives absence checks the production sentence's own fixed
// words, so a spelling change cannot leave a second copy behind in the tests.
func keptNothingOpening(t *testing.T) string {
	t.Helper()
	placeholder := strings.Index(keptNothing, "%s")
	if placeholder < 0 {
		t.Fatalf("keptNothing has no workspace placeholder: %q", keptNothing)
	}
	return keptNothing[:placeholder]
}

// TestTheSentenceIsSpelledInExactlyOnePlace proves C10: the person-facing
// sentence has exactly one non-test source literal and therefore one owner.
func TestTheSentenceIsSpelledInExactlyOnePlace(t *testing.T) {
	opening := keptNothingOpening(t)
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list cmd/aforge sources: %v", err)
	}
	var places []string
	fileSet := token.NewFileSet()
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr == nil && strings.Contains(value, opening) {
				places = append(places, fileSet.Position(literal.Pos()).String())
			}
			return true
		})
	}
	if len(places) != 1 {
		t.Fatalf("closing sentence opening appears in %d non-test string literals, want one: %v", len(places), places)
	}
}

// TestTheSentenceKeepsThePersonsVocabulary proves C11: the rendered closing
// line contains no internal labels, structured residue, or artifact dump.
func TestTheSentenceKeepsThePersonsVocabulary(t *testing.T) {
	rendered := fmt.Sprintf(keptNothing, "/srv/project")
	for _, banned := range []string{
		"auditor", "verdict", "verified", "refuted", "{", "}", "artifacts: []",
	} {
		if strings.Contains(rendered, banned) {
			t.Errorf("closing sentence contains %q: %q", banned, rendered)
		}
	}
}

// A worker can register its last artifact after the watcher has already read
// an empty list. The closing answer must include that late file.
func TestTheClosingTreeAccountIncludesFilesRegisteredDuringShutdown(t *testing.T) {
	workspace := t.TempDir()
	outcome := headlessOutcome{Nodes: 1, workspace: workspace, stop: stopDeadline, Deliverable: "The time limit was reached."}
	registry := &errandRegistry{}
	path := filepath.Join(workspace, "late.txt")
	if err := os.WriteFile(path, []byte("last work"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.add(path)
	outcome = groundedAfterShutdown(outcome, registry)
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != path {
		t.Fatalf("late artifact missing from closing record: %+v", outcome)
	}
	if !strings.Contains(outcome.Deliverable, "late.txt") || strings.Contains(outcome.Deliverable, keptNothingOpening(t)) {
		t.Fatalf("closing answer contradicts the late file: %q", outcome.Deliverable)
	}
}
