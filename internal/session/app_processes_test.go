package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestAppProcessOutputCapKeepsNewestLinesAndWriterLease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	sink := newJobSink(file, path)
	sink.appWindow = true
	sink.chunkBytes = AppProcessOutputBytes
	defer sink.close()
	original, _ := file.Stat()
	line := []byte(strings.Repeat("x", 1023) + "\n")
	if _, err := sink.Write(bytes.Repeat(line, 12<<10)); err != nil {
		t.Fatal(err)
	}
	_, _ = sink.Write([]byte("first server localhost:4321\n"))
	_, _ = sink.Write([]byte("other server localhost:5000\n"))
	_, _ = sink.Write([]byte("the last line\n"))
	if sink.processURL() != "http://localhost:4321" {
		t.Fatalf("first URL: %q", sink.processURL())
	}
	info, _ := file.Stat()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > AppProcessOutputBytes || !os.SameFile(original, info) || !bytes.HasSuffix(raw, []byte("the last line\n")) {
		t.Fatalf("cap=%d inode=%v suffix=%v", info.Size(), os.SameFile(original, info), bytes.HasSuffix(raw, []byte("the last line\n")))
	}
	if len(raw) > 0 && raw[0] != 'x' {
		t.Fatal("compaction left partial line")
	}
	if !strings.Contains(sink.notice(), "truncated") {
		t.Fatal("lost output was not disclosed")
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("app output allocated a second log")
	}
	if len(sink.tail(20)) > jobRingBytes {
		t.Fatal("model output ring grew")
	}
}
func TestAppProcessOutputSevenDaysKeepsActiveAndShelf(t *testing.T) {
	defer config.UseAppCredentials(config.NewAppCredentials())()
	dir := t.TempDir()
	registry := newJobRegistry(dir, Place{Dir: dir}, nil)
	active, err := registry.newJob("active", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	defer active.sink.close()
	ended, err := registry.newJob("ended", jobKindBash)
	if err != nil {
		t.Fatal(err)
	}
	ended.sink.close()
	registry.joinRetention()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	old := now.Add(-AppProcessOutputRetention - time.Second)
	for _, p := range []string{active.logPath, ended.logPath} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	legacy := filepath.Join(filepath.Dir(active.logPath), "999.log")
	_ = os.WriteFile(legacy, []byte("older writer"), 0600)
	_ = os.Chtimes(legacy, old, old)
	shelf := filepath.Join(dir, "processes.json")
	_ = os.WriteFile(shelf, []byte("[]"), 0600)
	if err := SweepProcessOutput(filepath.Join(dir, "transcript.jsonl"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ended.logPath); !os.IsNotExist(err) {
		t.Fatal("expired ended output kept")
	}
	for _, p := range []string{active.logPath, legacy, shelf} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expired protected %s: %v", p, err)
		}
	}
	if budget := defaultJobRetentionBudget(); budget.groups == 64 || budget.bytes == 128<<20 {
		t.Fatal("terminal directory budget shortened the app's seven-day promise")
	}
}
func TestAppProcessesStartStopRerunAndRecover(t *testing.T) {
	defer config.UseAppCredentials(config.NewAppCredentials())()
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	lane := rosterLane(t, agent)
	id := startJob(t, agent, "printf 'process output\\n'")
	awaitJob(t, lane, JobDone)
	rows, err := agent.JobNotices()
	if err != nil {
		t.Fatal(err)
	}
	var first JobNotice
	for _, n := range rows {
		if n.ID == id {
			first = n
		}
	}
	if first.Command == "" || first.State != JobDone || first.ExitCode != 0 {
		t.Fatalf("process: %+v", first)
	}
	rerun, err := agent.RerunProcess(id)
	if err != nil {
		t.Fatal(err)
	}
	if rerun.ID == id || rerun.Command != first.Command {
		t.Fatalf("rerun: %+v", rerun)
	}
	awaitJob(t, lane, JobDone)
	live := startJob(t, agent, "sleep 60")
	if _, err := agent.RerunProcess(live); err == nil {
		t.Fatal("reran a live process")
	}
	if _, err := agent.Cancel("job:" + strconv.Itoa(live)); err != nil {
		t.Fatal(err)
	}
	awaitJob(t, lane, JobStopped)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	saved, err := ReadProcessNotices(agent.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) < 3 {
		t.Fatalf("lost process shelf: %v", saved)
	}
	for _, n := range saved {
		if n.ID == id && n.Command != first.Command {
			t.Fatal("lost rerun command")
		}
	}
}
func TestAppProcessShelfMarksInterruptedRowsStopped(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	path := filepath.Join(dir, "processes.json")
	n := JobNotice{ID: 72, Command: "npm run dev", Kind: JobKindCommand, State: JobRunning, Started: time.Now()}
	raw, _ := json.Marshal([]JobNotice{n})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := agent.JobNotices()
	if err != nil || len(rows) != 1 || rows[0].State != JobStopped {
		t.Fatalf("recovery: %+v %v", rows, err)
	}
}

func TestAppProcessOutputDropsWholeLongLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	sink := newJobSink(file, path)
	sink.appWindow = true
	sink.chunkBytes = 128 << 10
	defer sink.close()
	raw := append(bytes.Repeat([]byte("a"), 100<<10), '\n')
	raw = append(raw, append(bytes.Repeat([]byte("b"), 100<<10), '\n')...)
	_, _ = sink.Write(raw)
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > 128<<10 || bytes.Contains(output, []byte("a")) || !bytes.HasSuffix(output, []byte("b\n")) {
		t.Fatalf("long line was cut instead of dropped: bytes=%d", len(output))
	}
}

func TestAppProcessCloseFencesLateRecords(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
	id := startJob(t, agent, "sleep 60")
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, processStoreName, processFileName(id))
	before, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadProcessNotices(agent.SessionPath())
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].State != JobStopped {
		t.Fatalf("close shelf: %+v %v", rows, err)
	}
	agent.keepProcessNotice(JobNotice{ID: 999, State: JobRunning, Kind: JobKindCommand, Command: "late callback"})
	after, err := os.ReadFile(record)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("late record wrote after Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, processStoreName, processFileName(999))); !os.IsNotExist(err) {
		t.Fatalf("late record created a file after Close: %v", err)
	}
}
