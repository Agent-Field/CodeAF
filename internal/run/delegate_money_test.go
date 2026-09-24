//go:build !windows

package run_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A PROGRAM'S CALLS ARE THE CONVERSATION'S CHILDREN, EACH ONCE. Every ledger
// row a delegated run writes names the conversation as its Root and its
// Session and the task as its Task, so the conversation's receipt counts the
// calls under the work it started — never under its own calls, never twice —
// and the spending page files them under the task. Every call also reaches
// the conversation's fold whole, tokens and model with its dollars.
func TestADelegatedRunsLedgerRowsNameTheConversationAndTheTask(t *testing.T) {
	store := runOpenStore(t)
	program, setup, _, ledger := realChild(t, 0.05, "2")
	const conversation = "3d6ddfd172b2960f"
	var mu sync.Mutex
	var charges []session.RunCharge
	setup.Conversation = conversation
	setup.OnCharge = func(charge session.RunCharge) {
		mu.Lock()
		defer mu.Unlock()
		charges = append(charges, charge)
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), program, setup, 0, 0)
	if _, err := worker.Run(runContext(t), *store.Task(store.RootID())); err != nil {
		t.Fatal(err)
	}
	rows := ledgerRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %+v", rows)
	}
	for _, row := range rows {
		if row.Root != conversation || row.Session != conversation || row.Task != store.RootID() {
			t.Fatalf("ledger row = %+v, want the conversation as Root and Session and the task as Task", row)
		}
	}
	receipt := session.UsageTree(rows, conversation)
	if receipt.Children != 0.1 || receipt.Direct != 0 || receipt.Calls != 2 {
		t.Fatalf("the conversation's receipt = %+v, want the two calls once each, under the work it started", receipt)
	}
	subjects := session.UsageBySubject(rows)
	if len(subjects) != 1 || subjects[0].Kind != session.SubjectTask || subjects[0].ID != store.RootID() ||
		subjects[0].Root != conversation || subjects[0].USD != 0.1 || subjects[0].Calls != 2 {
		t.Fatalf("spend by subject = %+v, want one task row holding both calls", subjects)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(charges) != 2 || charges[0].USD != 0.05 || charges[0].TokensIn != 100 || charges[0].Cached != 60 ||
		charges[0].Model != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("folded charges = %+v, want each call whole", charges)
	}
}

// owingFunnel answers every call at once without a usage block and owes its
// receipt, which it delivers after a delay — the provider's own order, owed
// before the fetch and answered after the sink has the money.
type owingFunnel struct {
	late time.Duration
	cost float64
}

func (f *owingFunnel) completerFor(string) session.Completer { return f }

func (f *owingFunnel) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	done := provider.ReceiptPendingFrom(ctx)()
	sink := provider.ReconcileSinkFrom(ctx)
	go func() {
		defer done()
		time.Sleep(f.late)
		sink(provider.Reconciled{Billed: provider.Billed{Model: request.Model, PromptTokens: 52139, CompletionTokens: 4895, Cost: f.cost}, Found: true})
	}()
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "cut short"}}},
		FinishReason: "stop",
	}}}, nil
}

// THE CALL A RUN WAS CUT IN THE MIDDLE OF IS IN THE RUN'S BOOKS. Its price
// arrives by receipt after the program has exited — twenty seconds late on the
// stopped runs of 2026-09-23 — and the worker waits for it before it reports,
// so the run's total, the task's spend rows and the conversation's fold all
// hold it while the store is still open. Before the wait, the report read $0
// and the store had no row.
func TestDelegateWorkerBanksTheReceiptThatArrivesAfterTheProgramExited(t *testing.T) {
	store := runOpenStore(t)
	program, setup, _, ledger := realChild(t, 0, "1")
	owing := &owingFunnel{late: 400 * time.Millisecond, cost: 0.058188488}
	setup.CompleterFor = owing.completerFor
	var mu sync.Mutex
	var folded float64
	setup.OnCharge = func(charge session.RunCharge) {
		mu.Lock()
		defer mu.Unlock()
		folded += charge.USD
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), program, setup, 0, 0)
	report, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err != nil {
		stderr, _ := os.ReadFile(filepath.Join(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID()), "delegate-stderr.log"))
		t.Fatalf("run: %v\n%s", err, stderr)
	}
	if report.USD != 0.058188488 {
		t.Fatalf("report usd = %v, want the late receipt's $0.058188488", report.USD)
	}
	if spend := store.SpendSummary().ByModel["delegate/fake"]; spend.USD != 0.058188488 || spend.Calls != 1 {
		t.Fatalf("spend rows = %+v, want the late receipt's row", store.SpendSummary().ByModel)
	}
	mu.Lock()
	if folded != 0.058188488 {
		mu.Unlock()
		t.Fatalf("folded %v before the worker reported, want the late receipt", folded)
	}
	mu.Unlock()
	if rows := ledgerRows(t, ledger); len(rows) != 1 || !rows[0].Reconciled || rows[0].USD != 0.058188488 {
		t.Fatalf("ledger rows = %+v", rows)
	}
}

