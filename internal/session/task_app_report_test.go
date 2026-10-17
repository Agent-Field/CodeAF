package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// reportFixture is programAppFixture's task with a program that declares one
// report, an ended run whose launch issued a receipt for it, and that report
// on the disk in the task's record folder.
func reportFixture(t *testing.T, body []byte) (*Agent, string, string) {
	t.Helper()
	agent, dir := programAppFixture(t)
	folder := plandb.TaskDir(dir, "alpha")
	agent.config.Delegates[0].Reports = []string{"review-report.json"}
	record, _ := delegate.ReadProgram(folder)
	record.EndedAt = record.StartedAt.Add(time.Hour)
	record.ReportReceipts = []delegate.ReportReceipt{delegate.IssueReportReceipt("review-report.json", body)}
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "review-report.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return agent, folder, "alpha"
}

// THE REAL OWNER SERVES THE RECEIPTED FILE OF THE RECORD'S LAUNCH, byte for byte, and the task's page names the
// launch and the issued files the request is made with.
func TestAppProgramReportServesTheDeclaredFileOfTheRecordsLaunch(t *testing.T) {
	body := []byte("\xef\xbb\xbf{\"x\":\"\xff\"}")
	agent, _, id := reportFixture(t, body)
	view := appTaskOf(t, agent)
	if view.Program.Launch != fixtureLaunch || len(view.Program.ReportFiles) != 1 || view.Program.ReportFiles[0] != "review-report.json" {
		t.Fatalf("launch %q files %v", view.Program.Launch, view.Program.ReportFiles)
	}
	got, found, err := agent.AppProgramReport(context.Background(), id, fixtureLaunch, "review-report.json")
	if err != nil || !found || !bytes.Equal(got.Data, body) || got.Program != "senior-dev" || got.Launch != fixtureLaunch || got.EndedAt.IsZero() ||
		got.SHA256 != delegate.IssueReportReceipt("review-report.json", body).SHA256 {
		t.Fatalf("%+v found %v err %v", got, found, err)
	}
}

func TestAppProgramReportRefusesWhatItDoesNotOwn(t *testing.T) {
	agent, folder, id := reportFixture(t, []byte("{}"))
	ask := func(task, launch, name string) (bool, error) {
		_, found, err := agent.AppProgramReport(context.Background(), task, launch, name)
		return found, err
	}
	// A foreign task, and a task that is not there.
	if found, err := ask("nope", fixtureLaunch, "review-report.json"); found || err != nil {
		t.Fatalf("foreign task: %v %v", found, err)
	}
	// A name the program did not declare, even a real file.
	if err := os.WriteFile(filepath.Join(folder, "other.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(id, fixtureLaunch, "other.json"); !errors.Is(err, delegate.ErrReportUndeclared) {
		t.Fatalf("undeclared: %v", err)
	}
	// A path is never a name: it reaches nothing on the disk.
	if _, err := ask(id, fixtureLaunch, "../alpha/review-report.json"); !errors.Is(err, delegate.ErrReportUnsafe) {
		t.Fatalf("traversal: %v", err)
	}
	// An earlier run's launch, and a malformed one.
	if _, err := ask(id, earlierLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportMoved) {
		t.Fatalf("earlier launch: %v", err)
	}
	if _, err := ask(id, "x", "review-report.json"); !errors.Is(err, delegate.ErrReportMoved) {
		t.Fatalf("malformed launch: %v", err)
	}
	// A run that has not ended has no final report.
	record, _ := delegate.ReadProgram(folder)
	record.EndedAt = time.Time{}
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(id, fixtureLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportNotFinal) {
		t.Fatalf("running: %v", err)
	}
}

// A RERUN IS REFUSED: the cursor of the first launch asked after the program was handed the task again.
func TestAppProgramReportRefusesARerunAndAFileASecondRunRewrote(t *testing.T) {
	agent, folder, id := reportFixture(t, []byte(`{"run":1}`))
	record, _ := delegate.ReadProgram(folder)
	record.Launch, record.EndedAt = earlierLaunch, record.StartedAt.Add(2*time.Hour)
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := agent.AppProgramReport(context.Background(), id, fixtureLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportMoved) {
		t.Fatalf("first launch after a rerun: %v", err)
	}
	// The record moving under the read, after the file is read.
	defer delegate.SetReportReadHookForTest(nil)
	delegate.SetReportReadHookForTest(func() {
		rewritten, _ := delegate.ReadProgram(folder)
		rewritten.Launch = fixtureLaunch
		_ = delegate.WriteProgram(folder, rewritten)
	})
	if got, _, err := agent.AppProgramReport(context.Background(), id, earlierLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportMoved) || got.Data != nil {
		t.Fatalf("a relaunch during the read served %q (%v)", got.Data, err)
	}
}

func TestAppProgramReportRefusesALinkedRecordFolderFile(t *testing.T) {
	agent, folder, id := reportFixture(t, []byte("{}"))
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "review-report.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if got, _, err := agent.AppProgramReport(context.Background(), id, fixtureLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportUnsafe) || got.Data != nil {
		t.Fatalf("served %q (%v)", got.Data, err)
	}
}

// A FAILED RERUN IS NOT SERVED THE EARLIER RUN'S REPORT, AND IS NOT OFFERED ONE.
// Run A wrote its report and issued its receipt; run B of the same task ended
// without writing one. The file on the disk is A's, so B's page names no
// report file and asking for B's report is answered "this run left no such
// report", never A's bytes.
func TestAppProgramReportRefusesTheEarlierRunsFileAfterAFailedRerun(t *testing.T) {
	agent, folder, id := reportFixture(t, []byte(`{"program":"review","run":"A"}`+"\n"))
	started := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "senior-dev", Launch: earlierLaunch,
		StartedAt: started, EndedAt: started.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "review-report.json")); err != nil {
		t.Fatal(err)
	}
	if view := appTaskOf(t, agent); view.Program.Launch != earlierLaunch || len(view.Program.ReportFiles) != 0 {
		t.Fatalf("the rerun is offered %v", view.Program.ReportFiles)
	}
	got, _, err := agent.AppProgramReport(context.Background(), id, earlierLaunch, "review-report.json")
	if !errors.Is(err, delegate.ErrReportNotIssued) || got.Data != nil {
		t.Fatalf("the rerun was served run A's file as its own: %q (%v)", got.Data, err)
	}
}

// A RECORD FROM BEFORE RECEIPTS IS NOT BACKFILLED: its declared file, there on
// the disk, is neither offered nor served.
func TestAppProgramReportServesNothingForARecordFromBeforeReceipts(t *testing.T) {
	agent, folder, id := reportFixture(t, []byte("{}"))
	record, _ := delegate.ReadProgram(folder)
	record.ReportReceipts = nil
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	if view := appTaskOf(t, agent); len(view.Program.ReportFiles) != 0 {
		t.Fatalf("a legacy record is offered %v", view.Program.ReportFiles)
	}
	if got, _, err := agent.AppProgramReport(context.Background(), id, fixtureLaunch, "review-report.json"); !errors.Is(err, delegate.ErrReportNotIssued) || got.Data != nil {
		t.Fatalf("legacy: %q (%v)", got.Data, err)
	}
}
