//go:build linux || darwin

package taskplace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
	"github.com/Agent-Field/codeaf/internal/taskplace"
)

// The helper is a real isolated child driven only by B's injected process
// seam. A transcript sentinel on A is never among its task-copy inputs.
func TestOtherHomeTaskChild(t *testing.T) {
	if os.Getenv("CODEAF_OTHER_HOME_CHILD") != "1" {
		return
	}
	value := func(flag string) string {
		for i, arg := range os.Args {
			if arg == flag && i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		return ""
	}
	work, record := value("--dir"), value("--task-store")
	if work == "" || record == "" {
		os.Exit(1)
	}
	var request taskdoor.Start
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(record), "request.json"))
	if err != nil || json.Unmarshal(raw, &request) != nil || request.Instruction != "settled instruction with acceptance and checks" || request.Chat != "chat-on-A" || request.Task != 1 || request.Model != "B/model" || request.Effort != "cheap" || request.MaxSteps != 17 || request.NoProgress != 9 || request.Limits.USD != .75 || request.Limits.Hours != .5 || request.OriginName != "chat computer" {
		os.Exit(2)
	}
	if value("--model") != "B/model" {
		os.Exit(6)
	}
	cheap := false
	for _, arg := range os.Args {
		if arg == "--cheap" {
			cheap = true
		}
	}
	if !cheap {
		os.Exit(7)
	}
	if _, err := os.Stat(filepath.Join(work, "conversation-only.txt")); !os.IsNotExist(err) {
		os.Exit(3)
	}
	dir := filepath.Join(record, "tasks", "1")
	_ = os.MkdirAll(dir, 0700)
	file, _ := os.Create(filepath.Join(dir, "trajectory.jsonl"))
	enc := json.NewEncoder(file)
	_ = enc.Encode(run.Step{Kind: "step", Step: 1, Command: "inspect on B", Observation: "first", RecordedOrder: 1, FullOutput: filepath.Join(record, "private-output")})
	_ = enc.Encode(run.Step{Kind: "step", Step: 2, Command: "write on B", Observation: "second", RecordedOrder: 2})
	_ = file.Close()
	// The child cannot finish until A has actually consumed both live steps.
	// Buffering the trajectory until finished would therefore fail this proof.
	gate := os.Getenv("CODEAF_OTHER_HOME_GATE")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(8)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(work, "made-on-B.txt"), []byte("B's child wrote this\n"), 0644); err != nil {
		os.Exit(4)
	}
	gitChild := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", work, "-c", "user.name=task test", "-c", "user.email=task@example.test"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(out))
			os.Exit(5)
		}
	}
	gitChild("add", "made-on-B.txt")
	gitChild("commit", "-qm", "change made on B")
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "answer": "the change is made on B", "model": "B/model", "files": []string{"made-on-B.txt"}})
	os.Exit(0)
}

