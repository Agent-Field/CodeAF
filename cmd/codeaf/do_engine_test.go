package main

// `codeaf do` ON THE RUN ENGINE — the second road do.go takes when the bash
// belt is asked for. These drive the whole command the way the resident road's
// own tests drive theirs (do_test.go): one doErrand, its real envelope on
// stdout, and the exit code read off the one ladder. The provider is scripted
// through the run road's own completer seam, so the belt worker's session is
// real and only the model's words are fake — the same bargain internal/run's
// bashworker tests make.
//
// THREE FACTS ARE UNDER TEST. A brief the scripted model completes leaves with
// exit 0 and an envelope naming the root's result and the file it wrote, left
// in place and uncommitted (do_engine_contract_test.go holds the rest of that
// contract). A ceiling of nothing leaves with exit 3 and
// `blocked_on` naming the price it was held to. And the usage ledger is the
// session's own — the worker the run hosts writes it, so the door adds no
// second accounting.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// beltAnswer is what the scripted worker reports as its own account of the
// work, and the text the envelope must carry as the root's result.
const beltAnswer = "the run engine wrote out.txt and reported it"

// beltSeat is the run road's scripted provider: one answer per call, in the
// order they arrive, and an `ever` answer for every call after the script runs
// out. It is [session.Completer], the type [run.CrewFactory]'s completer seam
// hands back, so the worker it hosts is the real belt worker and only the model
// is fake.
type beltSeat struct {
	mu     sync.Mutex
	script []func(context.Context, []ai.Message) (*ai.Response, error)
	ever   func(context.Context, []ai.Message) (*ai.Response, error)
	seen   int
}

func (s *beltSeat) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	s.mu.Lock()
	var next func(context.Context, []ai.Message) (*ai.Response, error)
	switch {
	case s.seen < len(s.script):
		next = s.script[s.seen]
	case s.ever != nil:
		next = s.ever
	}
	s.seen++
	s.mu.Unlock()
	if next == nil {
		return beltTextReply(beltAnswer), nil
	}
	return next(ctx, messages)
}

// beltTextReply is one plain assistant answer — the shape a belt turn ends on.
func beltTextReply(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}},
		}}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// beltToolReply is one bash call, the shape the belt's one-action envelope
// accepts: exactly one call, named bash, its argument one command.
func beltToolReply(command string) *ai.Response {
	arguments := `{"command":` + quoteJSON(command) + `}`
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant",
			ToolCalls: []ai.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "bash", Arguments: arguments},
			}},
		}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

