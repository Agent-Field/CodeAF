//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

const otherHomeModel = "stub/paired-task"
const otherHomeFile = "made-on-other-home.txt"
const otherHomeContents = "the paired worker wrote this\n"
const otherHomeCommit = "write the paired task result"
const otherHomeBrief = "Write made-on-other-home.txt containing exactly the paired worker wrote this followed by a newline, commit it with subject write the paired task result, and finish."
const otherHomeCallCost = 0.0125

// DESIGN 1–5: the connection invokes the session entry used by /task --on.
// Only the two providers are scripted; both engines, the child, encrypted
// transport, plan store, ledger and landing are the shipped binary's own.
func TestATaskRunsOnTheOtherHomeAndLandsHere(t *testing.T) {
	began := time.Now()
	f := newOtherHomeTask(t)
	f.brainB.gate = make(chan struct{})
	f.startServing(t)
	a := f.openConversation(t, false)
	f.say(t, a, "Remember ORIGIN_TRANSCRIPT_ONLY and reply CHAT_STAYS_HERE.")
	id := f.startTask(t, a)
	// The worker cannot receive its finishing call until its first step is
	// readable here, so an implementation that buffers until finish fails.
	otherHomeWait(t, f.ctx, "the first step while the other worker is running", func() bool {
		page, ok, err := a.ReadPlanTaskPage(strconv.FormatUint(id, 10))
		if err != nil || !ok {
			return false
		}
		for _, step := range page.Steps {
			if strings.Contains(step.Command, "TASK_INSPECTED_HERE") {
				return true
			}
		}
		return false
	})
	f.waitNotice(t, a, id, func(n session.TaskNotice) bool {
		return n.Computer == "work computer" && n.State == session.TaskRunning && !n.RanElsewhere
	})
	close(f.brainB.gate)
	f.waitDone(t, a, id)
	// R9.1 requires the finished A-side reading to carry the placement words,
	// rather than merely retaining an executor field that no row can draw.
	f.waitNotice(t, a, id, func(n session.TaskNotice) bool {
		return n.Computer == "work computer" && n.RanElsewhere && n.Doing == "ran on work computer"
	})

	t.Run("model_calls_reach_only_the_other_home", func(t *testing.T) {
		// Contract 1 distinguishes actual worker prompts from naming and chat calls.
		ac, aw := f.brainA.counts()
		bc, bw := f.brainB.counts()
		if ac == 0 || aw != 0 || bc == 0 || bw < 2 {
			t.Fatalf("provider calls: here=%d (worker=%d), there=%d (worker=%d)", ac, aw, bc, bw)
		}
	})
	t.Run("the_file_lands_in_this_project", func(t *testing.T) {
		// Contract 3 includes the worker's commit and excludes the chat's record.
		f.assertFile(t)
		if got := f.git(t, "log", "--format=%s"); !strings.Contains("\n"+got+"\n", "\n"+otherHomeCommit+"\n") {
			t.Fatalf("the worker's commit is absent from the project history: %s", got)
		}
		transcript := a.Client().Welcome().SessionFile
		raw, err := os.ReadFile(transcript)
		if err != nil || !strings.Contains(string(raw), "ORIGIN_TRANSCRIPT_ONLY") || !strings.HasPrefix(transcript, f.a+string(os.PathSeparator)) {
			t.Fatalf("origin transcript = %q, %v", transcript, err)
		}
		err = filepath.WalkDir(f.b, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !(strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".md")) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), "ORIGIN_TRANSCRIPT_ONLY") || entry.Name() == filepath.Base(transcript) {
				t.Errorf("the conversation travelled to %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the_steps_arrived_in_order", func(t *testing.T) {
		// Contract 2 reads A's ordinary task page, backed by its plan trajectory.
		page, ok, err := a.ReadPlanTaskPage(strconv.FormatUint(id, 10))
		if err != nil || !ok {
			t.Fatalf("task page: %v, %v", ok, err)
		}
		first, second := -1, -1
		for i, step := range page.Steps {
			if strings.Contains(step.Command, "TASK_INSPECTED_HERE") {
				first = i
			}
			if strings.Contains(step.Command, "git add -- "+otherHomeFile) {
				second = i
			}
		}
		if first < 0 || second <= first {
			t.Fatalf("worker steps missing or reordered: %+v", page.Steps)
		}
	})
	t.Run("spend_is_on_this_ledger_once", func(t *testing.T) {
		// Contract 5 compares the mirrored calls with B's own per-call charges.
		aLines := otherHomeLedger(t, f.a)
		bLines := otherHomeLedger(t, f.b)
		seen := map[string]bool{}
		transcript, err := os.Open(a.Client().Welcome().SessionFile)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			ID string `json:"id"`
		}
		err = json.NewDecoder(transcript).Decode(&header)
		_ = transcript.Close()
		if err != nil || header.ID == "" {
			t.Fatalf("conversation header: %+v, %v", header, err)
		}
		var here, there float64
		count := 0
		for _, line := range aLines {
			if line.RemoteEvent == "" {
				continue
			}
			if seen[line.RemoteEvent] || line.Computer != "work computer" || line.Task != strconv.FormatUint(id, 10) || line.Session != header.ID {
				t.Fatalf("duplicate or unattributed remote charge: %+v", line)
			}
			seen[line.RemoteEvent] = true
			count++
			here += line.USD
		}
		for _, line := range bLines {
			there += line.USD
		}
		bc, _ := f.brainB.counts()
		if count != bc || math.Abs(here-there) > 1e-9 || math.Abs(here-float64(bc)*otherHomeCallCost) > 1e-9 {
			t.Fatalf("charges here=%d/$%f, there=$%f, provider calls=%d", count, here, there, bc)
		}
		page, err := a.Client().Ledger(time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var pageUSD float64
		pageCalls := 0
		for _, line := range page.Lines {
			if line.RemoteEvent != "" {
				pageUSD += line.USD
				pageCalls += line.Calls
			}
		}
		if pageCalls != count || math.Abs(pageUSD-here) > 1e-9 {
			t.Fatalf("spend page calls=%d/$%f, ledger calls=%d/$%f", pageCalls, pageUSD, count, here)
		}
	})
	t.Run("private_copies_wait_for_review_and_apply", func(t *testing.T) {
		// Contract 4 takes another real remote result through the person-only doors.
		if err := os.Remove(filepath.Join(f.project, otherHomeFile)); err != nil {
			t.Fatal(err)
		}
		f.git(t, "add", "--", otherHomeFile)
		f.git(t, "commit", "-qm", "clear the previous result")
		private := f.openConversation(t, true)
		privateID := f.startTask(t, private)
		f.waitDone(t, private, privateID)
		if _, err := os.Stat(filepath.Join(f.project, otherHomeFile)); !os.IsNotExist(err) {
			t.Fatalf("private result landed before apply: %v", err)
		}
		var review session.TaskWorkspaceReview
		// The done notice precedes the final store close; Review itself is the
		// public readiness check and must keep refusing while that join lives.
		otherHomeWait(t, f.ctx, "the private result to be ready for review", func() bool {
			var err error
			review, err = private.ReviewTaskWorkspaceRoot(f.ctx, privateID, "work")
			if err == nil {
				return true
			}
			if err.Error() != "this workspace layout may still have a writer or checker; its private work was retained" {
				t.Fatal(err)
			}
			return false
		})
		if !strings.Contains(review.Diff, "+the paired worker wrote this") || review.Snapshot == "" {
			t.Fatalf("review: %+v", review)
		}
		// A person's later edit must not change the snapshot Apply installs.
		if err := os.WriteFile(filepath.Join(review.Copy, otherHomeFile), []byte("unreviewed later edit\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := private.ApplyTaskWorkspaceRoot(f.ctx, privateID, "work"); err != nil {
			t.Fatal(err)
		}
		f.assertFile(t)
	})
	t.Logf("two-home real-binary wall time: %s", time.Since(began).Round(time.Millisecond))
}

// DESIGN 8: the exact failed-start sentence and explicit local replacement
// cross the same connection doors as the card's run-it-here action.
func TestATaskForAComputerNotServingSaysSoAndRunsHere(t *testing.T) {
	f := newOtherHomeTask(t)
	stop := f.startServing(t)
	stop()
	a := f.openConversation(t, false)
	id := f.startTask(t, a)
	want := "work computer is not taking tasks right now — run codeaf serve --take-tasks on it"
	f.waitNotice(t, a, id, func(notice session.TaskNotice) bool {
		return notice.Remote != nil && notice.Remote.StartFailed && strings.Contains(notice.Report, want)
	})
	if _, err := a.RunTaskHere(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	f.waitDone(t, a, id)
	f.assertFile(t)
	if _, workers := f.brainA.counts(); workers < 2 {
		t.Fatal("local replacement did not use this home's worker")
	}
	if calls, _ := f.brainB.counts(); calls != 0 {
		t.Fatalf("stopped home received %d calls", calls)
	}
}

// S5: a real paired home running plain serve never opts into receiving tasks.
func TestPlainServeTakesNoTasks(t *testing.T) {
	f := newOtherHomeTask(t)
	f.startServe(t, false)
	a := f.openConversation(t, false)
	id := f.startTask(t, a)
	f.waitNotice(t, a, id, func(n session.TaskNotice) bool {
		return n.Remote != nil && n.Remote.StartFailed && strings.Contains(n.Report, taskdoor.NotTakingTasks("work computer"))
	})
	if calls, _ := f.brainB.counts(); calls != 0 {
		t.Fatalf("plain serve ran a task: provider calls=%d", calls)
	}
	if _, err := os.Stat(filepath.Join(f.b, "v3", "taskdoor")); !os.IsNotExist(err) {
		t.Fatalf("plain serve opened task storage: %v", err)
	}
}

type otherHomeTask struct {
	ctx                           context.Context
	a, b, project, relay, product string
	brainA, brainB                *otherHomeBrain
}

func newOtherHomeTask(t *testing.T) *otherHomeTask {
	t.Helper()
	product := binary(t)
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("paired tasks need Linux or macOS on both computers")
	}
	short := func(prefix string) string {
		dir, err := os.MkdirTemp("/tmp", prefix)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	f := &otherHomeTask{a: short("rn-l1-a"), b: short("rn-l1-b"), product: product}
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	t.Setenv("HOME", f.a)
	t.Setenv("CODEAF_HOME", f.a)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	f.brainA, f.brainB = newOtherHomeBrain(t, "synthetic-home-a-key"), newOtherHomeBrain(t, "synthetic-home-b-key")
	for i, home := range []string{f.a, f.b} {
		key := []string{f.brainA.key, f.brainB.key}[i]
		rows := map[string]any{"api_key": key, "memory.enabled": false, "tools.approvalMode": "allow", "model_pool": "off", "task.audit": "off", "task.start": "single", "task.max_load": 0, "task.min_free_mb": 0, "setup_seen_at": time.Now().UTC().Format(time.RFC3339)}
		for _, name := range []string{config.KeyChatModel, config.KeyTaskModel, config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel, config.KeyTierReflexModel, config.KeyTierMastermindModel} {
			rows[name] = otherHomeModel
		}
		raw, _ := json.Marshal(rows)
		if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var addr string
	for port := 8900; port <= 8999; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			addr = ln.Addr().String()
			_ = ln.Close()
			break
		}
	}
	if addr == "" {
		t.Fatal("no free task relay port in 8900–8999")
	}
	f.relay = "http://" + addr
	relay := guardedCommand(t, f.ctx, f.a, f.env(f.a), product, "relay", "--listen", addr, "--store", filepath.Join(f.a, "relay-store"), "--quiet")
	otherHomeProcess(t, relay, filepath.Join(f.a, "relay.log"))
	otherHomeWait(t, f.ctx, "local relay", func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	t.Setenv("CODEAF_SYNC_URL", f.relay)
	id, err := identity.Ensure(f.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(f.b, id, false); err != nil {
		t.Fatal(err)
	}
	for i, home := range []string{f.a, f.b} {
		name := []string{"chat computer", "work computer"}[i]
		if _, err := devname.Set(home, name); err != nil {
			t.Fatal(err)
		}
		paired, ok, err := syncsetup.Open(home)
		if err != nil || !ok {
			t.Fatalf("paired home: %v, %v", ok, err)
		}
		if err := syncsetup.PutOwnDevice(f.ctx, paired.Dir, paired.Identity, paired.Device, name); err != nil {
			t.Fatal(err)
		}
		// Stop only each disposable home's native service, after its engines exit.
		t.Cleanup(func() {
			matches, _ := filepath.Glob(filepath.Join(home, "v3", "*.sock"))
			for _, socket := range matches {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = (cellstore.Daemon{Socket: socket}).Stop(ctx)
				cancel()
			}
		})
	}
	f.project = filepath.Join(f.a, "project")
	if err := os.Mkdir(f.project, 0700); err != nil {
		t.Fatal(err)
	}
	f.git(t, "init", "-q", "-b", "task-e2e")
	if err := os.WriteFile(filepath.Join(f.project, "README.md"), []byte("paired task fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.git(t, "add", "README.md")
	f.git(t, "commit", "-qm", "initial project")
	t.Cleanup(func() {
		states, _ := filepath.Glob(filepath.Join(f.b, "v3", "taskdoor", "*", "state.json"))
		for _, path := range states {
			raw, _ := os.ReadFile(path)
			var state struct {
				PID   int    `json:"pid"`
				Phase string `json:"phase"`
			}
			if json.Unmarshal(raw, &state) == nil && state.PID > 0 && state.Phase == "running" {
				// These groups were launched only by this fixture's task door.
				_ = syscall.Kill(-state.PID, syscall.SIGKILL)
			}
		}
	})
	return f
}

func (f *otherHomeTask) env(home string) []string {
	brain := f.brainA
	if home == f.b {
		brain = f.brainB
	}
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CODEAF_HOME=" + home, "CODEAF_PROFILE_DIR=", config.APIKeyEnv + "=" + brain.key,
		"CODEAF_BASE_URL=" + brain.server.URL, "CODEAF_SYNC_URL=" + f.relay, "CODEAF_RELAY=" + f.relay, "CODEAF_TASK_BELT=bash",
		"CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_NO_UPDATE_CHECK=1", "CODEAF_NO_AUTOMATIONS=1"}
}

func (f *otherHomeTask) startServing(t *testing.T) func() {
	return f.startServe(t, true)
}

func (f *otherHomeTask) startServe(t *testing.T, takeTasks bool) func() {
	t.Helper()
	log := filepath.Join(f.b, "serve.log")
	args := []string{"serve", "--via", f.relay}
	ready := "this machine is reachable as"
	if takeTasks {
		args = append(args, "--take-tasks")
		ready = taskdoor.TakingTasksLine
	}
	cmd := guardedCommand(t, f.ctx, f.b, f.env(f.b), f.product, args...)
	cmd.Dir = f.b
	stop := otherHomeProcess(t, cmd, log)
	otherHomeWait(t, f.ctx, "codeaf serve", func() bool {
		raw, _ := os.ReadFile(log)
		return strings.Contains(string(raw), ready)
	})
	return stop
}

type otherHomePipe struct {
	io.Reader
	io.Writer
	close func() error
}

func (p otherHomePipe) Close() error { return p.close() }

func (f *otherHomeTask) openConversation(t *testing.T, private bool) *remote.Agent {
	t.Helper()
	env := append(f.env(f.a), "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=0")
	if private {
		env = append(env, "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1")
	}
	cmd := guardedCommand(t, f.ctx, f.a, env, f.product, "engine", "--no-host", "--workspace", f.project)
	cmd.Dir = f.project
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(f.a, fmt.Sprintf("engine-%v.log", private))
	stop := otherHomeProcess(t, cmd, log)
	client, err := remote.Dial(otherHomePipe{Reader: out, Writer: in, close: func() error { return in.Close() }}, "this computer", remote.Hello{Workspace: f.project, Headless: true, Model: otherHomeModel, New: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); stop() })
	if !client.Welcome().TaskComputers {
		t.Fatal("paired task controls are absent")
	}
	return client.Agent()
}

func (f *otherHomeTask) say(t *testing.T, a *remote.Agent, words string) {
	t.Helper()
	events, err := a.Submit(f.ctx, words)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("conversation: %+v", event)
		}
	}
}

func (f *otherHomeTask) startTask(t *testing.T, a *remote.Agent) uint64 {
	t.Helper()
	id, _, _, err := a.StartTaskOn(f.ctx, otherHomeBrief, true, "work computer")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *otherHomeTask) waitDone(t *testing.T, a *remote.Agent, id uint64) {
	t.Helper()
	f.waitNotice(t, a, id, func(n session.TaskNotice) bool {
		if n.State == session.TaskDone {
			return true
		}
		if n.State != session.TaskRunning && n.State != session.TaskQueued {
			t.Fatalf("task ended without done: %+v", n)
		}
		return false
	})
}

func (f *otherHomeTask) waitNotice(t *testing.T, a *remote.Agent, id uint64, ready func(session.TaskNotice) bool) {
	t.Helper()
	events, stop := a.WatchTaskUpdates()
	defer stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("task connection closed")
			}
			if event.Task != nil && event.Task.ID == id && ready(*event.Task) {
				return
			}
		case <-f.ctx.Done():
			t.Fatalf("waiting for task %d: %v", id, f.ctx.Err())
		}
	}
}

