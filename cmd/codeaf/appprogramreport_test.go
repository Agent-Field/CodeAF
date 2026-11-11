//go:build unix

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf"
	"github.com/Agent-Field/codeaf/internal/secaf"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	reportLaunch  = "0123456789abcdef0123456789abcdef"
	reportEarlier = "fedcba9876543210fedcba9876543210"
)

// reportConversation is the task owner in these tests: it resolves a task id to
// a real record folder on disk and serves from it with the engine's own read
// (session.ReadAppProgramReport), so the bytes the wire carries are what a
// producer left in a file. enter, when set, is called inside the read, holding
// it where a test wants.
type reportConversation struct {
	*fakeConversation
	tasks    map[string]string
	programs []delegate.Delegate
	enter    func(ctx context.Context)
	// plain leaves the conversation as the fake is, which serves no report.
	plain bool
}

func (c *reportConversation) AppProgramReport(ctx context.Context, id, launch, name string) (session.AppProgramReport, bool, error) {
	folder, ok := c.tasks[id]
	if !ok {
		return session.AppProgramReport{}, false, nil
	}
	if c.enter != nil {
		c.enter(ctx)
	}
	return session.ReadAppProgramReport(ctx, folder, c.programs, launch, name)
}

// reportTask makes a record folder for a finished run of program with the record the producer writes.
func reportTask(t *testing.T, program string, ended bool) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "tasks", "t")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	record := delegate.ProgramRecord{Name: program, Launch: reportLaunch, StartedAt: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	if ended {
		record.EndedAt = record.StartedAt.Add(time.Hour)
	}
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	return folder
}

