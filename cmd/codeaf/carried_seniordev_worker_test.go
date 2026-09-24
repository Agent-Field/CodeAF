//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/plandb"
	runengine "github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
	"github.com/Agent-Field/codeaf/internal/session"
)

// SENIOR-DEV ITSELF, THE CHAT'S WAY: the road `/senior-dev <brief>` takes once
// the conversation has opened its run — the run's worker serves the program the
// real model API, starts it as a real child of this executable with the line
// the chat hands it and an environment with no key in it, and senior-dev works
// a scripted task in a real repository to a passing ending. Every call it makes
// is metered once: one spend row on the task, one ledger row, one turn of the
// conversation the task page draws, and the program record the page names it
// by, ceiling included.
//
// The shell's road is TestSeniorDevWorksATaskThroughTheShellHostsModelAPI;
// this one is the worker's, which is where the chat's money, its page and its
// ceiling are kept.
func TestSeniorDevWorksATaskAsTheChatsRunWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("drives the real senior-dev engine")
	}
	program, carried := builtin.Find("senior-dev")
	if !carried {
		t.Skip("this build carries no senior-dev")
	}
	workspace := seniorDevWorkspace(t)
	seniorDevCatalogWithItsOwnPool(t)
	t.Setenv(carriedChildEnv, "real")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	// The key a program must never see, planted where a careless launch would
	// hand it on.
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-the-chat-run-must-not-hand-this-on")

	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "senior-dev-run", "root", "Add the feature", "Add the feature.")
	if err != nil {
		t.Fatalf("open plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	storeDir := filepath.Dir(store.Path())
	root := store.RootID()

	model := &seniorDevModel{}
	ledger := filepath.Join(t.TempDir(), "usage.jsonl")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const ceiling = 1.0
	worker := runengine.NewDelegateWorker(store, workspace, program, runengine.DelegateSetup{
		Exe:          self,
		Grace:        5 * time.Second,
		CompleterFor: func(string) session.Completer { return model },
		Ledger:       ledger,
	}, ceiling, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := worker.Run(ctx, *store.Task(root))
	taskDir := plandb.TaskDir(storeDir, root)
	if err != nil {
		stderr, _ := os.ReadFile(filepath.Join(taskDir, "delegate-stderr.log"))
		t.Fatalf("the chat's run of senior-dev failed: %v\nits stderr:\n%s", err, stderr)
	}
	if !strings.Contains(report.Result, "senior-dev's model said: feature.txt now holds the feature") {
		t.Fatalf("the run's result = %q, want senior-dev's own ending with its claim", report.Result)
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "feature.txt")); err != nil || string(content) != "implemented\n" {
		t.Fatalf("the work is not in the copy: %q %v", content, err)
	}

	model.mu.Lock()
	calls := model.calls
	model.mu.Unlock()
	if calls < 4 {
		t.Fatalf("senior-dev made %d calls through the API, want the scripted four", calls)
	}
	// ONE ROW PER CALL, AND NOTHING AT THE END: the page and the rail sum
	// these, so an end-of-run row would count the money twice.
	spent := store.SpendSummary().ByModel["delegate/senior-dev"]
	if spent.Calls != calls || math.Abs(spent.USD-0.002*float64(calls)) > 1e-9 {
		t.Fatalf("the task holds %d spend rows for $%.4f, want one per call (%d) at $0.002 each", spent.Calls, spent.USD, calls)
	}
	if math.Abs(report.USD-spent.USD) > 1e-9 {
		t.Fatalf("the run reported $%.4f and the task holds $%.4f; they must be one figure", report.USD, spent.USD)
	}
	session.FlushUsage()
	if data, err := os.ReadFile(ledger); err != nil || strings.Count(strings.TrimSpace(string(data)), "\n")+1 != calls {
		t.Fatalf("the ledger holds %q (%v), want one row per call", data, err)
	}
	turns, err := delegate.ReadTurns(taskDir, 0)
	if err != nil || len(turns) != calls {
		t.Fatalf("the task's conversation holds %d turns (%v), want one per call", len(turns), err)
	}
	record, ok := delegate.ReadProgram(taskDir)
	if !ok || record.Name != "senior-dev" || len(record.Stages) == 0 || record.CeilingUSD != ceiling {
		t.Fatalf("the program record = %+v %v, want senior-dev, its stages and the run's ceiling", record, ok)
	}
	// AND NO KEY WAS HANDED ON: the child's stderr is the program's own words,
	// and the planted key is nowhere in them.
	if stderr, _ := os.ReadFile(filepath.Join(taskDir, "delegate-stderr.log")); strings.Contains(string(stderr), "the-chat-run-must-not-hand-this-on") {
		t.Fatal("the planted key reached senior-dev's process")
	}
}

// A FOLDER WITH NO GIT HISTORY IS WORKED IN WHERE IT IS. The chat reads the
// folder before it starts the program and, finding no history to copy from,
// hands senior-dev its own flag for that (seniordev.Program's PlainFolder) on
// the line the run's worker builds. senior-dev then works the same scripted
// task to the same passing ending, and leaves the folder as plain as it found
// it: no repository is made in somebody's folder behind their back.
func TestSeniorDevWorksAPlainFolderAsTheChatsRunWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("drives the real senior-dev engine")
	}
	program, carried := builtin.Find("senior-dev")
	if !carried {
		t.Skip("this build carries no senior-dev")
	}
	workspace := seniorDevWorkspace(t)
	if err := os.RemoveAll(filepath.Join(workspace, ".git")); err != nil {
		t.Fatal(err)
	}
	seniorDevCatalogWithItsOwnPool(t)
	t.Setenv(carriedChildEnv, "real")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")

	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "senior-dev-run", "root", "Add the feature", "Add the feature.")
	if err != nil {
		t.Fatalf("open plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	model := &seniorDevModel{}
	worker := runengine.NewDelegateWorker(store, workspace, program, runengine.DelegateSetup{
		Exe:          self,
		Grace:        5 * time.Second,
		CompleterFor: func(string) session.Completer { return model },
		Ledger:       filepath.Join(t.TempDir(), "usage.jsonl"),
		PlainFolder:  true,
	}, 1.0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if err != nil {
		stderr, _ := os.ReadFile(filepath.Join(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID()), "delegate-stderr.log"))
		t.Fatalf("senior-dev on a plain folder failed: %v\nits stderr:\n%s", err, stderr)
	}
	if !strings.Contains(report.Result, "feature.txt now holds the feature") {
		t.Fatalf("the run's result = %q, want senior-dev's own passing ending", report.Result)
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "feature.txt")); err != nil || string(content) != "implemented\n" {
		t.Fatalf("the work is not in the folder: %q %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the plain folder was made into a repository: %v", err)
	}
}

// seniorDevCatalogWithItsOwnPool points senior-dev at a model catalog that
// carries its OWN default pool. The chat hands the program no `--high` — its
// line is the default command and the shared flags only — so senior-dev asks
// for the models it ships with, and it sizes its calls from the catalog's
// entry for each. The fixture's one model is copied under every pool id, so
// the run is hermetic and still the one a person's `/senior-dev` starts.
func seniorDevCatalogWithItsOwnPool(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("SENIOR_DEV_MODELS_PATH"))
	if err != nil {
		t.Fatalf("read senior-dev's fixture catalog: %v", err)
	}
	var catalog map[string]map[string]any
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	service := catalog["openrouter"]
	models, _ := service["models"].(map[string]any)
	template, ok := models["fixture/vendor-model"].(map[string]any)
	if !ok {
		t.Fatal("the fixture catalog lost its vendor model")
	}
	for _, id := range strings.Split(app.DefaultHighModels, ",") {
		id = strings.TrimPrefix(strings.TrimSpace(id), "openrouter/")
		entry := map[string]any{}
		for key, value := range template {
			entry[key] = value
		}
		entry["id"] = id
		models[id] = entry
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENIOR_DEV_MODELS_PATH", path)
}
