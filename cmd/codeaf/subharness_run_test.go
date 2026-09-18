package main

// The headless task door's promise to the Model Pool: a run with nobody
// watching records its landing where the restart sweep will find it, the way a
// chat task is judged the moment it lands. These tests drive [runSubharness]
// with a fake runner and read the pending file back, so they pin the row's
// door, worker, report, changed count, tokens and state together.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/session"
)

// headlessRunWith builds a run pointed at a profile of its own, so the pool it
// reads is the test's and never the state root of whoever ran it.
func headlessRunWith(t *testing.T, registry *exec.Registry, profileDir, name, input, model string, started time.Time) subharnessRun {
	t.Helper()
	return subharnessRun{
		registry: registry, name: name, input: json.RawMessage(input),
		journal: &runJournal{}, stdout: io.Discard, stderr: io.Discard,
		profileDir: profileDir, model: model, started: started,
	}
}

// TestAHeadlessRunLeavesAPendingJudgeRecordForTheNextProcess is the promise: a
// program run with nobody watching lands a row the restart sweep will score,
// carrying the door it ran on and the facts the judge reads.
func TestAHeadlessRunLeavesAPendingJudgeRecordForTheNextProcess(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "on")

	profileDir := t.TempDir()
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{
				Output:    json.RawMessage(`{"filed":3}`),
				Report:    "three notes filed, none left over",
				Artifacts: []exec.Artifact{{Path: "a.md"}, {Path: "b.md"}},
				Spend: exec.Spend{
					Model: "crew/worker", Calls: 1, Input: 900, Output: 120, CostUSD: 0.0031,
				},
			}, nil
		},
	}
	run := headlessRunWith(t, headlessRegistry(t, program), profileDir, "tidy-notes",
		`{"folder":"inbox"}`, "crew/worker", time.Date(2026, 9, 17, 22, 0, 0, 123, time.UTC))
	if err := runSubharness(context.Background(), run); err != nil {
		t.Fatalf("a finished run should leave with nothing to say, got %v", err)
	}

	rows := readPendingRows(t, profileDir)
	if len(rows) != 1 {
		t.Fatalf("the pool's pending file holds %d rows, want exactly one", len(rows))
	}
	row := rows[0]
	if row.Door != "run" {
		t.Errorf("the row names door %q, want run", row.Door)
	}
	if row.Landing.Worker != "crew/worker" {
		t.Errorf("the row names worker %q, want the run's model", row.Landing.Worker)
	}
	if row.Landing.Report != "three notes filed, none left over" {
		t.Errorf("the row carries report %q, want the run's own", row.Landing.Report)
	}
	if row.Landing.Changed != 2 {
		t.Errorf("the row says %d files changed, want the 2 the run wrote", row.Landing.Changed)
	}
	if row.Landing.Tokens != 1020 {
		t.Errorf("the row carries %d tokens, want the run's 900 in + 120 out", row.Landing.Tokens)
	}
	if row.Landing.State != session.TaskUnverified {
		t.Errorf("the row's state is %q, want unverified — nobody has judged it yet", row.Landing.State)
	}
	// The judge needs the model and the report; this door has no high seat, so
	// it is left empty rather than guessed.
	if row.Landing.High != "" {
		t.Errorf("the row names a high seat %q, but this door has none", row.Landing.High)
	}
}

