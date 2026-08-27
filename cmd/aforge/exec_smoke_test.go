package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The whole of the exec contract that a harness depends on lives outside this
// process: one JSON object on stdout, every word of diagnosis on stderr, and an
// exit code that says what happened. None of that can be tested by calling
// runExec in-process, because the thing under test is the streams and the
// status of a real program. So these tests build the binary and run it, with an
// httptest server standing in for OpenRouter.
//
// The build is the slow part (a few seconds, cached after the first), so
// `go test -short` skips it.

const smokeAnswer = "the smoke test answer"

// buildAforgeStamped compiles the binary under test with a revision stamped in,
// which also exercises the -ldflags path the release workflow depends on. An
// empty stamp builds it exactly as `go build ./cmd/aforge` would.
func buildAforgeStamped(t *testing.T, stamp string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	goTool, err := osexec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH to build the binary with")
	}
	binary := filepath.Join(t.TempDir(), "aforge")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	// -buildvcs=false because the version under test is the one this build
	// stamps, not the one git happens to be able to describe: a worktree, a
	// shallow checkout, or a concurrent git lock makes VCS stamping fail the
	// build outright, and none of that is the subject here.
	arguments := []string{"build", "-buildvcs=false"}
	if strings.TrimSpace(stamp) != "" {
		arguments = append(arguments,
			"-ldflags=-X github.com/Agent-Field/aforge-v2/internal/buildinfo.rev="+stamp)
	}
	arguments = append(arguments, "-o", binary, ".")
	build := osexec.Command(goTool, arguments...)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	return binary
}

