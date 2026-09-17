package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui3"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

type surfaceFrameWriter struct {
	mu sync.Mutex
	bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (writer *surfaceFrameWriter) Write(raw []byte) (int, error) {
	writer.mu.Lock()
	written, err := writer.Buffer.Write(raw)
	if strings.Contains(writer.Buffer.String(), "codeaf") {
		writer.once.Do(func() { close(writer.ready) })
	}
	writer.mu.Unlock()
	return written, err
}

func (writer *surfaceFrameWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.Buffer.String()
}

// TestRunSurfaceWiresTheDeferredLaunchCheckAndInstallerThroughRealInit proves
// C5, C7, and the live capture half of C14 at the shared surface door.
func TestRunSurfaceWiresTheDeferredLaunchCheckAndInstallerThroughRealInit(t *testing.T) {
	asset := []byte("new executable")
	digest := sha256.Sum256(asset)
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var requestOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases/latest"):
			requestOnce.Do(func() { close(requestStarted) })
			select {
			case <-releaseResponse:
				fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
			case <-request.Context().Done():
			}
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		default:
			_, _ = w.Write(asset)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldRun := runSurfaceProgram
	oldClient, oldExecutable := surfaceUpdateClient, surfaceExecutable
	oldRevision, oldArguments := surfaceRevision, surfaceArguments
	t.Cleanup(func() {
		runSurfaceProgram = oldRun
		surfaceUpdateClient, surfaceExecutable = oldClient, oldExecutable
		surfaceRevision, surfaceArguments = oldRevision, oldArguments
	})
	frame := &surfaceFrameWriter{ready: make(chan struct{})}
	input, inputWriter := io.Pipe()
	defer inputWriter.Close()
	optionsSeen := make(chan tui3.Options, 1)
	runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
		optionsSeen <- options
		options.Input = input
		options.Output = frame
		options.Width, options.Height = 80, 24
		return tui3.Run(ctx, options)
	}
	surfaceUpdateClient = func(revision string, timeout time.Duration) *codeupdate.Client {
		if revision != "v0.1.1" || timeout != 3*time.Second {
			t.Fatalf("client revision %q timeout %s", revision, timeout)
		}
		return &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL, Revision: revision}
	}
	resolved := 0
	surfaceExecutable = func(executable func() (string, error)) (string, error) {
		resolved++
		return target, nil
	}
	surfaceRevision = func() string { return "v0.1.1" }
	surfaceArguments = func() []string { return []string{"chat", "--model", "x"} }
	profile := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		finished <- runSurface(ctx, tui3.Options{
			Agent: &quietAgent{}, Workspace: "/tmp/lab", ProfileDir: profile, SessionFile: "/tmp/this.jsonl",
		})
	}()
	seen := <-optionsSeen
	if seen.UpdateCheck == nil || seen.ResolveUpdate == nil || seen.InstallUpdate == nil {
		cancel()
		t.Fatal("runSurface did not wire every update door")
	}
	restartArgs := codeupdate.RestartArgs(seen.UpdateArgs, seen.SessionFile)
	if strings.Join(restartArgs, " ") != "chat --model x --session /tmp/this.jsonl" || seen.UpdateRunning != "v0.1.1" {
		cancel()
		t.Fatalf("running %q restart args %q", seen.UpdateRunning, restartArgs)
	}
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Init never started the launch check")
	}
	select {
	case <-frame.ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the first frame did not render while the release request waited")
	}
	if resolved != 0 || !strings.Contains(frame.String(), "codeaf") {
		cancel()
		t.Fatalf("executable resolutions = %d; first frame:\n%s", resolved, frame.String())
	}
	close(releaseResponse)
	cancel()
	if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("surface exit: %v", err)
	}

	result, err := seen.InstallUpdate(context.Background(), codeupdate.Release{Tag: "v0.2.0", Repository: "Agent-Field/codeaf"})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(target)
	if err != nil || string(installed) != string(asset) || result.Path != target || resolved != 1 {
		t.Fatalf("result = %+v resolved = %d installed = %q, %v", result, resolved, installed, err)
	}
}

// A SURFACE THAT OWNS THE TERMINAL OWNS THE LOGGER, AND IT OWNS IT FROM ONE
// PLACE.
//
// The defect this guards was not a wrong redirect — it was four doors, one of
// which remembered (#404). The in-process door parked the standard logger in
// the profile's chat.log; the ssh, relay and unix-socket doors left it on
// stderr, so a recovered fault or a checkpoint warning tore through the alt
// screen and was gone with the next repaint. The next door will be written by
// copying one of these, and the copy is only safe while running the surface is
// a call rather than a paragraph a door has to remember. So this reads the
// source: [tui3.Run] is reached from chatv3_surface.go and from nowhere else in
// this command.
func TestOnlyTheSurfaceHelperRunsTheV3Surface(t *testing.T) {
	const helper = "chatv3_surface.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		calls := strings.Count(string(raw), "tui3.Run(")
		switch {
		case name == helper && calls == 1:
			found = true
		case name == helper:
			t.Errorf("%s runs the surface %d times, want exactly 1 — the helper is the "+
				"one path and it is a single sentence", name, calls)
			found = calls > 0
		case calls > 0:
			t.Errorf("%s calls tui3.Run itself; every door goes through runSurface in %s, "+
				"which is where the byte meter is armed and the standard logger is parked "+
				"in the profile's chat.log for as long as the surface owns the terminal",
				name, helper)
		}
	}
	if !found {
		t.Fatalf("no door runs the surface at all: %s does not call tui3.Run", helper)
	}

	// And the four doors take it. A door that stopped calling runSurface without
	// calling tui3.Run either stopped opening the surface or grew a third way to
	// do it, and both are worth a red line.
	for _, door := range []string{
		"chatv3.go", "chatv3_host.go", "chatv3_at.go", "chatv3_local.go",
	} {
		raw, err := os.ReadFile(door)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "runSurface(") {
			t.Errorf("%s opens the v3 surface and never calls runSurface", door)
		}
	}
}

