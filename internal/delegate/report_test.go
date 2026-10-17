//go:build linux || darwin

package delegate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	reportLaunchA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reportLaunchB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var reportStarted = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

func declaresReview(program string) bool { return program == "review" }

func writeReportFile(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// issuedRun is a launch that wrote each file in files and issued a receipt for
// it, as the worker keeps them: the record names the launch, its clock and its
// receipts. A nil body is a receipt-less name (nothing is written).
func issuedRun(t *testing.T, dir, launch string, files map[string][]byte) {
	t.Helper()
	record := ProgramRecord{Name: "review", Launch: launch, StartedAt: reportStarted, EndedAt: reportStarted.Add(time.Minute)}
	for name, body := range files {
		writeReportFile(t, dir, name, body)
		record.ReportReceipts = append(record.ReportReceipts, IssueReportReceipt(name, body))
	}
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
}

func readIssued(dir, launch, name string) (IssuedReport, error) {
	return ReadIssuedReport(context.Background(), dir, launch, name, declaresReview)
}

// THE BYTES ARE THE FILE'S, EXACTLY: invalid UTF-8, a byte-order mark, NUL and
// an empty file all come back as written, under the receipt that issued them.
func TestReadIssuedReportReturnsTheReceiptedBytes(t *testing.T) {
	for name, body := range map[string][]byte{
		"plain":   []byte(`{"program":"review"}` + "\n"),
		"invalid": []byte("{\"x\":\"\xff\xfe\xc3\x28\"}"),
		"bom":     append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"a":1}`)...),
		"nul":     {0, 1, 2, 0},
		"empty":   {},
	} {
		dir := t.TempDir()
		issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": body})
		got, err := readIssued(dir, reportLaunchA, "review-report.json")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got.Data, body) || got.Receipt != IssueReportReceipt("review-report.json", body) ||
			got.Program != "review" || got.Launch != reportLaunchA || !got.EndedAt.Equal(reportStarted.Add(time.Minute)) {
			t.Fatalf("%s: got %+v", name, got)
		}
	}
}

// A FAILED RERUN IS NOT SERVED THE EARLIER RUN'S FILE. Run A wrote and issued;
// run B ended with no receipt, so A's file under the declared name is not B's
// report. A receipt B issued for other bytes does not cover A's file either.
func TestReadIssuedReportRefusesTheEarlierRunsFile(t *testing.T) {
	dir := t.TempDir()
	issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": []byte(`{"run":"A"}`)})
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); err != nil {
		t.Fatalf("run A's own: %v", err)
	}
	if err := WriteProgram(dir, ProgramRecord{Name: "review", Launch: reportLaunchB, StartedAt: reportStarted.Add(time.Hour),
		EndedAt: reportStarted.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got, err := readIssued(dir, reportLaunchB, "review-report.json"); !errors.Is(err, ErrReportNotIssued) || got.Data != nil {
		t.Fatalf("failed rerun: %v %q", err, got.Data)
	}
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportMoved) {
		t.Fatalf("run A after B: %v", err)
	}
	record, _ := ReadProgram(dir)
	record.ReportReceipts = []ReportReceipt{IssueReportReceipt("review-report.json", []byte(`{"run":"B"}`))}
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
	if got, err := readIssued(dir, reportLaunchB, "review-report.json"); !errors.Is(err, ErrReportNotReceipted) || got.Data != nil {
		t.Fatalf("B's receipt over A's bytes: %v %q", err, got.Data)
	}
}

// A RECORD FROM BEFORE RECEIPTS IS UNAVAILABLE, never served on its declaration.
func TestReadIssuedReportServesNothingForALegacyRecord(t *testing.T) {
	dir := t.TempDir()
	writeReportFile(t, dir, "review-report.json", []byte("{}"))
	legacy := `{"name":"review","launch":"` + reportLaunchA + `","started_at":"2026-10-16T09:00:00Z","ended_at":"2026-10-16T09:01:00Z"}`
	writeReportFile(t, dir, ProgramFile, []byte(legacy))
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportNotIssued) {
		t.Fatalf("legacy: %v", err)
	}
}

// THE RECORD'S OWN QUESTIONS COME FIRST: an undeclared name, another launch, a
// run that has not ended, and no record.
func TestReadIssuedReportAnswersTheRecordsQuestions(t *testing.T) {
	dir := t.TempDir()
	issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": []byte("{}")})
	if _, err := ReadIssuedReport(context.Background(), dir, reportLaunchA, "review-report.json",
		func(string) bool { return false }); !errors.Is(err, ErrReportUndeclared) {
		t.Fatalf("undeclared: %v", err)
	}
	if _, err := readIssued(dir, reportLaunchB, "review-report.json"); !errors.Is(err, ErrReportMoved) {
		t.Fatalf("another launch: %v", err)
	}
	if _, err := readIssued(dir, "not-a-launch", "review-report.json"); !errors.Is(err, ErrReportMoved) {
		t.Fatalf("no launch: %v", err)
	}
	record, _ := ReadProgram(dir)
	record.EndedAt = time.Time{}
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportNotFinal) {
		t.Fatalf("not ended: %v", err)
	}
	if _, err := readIssued(t.TempDir(), reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportNoRecord) {
		t.Fatalf("no record: %v", err)
	}
	if _, err := readIssued(filepath.Join(dir, "no-such"), reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportNoRecord) {
		t.Fatalf("no folder: %v", err)
	}
}

// 2 MiB IS SERVED WHOLE AND ONE BYTE MORE IS NOT SERVED AT ALL.
func TestReadIssuedReportBoundIsExact(t *testing.T) {
	dir := t.TempDir()
	full := bytes.Repeat([]byte("x"), ReportMax)
	issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": full})
	if got, err := readIssued(dir, reportLaunchA, "review-report.json"); err != nil || len(got.Data) != ReportMax {
		t.Fatalf("exactly the bound: %d bytes, %v", len(got.Data), err)
	}
	writeReportFile(t, dir, "review-report.json", append(full, 'x'))
	if got, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportTooLarge) || got.Data != nil {
		t.Fatalf("one over: %d bytes, %v", len(got.Data), err)
	}
	// An issued report over the read bound stays known, but its bytes are refused.
	record, _ := ReadProgram(dir)
	record.ReportReceipts[0].Bytes = ReportMax + 1
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportTooLarge) {
		t.Fatalf("receipt over the bound: %v", err)
	}
}

// NOTHING BUT A BARE NAME REACHES THE DISK.
func TestReadIssuedReportRefusesAnythingButABareName(t *testing.T) {
	outer := t.TempDir()
	dir := filepath.Join(outer, "task")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeReportFile(t, outer, "secret.json", []byte("outside"))
	for _, name := range []string{"", ".", "..", "../secret.json", "a/b.json", `a\b.json`, filepath.Join(outer, "secret.json"),
		".hidden", "a b.json", "a\x00.json", "a\n.json", strings.Repeat("a", 65), "é.json"} {
		if _, err := readIssued(dir, reportLaunchA, name); !errors.Is(err, ErrReportUnsafe) {
			t.Errorf("%q: %v", name, err)
		}
		if ValidReportName(name) {
			t.Errorf("%q is valid", name)
		}
	}
}

// A LINK, A PIPE, A FOLDER OR A SECOND NAME WHERE THE REPORT GOES IS REFUSED,
// not followed, even with a receipt that matches what it leads to; so is a
// linked record folder; and a pipe never holds the read.
func TestReadIssuedReportRefusesLinksPipesFoldersAndSecondNames(t *testing.T) {
	outer := t.TempDir()
	dir := filepath.Join(outer, "task")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("inside")
	writeReportFile(t, outer, "secret.json", body)
	writeReportFile(t, dir, "real.json", body)
	names := []string{"out.json", "in.json", "dangling.json", "folder.json", "pipe.json", "hard.json"}
	record := ProgramRecord{Name: "review", Launch: reportLaunchA, StartedAt: reportStarted, EndedAt: reportStarted.Add(time.Minute)}
	for _, name := range append(names, "real.json", "nothing.json") {
		record.ReportReceipts = append(record.ReportReceipts, IssueReportReceipt(name, body))
	}
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink(filepath.Join(outer, "secret.json"), filepath.Join(dir, "out.json")))
	must(os.Symlink("real.json", filepath.Join(dir, "in.json")))
	must(os.Symlink("nowhere.json", filepath.Join(dir, "dangling.json")))
	must(os.Mkdir(filepath.Join(dir, "folder.json"), 0o700))
	must(syscall.Mkfifo(filepath.Join(dir, "pipe.json"), 0o600))
	writeReportFile(t, outer, "hard-source.json", body)
	must(os.Link(filepath.Join(outer, "hard-source.json"), filepath.Join(dir, "hard.json")))
	link := filepath.Join(outer, "linked")
	must(os.Symlink(dir, link))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, name := range names {
			if got, err := readIssued(dir, reportLaunchA, name); !errors.Is(err, ErrReportUnsafe) || got.Data != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, err := readIssued(link, reportLaunchA, "real.json"); !errors.Is(err, ErrReportUnsafe) {
			t.Errorf("a linked folder: %v", err)
		}
		if _, err := readIssued(dir, reportLaunchA, "nothing.json"); !errors.Is(err, ErrReportMissing) {
			t.Errorf("missing: %v", err)
		}
		if got, err := readIssued(dir, reportLaunchA, "real.json"); err != nil || !bytes.Equal(got.Data, body) {
			t.Errorf("the ordinary file beside them: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a pipe where the report goes held the read")
	}
}

// A REWRITE IN PLACE IS NEVER SERVED. Before the read, the same inode holding
// other bytes of the same size with its old mtime put back does not hash to
// the receipt; during the read, the change time moves even when the size and
// mtime are restored.
func TestReadIssuedReportRefusesASameInodeRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review-report.json")
	issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": []byte(`{"blocking":1}`)})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	rewrite := func(body string) {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteAt([]byte(body), 0); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	rewrite(`{"blocking":0}`)
	if got, err := readIssued(dir, reportLaunchA, "review-report.json"); !errors.Is(err, ErrReportNotReceipted) || got.Data != nil {
		t.Fatalf("rewritten before: %v %q", err, got.Data)
	}
	rewrite(`{"blocking":1}`)
	if _, err := readIssued(dir, reportLaunchA, "review-report.json"); err != nil {
		t.Fatalf("put back: %v", err)
	}
	defer SetReportReadHookForTest(nil)
	SetReportReadHookForTest(func() {
		// The change time has a clock's grain; a rewrite in the same tick
		// would leave it, so the rewrite waits one out.
		time.Sleep(20 * time.Millisecond)
		rewrite(`{"blocking":1}`)
	})
	if got, err := readIssued(dir, reportLaunchA, "review-report.json"); err == nil || got.Data != nil {
		t.Fatalf("rewritten during: served %q", got.Data)
	}
}

// THE FILE, THE FOLDER OR THE RECORD CHANGING UNDER THE READ IS AN ERROR,
// never the bytes it happened to hold.
func TestReadIssuedReportRefusesAChangeUnderTheRead(t *testing.T) {
	defer SetReportReadHookForTest(nil)
	body := []byte(`{"run":"A"}`)
	for name, check := range map[string]struct {
		change func(t *testing.T, dir string)
		want   error
	}{
		"file replaced": {func(t *testing.T, dir string) {
			writeReportFile(t, dir, "new", body)
			_ = os.Rename(filepath.Join(dir, "new"), filepath.Join(dir, "review-report.json"))
		}, ErrReportChanged},
		"file removed": {func(t *testing.T, dir string) { _ = os.Remove(filepath.Join(dir, "review-report.json")) }, ErrReportChanged},
		"relaunched": {func(t *testing.T, dir string) {
			_ = WriteProgram(dir, ProgramRecord{Name: "review", Launch: reportLaunchB, StartedAt: reportStarted})
		}, ErrReportMoved},
		"record gone": {func(t *testing.T, dir string) { _ = os.Remove(filepath.Join(dir, ProgramFile)) }, ErrReportMoved},
		"receipt rewritten": {func(t *testing.T, dir string) {
			record, _ := ReadProgram(dir)
			record.ReportReceipts = []ReportReceipt{IssueReportReceipt("review-report.json", []byte("other"))}
			_ = WriteProgram(dir, record)
		}, ErrReportChanged},
		// THE FOLDER SUBSTITUTED: the task folder renamed away and another real
		// folder holding a consistent record and file put in its place.
		"folder substituted": {func(t *testing.T, dir string) {
			_ = os.Rename(dir, dir+".old")
			_ = os.Mkdir(dir, 0o700)
			issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": body})
		}, ErrReportChanged},
		"folder swapped for a link": {func(t *testing.T, dir string) {
			_ = os.Rename(dir, dir+".old")
			_ = os.Symlink(dir+".old", dir)
		}, ErrReportChanged},
	} {
		dir := filepath.Join(t.TempDir(), "task")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": body})
		SetReportReadHookForTest(func() { check.change(t, dir) })
		got, err := readIssued(dir, reportLaunchA, "review-report.json")
		if !errors.Is(err, check.want) || got.Data != nil {
			t.Errorf("%s: %v, served %q", name, err, got.Data)
		}
	}
}

// A READ THAT IS CANCELLED OR PAST ITS DEADLINE ANSWERS THE CONTEXT'S ERROR,
// never an empty report.
func TestReadIssuedReportHonoursItsContext(t *testing.T) {
	dir := t.TempDir()
	issuedRun(t, dir, reportLaunchA, map[string][]byte{"review-report.json": bytes.Repeat([]byte("x"), 1<<20)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := ReadIssuedReport(ctx, dir, reportLaunchA, "review-report.json", declaresReview); !errors.Is(err, context.Canceled) || got.Data != nil {
		t.Fatalf("cancelled: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	defer SetReportReadHookForTest(nil)
	SetReportReadHookForTest(cancel)
	if got, err := ReadIssuedReport(ctx, dir, reportLaunchA, "review-report.json", declaresReview); got.Data != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mid-read served %d bytes (%v)", len(got.Data), err)
	}
}

func TestDeclaresReportNeedsAValidDeclaredName(t *testing.T) {
	program := Delegate{Reports: []string{"review-report.json", "../x", ".h"}}
	for name, want := range map[string]bool{"review-report.json": true, "other.json": false, "../x": false, ".h": false, "": false} {
		if program.DeclaresReport(name) != want {
			t.Errorf("%q: want %v", name, want)
		}
	}
}

// ONLY A WELL-FORMED RECEIPT FOR A DECLARED NAME IS KEPT from a terminal line,
// the first per name; anything else on the key issues nothing.
func TestIssuedReceiptsKeepsOnlyWellFormedDeclaredReceipts(t *testing.T) {
	program := Delegate{Reports: []string{"review-report.json", "other.json"}}
	good := IssueReportReceipt("review-report.json", []byte("{}"))
	line := func(value any) *Terminal {
		raw, _ := json.Marshal(value)
		return &Terminal{Status: StatusPass, Data: map[string]json.RawMessage{ReportReceiptsKey: raw}}
	}
	if got := program.IssuedReceipts(nil); got != nil {
		t.Fatalf("no line: %v", got)
	}
	if got := program.IssuedReceipts(&Terminal{}); got != nil {
		t.Fatalf("no key: %v", got)
	}
	if got := program.IssuedReceipts(line("not a list")); got != nil {
		t.Fatalf("not a list: %v", got)
	}
	upper := good
	upper.SHA256 = strings.ToUpper(good.SHA256)
	short := good
	short.SHA256 = good.SHA256[:10]
	negative := good
	negative.Bytes = -1
	second := good
	second.Bytes = 9
	got := program.IssuedReceipts(line([]ReportReceipt{
		{Name: "../x", Bytes: 2, SHA256: good.SHA256}, {Name: "undeclared.json", Bytes: 2, SHA256: good.SHA256},
		upper, short, negative, good, second,
	}))
	if len(got) != 1 || got[0] != good {
		t.Fatalf("kept %+v", got)
	}
	many := make([]ReportReceipt, reportReceiptsMax+1)
	for i := range many {
		many[i] = good
	}
	if got := program.IssuedReceipts(line(many)); got != nil {
		t.Fatalf("too many: %v", got)
	}
}

func TestOversizeIssuedReportRemainsKnownAndRefusesItsBytes(t *testing.T) {
	body := bytes.Repeat([]byte("x"), ReportMax+1)
	receipt := IssueReportReceipt("review-report.json", body)
	raw, err := json.Marshal([]ReportReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	program := Delegate{Reports: []string{"review-report.json"}}
	kept := program.IssuedReceipts(&Terminal{Status: StatusPass, Data: map[string]json.RawMessage{ReportReceiptsKey: raw}})
	if len(kept) != 1 || kept[0] != receipt {
		t.Fatalf("genuine large report lost its receipt: %+v", kept)
	}
	dir := t.TempDir()
	writeReportFile(t, dir, receipt.Name, body)
	record := ProgramRecord{Name: "review", Launch: reportLaunchA, StartedAt: reportStarted, EndedAt: reportStarted.Add(time.Minute), ReportReceipts: kept}
	if err := WriteProgram(dir, record); err != nil {
		t.Fatal(err)
	}
	reopened, ok := ReadProgram(dir)
	if !ok {
		t.Fatal("record not read")
	}
	if _, ok := reopened.Receipt(receipt.Name); !ok {
		t.Fatal("replay forgot genuine oversize report")
	}
	got, err := readIssued(dir, reportLaunchA, receipt.Name)
	if !errors.Is(err, ErrReportTooLarge) || got.Data != nil {
		t.Fatalf("oversize must be known but unread: bytes=%d err=%v", len(got.Data), err)
	}
}
