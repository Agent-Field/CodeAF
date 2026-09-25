//go:build e2e

// do_run_engine_e2e_test.go drives `codeaf do` ON THE RUN ENGINE end to end:
// the built binary, a real provider, the bash belt, and a run that writes a file
// in the directory it was handed, in place and uncommitted.
//
// where the column of this lane comes from
//
// THE DOOR IS THE BUILT BINARY, not a package seam. Every other rig in this
// repository reaches inside and hands a scripted agent to a struct; this one
// starts the process a person starts, with the belt's switch in its
// environment ([session.BashBeltAsked]), and reads back the one machine object
// `do --json` promises ([resultEnvelope]). That is the only way to prove the
// run road is reachable by a person at all — that the switch survives the door,
// that the run engine dispatches the belt worker, and that the run keeps the
// door's contract with the directory: edited in place, nothing committed.
//
// THE WHOLE RUN IS ONE FILE AND NO COMMIT. A throwaway repository is made with
// one committed file; the run works in it in place and writes `HELLO.md`; the
// lane reads that file off the working tree, finds it among the envelope's
// files, and checks the branch still stands on the commit it stood on, because
// `--dir` promises the directory "edited in place" and never a commit.
//
// THE SEATS ARE THE PROFILE'S, AND THE PROFILE IS WHERE THEY ARE PINNED. The
// run engine seats each task from the PROFILE's tier rows — [run.CrewFactory]
// reads [config.TierSeatAt] — not from the `--model` flags the door prints, so
// a run left to the profile would ride the person's own crew. The lane writes
// every tier row to the one cheap model this suite rides, exactly as the rest
// of the package pins its models ([newWorld]'s rows, [newHome]'s overrides),
// and names the same model on both flags so the door's receipt and the crew
// agree.
//
// IT SKIPS RATHER THAN FAILS WHEN IT CANNOT BE HONEST: no provider key on any
// road the product reads ([liveKey]), or no built binary (`make build` is the
// door, and a suite that rebuilt would be testing a binary nobody ran).
//
// THE CEILING OF NOTHING HAS NO DOOR, SO IT IS NOT DRIVEN HERE. The run road
// refuses a cost cap of zero before it opens the store or builds a worker — a
// refusal whose whole promise is exit 3 and a `blocked_on` naming the price, the
// one ending on this road a caller raises and reruns (do.go's `runErrand`). But
// the cap it reads is `doRequest.costCap`, an in-process field the door has no
// flag for ON PURPOSE: zero dollars is this product's word for NO ceiling
// (`internal/manual/pages/money-and-limits.md`), the same reading the chat
// door's `--max-cost` keeps (`chatv3.go`'s `chatBudget`, which ignores a zero),
// and a flag that read zero as "may spend nothing" would be a second reading of
// one number beside every settings row's. So a `codeaf do` a person starts
// cannot express a ceiling of nothing, and no untagged test can make it — the
// refusal is proven where it lives instead, by the do package's own
// `TestDoOnTheRunEngineStopsAtACostCapOfZero`, which hands the request a cap of
// zero and reads back the same exit 3 and `blocked_on` this lane would assert.

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// doBrief is the one errand this lane runs: write a file with one known word in
// it and stop. The word is the needle the lane reads HELLO.md for, and HELLO.md
// is the path the envelope's files name.
const doBrief = "write HELLO.md containing the word hello, then stop"

// doWall is the wall this lane gives the run, and it is the run's own clock. It
// is deliberately shorter than the Go test binary's timeout so the door's wall
// fires first and the run still composes an envelope.
const doWall = 6 * time.Minute

// doRunEngineLead bounds the harness itself, past the door's wall. A run that
// outlives this is a wedged process, not a slow model, and the test's own
// timeout must not be the thing that discovers it.
const doRunEngineLead = 8 * time.Minute

// doEnvelope is `codeaf do --json` as this lane reads it: the contract's own
// fields beside the two `do` spellings the answer and the ceiling ride under.
// Both are decoded because the contract renamed `deliverable` to `answer` and
// `blocked_on` is a fact only `do` has.
type doEnvelope struct {
	OK          bool     `json:"ok"`
	Stop        string   `json:"stop"`
	Answer      string   `json:"answer"`
	Deliverable string   `json:"deliverable"`
	BlockedOn   string   `json:"blocked_on"`
	Spend       float64  `json:"spend"`
	SpendUSD    float64  `json:"spend_usd"`
	Files       []string `json:"files"`
}

