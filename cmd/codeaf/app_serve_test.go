package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The child takes the real command dispatcher without building another
// product binary. Its stdout is the protocol, so testing's PASS line is absent.
func TestAppServeHelper(t *testing.T) {
	if os.Getenv("CODEAF_APP_SERVE_TEST_HELPER") != "1" {
		return
	}
	// Choose the fixture after package init so the test home guard allows it.
	if chosen := os.Getenv("CODEAF_APP_TEST_HOME"); chosen != "" {
		t.Setenv(home.EnvVar, chosen)
	}
	for index, argument := range os.Args {
		if argument == "--" {
			os.Args = append([]string{"codeaf", "app", "serve"}, os.Args[index+1:]...)
			os.Exit(execute())
		}
	}
	os.Exit(2)
}

func TestAppParentHelper(t *testing.T) {
	if os.Getenv("CODEAF_APP_PARENT_TEST_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

type appTestLaunch struct {
	hello   appapi.Hello
	command *exec.Cmd
	input   io.WriteCloser
	done    chan error
	rest    []byte
	errors  bytes.Buffer
	root    string
	tuiRoot string
}

func launchAppForTest(t *testing.T, args ...string) *appTestLaunch {
	t.Helper()
	return launchAppAtHomeForTest(t, t.TempDir(), false, args...)
}

func launchAppAtHomeForTest(t *testing.T, root string, shared bool, args ...string) *appTestLaunch {
	t.Helper()
	launch := &appTestLaunch{root: filepath.Join(root, "engine"), tuiRoot: filepath.Join(root, "tui"), done: make(chan error, 1)}
	words := []string{"-test.run=^TestAppServeHelper$", "--"}
	if shared {
		launch.root = launch.tuiRoot
	} else {
		words = append(words, "--home", launch.root)
	}
	words = append(words, args...)
	launch.command = exec.Command(os.Args[0], words...)
	launch.command.Env = []string{
		"CODEAF_APP_SERVE_TEST_HELPER=1", "CODEAF_HOME=" + launch.tuiRoot,
		"HOME=" + root, "PATH=" + os.Getenv("PATH"), "CODEAF_TELEMETRY=off",
		"GOMAXPROCS=2",
	}
	if shared {
		launch.command.Env = append(launch.command.Env, "CODEAF_APP_TEST_HOME="+launch.root)
		launch.command.Env[1] = "CODEAF_HOME=" + filepath.Join(root, "inherited")
	}
	launch.command.Stderr = &launch.errors
	var err error
	launch.input, err = launch.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := launch.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := launch.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = launch.input.Close()
		_ = launch.command.Process.Kill()
	})
	helloLine := make(chan []byte, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		line, err := reader.ReadBytes('\n')
		if err == nil {
			helloLine <- line
		}
		launch.rest, _ = io.ReadAll(reader)
		launch.done <- launch.command.Wait()
	}()
	select {
	case line := <-helloLine:
		if err := json.Unmarshal(line, &launch.hello); err != nil {
			t.Fatalf("hello is not one JSON line: %s (%v)", line, err)
		}
	case err := <-launch.done:
		t.Fatalf("exited before hello: %v\n%s", err, launch.errors.String())
	case <-time.After(10 * time.Second):
		t.Fatal("app did not announce its listener within 10 seconds")
	}
	return launch
}

func (launch *appTestLaunch) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-launch.done:
		if err != nil {
			t.Fatalf("app exit: %v\n%s", err, launch.errors.String())
		}
		if len(launch.rest) != 0 {
			t.Fatalf("stdout was not silent after hello: %s", launch.rest)
		}
		log, err := os.ReadFile(filepath.Join(launch.root, "logs", "app-serve.log"))
		if err != nil || !bytes.Contains(log, []byte("stopped")) {
			t.Fatalf("missing shutdown log: %v %s", err, log)
		}
		if bytes.Contains(log, []byte(launch.hello.Token)) || strings.Contains(launch.errors.String(), launch.hello.Token) {
			t.Fatal("bearer token was copied to a log")
		}
		files, err := os.ReadDir(launch.tuiRoot)
		if launch.root != launch.tuiRoot && err == nil && len(files) != 0 {
			t.Fatalf("app startup wrote into the TUI root: %v", files)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("app did not exit within 10 seconds")
	}
}