// quoteJSON spells one string as a JSON string literal. The command is the
// test's own and holds no character JSON needs escaped beyond the quotes it is
// wrapped in, so the encoding/json of it here is the whole of it.
func quoteJSON(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

// beltStubCLI is the plandb shim's override: a program that answers nothing and
// exits, which is all the shim's arming probes through its override road
// (internal/session's resolvePlanCLI). Without it the belt worker cannot arm
// its `plandb` and never runs.
func beltStubCLI(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

// beltRunEnv puts the door on the run road: the belt's switch, the shim's
// override, a home of its own to read the ledger under, and a profile the crew
// resolves its seats from.
//
// THE PROFILE TURNS THE MACHINE GATE OFF. The run road asks the host's own
// load and memory before every worker it starts, from the profile's
// `task.max_load` and `task.min_free_mb` rows, and both are on out of the box.
// A scripted run here is not a measurement of the machine running it: on a box
// busy with other suites the gate held every root until the run's wall, and a
// test about spending or landing failed as a deadline. The gate has its own
// tests (do_machine_gate_test.go), which set the rows they mean.
func beltRunEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	profile := filepath.Join(home, "profile")
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", beltStubCLI(t))
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	settings := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	for _, key := range []string{config.KeyTaskMaxLoad, config.KeyTaskMinFreeMB} {
		row, ok := settings.Row(key)
		if !ok {
			t.Fatalf("the %s row is absent", key)
		}
		if err := row.Apply("0"); err != nil {
			t.Fatalf("turn %s off: %v", key, err)
		}
	}
	return home
}

// beltPlandbDoor is the real plandb CLI behind the resolver's override, built
// once for the package. THE LOOP ENDS IN THE STORE: a worker's task is done
// when `plandb done` marks it so and no other way, so a scripted worker that
// is to finish must run that verb against a door that reaches the store — the
// exit-0 stub the other tests use would leave the task open and the loop
// asking for an action until it failed.
func beltPlandbDoor(t *testing.T) string {
	t.Helper()
	beltCLIOnce.Do(func() {
		dir, err := os.MkdirTemp("", "plandb-cli")
		if err != nil {
			beltCLIErr = err
			return
		}
		out := filepath.Join(dir, "plandb")
		build := exec.Command("go", "build", "-o", out, "github.com/Agent-Field/codeaf/cmd/plandb")
		if output, err := build.CombinedOutput(); err != nil {
			beltCLIErr = errors.New("go build cmd/plandb: " + err.Error() + "\n" + string(output))
			return
		}
		beltCLIPath = out
	})
	if beltCLIErr != nil {
		t.Skipf("cannot build the real plandb CLI: %v", beltCLIErr)
	}
	door := filepath.Join(t.TempDir(), "plandb-door")
	script := "#!/bin/sh\nif [ \"$1\" = plandb ]; then shift; fi\nexec " + beltCLIPath + " \"$@\"\n"
	if err := os.WriteFile(door, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return door
}

var (
	beltCLIOnce sync.Once
	beltCLIPath string
	beltCLIErr  error
)

// beltFinish is the command a scripted root worker ends its task with: the
// store's own done verb, claimed under the root's id, carrying the result the
// envelope is expected to name.
func beltFinish(result string) string {
	return "plandb done root --agent root --result '" + result + "'"
}

// beltDocument is the whole document a belt worker was handed, joined so a
// scripted seat can read which section it opened on — the section headings are
// its own and are not words a person types.
func beltDocument(messages []ai.Message) string {
	var b strings.Builder
	for _, m := range messages {
		for _, part := range m.Content {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// briefTaskID reads the plan task id a belt worker owns from the first line of
// its document — `t-<id> is your task in the plan.` — which is how a check
// worker learns the id the supervisor minted for it. The `t-` prefix the brief
// prints is the CLI's spelling of the id; the agent name a finish command must
// carry is the store's bare id, so it is stripped back off here.
func briefTaskID(document string) string {
	const tail = " is your task in the plan."
	i := strings.Index(document, tail)
	if i < 0 {
		return ""
	}
	line := document[:i]
	if j := strings.LastIndexByte(line, '\n'); j >= 0 {
		line = line[j+1:]
	}
	return strings.TrimPrefix(strings.TrimSpace(line), "t-")
}

// beltRepoWorkspace is the working copy the run lands on: a real repository on
// one committed file, so the landing has a branch to commit the run's work to
// and the envelope has a branch to name.
func beltRepoWorkspace(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	beltGit(t, dir, "init")
	beltGit(t, dir, "checkout", "-b", "work")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beltGit(t, dir, "add", "-A")
	beltGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "first")
	return dir
}

func beltGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// THE RUN ROAD COMPLETES A BRIEF AND NAMES THE ROOT'S RESULT.
//
// The scripted worker writes one file through bash and then finishes its task
// in the store with the result as its words; the caller reads on stdout the
// root's own result and the path the run wrote.
// A ROOT FINISH THAT LANDS IN THE STORE BEFORE ITS WORKER RETURNS STILL NAMES THE ROOT RESULT.
//
// The finish command writes the root done row before its shell exits. Holding that
// shell beyond the supervisor pass interval forces the store to look terminal while
// the worker return carrying its report is still in flight; the envelope must not
// substitute the later landing line for that report. This is the same ordering a
// real `codeaf do --json` belt worker can reach between its plandb write and return.
func TestDoOnTheRunEngineRootStoreFinishBeforeWorkerReturnNamesRootResult(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply("printf 'written by the run' > out.txt"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply(beltFinish(beltAnswer) + "; sleep 1"), nil
			},
		},
		ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
			if doc := beltDocument(msgs); strings.Contains(doc, "## Who checks this work") {
				id := briefTaskID(doc)
				return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
			}
			return beltTextReply(beltAnswer), nil
		},
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if err != nil {
		t.Fatalf("a brief the model completed left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Deliverable, beltAnswer) {
		t.Fatalf("the envelope does not carry the root's result: %q", outcome.Deliverable)
	}
}

func TestDoOnTheRunEngineCompletesABriefAndNamesTheRootResult(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply("printf 'written by the run' > out.txt"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply(beltFinish(beltAnswer)), nil
			},
		},
		// THE ROOT IS CHECKED ONCE IT FINISHES ALONE, so the seat's standing
		// answer must also serve the review check: it answers `holds` when the
		// document is the check's, and the text reply everywhere else.
		ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
			if doc := beltDocument(msgs); strings.Contains(doc, "## Who checks this work") {
				id := briefTaskID(doc)
				return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
			}
			return beltTextReply(beltAnswer), nil
		},
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if err != nil {
		t.Fatalf("a brief the model completed left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Deliverable, beltAnswer) {
		t.Fatalf("the envelope does not carry the root's result: %q", outcome.Deliverable)
	}
	if outcome.Nodes < 1 {
		t.Fatalf("the run reported %d nodes, want at least the root worker", outcome.Nodes)
	}
	if outcome.Seconds <= 0 {
		t.Fatal("the run reported no elapsed time")
	}
	// The worker's file is on the object, where the run left it: in the
	// directory it was handed, edited in place and not committed.
	want := filepath.Join(workspace, "out.txt")
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != want {
		t.Fatalf("artifacts = %v, want the one path the run wrote %s", outcome.Artifacts, want)
	}
}

// THE DOOR'S SEATS REACH EVERY LAUNCH, THE WAKE INCLUDED.
//
// `codeaf do --model <w> --plan-model <p>` must run every launch on those two
// models and no other. A root is born a leaf — it does the work itself — so its
// first launch rides the work seat; once it splits and is woken again to fold
// its child it is a coordinator and rides the plan seat. The profile names OTHER
// models on both rows, so a factory that fell back to the profile for either
// seat would ask the completer for those and this would see them.
//
// The one seat is served to every launch in the order they arrive, and slots=1
// makes that order the store's own: the root's first turn adds one child under a
// known id and parks on it, the child finishes, and the woken root folds it.
func TestDoOnTheRunEngineSeatsEveryLaunchOnTheDoorsModels(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	profileDir := filepath.Join(home, "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rows, err := json.Marshal(map[string]string{
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(profileDir), rows, 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := beltRepoWorkspace(t)
	const (
		workModel = "vendor/do-work"
		planModel = "vendor/do-plan"
	)
	// ONE SEAT, DISPATCHED ON WHICH TASK IS ASKING. A root's own document carries
	// no ask section (its work order IS the ask); every leaf's does, so the seat
	// can tell the two apart and give each the turn it needs without a script
	// whose order the supervisor's own launches would have to match. The root
	// adds one child under a known id and parks on it; the child finishes; the
	// woken root, now a coordinator, folds it.
	rootTurns := 0
	seat := &beltSeat{ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
		doc := beltDocument(msgs)
		// THE CHECK IS ANSWERED FIRST: its document carries the leaf's ask
		// section AND the check section, so testing the ask alone would hand the
		// check the leaf's own turn and loop it against a task it does not own.
		if strings.Contains(doc, "## Who checks this work") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
		}
		if strings.Contains(doc, "## The ask this run serves") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'the child is done'"), nil
		}
		rootTurns++
		switch rootTurns {
		case 1:
			return beltToolReply("plandb add 'the child' --as c1"), nil
		case 2:
			return beltToolReply("plandb wait root --agent root"), nil
		default:
			return beltToolReply(beltFinish(beltAnswer)), nil
		}
	}}

	var mu sync.Mutex
	built := map[string]int{}
	newBelt := func(model string) session.Completer {
		mu.Lock()
		built[model]++
		mu.Unlock()
		return seat
	}

	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), model: workModel, planModel: planModel,
		stdout: &stdout, stderr: &stderr, newBeltCompleter: newBelt,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	mu.Lock()
	models := make([]string, 0, len(built))
	for model := range built {
		models = append(models, model)
	}
	mu.Unlock()
	for _, model := range models {
		if model != workModel && model != planModel {
			t.Fatalf("the completer was asked for the model %q; the door named only %q and %q",
				model, workModel, planModel)
		}
	}
	if built[workModel] == 0 || built[planModel] == 0 {
		t.Fatalf("the completer was asked for %v, want both the work seat %q and the plan seat %q",
			models, workModel, planModel)
	}
}

