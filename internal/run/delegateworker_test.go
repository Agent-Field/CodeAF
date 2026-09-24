//go:build !windows

package run_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeDelegate writes a shell program that stands in for codeaf running a
// program — a hello, a stage, a v1 spend line (which nothing reads any more),
// two steps, then body — and answers the program's definition and the setup
// that starts the script in codeaf's place.
func fakeDelegate(t *testing.T, body string) (delegate.Delegate, run.DelegateSetup) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake.sh")
	program := "#!/bin/sh\n" + strings.Join([]string{
		`if [ -n "$FAKE_ARGS" ]; then printf '%s\n' "$@" > "$FAKE_ARGS"; fi`,
		`echo '{"type":"hello","protocol":2,"delegate":"fake","stages":["implement","verify"]}'`,
		`echo '{"type":"stage","stage":"implement","status":"running"}'`,
		`echo '{"type":"spend","cost_usd":0.05}'`,
		`echo '{"type":"step","command":"bash: go test ./...","observation":"ok"}'`,
		`echo '{"type":"step","command":"edit: a.go"}'`,
		body,
	}, "\n") + "\n"
	if err := os.WriteFile(script, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	return delegate.Delegate{Name: "fake", Summary: "a fake program", Default: "run"}, run.DelegateSetup{Exe: script}
}

func passLine(claim string) string {
	return `echo '{"type":"terminal","status":"pass","message":"submitted and verified","data":{"cost_usd":0.12,"submission_reason":"` + claim + `","status":"pass"}}'`
}

// funnel is the conversation's completer as a test writes it: every call is
// answered with words and billed at cost, the way the provider's decode bills
// one, and every model it was handed is kept.
type funnel struct {
	mu     sync.Mutex
	cost   float64
	models []string
}

func (f *funnel) completerFor(string) session.Completer { return f }

func (f *funnel) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	f.mu.Lock()
	f.models = append(f.models, request.Model)
	f.mu.Unlock()
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: request.Model, PromptTokens: 100, CompletionTokens: 10, CachedTokens: 60, Cost: f.cost})
	}
	question := ""
	if last := messages[len(messages)-1]; len(last.Content) > 0 {
		question = last.Content[0].Text
	}
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "answered " + question}}},
		FinishReason: "stop",
	}}}, nil
}

func (f *funnel) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.models...)
}

// realChild is the setup that starts THIS test binary as the program's process
// (delegate_child_test.go), with the model API served over a funnel costing
// cost a call and a spending ledger of the test's own.
func realChild(t *testing.T, cost float64, calls string) (delegate.Delegate, run.DelegateSetup, *funnel, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(delegateChildEnv, "1")
	t.Setenv("FAKE_CALLS", calls)
	ledger := filepath.Join(t.TempDir(), "usage.jsonl")
	calling := &funnel{cost: cost}
	return childProgram(), run.DelegateSetup{Exe: self, CompleterFor: calling.completerFor, Ledger: ledger, Grace: 5 * time.Second}, calling, ledger
}

// ledgerRows is the spending ledger's rows, once the writer has drained.
func ledgerRows(t *testing.T, path string) []session.UsageLine {
	t.Helper()
	session.FlushUsage()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("the spending ledger was never written: %v", err)
	}
	defer file.Close()
	var rows []session.UsageLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row session.UsageLine
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("a ledger row does not parse: %s", scanner.Bytes())
		}
		rows = append(rows, row)
	}
	return rows
}

