package session

// A PROGRAM'S REPORT FILE, AS THE APP'S REPORT READ SERVES IT: the exact bytes
// of one file a program declared ([delegate.Delegate.Reports]) in the record
// folder of the task it was handed, for the launch the app is looking at and
// no other, ON THAT LAUNCH'S OWN RECEIPT.
//
// THE REQUEST NAMES A TASK, A LAUNCH AND A DECLARED NAME, AND NOTHING THAT IS A
// PATH. The folder is the task's own ([plandb.TaskDir]); the name must be one
// the task's program declared; the launch must be the record's right now; and
// that launch's program must have issued a receipt for the name when it wrote
// it ([delegate.ProgramRecord.ReportReceipts]), which the bytes must hash to.
// A declaration, an ended run and a file under the name are not enough: a
// rerun that failed before it wrote leaves the earlier run's file standing, and
// that file is not the rerun's report. The ending's display names
// (EndFacts.Reports) are never consulted.

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// AppProgramReport is one report file, as the run wrote it.
type AppProgramReport struct {
	Program, Launch, Name string
	StartedAt, EndedAt    time.Time
	// Data is the file's exact bytes and SHA256 their lowercase hex digest,
	// which is the receipt's.
	Data   []byte
	SHA256 string
}

// AppProgramReport serves a task's program's report. Found is false for a task
// that is not this conversation's or has no program record.
func (a *Agent) AppProgramReport(ctx context.Context, id, launch, name string) (AppProgramReport, bool, error) {
	source, found := a.planProgramSourceOf(id)
	if !found {
		return AppProgramReport{}, false, nil
	}
	return ReadAppProgramReport(ctx, plandb.TaskDir(source.dir, source.id), a.config.Delegates, launch, name)
}

// ReadAppProgramReport is the read behind [Agent.AppProgramReport], on the
// record folder taskDir: the engine's single owner of what may be served from it.
func ReadAppProgramReport(ctx context.Context, taskDir string, programs []delegate.Delegate, launch, name string) (AppProgramReport, bool, error) {
	report, err := delegate.ReadIssuedReport(ctx, taskDir, launch, name, func(program string) bool {
		return planProgramOf(programs, program).DeclaresReport(name)
	})
	if errors.Is(err, delegate.ErrReportNoRecord) {
		return AppProgramReport{}, false, nil
	}
	if err != nil {
		return AppProgramReport{}, true, err
	}
	return AppProgramReport{Program: report.Program, Launch: report.Launch, Name: name, StartedAt: report.StartedAt,
		EndedAt: report.EndedAt, Data: report.Data, SHA256: report.Receipt.SHA256}, true, nil
}

// appProgramReportFiles are the names a reader may ask for: the reports the
// program declares, in its declared order, that the record's own launch issued
// a receipt for. A declared name with no receipt (a failed rerun, a record
// from before receipts) is not offered.
func appProgramReportFiles(program delegate.Delegate, record delegate.ProgramRecord) []string {
	if !delegate.ValidLaunch(record.Launch) || record.EndedAt.IsZero() {
		return nil
	}
	var names []string
	for _, name := range program.Reports {
		if _, issued := record.Receipt(name); issued && program.DeclaresReport(name) {
			names = append(names, name)
		}
	}
	return names
}

// appProgramLaunch is the record's launch when it has the shape a launch has,
// and nothing otherwise: what a report is later asked for with.
func appProgramLaunch(record delegate.ProgramRecord) string {
	if delegate.ValidLaunch(record.Launch) {
		return record.Launch
	}
	return ""
}
