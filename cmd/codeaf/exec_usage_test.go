package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec"
	homepkg "github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// readExecUsageRows reads this machine's usage ledger back into the rows it
// holds, for the throwaway home the test pinned with CODEAF_HOME.
//
// THE FLUSH IS THE WAIT AND NOT A SLEEP. A row is handed to a background writer
// (usage_ledger.go), so [session.ReadUsage] on its own may look before the
// write lands — [session.FlushUsage] is the one door that waits for it, and it
// is the same door a process on its way out uses.
func readExecUsageRows(t *testing.T) []session.UsageLine {
	t.Helper()
	session.FlushUsage()
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatalf("the usage ledger: %v", err)
	}
	return rows
}

// TestExecLeavesItsWorkerSpendInTheUsageLedger is the whole of what the headless
// door owes the ledger: one exec run driven end to end leaves exactly one row,
// carrying the run's own root, the worker model, the seat and role that say
// this is the run's work, and the cost and tokens the run itself reported — the
// same facts the envelope and the pending landing are built from.
func TestExecLeavesItsWorkerSpendInTheUsageLedger(t *testing.T) {
	model, _ := execPendingHome(t)

	envelope := runOnePendingExec(t, model)

	rows := readExecUsageRows(t)
	if len(rows) != 1 {
		t.Fatalf("one exec run left %d usage rows, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Root != envelope.Run {
		t.Fatalf("the row is rooted at %q, want the run's own id %q", row.Root, envelope.Run)
	}
	if row.Model != model {
		t.Fatalf("the row names model %q, want the run's worker %q", row.Model, model)
	}
	if row.Seat != session.SeatWorker {
		t.Fatalf("the row is seated %q, want %q — exec runs on the seat that does the work", row.Seat, session.SeatWorker)
	}
	if row.Role != string(roles.RoleWorker) {
		t.Fatalf("the row's role is %q, want %q", row.Role, roles.RoleWorker)
	}
	if want := envelope.Usage.PromptTokens + envelope.Usage.CompletionTokens; row.Input+row.Output != want {
		t.Fatalf("the row holds %d in + %d out tokens, want the run's own %d", row.Input, row.Output, want)
	}
	if row.USD != envelope.Usage.Cost {
		t.Fatalf("the row holds $%v, want the run's own $%v", row.USD, envelope.Usage.Cost)
	}
	if row.Input+row.Output == 0 {
		t.Fatal("the row is priced at nothing, so the ledger cannot tell this run spent anything")
	}
}

// TestExecLeavesNoUsageRowWhenNothingWasSpent is the other half: a run that made
// no provider call owes the ledger nothing.
//
// THE DOOR MINTS A ROW FROM WHAT THE LOOP REPORTED, so the case is the loop's
// own empty report — the nil outcome of a run that never started, and the empty
// one of a run that started and priced nothing. [session.RecordUsage] refuses a
// row whose cost and tokens are all zero itself, because a zero row is a day
// that looks measured and was not.
func TestExecLeavesNoUsageRowWhenNothingWasSpent(t *testing.T) {
	t.Setenv(homepkg.EnvVar, t.TempDir())

	recordExecUsage("test/model", nil, "run-root", "workspace")
	recordExecUsage("test/model", &exec.Outcome{}, "run-root", "workspace")

	if rows := readExecUsageRows(t); len(rows) != 0 {
		t.Fatalf("runs that spent nothing left %d usage rows: %+v", len(rows), rows)
	}
}