// THE WHOLE ROAD, WITH A REAL CHILD: the worker opens the run's model API,
// starts the program as a process of its own with the API's address and token
// and no key, the program asks it two questions over a real socket, and every
// call is metered into all three books as it happens — the run's bank, the
// task's spend rows and the machine's ledger, once each — and written down as
// a turn of the program's conversation. The program's own claim about what it
// spent is never believed, and the token is dead the moment the program is.
func TestDelegateWorkerServesItsChildTheModelAPIAndMetersEveryCall(t *testing.T) {
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	taskDir := plandb.TaskDir(storeDir, store.RootID())
	program, setup, calling, ledger := realChild(t, 0.05, "2")
	apiFile := filepath.Join(t.TempDir(), "api")
	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv("FAKE_API_FILE", apiFile)
	t.Setenv("FAKE_ENV", envFile)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-the-parents-own-key")
	workspace := t.TempDir()
	worker := run.NewDelegateWorker(store, workspace, program, setup, 2.5, 0)

	var mu sync.Mutex
	var banked []float64
	ctx := run.WithSpendBank(runContext(t), func(usd float64) {
		mu.Lock()
		defer mu.Unlock()
		banked = append(banked, usd)
	})
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if err != nil {
		stderr, _ := os.ReadFile(filepath.Join(taskDir, "delegate-stderr.log"))
		t.Fatalf("the delegate's run failed: %v\nstderr:\n%s", err, stderr)
	}
	if report.Steps != 2 || !strings.Contains(report.Result, "fake's model said: all green") {
		t.Fatalf("report = %+v", report)
	}
	// THE METER'S FIGURE, NOT THE PROGRAM'S 99.
	if report.USD != 0.1 {
		t.Fatalf("usd = %v, want the two metered calls' 0.10", report.USD)
	}
	mu.Lock()
	if len(banked) != 2 || banked[0] != 0.05 || banked[1] != 0.1 {
		t.Fatalf("banked = %v, want the run's total rising call by call", banked)
	}
	mu.Unlock()
	if spend := store.SpendSummary().ByModel["delegate/fake"]; spend.USD != 0.1 || spend.Calls != 2 {
		t.Fatalf("spend rows = %+v, want one per call under the program's name", store.SpendSummary().ByModel)
	}
	rows := ledgerRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %+v, want exactly one per call", rows)
	}
	for _, row := range rows {
		if row.Model != "deepseek/deepseek-v4-flash-0731" || row.USD != 0.05 || row.Input != 100 || row.Calls != 1 || row.Seat != session.SeatWorker || row.Workspace != workspace {
			t.Fatalf("ledger row = %+v", row)
		}
	}
	// The conversation: two turns, what the program said and what came back.
	turns, err := delegate.ReadTurns(taskDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Reply != "answered call 1: drive the plan to the ground" || turns[1].CostUSD != 0.05 || turns[1].Cached != 60 {
		t.Fatalf("turns = %+v", turns)
	}
	if len(turns[0].Sent) != 2 || turns[0].Sent[0].Role != "system" || turns[1].Restarted != true {
		// The fake asks each question on a fresh two-message history, which
		// is a history rewritten — said so, and sent whole.
		t.Fatalf("sent = %+v / restarted %v", turns[0].Sent, turns[1].Restarted)
	}
	if record, ok := delegate.ReadProgram(taskDir); !ok || record.Name != "fake" || strings.Join(record.Stages, ",") != "implement,verify" || record.CeilingUSD != 2.5 {
		t.Fatalf("program record = %+v %v, want the hello's name and stages and the run's ceiling", record, ok)
	}
	if models := calling.seen(); len(models) != 2 || models[0] != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("the funnel was asked for %q", models)
	}
	// NO KEY REACHED THE PROGRAM, and the API it was given is dead now.
	environ, _ := os.ReadFile(envFile)
	if strings.Contains(string(environ), "sk-or-v1-the-parents-own-key") || !strings.Contains(string(environ), delegate.EnvModelToken+"=") {
		t.Fatalf("the child's environment:\n%s", environ)
	}
	api, _ := os.ReadFile(apiFile)
	base, token, _ := strings.Cut(strings.TrimSpace(string(api)), "\n")
	if !strings.HasPrefix(base, "http://127.0.0.1:") || token == "" {
		t.Fatalf("the child was handed %q", api)
	}
	request, _ := http.NewRequest(http.MethodPost, base+"/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer "+token)
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
		t.Fatalf("the run's token still opens its API after the run: %d", response.StatusCode)
	}
}