// A SPEND ROW THE STORE REFUSES IS SAID, NOT DROPPED. A receipt so late it
// outlived the worker's wait reaches a store that has closed; the ledger has
// it, and the task's record folder says the task's rows do not.
func TestAChargeTheStoreRefusedIsWrittenDownInTheTasksRecord(t *testing.T) {
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	program, setup, _, ledger := realChild(t, 0.05, "1")
	setup.CompleterFor = (&closingFunnel{store: store}).completerFor
	worker := run.NewDelegateWorker(store, t.TempDir(), program, setup, 0, 0)
	if _, err := worker.Run(runContext(t), *store.Task(store.RootID())); err != nil {
		t.Fatal(err)
	}
	stderr, _ := os.ReadFile(filepath.Join(taskDir, "delegate-stderr.log"))
	if !strings.Contains(string(stderr), "codeaf: a charge of $0.050000 for a call on deepseek/deepseek-v4-flash-0731 is not in this task's spend rows") {
		t.Fatalf("delegate-stderr.log:\n%s", stderr)
	}
	if rows := ledgerRows(t, ledger); len(rows) != 1 || rows[0].USD != 0.05 {
		t.Fatalf("ledger rows = %+v", rows)
	}
}

// closingFunnel closes the run's store before it bills its one call, the way
// a store has closed under a receipt that arrived after the run was over.
type closingFunnel struct{ store *plandb.Store }

func (f *closingFunnel) completerFor(string) session.Completer { return f }

func (f *closingFunnel) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	_ = f.store.Close()
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: request.Model, PromptTokens: 100, CompletionTokens: 10, Cost: 0.05})
	}
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "ok"}}},
		FinishReason: "stop",
	}}}, nil
}

// A RUN HANDED NOTHING OF ITS LIMIT SPENDS NOTHING AND ENDS ON THE LIMIT. The
// conversation hands a run whose person's limit is already spent the smallest
// positive figure; the program's first call is refused before it is made, and
// the run ends on the person's cost limit rather than as work that broke.
func TestARunWhoseLimitIsAlreadySpentMakesNoCallAndEndsOnTheLimit(t *testing.T) {
	store := runOpenStore(t)
	m, setup, calling, _ := realChild(t, 0.139463, "3")
	t.Setenv("FAKE_ENDING", "crash")
	spent := run.Limits{CostUSD: math.SmallestNonzeroFloat64}
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, spent, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Slots: 1, Limits: spent, Factory: factory,
	})
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost {
		t.Fatalf("outcome %q limit %q, want the cost limit", outcome, summary.Limit)
	}
	if summary.USD != 0 || len(calling.seen()) != 0 {
		t.Fatalf("usd %v after %d funnel calls, want nothing made and nothing spent", summary.USD, len(calling.seen()))
	}
}

// AND NO WORKER OF ANY KIND IS SEATED on a run handed nothing of its limit: a
// worker that meters only its own total would otherwise make the one paid call
// that tells the loop the limit is gone.
func TestARunWhoseLimitIsAlreadySpentSeatsNoWorker(t *testing.T) {
	store := runOpenStore(t)
	var seated int
	var mu sync.Mutex
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Slots: 1,
		Limits: run.Limits{CostUSD: math.SmallestNonzeroFloat64},
		Factory: func(plandb.Task) run.Worker {
			mu.Lock()
			seated++
			mu.Unlock()
			return nil
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost || seated != 0 {
		t.Fatalf("outcome %q limit %q with %d workers seated, want the cost limit and none", outcome, summary.Limit, seated)
	}
}