func (launch *appTestLaunch) call(t *testing.T, method, path, body, token string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, launch.hello.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestAppServeHelloHealthTokenIsolationAndShutdown(t *testing.T) {
	first, second := launchAppForTest(t), launchAppForTest(t)
	if first.hello.Token == second.hello.Token || first.hello.Epoch == second.hello.Epoch {
		t.Fatal("two launches reused a token or epoch")
	}
	for _, launch := range []*appTestLaunch{first, second} {
		token, err := hex.DecodeString(launch.hello.Token)
		if err != nil || len(token) != 32 || launch.hello.Home != launch.root || launch.hello.PID != launch.command.Process.Pid || launch.hello.Contract != appapi.Contract || launch.hello.CodeafVersion == "" || launch.hello.Epoch == "" {
			t.Fatalf("incomplete hello: %+v", launch.hello)
		}
		if !strings.HasPrefix(launch.hello.URL, "http://127.0.0.1:") || strings.HasSuffix(launch.hello.URL, ":0") {
			t.Fatalf("hello did not name the allocated loopback port: %s", launch.hello.URL)
		}
		response := launch.call(t, "GET", "/app/v1/health", "", launch.hello.Token)
		var health map[string]any
		err = json.NewDecoder(response.Body).Decode(&health)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || health["status"] != "ready" || health["epoch"] != launch.hello.Epoch || health["codeaf_version"] != launch.hello.CodeafVersion {
			t.Fatalf("health did not describe hello: %v %v", health, err)
		}
	}
	response := second.call(t, "GET", "/app/v1/health", "", first.hello.Token)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("another launch's token authenticated")
	}
	for _, launch := range []*appTestLaunch{first, second} {
		response := launch.call(t, "POST", "/app/v1/shutdown", `{"reason":"quit"}`, launch.hello.Token)
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "{\"ok\":true}\n" {
			t.Fatalf("shutdown did not acknowledge: %d %s %v", response.StatusCode, body, err)
		}
		launch.wait(t)
	}
}

