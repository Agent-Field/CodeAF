package run_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/run"
)

// A receipt arriving after an errored turn must reach the worker before its books close.
func TestBashWorkerSettlesLateReceiptsBeforeReportingItsTotals(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	queued := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{})
	var released sync.Once
	releaseReceipt := func() { released.Do(func() { close(release) }) }
	defer releaseReceipt()
	s := &seat{ever: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		pending := provider.ReceiptPendingFrom(ctx)
		if pending == nil {
			return nil, errors.New("worker did not arm receipt settlement")
		}
		done := pending()
		sink := provider.ReconcileSinkFrom(ctx)
		go func() {
			defer close(delivered)
			defer done()
			<-release
			sink(provider.Reconciled{Billed: provider.Billed{Model: "test/model", PromptTokens: 91, CompletionTokens: 17, Cost: 0.37}, Found: true})
		}()
		close(queued)
		return nil, errors.New("API error (401): account refused")
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", s)
	report := make(chan run.Report, 1)
	go func() { rep, _ := worker.Run(runContext(t), *store.Task(store.RootID())); report <- rep }()
	select {
	case <-queued:
	case rep := <-report:
		t.Fatalf("worker ended before queuing receipt: %+v", rep)
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not ask")
	}
	// The final total must include the receipt even though no successful turn ended.
	// Release immediately; the pending registration itself is the settlement handshake.
	releaseReceipt()
	select {
	case rep := <-report:
		if rep.USD != 0.37 || rep.TokensIn != 91 || rep.TokensOut != 17 {
			t.Fatalf("late paid call lost: %+v", rep)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not settle")
	}
	<-delivered
}

// The real worker's ordinary error ending owes no receipt and must come home
// immediately rather than sitting out the provider's receipt grace.
func TestBashWorkerWithNoOutstandingReceiptEndsWithoutAReceiptWait(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	s := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("API error (401): account refused")
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", s)
	ended := make(chan error, 1)
	go func() { _, err := worker.Run(runContext(t), *store.Task(store.RootID())); ended <- err }()
	select {
	case err := <-ended:
		if err == nil {
			t.Fatal("worker lost its authentication ending")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary ending waited despite having no outstanding receipt")
	}
}
