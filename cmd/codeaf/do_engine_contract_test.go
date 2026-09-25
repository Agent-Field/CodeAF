package main

// `codeaf do` ON THE RUN ENGINE KEEPS THE DOOR'S CONTRACT. The run engine is
// the road every `codeaf do` takes unless CODEAF_TASK_BELT turns the belt off,
// so the promises the door's help makes are this road's to keep:
//
//   - `--dir` is "the directory to work in, edited in place". The run edits it
//     and commits nothing, and the files it names are the ones it changed —
//     never the person's own uncommitted edits or untracked files.
//   - `--yes-spend` is "spend past today's limit and past the plan-price
//     question, without stopping to ask". Without it an unattended run is
//     bounded: by the plan-price figure, and by what is left of today's limit.
//   - A flag the road cannot honour is refused in words, and one it can is
//     honoured; none is dropped without a sentence.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// finishingSeat is the scripted worker that writes out.txt, finishes the root
// in the store and answers `holds` to the review check, which is a run that
// ends done. Every reply costs usd dollars, so a spending bound can see it.
func finishingSeat(usd float64) *beltSeat {
	costed := func(reply *ai.Response) *ai.Response {
		if usd > 0 {
			cost := usd
			reply.Usage.Cost = &cost
		}
		return reply
	}
	return &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return costed(beltToolReply("printf 'written by the run' > out.txt")), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return costed(beltToolReply(beltFinish(beltAnswer))), nil
			},
		},
		ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
			if doc := beltDocument(msgs); strings.Contains(doc, "## Who checks this work") {
				id := briefTaskID(doc)
				return costed(beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'")), nil
			}
			return costed(beltTextReply(beltAnswer)), nil
		},
	}
}

// spendingSeat is a worker that never finishes: every reply is one more shell
// command costing usd dollars. Only a bound stops it before its wall.
func spendingSeat(usd float64) *beltSeat {
	return &beltSeat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		reply := beltToolReply("true")
		cost := usd
		reply.Usage.Cost = &cost
		return reply, nil
	}}
}

// doEnvelopeFields is the --json object as a map, for the words the typed
// outcome does not decode (`stop`).
func doEnvelopeFields(t *testing.T, stdout string) map[string]any {
	t.Helper()
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return fields
}

// doGitIn runs one command of the version-control tool in dir.
func doGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// THE PERSON'S OWN WORK IS NEVER SWEPT INTO A COMMIT.
//
// The copy holds an edit the person has not committed and an untracked
// secrets file. The run writes out.txt and finishes. Nothing is committed —
// the branch stands on the commit it stood on — the person's edit is still an
// uncommitted edit, the secrets file is still untracked, and the files the
// envelope names are the run's one file and nothing of the person's.
func TestDoOnTheRunEngineNeverCommitsThePersonsOwnWork(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("the person's own edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "secret.env"), []byte("TOKEN=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	head := doGitIn(t, workspace, "rev-parse", "HEAD")

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(0) },
	})
	if err != nil {
		t.Fatalf("a brief the model completed left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	if now := doGitIn(t, workspace, "rev-parse", "HEAD"); now != head {
		t.Fatalf("the run committed on the person's branch: HEAD moved %s -> %s\n%s",
			head, now, doGitIn(t, workspace, "show", "--stat", "HEAD"))
	}
	status := doGitIn(t, workspace, "status", "--porcelain", "--untracked-files=all")
	for _, want := range []string{" M README.md", "?? secret.env", "?? out.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("status after the run lacks %q — the copy was not left as edited in place:\n%s", want, status)
		}
	}
	outcome := decodeErrand(t, stdout.String())
	want := filepath.Join(workspace, "out.txt")
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != want {
		t.Fatalf("files = %v, want only the run's own %s", outcome.Artifacts, want)
	}
	if strings.Contains(outcome.Deliverable, "landed on") {
		t.Fatalf("the answer claims a landing the run never made:\n%s", outcome.Deliverable)
	}
}