// TestAHeadlessRunThatStoppedStillLeavesItsReportForTheJudge pins the second
// half of the promise: a run that did not finish still made something, and the
// judge is owed the report whatever the ending.
func TestAHeadlessRunThatStoppedStillLeavesItsReportForTheJudge(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "on")

	profileDir := t.TempDir()
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{
				Report:     "two of the three notes filed before it ran out of room",
				Incomplete: "it ran out of room before it was finished",
			}, nil
		},
	}
	run := headlessRunWith(t, headlessRegistry(t, program), profileDir, "tidy-notes",
		`{"folder":"inbox"}`, "crew/worker", time.Date(2026, 9, 17, 22, 0, 0, 456, time.UTC))
	err := runSubharness(context.Background(), run)
	var status exitStatus
	if !errors.As(err, &status) || status != exitIncomplete {
		t.Fatalf("a run that stopped leaves with %d, got %v", int(exitIncomplete), err)
	}

	rows := readPendingRows(t, profileDir)
	if len(rows) != 1 {
		t.Fatalf("a run that stopped left %d rows, want the one the judge is owed", len(rows))
	}
	if rows[0].Landing.Report != "two of the three notes filed before it ran out of room" {
		t.Errorf("the row carries report %q, want what the run managed", rows[0].Landing.Report)
	}
}

// TestTwoHeadlessRunsNeverShareAPendingID pins the sweep's own contract: it
// dedups on the landing id, so two runs drawn from two start moments must carry
// two ids and leave two rows.
func TestTwoHeadlessRunsNeverShareAPendingID(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "on")

	profileDir := t.TempDir()
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Output: json.RawMessage(`{"filed":3}`), Report: "filed"}, nil
		},
	}
	registry := headlessRegistry(t, program)
	first := time.Date(2026, 9, 17, 22, 0, 0, 111, time.UTC)
	second := time.Date(2026, 9, 17, 22, 0, 0, 222, time.UTC)
	for _, started := range []time.Time{first, second} {
		run := headlessRunWith(t, registry, profileDir, "tidy-notes", `{"folder":"inbox"}`, "crew/worker", started)
		if err := runSubharness(context.Background(), run); err != nil {
			t.Fatalf("a finished run should leave with nothing to say, got %v", err)
		}
	}

	rows := readPendingRows(t, profileDir)
	if len(rows) != 2 {
		t.Fatalf("two runs left %d rows, want two", len(rows))
	}
	if rows[0].Landing.ID == rows[1].Landing.ID {
		t.Fatalf("two runs share landing id %d — the sweep would judge the second only once", rows[0].Landing.ID)
	}
}

// TestAHeadlessRunLeavesNoPendingJudgeRecordWhenThePoolCannotRead is the
// switch: a pool whose mode forbids reading writes nothing at all.
func TestAHeadlessRunLeavesNoPendingJudgeRecordWhenThePoolCannotRead(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "off")

	profileDir := t.TempDir()
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Output: json.RawMessage(`{"filed":3}`), Report: "filed"}, nil
		},
	}
	run := headlessRunWith(t, headlessRegistry(t, program), profileDir, "tidy-notes",
		`{"folder":"inbox"}`, "crew/worker", time.Date(2026, 9, 17, 22, 0, 0, 789, time.UTC))
	if err := runSubharness(context.Background(), run); err != nil {
		t.Fatalf("a finished run should leave with nothing to say, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(profileDir, "pool", "pending.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("a pool that forbids reading was written to: %v", err)
	}
}