// Contracts 1–3 use the real relay, authenticated door, child executor and
// encrypted copy transport. No remote engine or filesystem transfer is faked.
func TestATaskSentToTheOtherHomeRunsThereAndItsChangeComesBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	shortHome := func(prefix string) string {
		path, err := os.MkdirTemp("", prefix)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(path) })
		return path
	}
	homeA, homeB := shortHome("rn-l1-a-"), shortHome("rn-l1-b-")
	t.Setenv("HOME", homeA)
	t.Setenv("CODEAF_HOME", homeA)
	id, err := identity.Ensure(homeA)
	if err != nil {
		t.Fatal(err)
	}
	a, err := identity.LoadDevice(homeA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Install(homeB, id, b); err != nil {
		t.Fatal(err)
	}
	_, _ = devname.Set(homeB, "work computer")
	relayServer := httptest.NewServer(&relay.Server{})
	defer relayServer.Close()
	defer relayServer.CloseClientConnections()
	memory := blobstore.NewMemory()
	blobs := httptest.NewServer(blobstore.Handler(func(*http.Request, []byte) (string, string, error) { return id.ID(), a.ID(), nil }, func(string) (blobstore.Store, error) { return memory, nil }))
	defer blobs.Close()
	bin := taskEngineBinary(t)
	makeCopy := func(home string, device identity.Dev) taskdoor.CopyDeps {
		socketHome, err := os.MkdirTemp("", "rn-l1-")
		if err != nil {
			t.Fatal(err)
		}
		daemon := cellstore.Daemon{Binary: bin, Socket: filepath.Join(socketHome, "e.sock")}
		t.Cleanup(func() { _ = daemon.Stop(context.Background()); _ = os.RemoveAll(socketHome) })
		engine := cellstore.EngineFor("")
		engine.Binary = bin
		engine.Transport = daemon
		engine.DataRoot = filepath.Join(home, "stores")
		engine.Guard.Home = home
		return taskdoor.CopyDeps{Root: filepath.Join(home, "copies"), Service: relayServer.URL, Identity: id, Device: device, Engine: engine, Store: blobstore.NewHTTP(blobs.URL, func(*http.Request, []byte) {}, blobs.Client())}
	}
	copiesA, copiesB := makeCopy(homeA, a), makeCopy(homeB, b)
	executor := taskdoor.NewExecutor(taskdoor.ExecutorConfig{Copies: copiesB, Ledger: filepath.Join(homeB, "usage.jsonl"), Command: func(args []string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestOtherHomeTaskChild$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "CODEAF_OTHER_HOME_CHILD=1", "HOME="+homeB, "CODEAF_HOME="+homeB, "CODEAF_OTHER_HOME_GATE="+filepath.Join(homeB, "steps-arrived"))
		return cmd
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	serving := make(chan error, 1)
	ready := make(chan struct{}, 1)
	go func() {
		defer close(serving)
		serving <- taskdoor.Serve(ctx, taskdoor.Config{Service: relayServer.URL, Identity: id, Device: b, Home: homeB, Root: filepath.Join(homeB, "door"), Executor: executor, Roster: func(context.Context) (map[string]bool, error) {
			return map[string]bool{a.ID(): true, b.ID(): true}, nil
		}, Say: func(line string) {
			if line == taskdoor.TakingTasksLine {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}})
	}()
	defer func() {
		cancel()
		if err := <-serving; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-ready:
	case err := <-serving:
		t.Fatalf("serve: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	work := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", work, "-c", "user.name=task test", "-c", "user.email=task@example.test"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-qb", "task-copy")
	_ = os.WriteFile(filepath.Join(work, "input.txt"), []byte("input\n"), 0644)
	git("add", "input.txt")
	git("commit", "-qm", "input")
	_ = os.WriteFile(filepath.Join(homeA, "conversation-only.txt"), []byte("A's transcript stays here"), 0600)
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "task", "1", "change", "brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	remote := &taskplace.Engine{Copies: copiesA, Computer: session.TaskComputer{Device: b.ID(), Name: "work computer", CanTakeTasks: true}, OriginName: "chat computer"}
	var saved session.TaskRemoteRecord
	var commands []string
	summary := remote.Start(ctx, session.RunSpec{Store: store, Workspace: work, Conversation: "chat-on-A", TaskNumber: 1, Title: "change", Brief: "settled instruction with acceptance and checks", NamedModel: "B/model", NamedEffort: "cheap", StepsPerTask: 17, NoProgress: 9, CostUSD: .75, Elapsed: time.Minute * 30, SaveRemote: func(r session.TaskRemoteRecord, force bool) error { saved = r; return nil }, OnRemoteStep: func(seq uint64, raw json.RawMessage) error {
		if saved.Attempt == "" {
			t.Error("watch began before persistence")
		}
		var step run.Step
		if err := json.Unmarshal(raw, &step); err != nil {
			return err
		}
		if step.FullOutput != "" {
			t.Error("remote output path offered locally")
		}
		commands = append(commands, step.Command)
		if len(commands) == 2 {
			return os.WriteFile(filepath.Join(homeB, "steps-arrived"), []byte("A has both live steps"), 0600)
		}
		return nil
	}})
	if summary.Outcome != "done" || summary.Result != "the change is made on B" {
		t.Fatalf("summary = %+v", summary)
	}
	body, err := os.ReadFile(filepath.Join(work, "made-on-B.txt"))
	if err != nil || string(body) != "B's child wrote this\n" {
		t.Fatalf("returned file = %q, %v", body, err)
	}
	if got := git("log", "-1", "--format=%s"); got != "change made on B" {
		t.Fatalf("returned commit = %s", got)
	}
	if strings.Join(commands, ",") != "inspect on B,write on B" {
		t.Fatalf("steps = %v", commands)
	}
	if !saved.Restored || saved.Seq == 0 {
		t.Fatalf("record = %+v", saved)
	}
	if err := remote.Acknowledge(ctx, saved); err != nil {
		t.Fatal(err)
	}
}
