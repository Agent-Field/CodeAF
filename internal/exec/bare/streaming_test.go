package bare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func envValue(env []string, name string) (string, bool) {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

// The bare streaming env is the one seam the foreground bash tool and the
// session's job registry both reach, so the tmux floor must hold here too.
func TestStreamingEnvCarriesTheTmuxFloor(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,4242,0")
	t.Setenv("TMUX_PANE", "%7")

	env := StreamingEnv()
	if value, ok := envValue(env, "TMUX"); ok {
		t.Errorf("TMUX reached the streaming env: %q", value)
	}
	if value, ok := envValue(env, "TMUX_PANE"); ok {
		t.Errorf("TMUX_PANE reached the streaming env: %q", value)
	}
	want := filepath.Join(profile, "tmux")
	if value, ok := envValue(env, "TMUX_TMPDIR"); !ok || value != want {
		t.Errorf("TMUX_TMPDIR = %q (present %v), want %q", value, ok, want)
	}
	// The strip must not have taken the buffering fix with it.
	if _, ok := envValue(env, "PYTHONUNBUFFERED"); !ok {
		t.Error("PYTHONUNBUFFERED was dropped from the streaming env")
	}
}

// A TEST THAT NEEDS ITS OWN TMUX MUST STILL WORK: the floor names a private
// socket directory rather than stripping the env to something broken, so a
// `tmux` a job runs starts its own server there and never reaches the one the
// chat sits in.
func TestAJobShellCanStartItsOwnTmuxButNotTheHosts(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux on PATH: a job's own tmux server cannot be exercised")
	}

	// A server the "chat" sits in, on a socket directory of its own.
	hostDir := t.TempDir()
	hostEnv := append(environWithout("TMUX", "TMUX_PANE"), "TMUX_TMPDIR="+hostDir, "TERM=xterm-256color")
	if out, err := tmuxRun(tmux, hostEnv, "new-session", "-d", "-s", "host", "sleep", "600"); err != nil {
		t.Skipf("could not start a host tmux server: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = tmuxRun(tmux, hostEnv, "kill-server") })

	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("TMUX", filepath.Join(hostDir, "default"))
	t.Setenv("TMUX_PANE", "%0")

	jobEnv := append(StreamingEnv(), "TERM=xterm-256color")

	// The job must not see the host's server.
	if out, err := tmuxRun(tmux, jobEnv, "ls"); err == nil && strings.Contains(out, "host") {
		t.Errorf("the job's tmux reached the host server:\n%s", out)
	}

	// But it can start its own, in the directory codeaf owns, and reach it.
	if out, err := tmuxRun(tmux, jobEnv, "new-session", "-d", "-s", "mine", "sleep", "600"); err != nil {
		t.Fatalf("the job could not start its own tmux: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = tmuxRun(tmux, jobEnv, "kill-server") })
	if out, err := tmuxRun(tmux, jobEnv, "ls"); err != nil || !strings.Contains(out, "mine") {
		t.Errorf("the job's own tmux server is not reachable: %v\n%s", err, out)
	}
}

func tmuxRun(tmux string, env []string, args ...string) (string, error) {
	cmd := exec.Command(tmux, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// environWithout is internal/env's EnvironWithout, kept local so this test
// states the host environment in its own terms.
func environWithout(names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}
	kept := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); !drop[name] {
			kept = append(kept, entry)
		}
	}
	return kept
}