// A `do` RUN CHECKS ITS LEAF AND STILL EXITS ZERO WHEN THE CHECK HOLDS.
//
// The review round rides every do run: a leaf that lands done is checked against
// its acceptance, and the run waits on the check. When the check holds — no fix
// is born — the run is the done run it was before the round existed, and the
// envelope carries the root's result on exit 0.
func TestDoOnTheRunEngineChecksALeafAndExitsZeroWhenItHolds(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	var mu sync.Mutex
	checks := 0
	rootTurns := 0
	seat := &beltSeat{ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
		doc := beltDocument(msgs)
		if strings.Contains(doc, "## Who checks this work") {
			mu.Lock()
			checks++
			mu.Unlock()
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
		}
		if strings.Contains(doc, "## The ask this run serves") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'the leaf is done'"), nil
		}
		rootTurns++
		switch rootTurns {
		case 1:
			return beltToolReply("plandb add 'the leaf' --as l1"), nil
		case 2:
			return beltToolReply("plandb wait root --agent root"), nil
		default:
			return beltToolReply(beltFinish(beltAnswer)), nil
		}
	}}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("a run whose check held left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	mu.Lock()
	served := checks
	mu.Unlock()
	if served == 0 {
		t.Fatal("no check worker was served: the review round did not run through the do door")
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Deliverable, beltAnswer) {
		t.Fatalf("the envelope does not carry the root's result: %q", outcome.Deliverable)
	}
}

