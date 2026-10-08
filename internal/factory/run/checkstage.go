package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// CheckOptions tunes the check executor. The zero value is the defaults.
type CheckOptions struct {
	// Timeout bounds one command; 0 is ten minutes.
	Timeout time.Duration
	// Shell runs the command as `<Shell> -c <command>`; "" is bash.
	Shell string
}

// checkTailLines is how much of a command's output a result keeps.
const checkTailLines = 50

// NewCheckExecutor runs a check stage: its ask is a command, and its exit
// code is the evidence.
//
// A CHECK NEVER ASKS A MODEL AND NEVER TRUSTS A SENTENCE: THE EXIT CODE IS THE
// ANSWER, READ BY CODE. The word `ci` alone is the one exception in form, not
// in spirit: it runs nothing and reads what the item's source already says
// about the pull request's checks.
//
// A command runs in the repository's checkout with the environment inherited
// minus anything that looks like a model key, so a test cannot spend or leak
// one. The person's steering words are ignored; cancelling ctx kills the whole
// process group, children included.
func NewCheckExecutor(opts CheckOptions) Executor {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.Shell == "" {
		opts.Shell = "bash"
	}
	return ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		return runCheck(ctx, opts, job)
	})
}

func runCheck(ctx context.Context, opts CheckOptions, job Job) (factory.StageResult, error) {
	command := strings.TrimSpace(job.Stage.Ask)
	log := job.Log
	if log == nil {
		log = func(string) {}
	}
	if command == "" {
		return factory.StageResult{
			Output: "this check has no command to run",
			Exit:   1, Findings: 1,
		}, nil
	}
	if command == "ci" {
		return readCI(job.Item), nil
	}
	if strings.TrimSpace(job.Dir) == "" {
		return factory.StageResult{
			Output: fmt.Sprintf("codeaf does not know where %s is checked out", job.Item.Repo),
			Exit:   1, Findings: 1,
		}, nil
	}

	log("check: " + command)
	start := time.Now()

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var buf bytes.Buffer
	cmd := exec.CommandContext(runCtx, opts.Shell, "-c", command)
	cmd.Dir = job.Dir
	cmd.Env = checkEnv(os.Environ())
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// The group, not the shell alone: a test's children die with it.
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// A child that escaped the group may hold the pipe open; do not wait on it.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	took := time.Since(start).Round(time.Second)
	if ctx.Err() != nil {
		return factory.StageResult{}, ctx.Err()
	}

	exit := 0
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	var ee *exec.ExitError
	switch {
	case timedOut:
		exit = 124
	case err == nil:
	case errors.As(err, &ee):
		exit = ee.ExitCode()
		if exit < 0 {
			exit = 1
		}
	default:
		// The shell could not start at all.
		exit = 127
		fmt.Fprintf(&buf, "%v\n", err)
	}

	out := tailLines(buf.String(), checkTailLines)
	evidence := fmt.Sprintf("exit %d · %s", exit, took)
	if timedOut {
		out = strings.TrimRight(out, "\n")
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("timed out after %s", opts.Timeout)
		evidence = fmt.Sprintf("timed out · %s", opts.Timeout)
	}
	log(evidence)

	findings := 0
	if exit != 0 {
		findings = 1
	}
	return factory.StageResult{
		Done:     exit == 0,
		Findings: findings,
		Exit:     exit,
		Output:   out,
		Claims: []factory.Claim{{
			Text:     command + " passes",
			OK:       exit == 0,
			Evidence: evidence,
			Medium:   "test",
		}},
	}, nil
}

// readCI answers the `ci` check from what the item already carries.
func readCI(it factory.Item) factory.StageResult {
	var good, bad, pending []string
	for _, r := range it.CheckRuns {
		switch strings.ToLower(strings.TrimSpace(r.State)) {
		case "success", "neutral", "skipped":
			good = append(good, r.Name)
		case "in_progress", "queued", "pending", "waiting", "requested", "":
			pending = append(pending, r.Name)
		default:
			bad = append(bad, r.Name)
		}
	}
	state := ""
	switch {
	case len(bad) > 0:
		state = "red"
	case len(pending) > 0:
		state = "running"
	case len(good) > 0:
		state = "green"
	default:
		switch c := it.Checks; {
		case strings.Contains(c, "✓"):
			state = "green"
		case strings.Contains(c, "✗"), strings.Contains(c, "✕"):
			state = "red"
		case strings.Contains(c, "…"), strings.Contains(strings.ToLower(c), "running"):
			state = "running"
		}
	}
	switch state {
	case "green":
		ev := strings.Join(good, ", ")
		if ev == "" {
			ev = strings.TrimSpace(it.Checks)
		}
		return factory.StageResult{
			Done: true,
			Claims: []factory.Claim{{
				Text: "ci is green", OK: true, Evidence: ev, Medium: "policy",
			}},
		}
	case "running":
		return factory.StageResult{Output: "ci is still running", Exit: 1}
	case "red":
		ev := strings.Join(bad, ", ")
		if ev == "" {
			ev = strings.TrimSpace(it.Checks)
		}
		return factory.StageResult{
			Exit: 1, Findings: 1,
			Output: "ci is red: " + ev,
			Claims: []factory.Claim{{
				Text: "ci is green", OK: false, Evidence: ev, Medium: "policy",
			}},
		}
	}
	return factory.StageResult{Output: "ci has not reported yet", Exit: 1}
}

// checkEnv drops the names a model key or token travels under.
func checkEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_TOKEN") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// tailLines keeps the last n lines of s.
func tailLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
