package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A JOB'S SHELL MUST NOT REACH THE TMUX SERVER HOSTING CODEAF.
//
// A bash call the model runs inherits the parent environment, TMUX and
// TMUX_PANE included, so a bare `tmux` it runs targets the very server the chat
// is sitting in. That is how `tmux kill-server` once took down the chat that
// ran it (issue #576), and on a shared socket it reached every run on the box.
//
// THE FLOOR IS BOTH HALVES. Unsetting TMUX/TMUX_PANE alone still leaves the
// user's own tmux server reachable; the private TMUX_TMPDIR alone leaves the
// inherited TMUX/TMUX_PANE pointing at the host server. Only together do they
// name a namespace a job's `tmux` can reach and nothing else — and a test that
// needs its own tmux still gets one, because the directory exists.
func TestJobShellEnvCannotReachTheHostTmux(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,4242,0")
	t.Setenv("TMUX_PANE", "%7")

	// The nil-Env path: no shelf and no rtk, so runShell would otherwise leave
	// cmd.Env unset and inherit the parent's whole environment verbatim.
	toolbox := NewToolbox(workspace(t), "1", nil)
	run := toolbox.runShell(t.Context(), "env", 10, "")
	if run.err != nil {
		t.Fatalf("runShell env: %v\n%s", run.err, run.body)
	}

	got := shellEnvMap(run.body)
	if value, present := got["TMUX"]; present {
		t.Errorf("TMUX reached the job's shell: %q", value)
	}
	if value, present := got["TMUX_PANE"]; present {
		t.Errorf("TMUX_PANE reached the job's shell: %q", value)
	}
	want := filepath.Join(profile, "tmux")
	if got["TMUX_TMPDIR"] != want {
		t.Errorf("TMUX_TMPDIR = %q, want %q (a directory codeaf owns)", got["TMUX_TMPDIR"], want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("the tmux directory %q was not created: %v", want, err)
	}
}

// shellEnvMap reads `env` output into name -> value.
func shellEnvMap(body string) map[string]string {
	got := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			got[name] = value
		}
	}
	return got
}

// The background-job registry builds its own environment (jobs.go) and used to
// leave cmd.Env unset on the bare path, inheriting the parent whole — so a job
// a model leaves running had the same reach on the host's tmux server.
func TestBackgroundJobShellCannotReachTheHostTmux(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,9,0")
	t.Setenv("TMUX_PANE", "%2")

	tools, space := backgroundToolbox(t)
	if result := tools.Execute(t.Context(), "sh", `{"cmd":"env","bg":true}`); result.IsError {
		t.Fatalf("background start failed: %s", result.Content)
	}
	waitForJobDone(t, tools, 1, slack(5*time.Second))
	body, err := os.ReadFile(filepath.Join(space.Root(), jobsDir, jobLogName("1", 1)))
	if err != nil {
		t.Fatalf("read job log: %v", err)
	}

	got := shellEnvMap(string(body))
	if value, present := got["TMUX"]; present {
		t.Errorf("TMUX reached the job's shell: %q", value)
	}
	if value, present := got["TMUX_PANE"]; present {
		t.Errorf("TMUX_PANE reached the job's shell: %q", value)
	}
	want := filepath.Join(profile, "tmux")
	if got["TMUX_TMPDIR"] != want {
		t.Errorf("TMUX_TMPDIR = %q, want %q", got["TMUX_TMPDIR"], want)
	}
}
