//go:build !windows

package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func helperCmd(t *testing.T, role string) (Command, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "seen")
	return Command{
		Path: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$", "--"},
		Env:   []string{"CLIPBOARD_TEST_ROLE=" + role, "CLIPBOARD_TEST_OUT=" + out},
		Stdin: secretText,
	}, out
}

func TestRealHelperReceivesTheTextOnStdinOnly(t *testing.T) {
	c, out := helperCmd(t, "ok")
	if err := runWith(context.Background(), c, 5*time.Second, time.Second); err != nil {
		t.Fatal(err)
	}
	seen, _ := os.ReadFile(out)
	stdin, rest, _ := strings.Cut(string(seen), "\n--argv--\n")
	if stdin != secretText {
		t.Fatalf("stdin = %q", stdin)
	}
	if strings.Contains(rest, "SECRET-X") {
		t.Fatalf("the text reached the helper's argv or environment: %q", rest)
	}
}

func TestRealHelperThatEchoesStderrAndStdoutLeaksNothing(t *testing.T) {
	c, _ := helperCmd(t, "echo")
	err := runWith(context.Background(), c, 5*time.Second, time.Second)
	if err != ErrFailed {
		t.Fatalf("non-zero exit = %v, want ErrFailed", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error carries the helper's echo: %v", err)
	}
	c, _ = helperCmd(t, "echo_ok")
	if err := runWith(context.Background(), c, 5*time.Second, time.Second); err != nil {
		t.Fatalf("an echoing helper that succeeded = %v", err)
	}
}

func TestNonZeroExitIsFailedAndASignalIsUnconfirmed(t *testing.T) {
	c, _ := helperCmd(t, "exit1")
	if err := runWith(context.Background(), c, 5*time.Second, time.Second); err != ErrFailed {
		t.Fatalf("exit 1 = %v", err)
	}
	c, _ = helperCmd(t, "signal")
	if err := runWith(context.Background(), c, 5*time.Second, time.Second); err != ErrUnconfirmed {
		t.Fatalf("killed helper = %v", err)
	}
}

func TestMissingHelperBinaryIsUnavailableNotFailed(t *testing.T) {
	err := runWith(context.Background(), Command{Path: "/nonexistent/helper", Stdin: "x"}, time.Second, time.Second)
	if err != ErrUnavailable {
		t.Fatalf("= %v", err)
	}
}

func TestTimeoutIsUnconfirmedAndBounded(t *testing.T) {
	c, _ := helperCmd(t, "hang")
	start := time.Now()
	err := runWith(context.Background(), c, 300*time.Millisecond, 200*time.Millisecond)
	if err != ErrUnconfirmed {
		t.Fatalf("= %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %v for a 300ms bound", took)
	}
}

func TestACancelledCallerIsUnconfirmed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := helperCmd(t, "ok")
	if err := runWith(ctx, c, 5*time.Second, time.Second); err != ErrUnconfirmed && err != ErrUnavailable {
		t.Fatalf("= %v", err)
	}
}

// A HELPER THAT FORKS A CHILD HOLDING EVERY DESCRIPTOR DOES NOT PIN THE CALL.
func TestForkingHelperDoesNotHangTheCall(t *testing.T) {
	c, _ := helperCmd(t, "fork")
	start := time.Now()
	err := runWith(context.Background(), c, 5*time.Second, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("a helper that exited 0 = %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a forked child held the call for %v", took)
	}
}

// The environment a real helper sees is only the allowlist: a nil Env would
// have handed it everything.
func TestAnEmptyAllowlistIsNotTheWholeEnvironment(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-LEAK-IN-PARENT")
	out := filepath.Join(t.TempDir(), "seen")
	c := Command{Path: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$"}, Stdin: "x",
		Env: nil}
	c.Env = []string{} // the role still has to reach the child
	c.Env = append(c.Env, "CLIPBOARD_TEST_ROLE=ok", "CLIPBOARD_TEST_OUT="+out)
	if err := runWith(context.Background(), c, 5*time.Second, time.Second); err != nil {
		t.Fatal(err)
	}
	seen, _ := os.ReadFile(out)
	if strings.Contains(string(seen), "LEAK-IN-PARENT") {
		t.Fatal("the parent's environment reached the helper")
	}
}
