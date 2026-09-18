package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	homepkg "github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// execPendingHome gives one exec run a profile and a router of its own, so the
// pool's pending file this test reads is this test's and not the developer's.
// The lane knobs and the shared belief are restored because all of them outlive
// a single client in this process.
func execPendingHome(t *testing.T) (model, profileDir string) {
	t.Helper()
	beforePin := provider.CurrentLanePin()
	beforeGuard := provider.LaneGuardOn()
	t.Cleanup(func() {
		provider.SetLanePin(beforePin)
		provider.SetLaneGuard(beforeGuard)
	})
	lanes.Default().Reset()
	t.Cleanup(lanes.Default().Reset)

	state := t.TempDir()
	profileDir = filepath.Join(state, "profile")
	t.Setenv(homepkg.EnvVar, state)
	t.Setenv(config.ProfileDirEnv, profileDir)
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	model = "test/exec-pending-door"
	stub := lanestub.New(model, routedLanes()...)
	t.Cleanup(stub.Close)
	t.Setenv("CODEAF_BASE_URL", stub.URL())
	t.Setenv(config.ModelEnv, model)
	t.Setenv(config.PlanModelEnv, model)
	return model, profileDir
}

// execPendingEnvelope is the one JSON object an exec run wrote to --out, read
// back for the two facts the landing has to echo: the answer and the tokens.
type execPendingEnvelope struct {
	Answer string `json:"answer"`
	Usage  struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// runOnePendingExec drives one real exec run in-process, writing the envelope
// to a file so the test can read the run's own answer and tokens back.
func runOnePendingExec(t *testing.T, model string) execPendingEnvelope {
	t.Helper()
	out := filepath.Join(t.TempDir(), "envelope.json")
	err := runExec([]string{
		"answer with one sentence", "--dir", t.TempDir(), "--max-turns", "2",
		"--token-budget", "2000", "--model", model, "--json", "--out", out,
	})
	if err != nil {
		t.Fatalf("the exec run did not settle cleanly: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the exec run wrote no envelope: %v", err)
	}
	var envelope execPendingEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("the envelope does not parse: %v\n%s", err, raw)
	}
	return envelope
}

// readPendingRows reads the pool's pending file back into the rows it holds.
func readPendingRows(t *testing.T, profileDir string) []pendingLanding {
	t.Helper()
	data, err := os.ReadFile(pendingPath(config.ProfilePath(profileDir, "pool")))
	if err != nil {
		t.Fatalf("the pending file: %v", err)
	}
	var rows []pendingLanding
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row pendingLanding
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("a pending row does not parse: %v\n%s", err, line)
		}
		rows = append(rows, row)
	}
	return rows
}

// TestExecLeavesAPendingJudgeRecordForTheRestartSweep is the whole of the
// contract: one exec run with the pool readable leaves exactly one pending row,
// under the exec door, carrying the run's own model, answer and tokens and the
// unverified state a judge scores; a pool that forbids reading leaves none; and
// two runs never share an id, because the restart sweep dedups on it.
func TestExecLeavesAPendingJudgeRecordForTheRestartSweep(t *testing.T) {
	model, profileDir := execPendingHome(t)

	envelope := runOnePendingExec(t, model)

	rows := readPendingRows(t, profileDir)
	if len(rows) != 1 {
		t.Fatalf("one exec run left %d pending rows, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Door != "exec" {
		t.Fatalf("the pending row names door %q, want exec", row.Door)
	}
	if row.Landing.Worker != model {
		t.Fatalf("the landing's worker is %q, want the run's model %q", row.Landing.Worker, model)
	}
	if row.Landing.State != session.TaskUnverified {
		t.Fatalf("the landing's state is %q, want unverified — the run is judged, not accepted", row.Landing.State)
	}
	if want := envelope.Usage.PromptTokens + envelope.Usage.CompletionTokens; row.Landing.Tokens != want {
		t.Fatalf("the landing holds %d tokens, want the run's own %d", row.Landing.Tokens, want)
	}
	if row.Landing.Tokens <= 0 {
		t.Fatal("the landing holds no tokens")
	}
	if row.Landing.Report != envelope.Answer {
		t.Fatalf("the landing's report is %q, want the run's own answer %q", row.Landing.Report, envelope.Answer)
	}

	// AND TWO RUNS NEVER SHARE AN ID. The restart sweep marks a run judged by
	// its id, so a reused id would swallow the second run's scoring.
	second := runOnePendingExec(t, model)
	if second.Answer == "" {
		t.Fatal("the second run produced no answer")
	}
	rows = readPendingRows(t, profileDir)
	if len(rows) != 2 {
		t.Fatalf("two exec runs left %d pending rows, want 2", len(rows))
	}
	if rows[0].Landing.ID == rows[1].Landing.ID {
		t.Fatalf("two runs shared landing id %d", rows[0].Landing.ID)
	}
}

// TestExecLeavesNoPendingRecordWhenThePoolIsOff is the gate poolJudgeHook keeps
// too: a mode that forbids reading writes nothing, so a run on a pool that is
// off costs no record and no later judge.
func TestExecLeavesNoPendingRecordWhenThePoolIsOff(t *testing.T) {
	model, profileDir := execPendingHome(t)
	t.Setenv("CODEAF_MODEL_POOL", "off")

	runOnePendingExec(t, model)

	if _, err := os.Stat(pendingPath(config.ProfilePath(profileDir, "pool"))); !os.IsNotExist(err) {
		t.Fatalf("a pool that forbids reading left a pending file (err %v)", err)
	}
}