// A CHILD THAT CURLS THE API — the way any program outside codeaf's tree
// would — is served by the worker, its call metered and written down, and the
// token it was handed opens nothing once the run has ended.
func TestDelegateWorkerServesAChildThatCurlsTheAPIAndCutsItOffAfter(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl on this machine")
	}
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	saved := filepath.Join(t.TempDir(), "saved")
	reply := filepath.Join(t.TempDir(), "reply")
	t.Setenv("FAKE_SAVED", saved)
	t.Setenv("FAKE_REPLY", reply)
	m, setup := fakeDelegate(t, strings.Join([]string{
		`printf '%s\n%s\n' "$CODEAF_MODEL_API" "$CODEAF_MODEL_TOKEN" > "$FAKE_SAVED"`,
		`curl -sS -X POST "$CODEAF_MODEL_API/chat/completions" -H "Authorization: Bearer $CODEAF_MODEL_TOKEN" -H "Content-Type: application/json" ` +
			`-d '{"model":"z-ai/glm-5.1","messages":[{"role":"user","content":"is it green"}]}' > "$FAKE_REPLY"`,
		passLine("curl was answered"),
	}, "\n"))
	calling := &funnel{cost: 0.03}
	setup.CompleterFor = calling.completerFor
	setup.Ledger = filepath.Join(t.TempDir(), "usage.jsonl")
	var banked []float64
	ctx := run.WithSpendBank(runContext(t), func(usd float64) { banked = append(banked, usd) })
	report, err := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0).Run(ctx, *store.Task(store.RootID()))
	if err != nil {
		t.Fatal(err)
	}
	answered, _ := os.ReadFile(reply)
	if !strings.Contains(string(answered), `"content":"answered is it green"`) || !strings.Contains(string(answered), `"cost":0.03`) {
		t.Fatalf("curl was answered %s", answered)
	}
	if report.USD != 0.03 || len(banked) != 1 || banked[0] != 0.03 {
		t.Fatalf("usd %v banked %v, want the one metered call", report.USD, banked)
	}
	if turns, _ := delegate.ReadTurns(taskDir, 0); len(turns) != 1 || turns[0].Model != "z-ai/glm-5.1" || turns[0].Sent[0].Text != "is it green" {
		t.Fatalf("turns = %+v", turns)
	}
	lines, _ := os.ReadFile(saved)
	base, token, _ := strings.Cut(strings.TrimSpace(string(lines)), "\n")
	after := exec.Command("curl", "-sS", "--max-time", "5", "-X", "POST", base+"/chat/completions",
		"-H", "Authorization: Bearer "+token, "-d", `{"messages":[{"role":"user","content":"again"}]}`)
	if out, err := after.CombinedOutput(); err == nil {
		t.Fatalf("the token still opened the API after the run:\n%s", out)
	}
}

// THE PROGRAM'S OWN WORD ABOUT MONEY IS NOT MONEY: a run whose program made no
// call through the API spent nothing, whatever its spend lines and its
// terminal said, and leaves no spend row.
func TestDelegateWorkerRecordsStepsAndBelievesNoSpendItWasTold(t *testing.T) {
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	m, setup := fakeDelegate(t, passLine("tests are green"))
	worker := run.NewDelegateWorker(store, workspace, m, setup, 2.5, 0)

	var banked []float64
	ctx := run.WithSpendBank(runContext(t), func(usd float64) { banked = append(banked, usd) })
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if err != nil {
		t.Fatalf("the delegate's run failed: %v", err)
	}
	if report.Steps != 2 {
		t.Fatalf("steps = %d, want the two step records the program sent", report.Steps)
	}
	if report.USD != 0 || len(banked) != 0 {
		t.Fatalf("usd %v banked %v, want nothing: no call was metered", report.USD, banked)
	}
	if spend := store.SpendSummary(); len(spend.ByModel) != 0 {
		t.Fatalf("spend rows = %+v, want none", spend.ByModel)
	}
	if !strings.Contains(report.Result, "submitted and verified") || !strings.Contains(report.Result, "fake's model said: tests are green") || !strings.Contains(report.Result, "fake observed: pass") {
		t.Fatalf("result = %q, want the message, the claim and the observation as separate sentences", report.Result)
	}
	// The brief the program was handed is the task's description, and the
	// ceiling is the run's.
	got, _ := os.ReadFile(args)
	if want := "fake\nrun\n--json\n--dir\n" + workspace + "\n--max-cost\n2.5\n--\ndrive the plan to the ground\n"; string(got) != want {
		t.Fatalf("argv =\n%s\nwant\n%s", got, want)
	}
	// The trajectory: the opening line, two steps, the ending.
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Command != "bash: go test ./..." || steps[0].Observation != "ok" || steps[1].Step != 2 {
		t.Fatalf("trajectory steps = %+v", steps)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if len(lines) != 4 {
		t.Fatalf("the trajectory holds %d lines, want the opening, two steps and the ending", len(lines))
	}
	end := endLine(t, lines)
	if end.Steps != 2 || !strings.HasPrefix(end.Reason, "finished: ") {
		t.Fatalf("ending = %+v", end)
	}
	// The live step was cleared with the process, stderr went to the task's
	// folder, and the hello left the program's record beside it.
	if live := store.LiveSteps(); len(live) != 0 {
		t.Fatalf("live steps = %+v, want none after the program ended", live)
	}
	taskDir := plandb.TaskDir(storeDir, store.RootID())
	if _, err := os.Stat(filepath.Join(taskDir, "delegate-stderr.log")); err != nil {
		t.Fatalf("no stderr file beside the trajectory: %v", err)
	}
	if record, ok := delegate.ReadProgram(taskDir); !ok || record.Name != "fake" || len(record.Stages) != 2 {
		t.Fatalf("program record = %+v %v", record, ok)
	}
}

func TestDelegateWorkerReportsAFailedEndingAsAnError(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, `echo '{"type":"terminal","status":"fail","message":"unsubmitted","data":{"cost_usd":0.2,"status":"unsubmitted"}}'`)
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	report, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "fake did not finish: unsubmitted") {
		t.Fatalf("err = %v", err)
	}
	// The steps are kept on a failed ending; the terminal's $0.20 is the
	// program's own word and is not money.
	if report.USD != 0 || report.Steps != 2 {
		t.Fatalf("report = %+v, want the steps kept and nothing banked on the program's word", report)
	}
}