func TestAppServeStdinEOFEndsLaunchWithoutLoggingInput(t *testing.T) {
	launch := launchAppForTest(t)
	const secret = "provider-secret-must-never-be-logged"
	if _, err := io.WriteString(launch.input, `{"keys":{"openrouter":"`+secret+`"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	response := launch.call(t, "GET", "/app/v1/health", "", launch.hello.Token)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("stdin data ended the launch before EOF")
	}
	launch.input.Close()
	launch.wait(t)
	log, err := os.ReadFile(filepath.Join(launch.root, "logs", "app-serve.log"))
	if err != nil || bytes.Contains(log, []byte(secret)) || strings.Contains(launch.errors.String(), secret) {
		t.Fatal("stdin secret appeared in logs")
	}
}

func TestAppServeParentDeathEndsLaunchWithStdinStillOpen(t *testing.T) {
	parent := exec.Command(os.Args[0], "-test.run=^TestAppParentHelper$")
	parent.Env = []string{"CODEAF_APP_PARENT_TEST_HELPER=1", "HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GOMAXPROCS=2"}
	input, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = parent.Process.Kill() })
	launch := launchAppForTest(t, "--parent-pid", strconv.Itoa(parent.Process.Pid))
	input.Close()
	if err := parent.Wait(); err != nil {
		t.Fatal(err)
	}
	launch.wait(t)
}

func TestAppParentClockChecksDeathAndStopsOnCancellation(t *testing.T) {
	for _, parentDies := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		checks := make(chan int)
		liveness := make(chan bool)
		ticks := make(chan time.Time)
		done := make(chan struct{})
		stopped := make(chan struct{}, 1)
		go func() {
			watchAppParent(ctx, 123, func(pid int) bool { checks <- pid; return <-liveness }, ticks, func() { stopped <- struct{}{} })
			close(done)
		}()
		if <-checks != 123 {
			t.Fatal("checked a different parent")
		}
		liveness <- true
		if parentDies {
			// The tick and liveness reply determine both observations; no
			// wall-clock sleep decides which observation wins.
			ticks <- time.Time{}
			<-checks
			liveness <- false
		} else {
			cancel()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("parent watcher did not finish")
		}
		cancel()
		if (len(stopped) == 1) != parentDies {
			t.Fatal("cancellation and parent death were confused")
		}
	}
}

func TestAppParentReparentingEndsLifeBeforeAZombieIsReaped(t *testing.T) {
	currentParent := 123
	alive := appParentCheck(123, func() int { return currentParent }, func(int) bool { return true })
	if !alive(123) {
		t.Fatal("live launch parent was refused")
	}
	currentParent = 1
	if alive(123) {
		t.Fatal("a reparented child kept waiting for its dead parent's pid to disappear")
	}
	other := appParentCheck(456, func() int { return currentParent }, func(pid int) bool { return pid == 456 })
	if !other(456) {
		t.Fatal("a named process outside the launch-parent chain was refused")
	}
}

func TestAppServeInterruptEndsLaunch(t *testing.T) {
	launch := launchAppForTest(t)
	if err := launch.command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	launch.wait(t)
}

func TestAppEngineHomeIsSharedAndExplicitHomeWins(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	t.Setenv(home.EnvVar, filepath.Join(login, "tui"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	want := home.Dir()
	root, err := appEngineHome("")
	if err != nil || root != want {
		t.Fatalf("shared app home: %q %v, want %q", root, err, want)
	}
	t.Setenv(home.EnvVar, "")
	root, err = appEngineHome("")
	if err != nil || root != home.DefaultUnder(login) {
		t.Fatalf("default home: %q %v", root, err)
	}
	root, err = appEngineHome("relative-engine")
	want, _ = filepath.Abs("relative-engine")
	if err != nil || root != want {
		t.Fatalf("explicit home: %q %v, want %q", root, err, want)
	}
}

// The app must use the terminal's live home without replacing its runner
// lock, settings or history, and stdin key material must never reach disk.
func TestAppServeSharesTheTerminalHomeWithoutPersistingKeys(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "tui")
	st, err := store.Open(filepath.Join(shared, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	release, ok := st.TryWorker()
	if !ok {
		t.Fatal("terminal could not hold runner lock")
	}
	defer release()
	for _, name := range []string{"config.json", "history.json"} {
		if err := os.WriteFile(filepath.Join(shared, name), []byte("terminal state"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	launch := launchAppAtHomeForTest(t, root, true)
	if launch.hello.Home != shared {
		t.Fatalf("hello home = %q, want %q", launch.hello.Home, shared)
	}
	const secret = "keychain-secret-never-on-disk"
	if _, err := io.WriteString(launch.input, `{"keys":{"openrouter":"`+secret+`"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	launch.input.Close()
	launch.wait(t)
	if otherRelease, ok := st.TryWorker(); ok {
		otherRelease()
		t.Fatal("app startup or exit released the terminal's runner lock")
	}
	for _, name := range []string{"config.json", "history.json"} {
		body, err := os.ReadFile(filepath.Join(shared, name))
		if err != nil || string(body) != "terminal state" {
			t.Fatalf("terminal state changed: %s %q %v", name, body, err)
		}
	}
	if err := filepath.WalkDir(shared, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("stdin key persisted in %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppServeHelpAndBadArgumentsDoNotLaunch(t *testing.T) {
	out, _ := captureUsage(t)
	if err := runApp([]string{"serve", "--help"}); !errors.Is(err, exitHelped) || !strings.Contains(out.String(), "--parent-pid") {
		t.Fatalf("app serve help: %v %s", err, out)
	}
	for _, args := range [][]string{nil, {"unknown"}, {"serve", "extra"}, {"serve", "--parent-pid", "-1"}} {
		if err := runApp(args); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
}