// A CHILDLESS ROOT THAT FINISHES THROUGH THE PLANDB BELT IS CHECKED ONCE.
//
// This is the do road real runs take most often: the root does the work itself
// and calls the CLI done verb rather than returning a report. The command must
// remain accepted for a root worker, the holding check must land, and the do
// envelope must still leave on the success rung with the worker's own result.
func TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
		doc := beltDocument(msgs)
		if strings.Contains(doc, "## Who checks this work") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
		}
		return beltToolReply(beltFinish(beltAnswer)), nil
	}}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "do the work alone and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("a self-finished root whose check held left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Deliverable, beltAnswer) {
		t.Fatalf("the envelope does not carry the root worker's result: %q", outcome.Deliverable)
	}

	store, err := plandb.Open(session.PlanStorePath(workspace), "", "root", "", "")
	if err != nil {
		t.Fatalf("open the do run's plan: %v", err)
	}
	defer store.Close()
	var checks []*plandb.Task
	for _, task := range store.Tasks() {
		if task.Role == plandb.RoleCheck {
			checks = append(checks, task)
		}
	}
	if len(checks) != 1 {
		t.Fatalf("check tasks in store = %d, want exactly one", len(checks))
	}
	if checks[0].Status != plandb.StatusDone {
		t.Fatalf("check status = %s, want done", checks[0].Status)
	}
}

// A CEILING OF NOTHING IS A LIMIT THAT STOPPED THE RUN, before any worker.
//
// The run road has no cost flag, and this is why the ceiling lives on the
// request: a caller holding a run to a price says so, and a price of zero
// admits no work at all. Exit 3 is the ladder's rung for a limit, and
// `blocked_on` names the price so a caller knows what to raise.
func TestDoOnTheRunEngineStopsAtACostCapOfZero(t *testing.T) {
	beltRunEnv(t)
	workspace := t.TempDir()
	zero := 0.0

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, costCap: &zero, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return &beltSeat{} },
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("a run held to nothing left with %v, want exit status 3", err)
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.BlockedOn, "cost cap") {
		t.Fatalf("blocked_on does not name the ceiling that stopped it: %q", outcome.BlockedOn)
	}
	if strings.TrimSpace(outcome.Deliverable) != "" {
		t.Fatalf("a run that did nothing carried a deliverable: %q", outcome.Deliverable)
	}
}

// THE USAGE LEDGER IS THE SESSION'S OWN, AND THE DOOR ADDS NO SECOND ONE.
//
// The worker the run hosts is a session agent, and that session is what writes
// the v3 usage ledger — the same file a conversation and every other task
// worker append to. This proves the run road leaves that accounting to the
// session rather than duplicating it into a ledger of its own: after a
// completed brief, the ledger this home holds has the worker's own call in it.
func TestDoOnTheRunEngineLeavesTheUsageLedgerToTheSession(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply("printf 'written by the run' > out.txt"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply(beltFinish(beltAnswer)), nil
			},
		},
		// THE ROOT IS CHECKED ONCE IT FINISHES ALONE, so the seat's standing
		// answer must also serve the review check: it answers `holds` when the
		// document is the check's, and the text reply everywhere else.
		ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
			if doc := beltDocument(msgs); strings.Contains(doc, "## Who checks this work") {
				id := briefTaskID(doc)
				return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
			}
			return beltTextReply(beltAnswer), nil
		},
	}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	// The ledger the session writes lives under this home; its path is the
	// session's own ([session.UsageLedgerPath]) and the door never spells it.
	lines, err := session.ReadUsage(filepath.Join(home, "v3", "usage.jsonl"), time.Time{})
	if err != nil {
		t.Fatalf("read the session's usage ledger: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("the session the worker hosted wrote no usage row, so the run road "+
			"is accounting somewhere of its own:\n%s", stderr.String())
	}
}

