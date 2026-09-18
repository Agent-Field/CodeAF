//go:build e2e

// do_run_engine_e2e_test.go drives `codeaf do` ON THE RUN ENGINE end to end:
// the built binary, a real provider, the bash belt, and a run that lands a file
// on a branch.
//
// where the column of this lane comes from
//
// THE DOOR IS THE BUILT BINARY, not a package seam. Every other rig in this
// repository reaches inside and hands a scripted agent to a struct; this one
// starts the process a person starts, with the belt's switch in its
// environment ([session.BashBeltAsked]), and reads back the one machine object
// `do --json` promises ([resultEnvelope]). That is the only way to prove the
// run road is reachable by a person at all — that the switch survives the door,
// that the run engine dispatches the belt worker, and that the landing commits
// onto the branch the envelope names.
//
// THE WHOLE RUN IS ONE FILE AND ONE BRANCH. A throwaway repository is made with
// one committed file; the run works in it in place and lands `HELLO.md` on its
// branch; and the lane reads that file back with `git show <branch>:HELLO.md`
// rather than off the working tree, because the branch is the one thing the
// envelope promises and the working tree proves nothing about it.
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

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// doBrief is the one errand this lane runs: write a file with one known word in
// it and stop. The word is the needle `git show <branch>:HELLO.md` looks for,
// and HELLO.md is the path the landing note names.
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
// finishes a trivial errand, the run lands the file it wrote, and the envelope
// names the branch the landing answered.
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
	// The working copy is a real repository with one committed file: the
	// landing needs a branch to commit onto and the envelope a branch to name.
	workspace := newWorkspace(t, "do-run-engine", false)

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
	branch, named := landedBranch(envelope.Deliverable)
	if !named {
		t.Fatalf("the deliverable never named the branch the run landed on:\n%s",
			envelope.Deliverable)
	}
	// THE BRANCH IS THE ANSWER, NOT THE TREE. The file is read back through
	// git at the branch the envelope named, so a run that wrote HELLO.md to the
	// working copy but landed nothing would fail here rather than pass.
	out, err := exec.Command("git", "-C", workspace, "show", branch+":HELLO.md").CombinedOutput()
	if err != nil {
		t.Fatalf("git show %s:HELLO.md: %v\n%s", branch, err, out)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("HELLO.md on %s does not contain the word hello:\n%s", branch, out)
	}
	t.Logf("landed HELLO.md on %s:\n%s", branch, out)
}

// landedBranch reads the branch out of the landing note the deliverable ends
// with — the one sentence [run.LandingNote] writes, "landed on <branch>: N
// files". The note is the only place a caller learns the branch, so the parse
// is written against its exact words rather than against anything looser.
func landedBranch(deliverable string) (string, bool) {
	const marker = "landed on "
	at := strings.LastIndex(deliverable, marker)
	if at < 0 {
		return "", false
	}
	rest := deliverable[at+len(marker):]
	colon := strings.Index(rest, ":")
	if colon <= 0 {
		return "", false
	}
	branch := strings.TrimSpace(rest[:colon])
	return branch, branch != ""
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
