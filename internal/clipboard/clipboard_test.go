package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secretText = "codeaf://factory/join?url=http%3A%2F%2Ffactory#token=SECRET-X-0123456789"

// fakeSystem is a System whose platform, environment, helpers and process
// runner are all stand-ins: nothing here starts a real helper.
type fakeSystem struct {
	*System
	ran []Command
	err error
}

func newFake(goos string, localGUI bool, env map[string]string, onPath ...string) *fakeSystem {
	f := &fakeSystem{}
	f.System = &System{
		localGUI: localGUI, goos: goos,
		getenv: func(k string) string { return env[k] },
		lookPath: func(name string) (string, error) {
			for _, p := range onPath {
				if p == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", exec.ErrNotFound
		},
		stat: func(p string) error {
			if goos == "darwin" && p == "/usr/bin/pbcopy" {
				return nil
			}
			return os.ErrNotExist
		},
		run: func(_ context.Context, c Command) error { f.ran = append(f.ran, c); return f.err },
	}
	return f
}

// NOTHING IS ENABLED BY THE PLATFORM, A DISPLAY OR AN INSTALLED HELPER ALONE:
// only the launcher's explicit assertion that this is the person's own session.
func TestNeverAvailableWithoutTheLaunchersAssertion(t *testing.T) {
	for _, c := range []struct {
		goos string
		env  map[string]string
		path []string
	}{
		{"darwin", nil, nil},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"wl-copy"}},
		{"linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel"}},
	} {
		f := newFake(c.goos, false, c.env, c.path...)
		if f.Available() {
			t.Errorf("%s %v was available without localGUI", c.goos, c.env)
		}
		if err := f.Copy(context.Background(), secretText); err != ErrUnavailable || len(f.ran) != 0 {
			t.Errorf("%s: Copy = %v after %d runs", c.goos, err, len(f.ran))
		}
		if !newFake(c.goos, true, c.env, c.path...).Available() {
			t.Errorf("%s %v was not available with localGUI", c.goos, c.env)
		}
	}
	var u Copier = Unavailable{}
	if u.Available() || u.Copy(context.Background(), "x") != ErrUnavailable {
		t.Fatal("the production default can copy")
	}
}

func TestSelectionTable(t *testing.T) {
	for _, c := range []struct {
		name  string
		goos  string
		env   map[string]string
		path  []string
		want  string
		wargs string
	}{
		{"darwin", "darwin", nil, nil, "/usr/bin/pbcopy", ""},
		{"wayland first", "linux", map[string]string{"WAYLAND_DISPLAY": "w", "DISPLAY": ":0"}, []string{"xclip", "wl-copy"}, "/usr/bin/wl-copy", "--type text/plain"},
		{"x11 xclip", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel"}, "/usr/bin/xclip", "-selection clipboard -in"},
		{"x11 xsel", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, "/usr/bin/xsel", "--clipboard --input"},
		{"no display", "linux", nil, []string{"xclip", "wl-copy"}, "", ""},
		{"display but no helper", "linux", map[string]string{"DISPLAY": ":0"}, nil, "", ""},
		{"wayland without wl-copy falls to x11", "linux", map[string]string{"WAYLAND_DISPLAY": "w", "DISPLAY": ":0"}, []string{"xclip"}, "/usr/bin/xclip", "-selection clipboard -in"},
		{"ssh connection", "linux", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "a b c d"}, []string{"xclip"}, "", ""},
		{"ssh client", "linux", map[string]string{"DISPLAY": ":0", "SSH_CLIENT": "a b c"}, []string{"xclip"}, "", ""},
		{"ssh tty", "linux", map[string]string{"WAYLAND_DISPLAY": "w", "SSH_TTY": "/dev/pts/1"}, []string{"wl-copy"}, "", ""},
		{"windows", "windows", map[string]string{"DISPLAY": ":0"}, []string{"xclip"}, "", ""},
		{"freebsd", "freebsd", map[string]string{"DISPLAY": ":0"}, []string{"xclip"}, "", ""},
	} {
		f := newFake(c.goos, true, c.env, c.path...)
		path, args, ok := f.pick()
		if (path != "") != ok || path != c.want || strings.Join(args, " ") != c.wargs {
			t.Errorf("%s: picked %q %v %v", c.name, path, args, ok)
		}
	}
}

// A HELPER FOUND BY A RELATIVE PATH IS NOT TRUSTED.
func TestRelativeHelperPathIsRefused(t *testing.T) {
	f := newFake("linux", true, map[string]string{"DISPLAY": ":0"}, "xclip")
	f.lookPath = func(string) (string, error) { return "./xclip", nil }
	if f.Available() {
		t.Fatal("a relative helper was accepted")
	}
}

