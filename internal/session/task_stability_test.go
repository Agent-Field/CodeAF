package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

type countedTaskComputers struct {
	testTaskComputers
	lists, engines int
}

func (c *countedTaskComputers) List(ctx context.Context) ([]TaskComputer, error) {
	c.lists++
	return c.testTaskComputers.List(ctx)
}

func (c *countedTaskComputers) Engine(ctx context.Context, device string) (TaskRunEngine, error) {
	c.engines++
	return c.testTaskComputers.Engine(ctx, device)
}

// S1 and S4 exercise proposal admission, a local join and normal landing with
// a paired home. There must be no placement I/O or remote spec/record fields.
func TestALocalTaskKeepsItsRunLandingAndRecords(t *testing.T) {
	computers := &countedTaskComputers{}
	a, local := remoteTaskAgent(t, nil, func(c *Config) { c.TaskComputers = computers })
	args := taskOnOtherComputer()
	args.Computer = ""
	if result, refused := approveRemoteProposal(t, a, args); refused {
		t.Fatal(result)
	}
	<-local.entered
	local.mu.Lock()
	spec := local.spec
	local.mu.Unlock()
	if spec.Remote != nil || spec.SaveRemote != nil || spec.WithRemoteCopy != nil || spec.OnRemoteStep != nil || spec.OnRemoteCharge != nil || spec.OnRemotePhase != nil || spec.RemoteStopped != nil || spec.TaskNumber != 0 || spec.NamedModel != "" || spec.NamedEffort != "" || spec.NoProgress != 0 {
		t.Fatal("an ordinary run received remote controls")
	}
	if strings.Contains(spec.Store.Task(spec.Store.RootID()).Description, "\n\nChecks\n") {
		t.Fatal("a local plan row gained the remote Checks section")
	}
	if result, refused := approveRemoteProposal(t, a, args); refused || !strings.Contains(result, "joined") {
		t.Fatalf("local join changed: %q, refused=%v", result, refused)
	}
	close(local.release)
	waitRemoteRun(t, a)
	if local.lands() == 0 || computers.lists != 0 || computers.engines != 0 {
		t.Fatalf("landing=%d, directory=%d, engines=%d", local.lands(), computers.lists, computers.engines)
	}
	if records, _ := filepath.Glob(filepath.Join(filepath.Dir(spec.Store.Path()), "tasks", "*", "remote.json")); len(records) != 0 {
		t.Fatalf("local tasks wrote remote sidecars: %v", records)
	}
	a.SettleWrites()
	checkpoint, err := os.ReadFile(a.config.checkpointFile())
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalLocalJSON(t, checkpoint, &originalTaskDocument{}, true)
	for _, row := range a.graph().runRows(1) {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		assertOriginalLocalJSON(t, raw, &originalTaskNotice{}, false)
	}
	// The real local billing door must retain the original per-call schema.
	a.config.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
	if err := a.recordUsageLine(bankedCall{model: "test/model", used: Usage{Input: 7, Output: 3, CostUSD: .25, Calls: 1}}); err != nil {
		t.Fatal(err)
	}
	if !FlushUsage() {
		t.Fatal("local usage did not reach disk")
	}
	ledger, err := os.ReadFile(a.config.usageLedger)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(ledger), []byte("\n")) {
		assertOriginalLocalJSON(t, line, &originalUsageLine{}, false)
	}
	a.file.appendUsage(Usage{Input: 7, Output: 3, CostUSD: .25, Calls: 1}, "test/model", true, "")
	journal, err := os.ReadFile(a.config.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	usageRows := 0
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		var entry struct {
			Type  string          `json:"type"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Type == "usage" {
			assertOriginalLocalJSON(t, entry.Usage, &originalJournalUsage{}, false)
			usageRows++
		}
	}
	if usageRows == 0 {
		t.Fatal("the local journal recorded no usage")
	}
}

// S4 keeps a paired local run's copy on recovery, stops it through the normal
// cancellation road, and never starts stopped work again on a later reopen.
func TestALocalTaskStopRecoveryAndReopenUseTheOriginalRoad(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done here")
	local.honoursStop = true
	registerBeltRunEngine(t, local)
	computers := &countedTaskComputers{}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace, c.Place, c.SessionFile = repo, Place{Dir: place}, filepath.Join(place, placeTranscript)
			c.ProfileDir, c.TaskComputers, c.AskConsent = profile, computers, false
		})
		return a
	}
	first := open()
	g := first.graph()
	id := g.reserve()
	tree, err := prepareTaskTree(first.config.Place, repo, "1111bbbb1111bbbb", id, "repair export")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(tree.dir, "partial.txt")
	if err := os.WriteFile(sentinel, []byte("keep this"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(g.planPath(), "repair export", strconv.FormatUint(id, 10), "repair export", "original local brief")
	if err != nil {
		t.Fatal(err)
	}
	first.publishRunRow(g, TaskNotice{ID: id, Title: "repair export", State: TaskRunning, Copy: runCopyOf(tree), CrewState: first.unroutedCrewRecord(), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
	// A stray sidecar may not turn a checkpoint without an executor into remote work.
	if err := writeRemoteRecord(store, TaskRemoteRecord{Executor: TaskExecutor{Device: "dev_0123456789abcdef0123456789abcdef", Name: "work computer"}, Attempt: "att_0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := open()
	select {
	case <-local.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the original local recovery did not start")
	}
	local.mu.Lock()
	workspace, brief := local.spec.Workspace, local.spec.Brief
	rootBrief := local.spec.Store.Task(local.spec.Store.RootID()).Description
	local.mu.Unlock()
	if workspace != tree.dir || brief != "" || rootBrief != "original local brief" {
		t.Fatalf("local recovery changed copy or instruction: %q, %q, %q", workspace, brief, rootBrief)
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "keep this" {
		t.Fatalf("local recovery changed the retained copy: %q, %v", raw, err)
	}
	if words, err := second.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10)); err != nil || !strings.HasPrefix(words, "stopping ") || !strings.Contains(words, "its branch is kept") {
		t.Fatalf("local stop changed: %q, %v", words, err)
	}
	waitRemoteRun(t, second)
	row, _ := runRowOf(second.graph(), id)
	if row.State != TaskFailed || !row.Stopped || row.Ending != "" || row.EndedAt.IsZero() || !strings.HasPrefix(row.Report, "stopped") {
		t.Fatalf("local stop changed its original ending: %+v", row)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	stopped, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := stopped.Task(stopped.RootID())
	_ = stopped.Close()
	if root == nil || root.Status != plandb.StatusCancelled {
		t.Fatalf("the stopped local plan can be replayed: %+v", root)
	}
	third := open()
	third.beltMu.Lock()
	live := third.beltRun
	third.beltMu.Unlock()
	if live != nil || computers.lists != 0 || computers.engines != 0 {
		t.Fatalf("a local reopen used a task computer or replayed stopped work: live=%v lists=%d engines=%d", live != nil, computers.lists, computers.engines)
	}
}

// S1 and S2 require the person's choice. An argument absent from the default
// schema remains an ignored unknown field, as it was before paired tasks.
func TestTheModelCannotOptIntoAnotherComputerWithTheDefaultTool(t *testing.T) {
	computers := &countedTaskComputers{}
	a, local := remoteTaskAgent(t, nil, func(c *Config) { c.TaskComputers = computers })
	if result, refused := approveRemoteProposal(t, a, taskOnOtherComputer()); refused {
		t.Fatalf("an argument outside the default schema changed local admission: %s", result)
	}
	<-local.entered
	close(local.release)
	waitRemoteRun(t, a)
	if computers.lists != 0 || computers.engines != 0 {
		t.Fatalf("the model opted in without the person: directory=%d, engine=%d", computers.lists, computers.engines)
	}
}

// S2 keeps the model's tool and belt facts unchanged until the person chooses
// a standing executor; pairing alone is not that choice.
func TestProposeTaskIsUnchangedUntilRunTasksOnNamesAComputer(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ProfileDir = t.TempDir()
	})
	original := a.taskTools()[0].Schema
	facts := promptWithBeltFacts(a.config)
	a.config.TaskComputers = testTaskComputers{}
	if got := a.taskTools()[0].Schema; !bytes.Equal(got, original) {
		t.Fatalf("pairing changed the default proposal schema:\n%s", got)
	}
	if got := promptWithBeltFacts(a.config); got != facts {
		t.Fatal("pairing changed the default belt facts")
	}
	if err := config.WriteTaskRunsOn(a.config.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if _, offered := schemaProperties(t, a.taskTools()[0].Schema)["computer"]; !offered {
		t.Fatal("the chosen standing executor has no computer argument")
	}
	if promptWithBeltFacts(a.config) == facts {
		t.Fatal("the chosen standing executor has no computer guidance")
	}
	if err := config.WriteTaskRunsOn(a.config.ProfileDir, ""); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.taskTools()[0].Schema, original) || promptWithBeltFacts(a.config) != facts {
		t.Fatal("resetting to this computer did not restore the original tool and facts")
	}
}

// S4 preserves the instruction a local proposal gives its root, even with checks.
func TestALocalTaskBriefHasNoChecksSection(t *testing.T) {
	spec := taskSpec{brief: "Repair export", deliverable: "working export", acceptance: "export succeeds", checks: []string{"go test ./export"}}
	const original = "THE WORK\n\nRepair export\n\nWHAT TO PRODUCE\n\nworking export\n\nDONE WHEN\n\nexport succeeds"
	if got := proposalBrief(spec, nil); got != original {
		t.Fatalf("local brief changed:\n%q", got)
	}
}

// S4 leaves every new placement field out of local checkpoints and wire notices.
func TestALocalTaskRecordOmitsEveryRemoteField(t *testing.T) {
	for _, value := range []any{TaskNotice{ID: 7, Title: "Repair export"}, runRowRecord(TaskNotice{ID: 7, Title: "Repair export"}), TaskIndexEntry{ID: "7"}, UsageLine{}, journalUsage{}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"Computer", "Unreachable", "RanElsewhere", "Remote", "computer", "unreachable", "ran_elsewhere", "remote", "remote_event"} {
			if strings.Contains(string(raw), `"`+field+`":`) {
				t.Fatalf("%T wrote an empty placement field: %s", value, raw)
			}
		}
	}
}

// S4 keeps local ending words on their existing mapping, even when an engine
// reports a word the remote door also uses.
func TestALocalTaskKeepsItsOriginalEndingMapping(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	notice := a.beltRunNotice(&beltRun{row: 7, title: "Repair export"}, RunSummary{Outcome: "interrupted"}, RunLanding{})
	if notice.State != TaskFailed {
		t.Fatalf("local ending took a remote branch: %s", notice.State)
	}
}

// S2 applies to the next turn after changing the setting, including resetting
// it before the ordinary prompt clock is due to refresh.
func TestProposeTaskFactsFollowTheSettingOnTheNextTurn(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.System = ""
		c.ProfileDir = t.TempDir()
		c.TaskComputers = testTaskComputers{}
	})
	now := time.Now().Truncate(time.Minute)
	a.mu.Lock()
	a.rerenderSystemLocked(now)
	original := a.system
	a.mu.Unlock()
	if err := config.WriteTaskRunsOn(a.config.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.refreshClockLocked(now.Add(time.Second))
	chosen := a.system
	a.mu.Unlock()
	if !strings.Contains(chosen, "optional `computer` argument") {
		t.Fatal("the next turn lacks the chosen computer's belt fact")
	}
	if err := config.WriteTaskRunsOn(a.config.ProfileDir, ""); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.refreshClockLocked(now.Add(2 * time.Second))
	restored := a.system
	a.mu.Unlock()
	if restored != original {
		t.Fatal("resetting the preference left a remote promise in the next turn")
	}
}