func (f *otherHomeTask) assertFile(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.project, otherHomeFile))
	if err != nil || string(raw) != otherHomeContents {
		t.Fatalf("landed file = %q, %v", raw, err)
	}
}

func (f *otherHomeTask) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.project, "-c", "user.name=paired task fixture", "-c", "user.email=task@example.test", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = f.env(f.a)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func otherHomeProcess(t *testing.T, cmd *exec.Cmd, log string) func() {
	t.Helper()
	file, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = file
	if cmd.Stdout == nil {
		cmd.Stdout = file
	}
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			_ = file.Close()
		})
	}
	t.Cleanup(func() {
		stop()
		if t.Failed() {
			raw, _ := os.ReadFile(log)
			t.Logf("%s:\n%s", filepath.Base(log), raw)
		}
	})
	return stop
}

func otherHomeWait(t *testing.T, ctx context.Context, what string, ready func() bool) {
	t.Helper()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", what, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func otherHomeLedger(t *testing.T, home string) []session.UsageLine {
	t.Helper()
	file, err := os.Open(filepath.Join(home, "v3", "usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []session.UsageLine
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		var line session.UsageLine
		if err := json.Unmarshal(scan.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

type otherHomeBrain struct {
	server         *httptest.Server
	key            string
	gate           chan struct{}
	mu             sync.Mutex
	calls, workers int
}

func newOtherHomeBrain(t *testing.T, key string) *otherHomeBrain {
	t.Helper()
	b := &otherHomeBrain{key: key}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Paired task fixture","context_length":200000,"pricing":{"prompt":"0.000001","completion":"0.000001"},"supported_parameters":["tools","tool_choice"]}]}`, otherHomeModel, otherHomeModel)
		case "/key", "/auth/key", "/credits":
			fmt.Fprint(w, `{"data":{"limit_remaining":100,"total_credits":100,"total_usage":0}}`)
		case "/chat/completions":
			if r.Header.Get("Authorization") != "Bearer "+key {
				t.Error("provider received the other home's key")
				http.Error(w, "wrong synthetic key", 401)
				return
			}
			var request struct {
				Stream   bool
				Model    string
				Messages []struct {
					Role      string
					Content   json.RawMessage
					ToolCalls []struct{ Function struct{ Arguments string } } `json:"tool_calls"`
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				http.Error(w, "bad fixture request", 400)
				return
			}
			var assignment []string
			checker := false
			turn := 0
			for _, message := range request.Messages {
				checker = checker || strings.Contains(string(message.Content), "## Who checks this work")
				if message.Role == "tool" && strings.Contains(string(message.Content), "TASK_INSPECTED_HERE") {
					turn = max(turn, 1)
				}
				for _, call := range message.ToolCalls {
					if strings.Contains(call.Function.Arguments, "git add -- "+otherHomeFile) {
						turn = 2
					}
				}
				if match := freeConsentAssignment.FindStringSubmatch(string(message.Content)); len(match) == 3 {
					assignment = match
				}
			}
			b.mu.Lock()
			b.calls++
			if len(assignment) == 3 {
				b.workers++
			}
			b.mu.Unlock()
			if turn == 1 && b.gate != nil {
				if request.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				beat := time.NewTicker(time.Second)
				defer beat.Stop()
			waiting:
				for {
					select {
					case <-b.gate:
						break waiting
					case <-r.Context().Done():
						return
					case <-beat.C:
						if request.Stream {
							keepalive(w)
						}
					}
				}
			}
			var call any
			text := "CHAT_STAYS_HERE"
			if len(assignment) == 3 {
				command := "printf TASK_INSPECTED_HERE"
				if turn == 1 {
					command = "printf 'the paired worker wrote this\\n' > " + otherHomeFile + " && git add -- " + otherHomeFile + " && git -c user.name='paired worker' -c user.email=worker@example.test -c commit.gpgsign=false commit -qm '" + otherHomeCommit + "' && plandb done " + assignment[1] + " --agent " + assignment[2] + " --result 'The requested file and commit are ready.'"
				}
				if checker || turn > 1 {
					command = "grep -qx 'the paired worker wrote this' " + otherHomeFile + " && plandb done " + assignment[1] + " --agent " + assignment[2] + " --result 'holds: the requested file and commit are ready.'"
				}
				args, _ := json.Marshal(map[string]string{"command": command})
				call = map[string]any{"index": 0, "id": fmt.Sprintf("paired-%s-%d", assignment[2], turn), "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}
			}
			otherHomeResponse(w, request.Model, text, call, request.Stream)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.server.Close)
	return b
}

func (b *otherHomeBrain) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, b.workers
}

func otherHomeResponse(w http.ResponseWriter, model, text string, call any, stream bool) {
	message := map[string]any{"role": "assistant", "content": text}
	finish := "stop"
	if call != nil {
		message["content"] = ""
		message["tool_calls"] = []any{call}
		finish = "tool_calls"
	}
	usage := map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18, "cost": otherHomeCallCost}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": message, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			raw, _ := json.Marshal(map[string]any{"id": "paired-fixture", "model": model, "choices": []any{choice}, "usage": usage})
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "paired-fixture", "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
}