func TestMissingPbcopyIsUnavailable(t *testing.T) {
	f := newFake("darwin", true, nil)
	f.stat = func(string) error { return os.ErrNotExist }
	if f.Available() {
		t.Fatal("available without pbcopy")
	}
}

// THE TEXT IS ONLY EVER STDIN: not argv, not the environment, not an error.
func TestTheTextIsOnlyEverStdin(t *testing.T) {
	secrets := map[string]string{
		"OPENROUTER_API_KEY": "sk-or-LEAK", "GITHUB_TOKEN": "ghp_LEAK", "GH_TOKEN": "gho_LEAK",
		"ANTHROPIC_API_KEY": "sk-ant-LEAK", "OPENAI_API_KEY": "sk-LEAK",
		"DISPLAY": ":0", "XAUTHORITY": "/home/p/.Xauthority", "PATH": "/usr/bin:/bin", "HOME": "/home/p",
	}
	for _, goos := range []string{"darwin", "linux"} {
		f := newFake(goos, true, secrets, "xclip")
		if err := f.Copy(context.Background(), secretText); err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if len(f.ran) != 1 {
			t.Fatalf("%s: ran %d", goos, len(f.ran))
		}
		c := f.ran[0]
		if c.Stdin != secretText {
			t.Errorf("%s: stdin = %q", goos, c.Stdin)
		}
		if strings.Contains(c.Path+strings.Join(c.Args, "\x00")+strings.Join(c.Env, "\x00"), "SECRET-X") {
			t.Errorf("%s: the text is in the path, argv or env: %+v", goos, c)
		}
		for _, e := range c.Env {
			if strings.Contains(e, "LEAK") || strings.HasPrefix(e, "HOME=") {
				t.Errorf("%s: env passes %q on", goos, e)
			}
		}
		want := map[string]bool{"DISPLAY=:0": true, "XAUTHORITY=/home/p/.Xauthority": true, "PATH=/usr/bin:/bin": true}
		for _, e := range c.Env {
			delete(want, e)
		}
		if len(want) != 0 {
			t.Errorf("%s: env lacks %v: %v", goos, want, c.Env)
		}
	}
}

func TestTextBoundsAreCheckedBeforeAnythingStarts(t *testing.T) {
	f := newFake("linux", true, map[string]string{"DISPLAY": ":0"}, "xclip")
	for _, text := range []string{"", strings.Repeat("a", MaxText+1), "a\x00b"} {
		if err := f.Copy(context.Background(), text); err != ErrFailed {
			t.Errorf("%d bytes: %v", len(text), err)
		}
	}
	if len(f.ran) != 0 {
		t.Fatal("a helper started for text that was refused")
	}
	if err := f.Copy(context.Background(), strings.Repeat("a", MaxText)); err != nil {
		t.Fatalf("the largest text: %v", err)
	}
}

func TestErrorsAreFixedWords(t *testing.T) {
	for _, err := range []error{ErrUnavailable, ErrFailed, ErrUnconfirmed} {
		if strings.Contains(err.Error(), "SECRET") || errors.Unwrap(err) != nil {
			t.Errorf("%v wraps or carries text", err)
		}
	}
}

// ── a real child process: the test binary, in a role ───────────────────────

// TestHelperProcess is the stand-in clipboard helper. It does nothing unless a
// test started it with the role in the environment.
func TestHelperProcess(t *testing.T) {
	role := os.Getenv("CLIPBOARD_TEST_ROLE")
	if role == "" {
		t.Skip("not a helper process")
	}
	in, _ := io.ReadAll(os.Stdin)
	if out := os.Getenv("CLIPBOARD_TEST_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(string(in)+"\n--argv--\n"+strings.Join(os.Args, "\x00")+"\n--env--\n"+strings.Join(os.Environ(), "\x00")), 0o600)
	}
	switch role {
	case "ok":
	case "echo":
		fmt.Fprint(os.Stderr, "error: cannot copy ", string(in))
		fmt.Fprint(os.Stdout, string(in))
		os.Exit(3)
	case "echo_ok":
		fmt.Fprint(os.Stderr, string(in))
		fmt.Fprint(os.Stdout, string(in))
	case "exit1":
		os.Exit(1)
	case "hang":
		time.Sleep(time.Minute)
	case "signal":
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Kill()
		time.Sleep(time.Minute)
	case "fork":
		// Leave a child that inherited every descriptor and outlives this one,
		// as wl-copy and xclip do to keep serving a selection.
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		child.Env = append(os.Environ(), "CLIPBOARD_TEST_ROLE=sleep", "CLIPBOARD_TEST_OUT=")
		child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
	case "sleep":
		time.Sleep(3 * time.Second)
	}
	os.Exit(0)
}

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
