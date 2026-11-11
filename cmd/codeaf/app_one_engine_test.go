package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The command's real signal and wire doors are exercised in separate processes.
// Only final disk saving is replaced, by a disk that never answers its caller.
func TestAppOneEngine(t *testing.T) {
	if mode := os.Getenv("CODEAF_TEST_ONE_ENGINE"); mode != "" {
		switch mode {
		case "stuck":
			flushFinalSaves = func(context.Context) error {
				_ = os.WriteFile(home.Join("saving"), nil, 0600)
				select {}
			}
		case "impatient":
			appHomeWait = 300 * time.Millisecond
		case "parent":
			t.Setenv(home.EnvVar, os.Getenv("CODEAF_TEST_ONE_HOME"))
			if err := holdAppHome(); err != nil {
				t.Fatal(err)
			}
			child := exec.Command("sleep", "60")
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			fmt.Printf("child %d\n", child.Process.Pid)
			select {}
		}
		root := os.Getenv("CODEAF_TEST_ONE_HOME")
		t.Setenv(home.EnvVar, root)
		os.Args = []string{os.Args[0], "app", "--home", root}
		if mode == "schema" {
			os.Args = append(os.Args, "--schema")
		}
		if mode == "version" {
			os.Args = []string{os.Args[0], "version"}
		}
		main()
		os.Exit(0)
	}
	closing := func(t *testing.T, root string) *oneEngineProcess {
		t.Helper()
		first := startOneEngine(t, root, "stuck")
		first.hello(t)
		_ = first.cmd.Process.Signal(syscall.SIGTERM)
		waitOneEngineFile(t, filepath.Join(root, "saving"))
		return first
	}
	t.Run("the second engine waits and starts when the first ends", func(t *testing.T) {
		root := t.TempDir()
		first := closing(t, root)
		second := startOneEngine(t, root, "ordinary")
		select {
		case line := <-second.lines:
			t.Fatalf("second engine admitted while the first lives: %s", line)
		case <-second.done:
			t.Fatalf("second engine exited instead of waiting: %s", second.stderr.String())
		case <-time.After(2 * time.Second):
		}
		// A crash releases ownership without a stale-pid recovery or a deleted lock.
		first.kill(t)
		second.hello(t)
		second.leave(t)
		// The waiting engine said whom it waited for, so a start that is
		// waiting is never mistaken for one that is wedged. Its stderr is read
		// only once it has exited.
		if want := fmt.Sprintf("waiting for the engine still closing this home (pid %d)", first.cmd.Process.Pid); !strings.Contains(second.stderr.String(), want) {
			t.Fatalf("the waiting engine did not name the one it waits for: %q", second.stderr.String())
		}
	})
	t.Run("a crash leaves nothing to wait for", func(t *testing.T) {
		root := t.TempDir()
		first := startOneEngine(t, root, "ordinary")
		first.hello(t)
		first.kill(t)
		began := time.Now()
		next := startOneEngine(t, root, "ordinary")
		next.hello(t)
		next.leave(t)
		if waited := time.Since(began); waited > 8*time.Second {
			t.Fatalf("relaunch after a crash took %s", waited)
		}
	})
	t.Run("the wait ends with a clear message and starts nothing", func(t *testing.T) {
		root := t.TempDir()
		closing(t, root)
		second := startOneEngine(t, root, "impatient")
		select {
		case line := <-second.lines:
			t.Fatalf("engine spoke after giving up: %s", line)
		case <-second.done:
			if !strings.Contains(second.stderr.String(), "another codeaf app is still closing this home") {
				t.Fatalf("missing message: %s", second.stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the wait had no bound")
		}
	})
	t.Run("a waiting engine starts nothing and asks for nothing", func(t *testing.T) {
		root := t.TempDir()
		held, err := os.OpenFile(filepath.Join(root, "app.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
		if err := filelock.Lock(held, true, true); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int32
		provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
		defer provider.Close()
		t.Setenv("CODEAF_BASE_URL", provider.URL)
		t.Setenv("OPENROUTER_API_KEY", "fixture")
		waiting := startOneEngine(t, root, "ordinary")
		select {
		case line := <-waiting.lines:
			t.Fatalf("engine spoke while waiting: %s", line)
		case <-waiting.done:
			t.Fatalf("engine left while waiting: %s", waiting.stderr.String())
		case <-time.After(2 * time.Second):
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || requests.Load() != 0 {
			t.Fatalf("while waiting: home holds %v, %d provider requests", entries, requests.Load())
		}
		if err := filelock.Unlock(held); err != nil {
			t.Fatal(err)
		}
		waiting.hello(t)
		waiting.leave(t)
	})
	t.Run("a child that outlives the engine doesn't hold the home", func(t *testing.T) {
		root := t.TempDir()
		parent := startOneEngine(t, root, "parent")
		var pid int
		select {
		case line := <-parent.lines:
			if _, err := fmt.Sscanf(line, "child %d", &pid); err != nil {
				t.Fatalf("no child line: %s", line)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("parent didn't start its child: %s", parent.stderr.String())
		}
		t.Cleanup(func() {
			if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil && bytes.HasPrefix(raw, []byte("sleep")) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		})
		parent.kill(t)
		next := startOneEngine(t, root, "ordinary")
		next.hello(t)
		next.leave(t)
	})
	// A QUIT SAVES EVERY OPEN CONVERSATION AND IS A CLEAN EXIT. The sixth live
	// A+B run's quit, with about 24 chats open, ended in "codeaf could not
	// save this chat" and status 1. Here a dozen conversations each take a
	// real turn against a provider on this machine, the app is quit the way
	// the desk quits it, and every journal must hold its own turn, the
	// process must exit 0, and stderr must say nothing failed.
	t.Run("a quit with many conversations saves every one and exits zero", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(oneEngineModel))
		defer provider.Close()
		t.Setenv("CODEAF_BASE_URL", provider.URL)
		t.Setenv("OPENROUTER_API_KEY", "fixture")
		root := t.TempDir()
		workspace := t.TempDir()
		// The app reads its key from the profile, as a key pasted into setup is.
		if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"api_key":"sk-or-fixture-not-real"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		p := startOneEngine(t, root, "ordinary")
		p.hello(t)
		const chats = 12
		for n := 1; n <= chats; n++ {
			fmt.Fprintf(p.stdin, `{"id":%d,"method":"agents.create","params":{"workspace":%q,"model":"stub/scripted","text":"keep turn %02d","client_id":"quit-%d"}}`+"\n", n, workspace, n, n)
		}
		done := 0
		deadline := time.After(60 * time.Second)
		for done < chats {
			select {
			case line := <-p.lines:
				if strings.Contains(line, `"event":"turn.done"`) {
					done++
				}
			case <-p.done:
				t.Fatalf("engine ended before its turns did: %s", p.stderr.String())
			case <-deadline:
				t.Fatalf("%d of %d turns finished: %s", done, chats, p.stderr.String())
			}
		}
		began := time.Now()
		fmt.Fprintln(p.stdin, `{"id":999,"method":"shutdown"}`)
		_ = p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(60 * time.Second):
			t.Fatal("the quit had no end")
		}
		if code := p.cmd.ProcessState.ExitCode(); code != 0 {
			t.Fatalf("a clean quit exited %d: %s", code, p.stderr.String())
		}
		if strings.Contains(p.stderr.String(), "could not save") {
			t.Fatalf("a clean quit lost a save: %s", p.stderr.String())
		}
		if took := time.Since(began); took > 15*time.Second {
			t.Fatalf("a quit with %d idle conversations took %s, longer than a new engine's hello waits", chats, took)
		}
		kept := map[string]bool{}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "transcript.jsonl" {
				raw, _ := os.ReadFile(path)
				for n := 1; n <= chats; n++ {
					if word := fmt.Sprintf("keep turn %02d", n); bytes.Contains(raw, []byte(word)) && bytes.Contains(raw, []byte("kept: "+word)) {
						kept[word] = true
					}
				}
			}
			return nil
		})
		if len(kept) != chats {
			var found []string
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					found = append(found, path)
				}
				return nil
			})
			t.Fatalf("%d of %d conversations kept their turn after the quit: %v\nfiles %v\nstderr %s", len(kept), chats, kept, found, p.stderr.String())
		}
	})
	t.Run("only one home's app is held", func(t *testing.T) {
		root := t.TempDir()
		closing(t, root)
		other := startOneEngine(t, t.TempDir(), "ordinary")
		other.hello(t)
		other.leave(t)
		for _, mode := range []string{"schema", "version"} {
			p := startOneEngine(t, root, mode)
			select {
			case <-p.done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s waited on the app's lock", mode)
			}
		}
	})
}

type oneEngineProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	done   chan struct{}
	stderr bytes.Buffer
}

func startOneEngine(t *testing.T, root, mode string) *oneEngineProcess {
	t.Helper()
	cleanupAppChildDaemons(t, root)
	p := &oneEngineProcess{lines: make(chan string, 4096), done: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestAppOneEngine$")
	p.cmd.Env = append(os.Environ(), "CODEAF_TEST_ONE_ENGINE="+mode, home.EnvVar+"="+filepath.Join(root, "inherited"), "CODEAF_TEST_ONE_HOME="+root)
	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		for scanner.Scan() {
			select {
			case p.lines <- scanner.Text():
			default:
			}
		}
	}()
	go func() { <-scanned; _ = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { p.kill(t); _ = p.stdin.Close() })
	return p
}

func (p *oneEngineProcess) hello(t *testing.T) {
	t.Helper()
	select {
	case line := <-p.lines:
		if !strings.Contains(line, `"event":"hello"`) {
			t.Fatalf("not hello: %s", line)
		}
	case <-p.done:
		t.Fatalf("engine ended before hello: %s", p.stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not say hello")
	}
}

func (p *oneEngineProcess) leave(t *testing.T) {
	t.Helper()
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not close")
	}
}

func (p *oneEngineProcess) kill(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		return
	default:
	}
	// Check the exact child before killing it; never select processes by name.
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", p.cmd.Process.Pid)); err == nil && !bytes.Contains(raw, []byte("TestAppOneEngine")) {
		t.Fatalf("unexpected child command: %q", raw)
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}

func waitOneEngineFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("signal did not reach final saving")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// oneEngineModel is a provider on this machine that answers every turn by
// quoting the person's last words, in OpenRouter's streaming dialect, so a
// journal that kept a turn holds both the words and the reply to them.
func oneEngineModel(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/models") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"stub/scripted","canonical_slug":"stub/scripted","name":"Scripted","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},"supported_parameters":["tools","tool_choice","max_tokens"]}]}`)
		return
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	said := ""
	for _, m := range body.Messages {
		if m.Role == "user" {
			said = string(m.Content)
		}
	}
	reply := "kept"
	if at := strings.Index(said, "keep turn "); at >= 0 && len(said) >= at+len("keep turn 00") {
		reply = "kept: " + said[at:at+len("keep turn 00")]
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(payload string) {
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	at := time.Now().Unix()
	send(fmt.Sprintf(`{"id":"q","object":"chat.completion.chunk","created":%d,"model":"stub/scripted","choices":[{"index":0,"delta":{"role":"assistant","content":%q},"finish_reason":null}]}`, at, reply))
	send(fmt.Sprintf(`{"id":"q","object":"chat.completion.chunk","created":%d,"model":"stub/scripted","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`, at))
	send("[DONE]")
}