// A brief that named the folder the task was proposed on reaches the program
// naming the copy it works in, so the program is never told a path it must not
// work in.
func TestDelegateWorkerHandsTheProgramABriefThatNamesItsCopy(t *testing.T) {
	store := runOpenStore(t)
	ground := filepath.Join(t.TempDir(), "project")
	if _, err := store.Amend(store.RootID(), "work in the checkout at "+ground+" and commit there."); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	m, setup := fakeDelegate(t, passLine("done"))
	setup.Ground = []string{ground}
	worker := run.NewDelegateWorker(store, workspace, m, setup, 0, 0)
	if _, err := worker.Run(runContext(t), *store.Task(store.RootID())); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "work in the checkout at "+workspace+" and commit there.") || strings.Contains(string(got), ground) {
		t.Fatalf("the program was handed:\n%s\nwant the brief naming its copy %s and never %s", got, workspace, ground)
	}
}

// A program that ended without finishing says why, and the run carries its
// words whole to whoever drew the row: its status word, its sentence and its
// account, not only the run's one word for every unfinished ending.
func TestARunCarriesTheProgramsOwnEndingWhenItDidNotFinish(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, `echo '{"type":"terminal","status":"fail","message":"submitted a change the project tests do not pass","data":{"submission_reason":"all done","status":"fail"}}'`)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Title: "The run", Brief: "drive the plan to the ground", Slots: 1,
		Factory: run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{}, nil),
	})
	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want incomplete", outcome)
	}
	ended := summary.Program
	if ended == nil || ended.Status != delegate.StatusFail ||
		ended.Reason != "fake did not finish: submitted a change the project tests do not pass" ||
		!strings.Contains(ended.Result, "fake's model said: all done") {
		t.Fatalf("the run's program ending = %+v, want the program's own status, sentence and account", ended)
	}
}

func TestDelegateWorkerNamesAnExitWithoutATerminal(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, "exit 7")
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || err.Error() != "fake exited 7 without a terminal record; its last stage was implement" {
		t.Fatalf("err = %v", err)
	}
}

