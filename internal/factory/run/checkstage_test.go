package run

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }

func checkJob(t *testing.T, ask string, log *logSink) Job {
	t.Helper()
	return Job{
		Item:  factory.Item{Repo: "codeaf"},
		Stage: factory.Stage{Name: "test", Kind: factory.StageKind("check"), Ask: ask, Until: factory.UntilGreen},
		Dir:   t.TempDir(),
		Log:   log.add,
	}
}

func TestCheckPass(t *testing.T) {
	var l logSink
	job := checkJob(t, "echo hello", &l)
	res, err := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Exit != 0 || res.Findings != 0 || !strings.Contains(res.Output, "hello") {
		t.Fatalf("bad result %+v", res)
	}
	c := res.Claims[0]
	if c.Text != "echo hello passes" || !c.OK || c.Medium != "test" || !strings.HasPrefix(c.Evidence, "exit 0 · ") {
		t.Fatalf("bad claim %+v", c)
	}
	if !factory.Met(job.Stage, res) {
		t.Fatal("until green not met")
	}
	if len(l.lines) != 2 || l.lines[0] != "check: echo hello" || !strings.HasPrefix(l.lines[1], "exit 0 · ") {
		t.Fatalf("log %v", l.lines)
	}
}

func TestCheckFailKeepsTail(t *testing.T) {
	var l logSink
	job := checkJob(t, "for i in $(seq 1 80); do echo line$i; done; echo boom >&2; exit 3", &l)
	res, _ := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if res.Done || res.Exit != 3 || res.Findings != 1 {
		t.Fatalf("bad result %+v", res)
	}
	lines := strings.Split(res.Output, "\n")
	if len(lines) != 50 || lines[49] != "boom" || lines[0] != "line32" {
		t.Fatalf("tail wrong: %d lines, first %q last %q", len(lines), lines[0], lines[len(lines)-1])
	}
	if res.Claims[0].OK || factory.Met(job.Stage, res) {
		t.Fatal("failure counted as green")
	}
}

func TestCheckTimeout(t *testing.T) {
	var l logSink
	job := checkJob(t, "echo start; sleep 30", &l)
	began := time.Now()
	res, err := NewCheckExecutor(CheckOptions{Timeout: 300 * time.Millisecond}).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > 10*time.Second {
		t.Fatal("timeout did not kill")
	}
	if res.Done || res.Exit == 0 || !strings.Contains(res.Output, "timed out") || res.Claims[0].OK {
		t.Fatalf("bad result %+v", res)
	}
}

func TestCheckCancelKillsChild(t *testing.T) {
	var l logSink
	pidfile := t.TempDir() + "/pid"
	job := checkJob(t, fmt.Sprintf("sleep 60 & echo $! > %s; wait", pidfile), &l)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewCheckExecutor(CheckOptions{}).Run(ctx, job)
		done <- err
	}()
	var pid int
	for i := 0; i < 100; i++ {
		b, _ := os.ReadFile(pidfile)
		if n, _ := fmt.Sscanf(string(b), "%d", &pid); n == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel returned no error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not return")
	}
	dead := false
	for i := 0; i < 50; i++ {
		// A killed child is reaped by init; signal 0 then fails.
		if err := killZero(pid); err != nil {
			dead = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !dead {
		t.Fatalf("child %d still alive", pid)
	}
}

func TestCheckCI(t *testing.T) {
	ex := NewCheckExecutor(CheckOptions{})
	run := func(it factory.Item) factory.StageResult {
		res, err := ex.Run(context.Background(), Job{Item: it, Stage: factory.Stage{Ask: "ci", Until: factory.UntilGreen}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	g := run(factory.Item{Checks: "ci ✓"})
	if !g.Done || g.Exit != 0 || !g.Claims[0].OK || g.Claims[0].Text != "ci is green" || g.Claims[0].Medium != "policy" {
		t.Fatalf("green %+v", g)
	}
	g = run(factory.Item{CheckRuns: []factory.CheckRun{{Name: "lint", State: "success"}, {Name: "unit", State: "success"}}})
	if !g.Done || g.Claims[0].Evidence != "lint, unit" {
		t.Fatalf("green runs %+v", g)
	}

	r := run(factory.Item{CheckRuns: []factory.CheckRun{{Name: "lint", State: "success"}, {Name: "unit", State: "in_progress"}}})
	if r.Done || r.Output != "ci is still running" {
		t.Fatalf("running %+v", r)
	}

	red := run(factory.Item{CheckRuns: []factory.CheckRun{{Name: "lint", State: "success"}, {Name: "unit", State: "failure"}}})
	if red.Done || red.Exit != 1 || red.Claims[0].OK || red.Claims[0].Evidence != "unit" {
		t.Fatalf("red %+v", red)
	}
}

func TestCheckUnknownDir(t *testing.T) {
	var l logSink
	job := checkJob(t, "echo hi", &l)
	job.Dir = ""
	res, _ := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if res.Done || res.Output != "codeaf does not know where codeaf is checked out" {
		t.Fatalf("bad result %+v", res)
	}
	if len(l.lines) != 0 {
		t.Fatalf("ran anyway: %v", l.lines)
	}
}

func TestCheckStripsKeys(t *testing.T) {
	t.Setenv("FAKE_API_KEY", "secret1")
	t.Setenv("FAKE_TOKEN", "secret2")
	t.Setenv("FAKE_KEEP", "visible")
	var l logSink
	job := checkJob(t, `echo "[$FAKE_API_KEY][$FAKE_TOKEN][$FAKE_KEEP]"`, &l)
	res, _ := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if strings.TrimSpace(res.Output) != "[][][visible]" {
		t.Fatalf("env leaked: %q", res.Output)
	}
}

func TestCheckIgnoresSteer(t *testing.T) {
	var l logSink
	job := checkJob(t, "true", &l)
	steer := make(chan string, 1)
	steer <- "stop it"
	job.Steer = steer
	res, _ := NewCheckExecutor(CheckOptions{}).Run(context.Background(), job)
	if !res.Done {
		t.Fatalf("%+v", res)
	}
}

func killZero(pid int) error { return syscall.Kill(pid, 0) }