// fakeOpenRouter answers every completion with the same finished message. The
// number of calls exec makes is not part of the contract under test; that it
// ends with one envelope on stdout is.
func fakeOpenRouter(t *testing.T) *httptest.Server {
	t.Helper()
	answer, err := json.Marshal(smokeAnswer)
	if err != nil {
		t.Fatal(err)
	}
	completion := `{"model":"test/model","choices":[{"index":0,"finish_reason":"stop",` +
		`"message":{"role":"assistant","content":` + string(answer) + `}}],` +
		`"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.URL.Path, "/models") {
			// An empty catalog is the honest offline answer and is what every
			// price lookup below then falls back from.
			_, _ = writer.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = writer.Write([]byte(completion))
	}))
	t.Cleanup(server.Close)
	return server
}

// smokeEnv is a deliberately closed environment: the developer's own API key,
// model, state root and EXA key must not reach the child, or the test would
// pass for reasons that have nothing to do with the build.
func smokeEnv(t *testing.T, baseURL string) (env []string, home string) {
	t.Helper()
	home = t.TempDir()
	return []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"OPENROUTER_API_KEY=smoke-test-key",
		"AFORGE_BASE_URL=" + baseURL,
		"AFORGE_HOME=" + home,
		"AFORGE_PROFILE_DIR=" + home,
		"AFORGE_MODEL=test/model",
		"AFORGE_DAILY_BUDGET=0",
	}, home
}

func runSmoke(t *testing.T, binary string, env []string, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := osexec.CommandContext(ctx, binary, args...)
	command.Env = env
	command.Stdin = strings.NewReader(stdin)
	var out, errs bytes.Buffer
	command.Stdout = &out
	command.Stderr = &errs
	err := command.Run()
	var exitErr *osexec.ExitError
	switch {
	case err == nil:
		code = 0
	case asExitError(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run %v: %v\nstderr:\n%s", args, err, errs.String())
	}
	return out.String(), errs.String(), code
}

func asExitError(err error, target **osexec.ExitError) bool {
	exitErr, ok := err.(*osexec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

// The machine contract, end to end: a prompt on stdin, one JSON object on
// stdout and nothing else, every diagnostic on stderr, exit 0.
func TestExecBinaryWritesOneEnvelopeToStdout(t *testing.T) {
	binary := buildAforgeStamped(t, "v0.0.0-smoke")
	server := fakeOpenRouter(t)
	env, _ := smokeEnv(t, server.URL)
	workspace := t.TempDir()

	stdout, stderr, code := runSmoke(t, binary, env, "say hello\n",
		"exec", "--json", "-w", workspace, "--turns", "3", "--budget", "20000", "--timeout", "120")

	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout has %d lines, want exactly 1:\n%s", len(lines), stdout)
	}
	var envelope struct {
		Text      string          `json:"text"`
		Stop      string          `json:"stop"`
		Artifacts []string        `json:"artifacts"`
		Turns     int             `json:"turns"`
		ElapsedMS int64           `json:"elapsed_ms"`
		Usage     json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, lines[0])
	}
	if !strings.Contains(envelope.Text, smokeAnswer) {
		t.Fatalf("text = %q, want it to carry the model's answer %q", envelope.Text, smokeAnswer)
	}
	if envelope.Stop != "done" {
		t.Fatalf("stop = %q, want %q", envelope.Stop, "done")
	}
	if envelope.Artifacts == nil {
		t.Fatal("artifacts is null; the envelope promises a list")
	}
	if len(envelope.Usage) == 0 {
		t.Fatal("usage is missing from the envelope")
	}

	// Stream discipline, positively: the result is on stdout and only on
	// stdout. A harness that also read stderr — or that had to strip a banner
	// out of stdout — would be parsing something this contract does not promise.
	if strings.Contains(stderr, smokeAnswer) {
		t.Fatalf("the result leaked onto stderr:\n%s", stderr)
	}
}

// Without --json the same run prints the deliverable and nothing else, which is
// the other half of the stdout promise.
func TestExecBinaryWithoutJSONPrintsOnlyTheText(t *testing.T) {
	binary := buildAforgeStamped(t, "v0.0.0-smoke")
	server := fakeOpenRouter(t)
	env, _ := smokeEnv(t, server.URL)
	workspace := t.TempDir()

	stdout, stderr, code := runSmoke(t, binary, env, "say hello\n",
		"exec", "-w", workspace, "--turns", "3", "--budget", "20000", "--timeout", "120")

	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != smokeAnswer {
		t.Fatalf("stdout = %q, want exactly the answer %q", stdout, smokeAnswer)
	}
	if strings.Contains(stdout, "{") {
		t.Fatalf("the envelope reached stdout without --json:\n%s", stdout)
	}
}

// The release workflow's claim is that the revision it stamps is the revision
// the binary reports. This tests the real -ldflags seam rather than a variable
// rewritten inside the test process.
func TestExecBinaryReportsTheStampedVersion(t *testing.T) {
	binary := buildAforgeStamped(t, "abcdef01")
	env, _ := smokeEnv(t, "http://127.0.0.1:1")

	for _, spelling := range []string{"version", "--version", "-v"} {
		stdout, stderr, code := runSmoke(t, binary, env, "", spelling)
		if code != 0 {
			t.Fatalf("aforge %s exited %d\nstderr:\n%s", spelling, code, stderr)
		}
		if strings.TrimSpace(stdout) != "aforge abcdef01" {
			t.Fatalf("aforge %s printed %q, want %q", spelling, stdout, "aforge abcdef01")
		}
	}
}

// Probing for the binary must not be a configuration problem: `aforge version`
// answers with no API key in the environment at all.
func TestExecBinaryVersionNeedsNoAPIKey(t *testing.T) {
	binary := buildAforgeStamped(t, "v0.0.0-smoke")
	home := t.TempDir()
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "AFORGE_HOME=" + home, "AFORGE_PROFILE_DIR=" + home}

	stdout, stderr, code := runSmoke(t, binary, env, "", "version")
	if code != 0 {
		t.Fatalf("exit %d with no key set, want 0\nstderr:\n%s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "aforge v0.0.0-smoke" {
		t.Fatalf("printed %q, want %q", stdout, "aforge v0.0.0-smoke")
	}
}

// The other half of the stream contract: when exec cannot run at all, the
// failure is on stderr, the exit code is non-zero, and stdout stays empty so a
// harness never parses an error as a result.
func TestExecBinaryWithoutAKeyFailsCleanly(t *testing.T) {
	binary := buildAforgeStamped(t, "v0.0.0-smoke")
	home := t.TempDir()
	workspace := t.TempDir()
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"AFORGE_HOME=" + home,
		"AFORGE_PROFILE_DIR=" + home,
	}

	stdout, stderr, code := runSmoke(t, binary, env, "say hello\n",
		"exec", "--json", "-w", workspace, "--turns", "1", "--budget", "1000", "--timeout", "30")

	if code == 0 {
		t.Fatalf("exit 0 with no API key, want non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout is not empty on a failed run:\n%s", stdout)
	}
	if !strings.Contains(stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr does not say what is missing:\n%s", stderr)
	}
}
