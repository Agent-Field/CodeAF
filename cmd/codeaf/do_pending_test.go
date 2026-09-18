package main

// `codeaf do` leaves a pending landing at its tail, for the Model Pool's judge
// to score on the next process that holds a live key — the way a chat task is
// judged when it lands. The do door builds no session.Agent, so the live
// landing hook never fires for it; the row is written by hand instead
// (poolrecord.go's writePendingLanding). These tests drive the whole errand the
// way the neighbouring ones do and read the pending file back.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/lease"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// pendingFile is where a headless run's pending landing lands, under the
// profile the scripted brain pinned.
func pendingFile(profileDir string) string {
	return filepath.Join(profileDir, "pool", "pending.jsonl")
}

// readPendingLandings reads the pending file back into the rows it carries.
// Exactly one row per line, as every row-per-line file here is written.
func readPendingLandings(t *testing.T, profileDir string) []pendingLanding {
	t.Helper()
	data, err := os.ReadFile(pendingFile(profileDir))
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
			t.Fatalf("a pending row: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

// ONE ERRAND, ONE PENDING ROW. A headless run that settles writes exactly one
// landing at its tail — door `do`, the worker it ran on, the deliverable it
// produced, the artifact count, a positive token count and the unverified
// state a judge exists to resolve — so the next process with a live key can
// score the run the way a chat landing is scored.
func TestADoErrandLeavesAPendingLandingForTheNextProcessToJudge(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv("CODEAF_MODEL_POOL", "on")
	script.writeFile = true
	script.gatePasses = true

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:  "write the release note and include the migration steps",
		model: "probe/worker", asJSON: true, workspace: t.TempDir(),
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())

	rows := readPendingLandings(t, script.dir)
	if len(rows) != 1 {
		t.Fatalf("the pending file holds %d rows, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Door != "do" {
		t.Fatalf("the row names door %q, want do", row.Door)
	}
	landing := row.Landing
	if landing.ID == 0 {
		t.Fatal("the landing carries no id")
	}
	if landing.State != session.TaskUnverified {
		t.Fatalf("the landing state is %q, want unverified", landing.State)
	}
	if landing.Worker != "probe/worker" {
		t.Fatalf("the landing worker is %q, want the model the run resolved", landing.Worker)
	}
	if landing.Deliverable != outcome.Deliverable || strings.TrimSpace(landing.Deliverable) == "" {
		t.Fatalf("the landing deliverable is %q, want the outcome's %q", landing.Deliverable, outcome.Deliverable)
	}
	if landing.Changed != len(outcome.Artifacts) || landing.Changed == 0 {
		t.Fatalf("the landing changed %d files over %v, want the outcome's artifacts", landing.Changed, outcome.Artifacts)
	}
	if landing.Tokens <= 0 {
		t.Fatalf("the landing carries %d tokens, want the run's own count", landing.Tokens)
	}
}

// A POOL THAT CANNOT READ WRITES NOTHING. Under a mode word of `off` the gate
// refuses before the row is built, so no file and no directory is made.
func TestADoErrandLeavesNoPendingLandingWhenThePoolCannotRead(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv("CODEAF_MODEL_POOL", "off")
	script.gatePasses = true

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		asJSON:  true,
		timeout: 60 * time.Second, workspace: t.TempDir(),
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\n%s", err, stderr.String())
	}
	if _, err := os.Stat(pendingFile(script.dir)); !os.IsNotExist(err) {
		t.Fatalf("a pool that cannot read was written to: %v", err)
	}
}

// A RUN HANDED TO A RESIDENT WRITES NOTHING. When another process already holds
// the lock for this store, this run defers: the work happens in the resident's
// process, so the landing written here would name a model and a deliverable
// that are not this errand's. The row belongs to whichever process does the
// work, and this one leaves none behind.
func TestADoErrandHandedToAResidentLeavesNoPendingLanding(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv("CODEAF_MODEL_POOL", "on")

	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	release, heldBy, err := lease.AcquireResident(path, "chat")
	if err != nil || release == nil || heldBy != nil {
		t.Fatalf("could not stand in for the resident: release %v, held %+v, err %v", release != nil, heldBy, err)
	}
	defer release()

	// The resident that serves this store, in short: it takes the command and
	// declines it, which settles the run on this store and so carries the
	// deferred errand all the way to its tail.
	done := make(chan struct{})
	go func() {
		defer close(done)
		graph, err := store.Open(path)
		if err != nil {
			return
		}
		defer graph.Close()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			pending, err := graph.PendingCommands(10)
			if err == nil && len(pending) > 0 {
				_ = graph.ResolveCommand(pending[0].Seq, store.CommandRejected, "the resident declined it")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	var stdout, stderr strings.Builder
	_ = doErrand(doRequest{
		task: "write the release note", database: path,
		timeout: 40 * time.Second, residentWait: 20 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	<-done

	if _, err := os.Stat(pendingFile(script.dir)); !os.IsNotExist(err) {
		t.Fatalf("a run handed to a resident left a pending landing: %v", err)
	}
}
