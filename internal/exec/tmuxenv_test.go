package exec

import (
	"fmt"
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
	run := toolbox.runShell(t.Context(), jobTmuxMarker, 10, "")
	if run.err != nil {
		t.Fatalf("runShell marker: %v\n%s", run.err, run.body)
	}
	assertJobTmuxFloor(t, run.body, profile)
}

// jobTmuxMarker asks the job shell to print the three tmux variables on one
// short line, with an explicit "unset" for any that is absent. One line always
// fits the captured preview; a full env dump does not, and the output collector
// truncates it on a machine with a large ambient environment, cutting the
// TMUX_TMPDIR line so the test reads it absent even though the job shell had it.
// The JOBTMUX word lets the test insist the line was actually captured, so a
// truncated dump can never read as a vacuous pass on any of the three.
const jobTmuxMarker = `printf 'JOBTMUX TMUX=[%s] TMUX_PANE=[%s] TMUX_TMPDIR=[%s]\n' "${TMUX-unset}" "${TMUX_PANE-unset}" "${TMUX_TMPDIR-unset}"`

// assertJobTmuxFloor fails unless the captured output carries the marker line
// and it shows TMUX and TMUX_PANE unset and TMUX_TMPDIR at the profile's own
// tmux directory: the floor a job shell must land on, no reach to the host tmux
// server and a private socket namespace it owns.
func assertJobTmuxFloor(t *testing.T, output, profile string) {
	t.Helper()
	var line string
	for _, candidate := range strings.Split(output, "\n") {
		if strings.HasPrefix(candidate, "JOBTMUX ") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("the marker line never reached the captured output, so nothing here proves the floor:\n%s", output)
	}
	tmuxDir := filepath.Join(profile, "tmux")
	want := "JOBTMUX TMUX=[unset] TMUX_PANE=[unset] TMUX_TMPDIR=[" + tmuxDir + "]"
	if line != want {
		t.Errorf("job shell tmux floor = %q, want %q", line, want)
	}
	if info, err := os.Stat(tmuxDir); err != nil || !info.IsDir() {
		t.Errorf("the tmux directory %q was not created: %v", tmuxDir, err)
	}
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
	if result := tools.Execute(t.Context(), "sh", fmt.Sprintf(`{"cmd":%q,"bg":true}`, jobTmuxMarker)); result.IsError {
		t.Fatalf("background start failed: %s", result.Content)
	}
	waitForJobDone(t, tools, 1, slack(5*time.Second))
	body, err := os.ReadFile(filepath.Join(space.Root(), jobsDir, jobLogName("1", 1)))
	if err != nil {
		t.Fatalf("read job log: %v", err)
	}

	assertJobTmuxFloor(t, string(body), profile)
}