func TestDelegateWorkerComesHomeWithTheContextsEndingWhenTheRunStopsIt(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, strings.Join([]string{
		`trap 'echo "{\"type\":\"terminal\",\"status\":\"budget-exhausted\",\"message\":\"told to stop\",\"data\":{\"cost_usd\":0.11}}"; exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n"))
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	ctx, cancel := context.WithCancel(runContext(t))
	go func() {
		// Once the store has the program's live step, the program is past its
		// trap line and the signal will be caught.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if live := store.LiveSteps(); len(live) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's own so the run records the cut", err)
	}
	if report.USD != 0 {
		t.Fatalf("usd = %v, want nothing: the program made no metered call", report.USD)
	}
	lines := rawTrajectory(t, filepath.Dir(store.Path()), store.RootID())
	end := endLine(t, lines)
	if end.Reason != "stopped by the run: fake said told to stop" {
		t.Fatalf("ending reason = %q", end.Reason)
	}
}

// The whole road: a run of one task whose root is the delegate — a real child
// calling the real API — driven by the supervisor to done, with the
// delegate's words as the run's result and the metered calls as its dollars.
func TestARunSeatsTheDelegateOnItsRootAndEndsDone(t *testing.T) {
	store := runOpenStore(t)
	m, setup, _, _ := realChild(t, 0.06, "2")
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{CostUSD: 5}, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "The run",
		Brief:     "drive the plan to the ground",
		Slots:     1,
		Limits:    run.Limits{CostUSD: 5},
		Factory:   factory,
	})
	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want done", outcome)
	}
	if !strings.Contains(summary.Result, "fake's model said: all green") {
		t.Fatalf("result = %q", summary.Result)
	}
	if summary.USD != 0.12 || summary.Steps != 2 || summary.Nodes != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusDone {
		t.Fatalf("root status = %q", root.Status)
	}
}

// A run whose dollar ceiling the delegate's METERED calls cross is ended by
// the run on the limit word, with the program terminated and its own terminal
// kept — and the API refuses every call past the ceiling, so the run spent
// exactly what the calls under it cost.
func TestARunEndsADelegateThatCrossesTheCostCeiling(t *testing.T) {
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	m, setup, calling, _ := realChild(t, 0.06, "6")
	t.Setenv("FAKE_ENDING", "wait")
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{CostUSD: 0.10}, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Slots: 1,
		Limits:  run.Limits{CostUSD: 0.10},
		Factory: factory,
	})
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost {
		t.Fatalf("outcome = %q limit = %q, want the cost limit", outcome, summary.Limit)
	}
	if len(summary.Cut) != 1 {
		t.Fatalf("cut = %v, want the root cut by the run's own ending", summary.Cut)
	}
	if summary.USD != 0.12 || len(calling.seen()) != 2 {
		t.Fatalf("usd %v after %d funnel calls, want exactly the two calls that crossed the ceiling", summary.USD, len(calling.seen()))
	}
	turns, _ := delegate.ReadTurns(taskDir, 0)
	for _, turn := range turns[2:] {
		if turn.Refused == "" || turn.CostUSD != 0 {
			t.Fatalf("a call past the ceiling was made: %+v", turn)
		}
	}
}

// A PROGRAM REFUSED AT THE CEILING WAS STOPPED BY THE CEILING, however it says
// it ended: the fake ends as `crashed` the moment a call is refused, the way
// senior-dev does, and the task keeps the ceiling's words and not a crash's.
func TestDelegateWorkerReportsAProgramRefusedAtTheCeilingAsTheCeiling(t *testing.T) {
	store := runOpenStore(t)
	m, setup, calling, _ := realChild(t, 0.06, "5")
	t.Setenv("FAKE_ENDING", "crash")
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0.10, 0)
	report, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "fake reached the run's dollar ceiling of $0.10") || strings.Contains(err.Error(), "crashed") {
		t.Fatalf("err = %v, want the ceiling named and no crash", err)
	}
	if report.USD != 0.12 || len(calling.seen()) != 2 {
		t.Fatalf("usd %v after %d calls, want the two calls that reached the ceiling", report.USD, len(calling.seen()))
	}
	end := endLine(t, rawTrajectory(t, filepath.Dir(store.Path()), store.RootID()))
	if !strings.HasPrefix(end.Reason, "fake reached the run's dollar ceiling") {
		t.Fatalf("the trajectory ends %q", end.Reason)
	}
}

// AND THE RUN ENDS ON THE PERSON'S COST LIMIT, whichever comes home first —
// the supervisor's own stop or the program's ending after its refusal.
func TestARunWhoseDelegateWasRefusedAtTheCeilingEndsOnTheCostLimit(t *testing.T) {
	store := runOpenStore(t)
	m, setup, _, _ := realChild(t, 0.06, "5")
	t.Setenv("FAKE_ENDING", "crash")
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{CostUSD: 0.10}, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Slots: 1,
		Limits:  run.Limits{CostUSD: 0.10},
		Factory: factory,
	})
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost || summary.USD != 0.12 {
		t.Fatalf("outcome %q limit %q usd %v, want the cost limit at the two calls' 0.12", outcome, summary.Limit, summary.USD)
	}
}

// TWO BUILDS, ONE RUN: a child that says another protocol version than this
// build reads is stopped before it spends, and the reason names the fix.
func TestDelegateWorkerStopsAChildOfAnotherBuild(t *testing.T) {
	store := runOpenStore(t)
	script := filepath.Join(t.TempDir(), "newer.sh")
	program := "#!/bin/sh\n" + strings.Join([]string{
		`echo '{"type":"hello","protocol":99,"delegate":"fake"}'`,
		`trap 'exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n") + "\n"
	if err := os.WriteFile(script, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), delegate.Delegate{Name: "fake", Default: "run"}, run.DelegateSetup{Exe: script, Grace: time.Second}, 0, 0)
	started := time.Now()
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "restart codeaf to run fake") || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the rebuild named and not the run's own ending", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("the mismatched child was not stopped")
	}
	if _, ok := delegate.ReadProgram(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())); ok {
		t.Fatal("a child of another build was written down as this run's program")
	}
}