// WITHOUT --yes-spend, AN UNATTENDED RUN STOPS AT THE PLAN PRICE.
//
// The worker never finishes and every call costs a dollar; the plan-price
// figure is fifty cents. The run stops on its first call's bill with exit 3,
// `stop` is `price`, and `blocked_on` says what to pass to go past it — the
// same contract the older road kept by asking before it bought.
func TestDoOnTheRunEngineStopsAtThePlanPriceWithoutYesSpend(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLAN_CONSENT", "0.5")
	t.Setenv("CODEAF_DAILY_BUDGET", "0")
	t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
	workspace := beltRepoWorkspace(t)

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "keep working", workspace: workspace, asJSON: true,
		timeout: 30 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return spendingSeat(1) },
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("an unattended run past the plan price left with %v, want exit 3\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	fields := doEnvelopeFields(t, stdout.String())
	if fields["stop"] != string(stopPrice) {
		t.Fatalf("stop = %v, want %q", fields["stop"], stopPrice)
	}
	if blocked, _ := fields["blocked_on"].(string); !strings.Contains(blocked, "--yes-spend") || !strings.Contains(blocked, "$0.50") {
		t.Fatalf("blocked_on does not name the price and the way past it: %q", blocked)
	}
}

// AND TODAY'S LIMIT IS THE OTHER RUNG. With the plan-price question off, the
// day's limit still bounds a run nobody is watching.
func TestDoOnTheRunEngineStopsAtTodaysLimitWithoutYesSpend(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLAN_CONSENT", "0")
	t.Setenv("CODEAF_DAILY_BUDGET", "0.5")
	t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
	workspace := beltRepoWorkspace(t)

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "keep working", workspace: workspace, asJSON: true,
		timeout: 30 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return spendingSeat(1) },
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("an unattended run past today's limit left with %v, want exit 3\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	fields := doEnvelopeFields(t, stdout.String())
	if fields["stop"] != string(stopBudget) {
		t.Fatalf("stop = %v, want %q", fields["stop"], stopBudget)
	}
	if blocked, _ := fields["blocked_on"].(string); !strings.Contains(blocked, "today's spending limit") {
		t.Fatalf("blocked_on does not name today's limit: %q", blocked)
	}
}

// --yes-spend IS THE PERSON SAYING OTHERWISE. The same run, the same price, and
// the flag: it spends past the figure and finishes.
func TestDoOnTheRunEngineYesSpendRunsPastThePlanPrice(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	t.Setenv("CODEAF_PLAN_CONSENT", "0.5")
	t.Setenv("CODEAF_DAILY_BUDGET", "0.5")
	// The per-task limit is not what this run is about, and --yes-spend does
	// not lift it: it is raised past what the scripted calls are priced at.
	if err := config.SetCrewTaskCap(config.ProfileDir(), "1000"); err != nil {
		t.Fatal(err)
	}
	workspace := beltRepoWorkspace(t)

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), yesSpend: true, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(1) },
	})
	if err != nil {
		t.Fatalf("a run with --yes-spend left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
}

// --db IS REFUSED IN WORDS. The run keeps its plan in the directory it works in,
// so a store named on the command line is one it would never touch; the door
// says so and starts nothing, rather than working somewhere else in silence.
func TestDoOnTheRunEngineRefusesDbInWords(t *testing.T) {
	beltRunEnv(t)
	workspace := beltRepoWorkspace(t)
	named := filepath.Join(t.TempDir(), "graph.db")

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt", workspace: workspace, database: named, asJSON: true,
		timeout: 10 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return spendingSeat(0) },
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitCannotRun {
		t.Fatalf("--db on the run engine left with %v, want exit 1\nstdout:\n%s", err, stdout.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Error, "--db") || !strings.Contains(outcome.Error, "CODEAF_TASK_BELT") {
		t.Fatalf("the refusal does not say what --db cannot do here and how to reach it: %q", outcome.Error)
	}
	if _, err := os.Stat(session.PlanStorePath(workspace)); err == nil {
		t.Fatal("a refused run opened a plan store anyway")
	}
}

// --keep IS HONOURED BY SAYING WHERE THE RECORD IS. The run's store is the
// working copy's own and is never deleted, so what the flag asks for is kept;
// the door says where, the way the older road does.
func TestDoOnTheRunEngineKeepSaysWhereTheRecordIs(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, keep: true, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(0) },
	})
	if err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	want := "record kept at " + session.PlanStorePath(workspace)
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("--keep never said where the record is; want %q in:\n%s", want, stderr.String())
	}
	if _, err := os.Stat(session.PlanStorePath(workspace)); err != nil {
		t.Fatalf("the record --keep named is not there: %v", err)
	}
}