// issueReport is the producer and its worker for one report: the file written
// in the folder, and its receipt kept on the launch's record. A nil body keeps
// a receipt for the empty file and writes nothing, for a test that plants
// something else under the name.
func issueReport(t *testing.T, folder, name string, body []byte) {
	t.Helper()
	if body != nil {
		if err := os.WriteFile(filepath.Join(folder, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	record, ok := delegate.ReadProgram(folder)
	if !ok {
		t.Fatal("no record")
	}
	record.ReportReceipts = append(record.ReportReceipts, delegate.IssueReportReceipt(name, body))
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
}

type reportWire struct {
	t       *testing.T
	in      io.Writer
	scanner *bufio.Scanner
	mu      sync.Mutex
	n       int
	pending map[int]map[string]any
	backend *appBackend
	agent   string
}

func newReportWire(t *testing.T, conv *reportConversation) *reportWire {
	t.Helper()
	var mu sync.Mutex
	var opens, releases int
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		conn, err := fakeOpener(conv.fakeConversation, &opens, &releases, &mu)(ask)
		if conn != nil && !conv.plain {
			conn.Conv = conv
		}
		return conn, err
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	w := &reportWire{t: t, in: inW, scanner: bufio.NewScanner(outR), pending: map[int]map[string]any{}, backend: backend, agent: agent.ID}
	w.scanner.Buffer(make([]byte, 8<<20), 8<<20)
	if !w.scanner.Scan() {
		t.Fatal("no hello")
	}
	return w
}

// send writes one tasks.program_report request and answers its number.
func (w *reportWire) send(task, launch, name string) int {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n++
	params, _ := json.Marshal(map[string]string{"id": w.agent + ":" + task, "launch": launch, "name": name})
	if _, err := fmt.Fprintf(w.in, `{"id":%d,"method":"tasks.program_report","params":%s}`+"\n", w.n, params); err != nil {
		w.t.Fatal(err)
	}
	return w.n
}

// answer waits for the answer to request n.
func (w *reportWire) answer(n int) (map[string]any, map[string]any) {
	w.t.Helper()
	for {
		if frame, ok := w.pending[n]; ok {
			delete(w.pending, n)
			result, _ := frame["result"].(map[string]any)
			failed, _ := frame["error"].(map[string]any)
			return result, failed
		}
		if !w.scanner.Scan() {
			w.t.Fatal("the wire ended")
		}
		var frame map[string]any
		if json.Unmarshal(w.scanner.Bytes(), &frame) != nil {
			w.t.Fatalf("bad frame %q", w.scanner.Text())
		}
		if id, ok := frame["id"].(float64); ok {
			w.pending[int(id)] = frame
		}
	}
}

func (w *reportWire) ask(task, launch, name string) (map[string]any, map[string]any) {
	return w.answer(w.send(task, launch, name))
}

func decodedReport(t *testing.T, result map[string]any) []byte {
	t.Helper()
	text, ok := result["data"].(string)
	if !ok {
		t.Fatalf("data is %T, not a string: %v", result["data"], result)
	}
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func reportConv(tasks map[string]string, programs ...delegate.Delegate) *reportConversation {
	return &reportConversation{fakeConversation: &fakeConversation{}, tasks: tasks, programs: programs}
}

// THROUGH THE REAL SERVER, BACKEND AND A RECORD FOLDER ON DISK: the exact bytes
// of each real producer's declared file arrive as base64 text — invalid UTF-8
// and a byte-order mark included — with their length, digest and identity.
func TestTasksProgramReportServesExactBytesOverTheRealServer(t *testing.T) {
	for name, body := range map[string][]byte{
		"valid":   []byte(`{"program":"review"}` + "\n"),
		"invalid": []byte("{\"x\":\"\xff\xfe\xc3\x28\"}"),
		"bom":     append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"a":1}`)...),
		"exactly": bytes.Repeat([]byte("x"), appwire.ProgramReportMax),
	} {
		folder := reportTask(t, "review", true)
		issueReport(t, folder, "review-report.json", body)
		wire := newReportWire(t, reportConv(map[string]string{"t": folder}, praf.Program))
		result, failed := wire.ask("t", reportLaunch, "review-report.json")
		if failed != nil {
			t.Fatalf("%s: %v", name, failed)
		}
		sum := sha256.Sum256(body)
		if got := decodedReport(t, result); !bytes.Equal(got, body) {
			t.Fatalf("%s: bytes differ (%d vs %d)", name, len(got), len(body))
		}
		if result["bytes"] != float64(len(body)) || result["sha256"] != hex.EncodeToString(sum[:]) || result["launch"] != reportLaunch ||
			result["program"] != "review" || result["name"] != "review-report.json" || result["id"] != wire.agent+":t" ||
			result["started_at"] == nil || result["ended_at"] == nil {
			t.Fatalf("%s: %v", name, result)
		}
	}
	// The other real producer declares its own file, and each serves only its own.
	folder := reportTask(t, "sec", true)
	issueReport(t, folder, "sec-report.json", []byte("{}"))
	wire := newReportWire(t, reportConv(map[string]string{"t": folder}, praf.Program, secaf.Program))
	if _, failed := wire.ask("t", reportLaunch, "sec-report.json"); failed != nil {
		t.Fatalf("sec: %v", failed)
	}
	if _, failed := wire.ask("t", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeBadParams {
		t.Fatalf("a review report on a sec task answered %v", failed)
	}
}

// EVERY REFUSAL IS AN ERROR WITH ITS OWN WORD, never an empty or a clean answer.
func TestTasksProgramReportRefusalsOverTheRealServer(t *testing.T) {
	folder := reportTask(t, "review", true)
	write := func(name string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(folder, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	issueReport(t, folder, "review-report.json", []byte("{}"))
	write("review-report.md", []byte("# report"))
	running := reportTask(t, "review", false)
	issueReport(t, running, "review-report.json", []byte("{}"))
	conv := reportConv(map[string]string{"t": folder, "running": running}, praf.Program)
	wire := newReportWire(t, conv)
	for _, tc := range []struct {
		name, task, launch, file, code string
	}{
		{"a rerun's earlier launch", "t", reportEarlier, "review-report.json", appwire.CodeConflict},
		{"a foreign task", "other", reportLaunch, "review-report.json", appwire.CodeNotFound},
		{"a file the program did not declare", "t", reportLaunch, "review-report.md", appwire.CodeBadParams},
		{"a traversal", "t", reportLaunch, "../t/review-report.json", appwire.CodeBadParams},
		{"an absolute path", "t", reportLaunch, filepath.Join(folder, "review-report.json"), appwire.CodeBadParams},
		{"a malformed launch", "t", "nope", "review-report.json", appwire.CodeBadParams},
		{"a run that has not ended", "running", reportLaunch, "review-report.json", appwire.CodeNotFound},
	} {
		if result, failed := wire.ask(tc.task, tc.launch, tc.file); failed == nil || failed["code"] != tc.code || result != nil {
			t.Errorf("%s: answered %v / %v, want %s", tc.name, result, failed, tc.code)
		}
	}
	// A task id that is not agent:task.
	if _, failed := wire.answer(wire.sendRaw(`{"id":"nocolon","launch":"` + reportLaunch + `","name":"review-report.json"}`)); failed == nil || failed["code"] != appwire.CodeBadParams {
		t.Errorf("a bad task id answered %v", failed)
	}
}

// An engine whose conversation does not serve reports (a far one) says so as unknown_method, never "no report".
func TestTasksProgramReportIsUnknownWhereTheConversationCannotServeIt(t *testing.T) {
	wire := newReportWire(t, &reportConversation{fakeConversation: &fakeConversation{}, plain: true})
	if result, failed := wire.ask("t", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeUnknownMethod || result != nil {
		t.Fatalf("answered %v / %v", result, failed)
	}
}

func (w *reportWire) sendRaw(params string) int {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n++
	if _, err := fmt.Fprintf(w.in, `{"id":%d,"method":"tasks.program_report","params":%s}`+"\n", w.n, params); err != nil {
		w.t.Fatal(err)
	}
	return w.n
}

// A FILE THAT CANNOT BE SERVED SAFELY IS REFUSED: a link out of the folder, a
// link inside it, a pipe where the report goes (which must not hold the call),
// 2 MiB and one byte, and a linked record folder.
func TestTasksProgramReportRefusesUnsafeAndOversizeFilesOverTheRealServer(t *testing.T) {
	folder := reportTask(t, "review", true)
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "review-report.json")
	// The launch issued the empty file under the name, so every refusal below
	// is the file's own and not a missing receipt's.
	issueReport(t, folder, "review-report.json", nil)
	conv := reportConv(map[string]string{"t": folder}, praf.Program)
	wire := newReportWire(t, conv)

	check := func(label, code string) {
		t.Helper()
		done := make(chan struct{})
		var result, failed map[string]any
		go func() { defer close(done); result, failed = wire.ask("t", reportLaunch, "review-report.json") }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the call hung", label)
		}
		if failed == nil || failed["code"] != code || result != nil {
			t.Fatalf("%s: answered %v / %v, want %s", label, result, failed, code)
		}
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	check("a link out of the folder", appwire.CodeEngine)
	_ = os.Remove(path)
	if err := os.WriteFile(filepath.Join(folder, "real.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.json", path); err != nil {
		t.Fatal(err)
	}
	check("a link inside the folder", appwire.CodeEngine)
	_ = os.Remove(path)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	check("a pipe", appwire.CodeEngine)
	_ = os.Remove(path)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), appwire.ProgramReportMax+1), 0o600); err != nil {
		t.Fatal(err)
	}
	check("2 MiB and a byte", appwire.CodeTooLarge)
	// A FILE WITH A SECOND NAME, even holding the receipted bytes.
	_ = os.Remove(path)
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, path); err != nil {
		t.Fatal(err)
	}
	check("a hard link", appwire.CodeEngine)
	_ = os.Remove(path)
	check("a file that is not there", appwire.CodeNotFound)
	// A record folder that is itself a link.
	real := reportTask(t, "review", true)
	issueReport(t, real, "review-report.json", []byte("{}"))
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	conv.tasks["t"] = link
	check("a linked record folder", appwire.CodeEngine)
}

// THE ADMISSION IS HARD AND HELD UNTIL THE CALL RETURNS: two reads in flight
// refuse a third at once as busy (no queue, nothing parked), and the slot comes
// back only when a held read has finished.
func TestTasksProgramReportAdmissionIsTwoHeldUntilSettled(t *testing.T) {
	folder := reportTask(t, "review", true)
	issueReport(t, folder, "review-report.json", []byte("{}"))
	conv := reportConv(map[string]string{"t": folder}, praf.Program)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	conv.enter = func(ctx context.Context) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	wire := newReportWire(t, conv)
	first := wire.send("t", reportLaunch, "review-report.json")
	second := wire.send("t", reportLaunch, "review-report.json")
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("two reads were not admitted")
		}
	}
	result, failed := wire.ask("t", reportLaunch, "review-report.json")
	if failed == nil || failed["code"] != appwire.CodeBusy || result != nil {
		t.Fatalf("a third read answered %v / %v", result, failed)
	}
	select {
	case <-entered:
		t.Fatal("a refused read entered the owner")
	default:
	}
	close(release)
	for _, n := range []int{first, second} {
		if result, failed := wire.answer(n); failed != nil || result["bytes"] != float64(2) {
			t.Fatalf("held read %d answered %v / %v", n, result, failed)
		}
	}
	conv.enter = nil
	if result, failed := wire.ask("t", reportLaunch, "review-report.json"); failed != nil || result["bytes"] != float64(2) {
		t.Fatalf("a read after the slots settled answered %v / %v", result, failed)
	}
	wire.backend.locked(func() {
		if wire.backend.reportReads != 0 {
			t.Fatalf("%d slots still held", wire.backend.reportReads)
		}
	})
}

// A READ PAST ITS DEADLINE IS BUSY, NEVER EMPTY OR PARTIAL, and gives its slot back.
func TestTasksProgramReportDeadlineIsBusyAndReleasesTheSlot(t *testing.T) {
	folder := reportTask(t, "review", true)
	issueReport(t, folder, "review-report.json", []byte("{}"))
	conv := reportConv(map[string]string{"t": folder}, praf.Program)
	conv.enter = func(ctx context.Context) { <-ctx.Done() }
	was := appProgramReportTime
	appProgramReportTime = 50 * time.Millisecond
	t.Cleanup(func() { appProgramReportTime = was })
	wire := newReportWire(t, conv)
	if result, failed := wire.ask("t", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeBusy || result != nil {
		t.Fatalf("a read past its deadline answered %v / %v", result, failed)
	}
	wire.backend.locked(func() {
		if wire.backend.reportReads != 0 {
			t.Fatalf("%d slots still held", wire.backend.reportReads)
		}
	})
}

// STOP SETTLES: closing the backend while a read is held cancels it, the call
// answers closed (not a report), the slot is released, and nothing new is admitted.
func TestTasksProgramReportCloseSettlesAHeldRead(t *testing.T) {
	folder := reportTask(t, "review", true)
	issueReport(t, folder, "review-report.json", []byte("{}"))
	conv := reportConv(map[string]string{"t": folder}, praf.Program)
	entered := make(chan struct{}, 1)
	conv.enter = func(ctx context.Context) {
		entered <- struct{}{}
		<-ctx.Done()
	}
	wire := newReportWire(t, conv)
	n := wire.send("t", reportLaunch, "review-report.json")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the read was not admitted")
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = wire.backend.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not settle with a read held")
	}
	if result, failed := wire.answer(n); failed == nil || failed["code"] != appwire.CodeClosed || result != nil {
		t.Fatalf("the held read answered %v / %v", result, failed)
	}
	wire.backend.locked(func() {
		if wire.backend.reportReads != 0 {
			t.Fatalf("%d slots still held after close", wire.backend.reportReads)
		}
	})
	if _, _, err := wire.backend.reserveProgramReport(); err == nil || !strings.Contains(err.Error(), appwire.CodeClosed) {
		t.Fatalf("a closed backend admitted a read: %v", err)
	}
}

// THE WIRE'S TASK PROGRAM NAMES THE LAUNCH AND THE ISSUED FILES, and only in the shapes a request accepts.
func TestTaskProgramCarriesTheLaunchAndTheDeclaredReportFiles(t *testing.T) {
	view := &session.AppProgramView{Name: "review", Read: session.AppProgramReadComplete, Launch: reportLaunch,
		ReportFiles: []string{"review-report.json", "../x", "a b"}}
	out := appTaskProgram(view)
	if out.Launch != reportLaunch || len(out.ReportFiles) != 1 || out.ReportFiles[0] != "review-report.json" {
		t.Fatalf("%+v", out)
	}
	view.Launch = "short"
	if out := appTaskProgram(view); out.Launch != "" || out.ReportFiles != nil {
		t.Fatalf("an invalid launch crossed: %+v", out)
	}
	view = &session.AppProgramView{Name: "review", Launch: reportLaunch, ReportFiles: []string{"review-report.json"}}
	if out := appTaskProgram(view); out.Launch != "" || out.ReportFiles != nil {
		t.Fatalf("a far engine with no read state kept a launch: %+v", out)
	}
}

// NO RECEIPT OF THIS LAUNCH, NO REPORT, OVER THE REAL SERVER. A failed rerun
// over an earlier run's file, a record from before receipts, and a file
// rewritten in place after the run issued its receipt (the same inode, size
// and mtime) are each refused, never served.
func TestTasksProgramReportServesOnlyWhatTheLaunchIssued(t *testing.T) {
	folder := reportTask(t, "review", true)
	issueReport(t, folder, "review-report.json", []byte(`{"blocking":1}`))
	legacy := reportTask(t, "review", true)
	if err := os.WriteFile(filepath.Join(legacy, "review-report.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	wire := newReportWire(t, reportConv(map[string]string{"t": folder, "legacy": legacy}, praf.Program))
	if result, failed := wire.ask("legacy", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeNotFound || result != nil {
		t.Fatalf("a record from before receipts answered %v / %v", result, failed)
	}
	// Rewritten in place, its clock put back.
	path := filepath.Join(folder, "review-report.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte(`{"blocking":0}`), 0); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if result, failed := wire.ask("t", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeConflict || result != nil {
		t.Fatalf("a rewrite in place answered %v / %v", result, failed)
	}
	// The run is handed its task again and fails before it writes.
	if err := delegate.WriteProgram(folder, delegate.ProgramRecord{Name: "review", Launch: reportEarlier,
		StartedAt: time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC), EndedAt: time.Date(2026, 10, 16, 11, 1, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if result, failed := wire.ask("t", reportEarlier, "review-report.json"); failed == nil || failed["code"] != appwire.CodeNotFound || result != nil {
		t.Fatalf("a failed rerun answered %v / %v", result, failed)
	}
	if result, failed := wire.ask("t", reportLaunch, "review-report.json"); failed == nil || failed["code"] != appwire.CodeConflict || result != nil {
		t.Fatalf("the earlier launch after the rerun answered %v / %v", result, failed)
	}
}
