//go:build linux || darwin

package run_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE WHOLE LIFE OF A REPORT, THROUGH THE REAL WORKER AND THE REAL READER. A
// program writes its report into the record folder codeaf named and issues
// its receipt on its terminal line; the worker keeps that receipt on the
// launch's record; the engine's reader serves exactly those bytes for that
// launch. The task is then handed to its program again and the rerun FAILS
// BEFORE IT WRITES: the first run's file is still on the disk under the
// declared name, and it is not the rerun's report, nor is the first launch
// any longer the record's.
func TestAReportIsServedOnlyOnTheReceiptOfTheLaunchThatWroteIt(t *testing.T) {
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	body := []byte("\xef\xbb\xbf{\"program\":\"fake\",\"x\":\"\xff\"}\n")
	receipt := delegate.IssueReportReceipt("review-report.json", body)
	undeclared := delegate.IssueReportReceipt("other.json", []byte("{}"))
	receipts, err := json.Marshal([]delegate.ReportReceipt{receipt, undeclared})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(source, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m, setup := fakeDelegate(t, `cp `+strconv.Quote(source)+` "$`+delegate.EnvRecords+`/review-report.json" && echo '{"type":"terminal","status":"pass","message":"reviewed","data":{"report_receipts":`+string(receipts)+`}}'`)
	m.Reports = []string{"review-report.json"}
	programs := []delegate.Delegate{m}
	if _, err := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0).Run(runContext(t), *store.Task(store.RootID())); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first, ok := delegate.ReadProgram(taskDir)
	if !ok || !delegate.ValidLaunch(first.Launch) || first.EndedAt.IsZero() || len(first.ReportReceipts) != 1 || first.ReportReceipts[0] != receipt {
		t.Fatalf("the first launch's record = %+v %v; want its one declared receipt kept with its end", first, ok)
	}
	got, found, err := session.ReadAppProgramReport(context.Background(), taskDir, programs, first.Launch, "review-report.json")
	if err != nil || !found || !bytes.Equal(got.Data, body) || got.SHA256 != receipt.SHA256 || got.Launch != first.Launch {
		t.Fatalf("the first run's report = %+v %v %v", got, found, err)
	}

	m2, setup2 := fakeDelegate(t, `echo '{"type":"terminal","status":"fail","message":"could not reach the pull request","data":{}}'`)
	m2.Reports = m.Reports
	if _, err := run.NewDelegateWorker(store, t.TempDir(), m2, setup2, 0, 0).Run(runContext(t), *store.Task(store.RootID())); err == nil {
		t.Fatal("the failed rerun finished")
	}
	second, ok := delegate.ReadProgram(taskDir)
	if !ok || second.Launch == first.Launch || second.EndedAt.IsZero() || len(second.ReportReceipts) != 0 {
		t.Fatalf("the rerun's record = %+v %v; want a new ended launch with no receipt", second, ok)
	}
	if on, err := os.ReadFile(filepath.Join(taskDir, "review-report.json")); err != nil || !bytes.Equal(on, body) {
		t.Fatalf("the first run's file is not on the disk to be mistaken: %v", err)
	}
	if got, _, err := session.ReadAppProgramReport(context.Background(), taskDir, programs, second.Launch, "review-report.json"); !errors.Is(err, delegate.ErrReportNotIssued) || got.Data != nil {
		t.Fatalf("the rerun was served the first run's file: %q %v", got.Data, err)
	}
	if _, _, err := session.ReadAppProgramReport(context.Background(), taskDir, programs, first.Launch, "review-report.json"); !errors.Is(err, delegate.ErrReportMoved) {
		t.Fatalf("the first launch after the rerun: %v", err)
	}
}