// The redirect itself, asked the three ways it can be met: the ordinary one,
// the way back out, and the profile that cannot take the file.
func TestTheSurfaceLoggerLandsInTheProfileAndGivesTheTerminalBack(t *testing.T) {
	// Whatever this process's logger was set to is restored whichever way the
	// subtests end; the standard logger is global, and a test that left it in a
	// temporary directory would take the rest of the package with it.
	before := log.Writer()
	t.Cleanup(func() { log.SetOutput(before) })

	t.Run("a line written while the surface is up lands in the profile's chat.log", func(t *testing.T) {
		profile := t.TempDir()
		terminal := &bytes.Buffer{}
		log.SetOutput(terminal)
		ran := false
		err := withSurfaceLogger(profile, func() error {
			ran = true
			log.Printf("a checkpoint warning nobody should have to read off the frame")
			if log.Writer() == terminal {
				t.Error("the logger is still on the terminal while the surface owns it")
			}
			return nil
		})
		if err != nil || !ran {
			t.Fatalf("run the surface: ran=%v err=%v", ran, err)
		}
		kept, err := os.ReadFile(filepath.Join(profile, "chat.log"))
		if err != nil {
			t.Fatalf("read the profile's chat.log: %v", err)
		}
		if !strings.Contains(string(kept), "a checkpoint warning") {
			t.Errorf("chat.log does not hold the line: %q", string(kept))
		}
		if terminal.Len() != 0 {
			t.Errorf("the line tore through the frame anyway: %q", terminal.String())
		}
		if log.Writer() != terminal {
			t.Error("the terminal did not get its logger back when the surface handed it over")
		}
	})

	t.Run("the error the surface answers with is the one the door sees", func(t *testing.T) {
		profile := t.TempDir()
		want := errSurfaceTest
		if got := withSurfaceLogger(profile, func() error { return want }); got != want {
			t.Errorf("withSurfaceLogger answered %v, want the surface's own %v", got, want)
		}
	})

	t.Run("a profile that cannot take the file keeps stderr and still runs", func(t *testing.T) {
		// A FILE USED AS A DIRECTORY: chat.log cannot be created under it, which
		// is the shape a read-only or occupied profile takes. A lost frame is
		// better than a lost warning, so the surface runs and the logger stays
		// where it was.
		blocked := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocked, []byte("in the way"), 0o644); err != nil {
			t.Fatal(err)
		}
		terminal := &bytes.Buffer{}
		log.SetOutput(terminal)
		ran := false
		err := withSurfaceLogger(blocked, func() error {
			ran = true
			if log.Writer() != terminal {
				t.Error("the logger moved off the terminal with nowhere to move to")
			}
			log.Printf("a warning that had to go somewhere")
			return nil
		})
		if err != nil || !ran {
			t.Fatalf("run the surface anyway: ran=%v err=%v", ran, err)
		}
		if !strings.Contains(terminal.String(), "a warning that had to go somewhere") {
			t.Errorf("the warning was lost rather than kept on the terminal: %q", terminal.String())
		}
	})
}

// errSurfaceTest stands in for whatever the surface itself failed with.
var errSurfaceTest = errSurface("the surface stopped")

type errSurface string

func (e errSurface) Error() string { return string(e) }

// THE CRASH LOG AND THE RUNNING LOG ARE ONE FILE, under a profile too. A person
// who moved their profile with CODEAF_PROFILE_DIR has the surface's warnings
// written inside it; the fatal fault's "Details: <path>" has to name the same
// file, or the one place to look becomes two.
func TestTheCrashLogAndTheRunningLogAreOneFileUnderAProfile(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))

	previous := log.Writer()
	t.Cleanup(func() { log.SetOutput(previous) })
	if err := withSurfaceLogger(config.ProfileDir(), func() error {
		log.Print("a warning while the surface is up")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	reportFault(stderr, "runtime error: nil pointer", []byte("goroutine 1 [running]:\n"))

	want := filepath.Join(profile, "chat.log")
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("the profile's chat.log was not written: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "a warning while the surface is up") {
		t.Fatalf("the running log did not land in the profile:\n%s", got)
	}
	if !strings.Contains(got, "fatal fault: runtime error: nil pointer") {
		t.Fatalf("the crash append did not land in the same file:\n%s", got)
	}
	if !strings.Contains(stderr.String(), want) && !strings.Contains(stderr.String(), displayPath(want)) {
		t.Fatalf("the sentence on screen names %q, not the file both wrote: %s", stderr.String(), want)
	}
}
