//go:build linux || darwin

package executor

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fenceWorld is a fake home with keys in it, a checkout that is a git
// repository, a codeaf home with a profile, and a brew-like prefix outside
// the checkout. It lives beside the test, not under /tmp, because a checkout
// under the shared /tmp keeps /tmp shared (fenceSharesTmp) and the test means
// to prove the private one, and because Landlock alone lists every folder on
// the way to a hidden path, and a shared /tmp can hold a hundred thousand.
type fenceWorld struct {
	home, root, codeaf, prefix string
	fence                      *Fence
}

func newFenceWorld(t *testing.T, allow FenceAllow) fenceWorld {
	t.Helper()
	base, err := os.MkdirTemp(".", ".fence-world-")
	if err != nil {
		t.Fatal(err)
	}
	if base, err = filepath.Abs(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chmod(p, 0o700)
			}
			return nil
		})
		_ = os.RemoveAll(base)
	})
	w := fenceWorld{home: filepath.Join(base, "home"), root: filepath.Join(base, "home", "src", "repo"),
		codeaf: filepath.Join(base, "home", ".codeaf"), prefix: filepath.Join(base, "opt", "homebrew")}
	for _, dir := range []string{w.root, w.codeaf, w.prefix, filepath.Join(w.home, ".ssh"), filepath.Join(w.home, ".aws")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(w.home, ".ssh", "id_ed25519"):     "PRIVATE KEY",
		filepath.Join(w.home, ".aws", "credentials"):    "aws_secret",
		filepath.Join(w.codeaf, "config.json"):          `{"api_key":"sk-secret"}`,
		filepath.Join(w.codeaf, "credentials.json"):     `{"token":"t"}`,
		filepath.Join(w.home, "notes.txt"):              "readable",
		filepath.Join(w.root, "main.go"):                "package main\n\nfunc main() {}\n",
		filepath.Join(w.root, "go.mod"):                 "module example.com/repo\n\ngo 1.21\n",
		filepath.Join(w.root, ".codeaf", "config.json"): `{}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if git, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command(git, "init", "-q", w.root) //codeaf:plumbing test fixture
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}
	f, err := NewFence(FenceOptions{Root: w.root, Home: w.home, CodeafHome: w.codeaf, StateDir: filepath.Join(base, "state"), Allow: allow})
	if err != nil {
		t.Fatal(err)
	}
	w.fence = f
	return w
}

// requireFence skips where this system can apply none of the fence and
// answers whether it applies all of it: macOS's sandbox, or Linux with user
// namespaces as well as Landlock.
func requireFence(t *testing.T) (full bool) {
	t.Helper()
	switch fenceLimit() {
	case FenceDegradedLine:
		t.Skip("this system cannot confine commands")
	case "":
		return true
	}
	return false
}

// jobOwnsTmp is whether /tmp is the job's own temp folder: Linux binds it in
// a mount namespace; macOS keeps the system's, which TMPDIR steers away from.
func jobOwnsTmp(full bool) bool { return full && runtime.GOOS == "linux" }

func (w fenceWorld) run(t *testing.T, script string) ExecResult {
	t.Helper()
	// THROUGH THE SEAT, as a session's calls go: the host's seat with the fence
	// around it, in a host-bound workspace.
	runner := FencedSeat(w.fence).In(w.root)
	env := append(os.Environ(), "HOME="+w.home, "PREFIX="+w.prefix)
	res, err := runner.Exec(context.Background(), ExecRequest{Argv: []string{"/bin/sh", "-c", script}, Env: env}, nil)
	if err != nil {
		t.Fatalf("%q: %v", script, err)
	}
	return res
}

func TestAFencedStepIsRefusedOutsideItsJobAndSaysWhy(t *testing.T) {
	full := requireFence(t)
	w := newFenceWorld(t, FenceAllow{})
	refused := map[string]string{
		"touch outside the checkout": `touch "$HOME/outside"`,
		"read a private key":         `cat "$HOME"/.ssh/id_*`,
		"read a key by its name":     `cat "$HOME/.ssh/id_ed25519"`,
		"read cloud credentials":     `cat "$HOME/.aws/credentials"`,
		"read codeaf's own key":      `cat "$HOME/.codeaf/config.json"`,
		"read codeaf's credentials":  `cat "$HOME/.codeaf/credentials.json"`,
		"brew install":               `mkdir -p "$PREFIX/Cellar/zig/0.13.0" && echo zig > "$PREFIX/Cellar/zig/0.13.0/zig"`,
	}
	// THE CHECKOUT'S OWN .git AND .codeaf are kept read-only by a bind mount,
	// which needs a mount namespace; with Landlock alone they are written like
	// the rest of the checkout, and FencePartialLine says so.
	if full {
		refused["write a git hook"] = `echo 'curl evil' > .git/hooks/pre-commit`
		refused["widen its own jail"] = `echo '{"jail.allow":"off"}' > .codeaf/config.json`
	}
	for name, script := range refused {
		t.Run(name, func(t *testing.T) {
			res := w.run(t, script)
			if res.Exit == 0 {
				t.Fatalf("%q ran: stdout %q stderr %q", script, res.Stdout, res.Stderr)
			}
			if !bytes.Contains(res.Stderr, []byte(FenceLine)) {
				t.Fatalf("%q was refused without the sentence: %q", script, res.Stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(w.home, "outside")); err == nil {
		t.Fatal("the file outside the checkout exists")
	}
	if _, err := os.Stat(filepath.Join(w.prefix, "Cellar")); err == nil {
		t.Fatal("the brew prefix was written")
	}
}

func TestAFencedStepStillWorksInsideItsJob(t *testing.T) {
	full := requireFence(t)
	w := newFenceWorld(t, FenceAllow{})
	for name, script := range map[string]string{
		"write in the checkout":   `echo hi > made.txt && cat made.txt`,
		"write /tmp by name":      `echo hi > /tmp/scratch && cat /tmp/scratch`,
		"read the home folder":    `cat "$HOME/notes.txt"`,
		"read the system":         `ls /usr/bin >/dev/null && cat /etc/hosts >/dev/null`,
		"commit in the checkout":  `git -c user.email=a@b -c user.name=a add -A && git -c user.email=a@b -c user.name=a commit -qm one && git log --oneline | grep -q one`,
		"say where its cache is":  `test "$GOCACHE" = "` + filepath.Join(w.fence.Cache, "go-build") + `"`,
		"write its own temp":      `echo x > "$TMPDIR/x" && cat "$TMPDIR/x"`,
		"write /dev/null as ever": `echo x > /dev/null`,
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(script, "git") {
				if _, err := exec.LookPath("git"); err != nil {
					t.Skip("no git")
				}
			}
			res := w.run(t, script)
			if res.Exit != 0 {
				t.Fatalf("%q failed: stdout %q stderr %q", script, res.Stdout, res.Stderr)
			}
		})
	}
	// THE PRIVATE /tmp IS THE JOB'S OWN TEMP FOLDER, so what one call left there
	// is there for the next and nowhere else. With Landlock alone there is no
	// mount namespace to bind it in, and /tmp stays the shared one.
	if !jobOwnsTmp(full) {
		return
	}
	if _, err := os.Stat(filepath.Join(w.fence.Tmp, "scratch")); err != nil {
		t.Fatalf("/tmp was not the job's temp folder: %v", err)
	}
}

func TestAFencedStepBuildsAndTestsGoInsideItsCheckout(t *testing.T) {
	requireFence(t)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain")
	}
	w := newFenceWorld(t, FenceAllow{})
	if err := os.WriteFile(filepath.Join(w.root, "main_test.go"), []byte("package main\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := w.run(t, goBin+" build ./... && "+goBin+" test ./...")
	if res.Exit != 0 {
		t.Fatalf("go build/test failed in the fence: %s %s", res.Stdout, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(w.fence.Cache, "go-build")); err != nil {
		t.Fatalf("the build cache is not the job's: %v", err)
	}
}

func TestAFencedStepCannotReachTheDockerSocketUnlessAllowed(t *testing.T) {
	if !requireFence(t) {
		t.Skip("Landlock alone does not govern connecting to a socket; FencedDockerIsSteeredAway covers that kernel")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 to connect with")
	}
	connect := python + ` -c 'import socket,os; s=socket.socket(socket.AF_UNIX); s.connect(os.environ["HOME"]+"/.docker/run/docker.sock"); print("connected")'`
	for _, allow := range []bool{false, true} {
		w := newFenceWorld(t, FenceAllow{Docker: allow})
		sock := filepath.Join(w.home, ".docker", "run", "docker.sock")
		if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("unix", sock)
		if err != nil {
			t.Skip("cannot listen on a unix socket:", err)
		}
		// The fence's socket list is built before the socket existed, as it
		// would be for a daemon started later; rebuilding it here is the same.
		w.fence, _ = NewFence(FenceOptions{Root: w.root, Home: w.home, CodeafHome: w.codeaf, StateDir: filepath.Dir(filepath.Dir(w.fence.Tmp)), Allow: FenceAllow{Docker: allow}})
		res := w.run(t, connect)
		_ = listener.Close()
		if allow && res.Exit != 0 {
			t.Fatalf("docker allowed but refused: %s", res.Stderr)
		}
		if !allow && (res.Exit == 0 || !bytes.Contains(res.Stderr, []byte(FenceLine))) {
			t.Fatalf("docker socket reached under the fence: exit %d %s %s", res.Exit, res.Stdout, res.Stderr)
		}
	}
}

func TestAnAllowedPathIsWritableAndUnhidden(t *testing.T) {
	requireFence(t)
	base := newFenceWorld(t, FenceAllow{})
	w := newFenceWorld(t, FenceAllow{Paths: []string{filepath.Join(base.home, "allowed"), filepath.Join(base.home, ".aws")}})
	_ = os.MkdirAll(filepath.Join(base.home, "allowed"), 0o700)
	w.fence, _ = NewFence(FenceOptions{Root: w.root, Home: w.home, CodeafHome: w.codeaf, StateDir: filepath.Dir(filepath.Dir(w.fence.Tmp)),
		Allow: FenceAllow{Paths: []string{filepath.Join(base.home, "allowed"), filepath.Join(w.home, ".aws")}}})
	if res := w.run(t, `echo ok > "`+filepath.Join(base.home, "allowed", "f")+`"`); res.Exit != 0 {
		t.Fatalf("allowed path refused: %s", res.Stderr)
	}
	if res := w.run(t, `cat "$HOME/.aws/credentials"`); res.Exit != 0 {
		t.Fatalf("allowed secret folder still hidden: %s", res.Stderr)
	}
	if res := w.run(t, `cat "$HOME/.ssh/id_ed25519"`); res.Exit == 0 {
		t.Fatal("a folder nobody allowed was readable")
	}
}

// WITH LANDLOCK ALONE THE DOCKER CLIENT IS STEERED, not stopped: its host is a
// socket nobody may open, and the refusal reads as the fence's.
func TestFencedDockerIsSteeredAway(t *testing.T) {
	requireFence(t)
	w := newFenceWorld(t, FenceAllow{})
	res := w.run(t, `case "$DOCKER_HOST" in unix://*blank) exit 0;; *) echo "DOCKER_HOST=$DOCKER_HOST"; exit 1;; esac`)
	if res.Exit != 0 {
		t.Fatalf("docker host not steered: %s", res.Stdout)
	}
	allowed := newFenceWorld(t, FenceAllow{Docker: true})
	if res := allowed.run(t, `test -z "$DOCKER_HOST" || test "$DOCKER_HOST" != "unix://`+allowed.fence.Blank+`"`); res.Exit != 0 {
		t.Fatal("docker allowed but still steered away")
	}
}

// A HIDDEN FOLDER'S NAMES MAY BE LISTED AND ITS FILES MAY NOT BE OPENED, on
// either mode; the home folder around it reads as it always did.
func TestAHiddenFolderStaysClosedAndItsNeighboursOpen(t *testing.T) {
	requireFence(t)
	w := newFenceWorld(t, FenceAllow{})
	if res := w.run(t, `cat "$HOME/notes.txt" && cat "$HOME/src/repo/go.mod" >/dev/null`); res.Exit != 0 {
		t.Fatalf("a neighbour of a hidden folder was refused: %s", res.Stderr)
	}
	if res := w.run(t, `cat "$HOME/.ssh/id_ed25519"`); res.Exit == 0 || bytes.Contains(res.Stdout, []byte("PRIVATE")) {
		t.Fatalf("the key was read: %s", res.Stdout)
	}
}