// THE CHECK SEAT HAS A FLAG OF ITS OWN, and the door resolves it at the seam:
// `--check-model` seats the run's review checks on the model the person typed,
// beside a work flag and a plan flag, and no other seat moves. The profile
// names OTHER models on every row, so a factory that fell to the profile for
// the check would ask the completer for those and this would see them.
func TestDoOnTheRunEngineSeatsACheckOnTheCheckModel(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	profileDir := filepath.Join(home, "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rows, err := json.Marshal(map[string]string{
		config.KeyTierLowModel:        "vendor/profile-small",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(profileDir), rows, 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := beltRepoWorkspace(t)
	const (
		workModel  = "vendor/do-work"
		planModel  = "vendor/do-plan"
		checkModel = "vendor/do-check"
	)
	// The same one-seat shape as the door's seats test: the root adds one
	// child and parks on it, the child finishes, the woken root folds it, and
	// the review check on the child's leaf is answered with holds.
	rootTurns := 0
	seat := &beltSeat{ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
		doc := beltDocument(msgs)
		if strings.Contains(doc, "## Who checks this work") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
		}
		if strings.Contains(doc, "## The ask this run serves") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'the child is done'"), nil
		}
		rootTurns++
		switch rootTurns {
		case 1:
			return beltToolReply("plandb add 'the child' --as c1"), nil
		case 2:
			return beltToolReply("plandb wait root --agent root"), nil
		default:
			return beltToolReply(beltFinish(beltAnswer)), nil
		}
	}}

	var mu sync.Mutex
	built := map[string]int{}
	newBelt := func(model string) session.Completer {
		mu.Lock()
		built[model]++
		mu.Unlock()
		return seat
	}

	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1),
		model: workModel, planModel: planModel, checkModel: checkModel,
		stdout: &stdout, stderr: &stderr, newBeltCompleter: newBelt,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	mu.Lock()
	models := make([]string, 0, len(built))
	for model := range built {
		models = append(models, model)
	}
	checkBuilt := built[checkModel]
	mu.Unlock()
	for _, model := range models {
		if model != workModel && model != planModel && model != checkModel {
			t.Fatalf("the completer was asked for the model %q; the door named only %q, %q and %q",
				model, workModel, planModel, checkModel)
		}
	}
	if checkBuilt == 0 {
		t.Fatalf("the completer was never asked for the check seat %q; the review round did not run or was seated elsewhere (built %v)",
			checkModel, models)
	}
	if built[workModel] == 0 || built[planModel] == 0 {
		t.Fatalf("the completer was asked for %v, want all three seats the door named", models)
	}
}

// AN UNPINNED RUN CHECKS ON THE CREW'S CHECKER. With no seat flag typed at all,
// the work and plan seats come from the profile's rows and the check seat is
// empty, so the factory seats the review check on the profile's careful row:
// one model that works, one that checks, one that thinks, and no fourth model
// from anywhere.
func TestDoOnTheRunEngineSeatsAnUnpinnedCheckOnTheCrewsChecker(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	profileDir := filepath.Join(home, "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rows, err := json.Marshal(map[string]string{
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(profileDir), rows, 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := beltRepoWorkspace(t)
	// The same one-seat shape as the two tests above.
	rootTurns := 0
	seat := &beltSeat{ever: func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
		doc := beltDocument(msgs)
		if strings.Contains(doc, "## Who checks this work") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
		}
		if strings.Contains(doc, "## The ask this run serves") {
			id := briefTaskID(doc)
			return beltToolReply("plandb done " + id + " --agent " + id + " --result 'the child is done'"), nil
		}
		rootTurns++
		switch rootTurns {
		case 1:
			return beltToolReply("plandb add 'the child' --as c1"), nil
		case 2:
			return beltToolReply("plandb wait root --agent root"), nil
		default:
			return beltToolReply(beltFinish(beltAnswer)), nil
		}
	}}

	var mu sync.Mutex
	built := map[string]int{}
	newBelt := func(model string) session.Completer {
		mu.Lock()
		built[model]++
		mu.Unlock()
		return seat
	}

	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1),
		stdout: &stdout, stderr: &stderr, newBeltCompleter: newBelt,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	mu.Lock()
	models := make([]string, 0, len(built))
	for model := range built {
		models = append(models, model)
	}
	carefulBuilt := built["vendor/profile-careful"]
	mu.Unlock()
	crew := map[string]bool{
		"vendor/profile-worker":   true,
		"vendor/profile-careful":  true,
		"vendor/profile-thinking": true,
	}
	for _, model := range models {
		if !crew[model] {
			t.Fatalf("the completer was asked for the model %q; the crew names only %v", model, models)
		}
	}
	if carefulBuilt == 0 {
		t.Fatalf("the completer was never asked for the crew's careful row; the check was seated elsewhere (built %v)", models)
	}
}

// bound is a named slot count for a request, the way the flag would name one.
func bound(n int) *int { return &n }