// TestDoOnTheRunEngine is the run road's completion lane: a real provider
// finishes a trivial errand, the file it wrote is in the working tree and among
// the envelope's files, and nothing was committed.
func TestDoOnTheRunEngine(t *testing.T) {
	key := liveKey(t)
	product := binary(t)

	// The throwaway home is the person's own profile with every model row
	// pinned to the suite's model, so the crew the run engine seats from the
	// profile is the one cheap model this lane rides rather than the person's
	// own.
	root := newHome(t, map[string]any{
		config.KeyTierWorkerModel:     e2eModel,
		config.KeyTierMastermindModel: e2eModel,
		config.KeyTierHighModel:       e2eModel,
		config.KeyTierLowModel:        e2eModel,
		config.KeyTierReflexModel:     e2eModel,
	})
	// The working copy is a real repository with one committed file, so a
	// commit the run should not have made would move its HEAD.
	workspace := newWorkspace(t, "do-run-engine", false)
	headBefore, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, headBefore)
	}

	ctx, cancel := context.WithTimeout(context.Background(), doRunEngineLead)
	defer cancel()
	command := exec.CommandContext(ctx, product, "do",
		"--json", "--yes-spend", "--timeout", "6m",
		"--model", e2eModel, "--plan-model", e2eModel)
	command.Dir = workspace
	command.Env = doRunEngineEnv(root, key)
	// The ask is the whole interface, and it goes in the way a person pipes
	// one: on stdin, with no positional argument for the door to fold a flag
	// into.
	command.Stdin = strings.NewReader(doBrief)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		t.Fatalf("codeaf do left with %v, want exit 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	var envelope doEnvelope
	if err := json.Unmarshal([]byte(stdout.String()), &envelope); err != nil {
		t.Fatalf("the envelope did not parse: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	if !envelope.OK || envelope.Stop != "done" {
		t.Fatalf("the envelope says ok=%v stop=%q, want a done run\nstdout:\n%s\nstderr:\n%s",
			envelope.OK, envelope.Stop, stdout.String(), stderr.String())
	}
	// The spend is the figure the run's own journal sums to, and it is printed
	// so a person reading the log sees what the lane cost.
	t.Logf("SPEND codeaf do $%.6f (stop=%s)", envelope.Spend, envelope.Stop)
	t.Logf("codeaf do stdout:\n%s", stdout.String())

	if strings.TrimSpace(envelope.Deliverable) == "" {
		t.Fatalf("the envelope carries no deliverable, so nothing answered the brief:\n%s",
			stdout.String())
	}
	// THE DIRECTORY IS THE ANSWER, EDITED IN PLACE. The file is read off the
	// working tree the run was handed, it is among the files the envelope
	// names, and the branch stands where it stood: a run that committed on the
	// person's branch fails here.
	out, err := os.ReadFile(filepath.Join(workspace, "HELLO.md"))
	if err != nil {
		t.Fatalf("the run left no HELLO.md in the directory it was handed: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("HELLO.md does not contain the word hello:\n%s", out)
	}
	named := false
	for _, file := range envelope.Files {
		if filepath.Base(file) == "HELLO.md" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the envelope's files %v do not name HELLO.md", envelope.Files)
	}
	headAfter, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, headAfter)
	}
	if strings.TrimSpace(string(headAfter)) != strings.TrimSpace(string(headBefore)) {
		t.Fatalf("the run committed on the person's branch: HEAD moved %s -> %s",
			strings.TrimSpace(string(headBefore)), strings.TrimSpace(string(headAfter)))
	}
	t.Logf("HELLO.md left in place:\n%s", out)
}

// doRunEngineEnv is the environment the run rides: the throwaway home, the
// belt's switch, and the key this lane resolved through [liveKey]. Any
// inherited answer to those three questions is dropped first, so the child
// cannot read two homes, two belt settings or two keys out of one environment.
func doRunEngineEnv(root, key string) []string {
	overridden := map[string]bool{
		home.EnvVar:          true,
		"CODEAF_PROFILE_DIR": true,
		"CODEAF_TASK_BELT":   true,
		config.APIKeyEnv:     true,
	}
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if overridden[name] {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		home.EnvVar+"="+root,
		"CODEAF_TASK_BELT=bash",
		config.APIKeyEnv+"="+key,
	)
}