// A NON-VERIFIED RUN-DOOR RUN NAMES WHERE ITS WORK IS STANDING AND THE WORD
// THAT LEFT IT THERE, on the same terms #1182 gave `do` and #1184 gave the chat
// tasks text. The run door edits the directory it was pointed at in place and
// leaves its landing for the pool's judge on every ending that ran, so both the
// finished and the stopped road say `unverified`; the road that could not be
// made to happen writes no landing and says `failed`.
func TestARunDoorsRunNamesItsKeptBranchAndVerdict(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "off")
	workspace := gitWorkspaceOn(t, "work-branch")
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
	}
	for _, ending := range []struct {
		what string
		body func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error)
	}{
		{
			what: "a finished run",
			body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
				return exec.RunResult{
					Output: json.RawMessage(`{"filed":3}`),
					Report: "three notes filed, none left over",
				}, nil
			},
		},
		{
			what: "a run that stopped part way",
			body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
				return exec.RunResult{
					Report:     "two of the three notes filed",
					Incomplete: "it ran out of room before it was finished",
				}, nil
			},
		},
	} {
		program.body = ending.body
		run := headlessRunWith(t, headlessRegistry(t, program), t.TempDir(), "tidy-notes",
			`{"folder":"inbox"}`, "crew/worker", time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC))
		run.workspace = workspace
		run.asJSON = true
		stdout := &bytes.Buffer{}
		run.stdout = stdout
		err := runSubharness(context.Background(), run)
		if ending.what == "a finished run" {
			if err != nil {
				t.Fatalf("%s left with %v", ending.what, err)
			}
		} else {
			var status exitStatus
			if !errors.As(err, &status) || status != exitIncomplete {
				t.Fatalf("%s left with %v, want %d", ending.what, err, int(exitIncomplete))
			}
		}
		fields := errandJSONFields(t, stdout.String())
		if got := fields["verdict"]; got != string(session.TaskUnverified) {
			t.Fatalf("%s: verdict = %v, want %q\n%s", ending.what, got, session.TaskUnverified, fields)
		}
		if got := fields["kept_branch"]; got != "work-branch" {
			t.Fatalf("%s: kept_branch = %v, want work-branch\n%s", ending.what, got, fields)
		}
	}
}

// A WORKSPACE THAT IS NO REPOSITORY NAMES THE VERDICT ALONE: the word is the
// record's and does not depend on git, and there is no branch a person could
// check out.
func TestARunDoorRunInAWorkspaceThatIsNoRepositoryNamesTheVerdictAlone(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "off")
	program := &scriptedRunner{
		manifest: testManifest("tidy-notes", "file loose notes under the right headings"),
		body: func(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
			return exec.RunResult{Report: "three notes filed, none left over"}, nil
		},
	}
	run := headlessRunWith(t, headlessRegistry(t, program), t.TempDir(), "tidy-notes",
		`{"folder":"inbox"}`, "crew/worker", time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC))
	run.workspace = t.TempDir()
	run.asJSON = true
	stdout := &bytes.Buffer{}
	run.stdout = stdout
	if err := runSubharness(context.Background(), run); err != nil {
		t.Fatalf("a finished run left with %v", err)
	}
	fields := errandJSONFields(t, stdout.String())
	if got := fields["verdict"]; got != string(session.TaskUnverified) {
		t.Fatalf("verdict = %v, want %q\n%s", got, session.TaskUnverified, fields)
	}
	if got, present := fields["kept_branch"]; present {
		t.Fatalf("a folder named a kept branch it does not have: %v", got)
	}
}

// THE ROAD THAT COULD NOT BE MADE TO HAPPEN SAYS `failed`, and nothing else on
// this door does: a run that produced no landing left no work for a judge to
// score, and the record's word for that is the no.
func TestARunDoorRunThatCouldNotBeMadeToHappenIsSaidFailed(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "off")
	workspace := gitWorkspaceOn(t, "work-branch")
	run := subharnessRun{
		name: "nosuch", journal: &runJournal{}, model: "crew/worker",
		asJSON: true, workspace: workspace,
	}
	stdout := &bytes.Buffer{}
	run.stdout = stdout
	err := run.sayFailedEnvelope(noSuchSubharnessNamed("nosuch", nil))
	var status exitStatus
	if !errors.As(err, &status) || status != exitCannotRun {
		t.Fatalf("a run that could not happen left with %v, want %d", err, int(exitCannotRun))
	}
	fields := errandJSONFields(t, stdout.String())
	if got := fields["verdict"]; got != string(session.TaskFailed) {
		t.Fatalf("verdict = %v, want %q\n%s", got, session.TaskFailed, fields)
	}
	if got := fields["kept_branch"]; got != "work-branch" {
		t.Fatalf("kept_branch = %v, want work-branch\n%s", got, fields)
	}
}
